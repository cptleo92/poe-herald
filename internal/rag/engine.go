package rag

import (
	"context"
	"fmt"
	"log"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// The Engine is the coordinator of our RAG system.
// It holds our Cohere API client to generate embeddings, and our Postgres Pool to store them.
type Engine struct {
	db     *pgxpool.Pool
	client *CohereClient
}

// SearchResult represents a single piece of retrieved context.
// We return the title and category along with the actual text so the LLM knows WHERE this info came from.
type SearchResult struct {
	Title      string
	Category   string
	Game       string
	SourceURL  string
	Content    string
	Similarity float32 // How close was this to our query? (1.0 = exact match usually, though depends on distance metric)
}

func NewEngine(db *pgxpool.Pool, client *CohereClient) *Engine {
	return &Engine{
		db:     db,
		client: client,
	}
}

// IngestDocument takes a raw, massive string from a source (like Wiki),
// chunks it into smaller pieces, gets the embeddings for those pieces,
// and saves them to PostgreSQL with the pgvector extension.
func (e *Engine) IngestDocument(ctx context.Context, title, category, game, sourceURL, rawContent string) error {
	// 1. Break the large document into smaller, overlapping chunks.
	// You can read chunks.go for why we do this.
	chunks := ChunkText(rawContent)
	if len(chunks) == 0 {
		return fmt.Errorf("no chunks generated from content")
	}
	fmt.Printf("[Engine] Split '%s' into %d chunks.\n", title, len(chunks))

	// Wait, we need an array of purely strings to send to Cohere.
	var rawTexts []string
	for _, chunk := range chunks {
		rawTexts = append(rawTexts, chunk.Text)
	}

	// 2. Pass the list of strings to Cohere so it returns a list of vectors.
	// We use EmbedDocuments instead of EmbedQuery here because these are the target facts we are saving.
	embeddings, err := e.client.EmbedDocuments(ctx, rawTexts)
	if err != nil {
		return fmt.Errorf("embedding generation failed: %w", err)
	}

	// Safety check: Cohere should return 1 vector for every 1 string we sent.
	if len(embeddings) != len(chunks) {
		return fmt.Errorf("mismatch: got %d embeddings for %d chunks", len(embeddings), len(chunks))
	}

	// 3. Save these to the database!
	// We use a transaction because we want all chunks of a document to save, or none at all.
	tx, err := e.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to start database transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Idempotency: Remove any existing chunks for this title before adding new ones.
	// This prevents the "Duplicate Ingestion" bug if you run the script twice.
	_, err = tx.Exec(ctx, "DELETE FROM game_knowledge WHERE title = $1", title)
	if err != nil {
		return fmt.Errorf("failed to clean up old chunks for %s: %w", title, err)
	}

	insertQuery := `
		INSERT INTO game_knowledge (title, category, game, source_url, content, embedding)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	for i, chunk := range chunks {
		vec := pgvector.NewVector(embeddings[i])

		_, err := tx.Exec(ctx, insertQuery, title, category, game, sourceURL, chunk.Text, vec)
		if err != nil {
			return fmt.Errorf("failed to insert chunk %d: %w", i, err)
		}

		// Preview the chunk content in the logs
		preview := chunk.Text
		if len(preview) > 50 {
			preview = preview[:47] + "..."
		}
		fmt.Printf("  -> [DB] Saved Chunk %d/%d: %s\n", i+1, len(chunks), preview)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Search queries the database for the top K closest chunks related to the user's question.
// Pass an empty string for game to search across all games.
func (e *Engine) Search(ctx context.Context, userQuery string, topK int, game string) ([]SearchResult, error) {
	queryVec, err := e.client.EmbedQuery(ctx, userQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to embed search query: %w", err)
	}

	vec := pgvector.NewVector(queryVec)

	var query string
	var args []any

	if game != "" {
		query = `
			SELECT title, category, game, source_url, content, 1 - (embedding <=> $1) AS similarity
			FROM game_knowledge
			WHERE game = $3
			ORDER BY embedding <=> $1
			LIMIT $2
		`
		args = []any{vec, topK, game}
	} else {
		query = `
			SELECT title, category, game, source_url, content, 1 - (embedding <=> $1) AS similarity
			FROM game_knowledge
			ORDER BY embedding <=> $1
			LIMIT $2
		`
		args = []any{vec, topK}
	}

	rows, err := e.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("database search failed: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Title, &r.Category, &r.Game, &r.SourceURL, &r.Content, &r.Similarity); err != nil {
			return nil, fmt.Errorf("failed to scan search row: %w", err)
		}
		results = append(results, r)
	}

	return results, nil
}

const answerRetrieveK = 20
const answerRerankTopN = 5

// ChatRerankMinScore is the default floor for /chat: if the best rerank score is below this, wiki docs are dropped.
const ChatRerankMinScore = 0.35

// Authoring controls optional wiki game filter and message framing for Command R.
// Embedding search and reranking use RetrievalQuery when set, otherwise question; user message is UserPrefix+question.
type Authoring struct {
	// WikiGameFilter limits vector search to poe1 or poe2 when non-empty; empty searches both.
	WikiGameFilter string
	// SystemSuffix is appended to SystemPrompt (player context and instructions).
	SystemSuffix string
	// UserPrefix is prepended to the player's question in the user message (e.g. character JSON).
	UserPrefix string
	// RetrieveK overrides answerRetrieveK when > 0.
	RetrieveK int
	// RerankTopN overrides answerRerankTopN when > 0.
	RerankTopN int
	// RetrievalQuery if non-empty is used for vector search and rerank instead of question (user message unchanged).
	RetrievalQuery string
	// RerankMinScore if > 0: when the top rerank relevance is below this, wiki documents are omitted (weak match).
	RerankMinScore float64
	// LogPipeline when non-empty enables verbose pipeline logs (retrieval, rerank, prompts). e.g. "chat_linked".
	LogPipeline string
}

// Answer runs retrieval (top answerRetrieveK), reranks to answerRerankTopN, then Command R chat.
// Use Authoring.WikiGameFilter "" to search both PoE1 and PoE2 chunks; otherwise "poe1" or "poe2".
// If retrieval (or rerank) yields no usable wiki chunks, chat runs without documents so the model
// can still answer from general knowledge (see SystemPromptNoWikiDocuments).
func (e *Engine) Answer(ctx context.Context, question string, auth Authoring) (string, error) {
	game := auth.WikiGameFilter
	system := SystemPrompt
	if auth.SystemSuffix != "" {
		system += "\n\n" + auth.SystemSuffix
	}
	userMessage := question
	if auth.UserPrefix != "" {
		userMessage = auth.UserPrefix + question
	}

	retrieveK := answerRetrieveK
	if auth.RetrieveK > 0 {
		retrieveK = auth.RetrieveK
	}
	topN := answerRerankTopN
	if auth.RerankTopN > 0 {
		topN = auth.RerankTopN
	}

	retrievalQ := question
	if auth.RetrievalQuery != "" {
		retrievalQ = auth.RetrievalQuery
	}

	if auth.LogPipeline != "" {
		log.Printf("rag.pipeline source=%s phase=input wiki_game_filter=%q retrieve_k=%d rerank_top_n=%d q_len=%d q_preview=%q retrieval_q_len=%d retrieval_q_preview=%q user_prefix_len=%d user_prefix_preview=%q system_len=%d system_preview=%q full_user_message_len=%d rerank_min_score=%.2f",
			auth.LogPipeline, game, retrieveK, topN,
			len(question), truncateRunes(question, 400),
			len(retrievalQ), truncateRunes(retrievalQ, 450),
			len(auth.UserPrefix), truncateRunes(auth.UserPrefix, 350),
			len(system), truncateRunes(system, 280),
			len(userMessage), auth.RerankMinScore)
	}

	candidates, err := e.Search(ctx, retrievalQ, retrieveK, game)
	if err != nil {
		return "", err
	}

	if auth.LogPipeline != "" {
		log.Printf("rag.pipeline source=%s phase=retrieve hits=%d", auth.LogPipeline, len(candidates))
		for i, c := range candidates {
			log.Printf("rag.pipeline source=%s retrieve[%d] title=%q category=%q game=%q similarity=%.4f content_len=%d preview=%q",
				auth.LogPipeline, i, c.Title, c.Category, c.Game, c.Similarity, len(c.Content), truncateRunes(c.Content, 160))
		}
	}

	if len(candidates) == 0 {
		system += "\n\n" + SystemPromptNoWikiDocuments
		log.Printf("rag.Answer wiki_game_filter=%q retrieve_hits=0 reranked_chunks=0 user_prefix_bytes=%d system_suffix_bytes=%d user_message_bytes=%d",
			game, len(auth.UserPrefix), len(auth.SystemSuffix), len(userMessage))
		if auth.LogPipeline != "" {
			log.Printf("rag.pipeline source=%s phase=chat no_wiki_docs=1 (appended SystemPromptNoWikiDocuments)", auth.LogPipeline)
		}
		return e.client.Chat(ctx, userMessage, nil, system)
	}

	docTexts := make([]string, len(candidates))
	for i, c := range candidates {
		docTexts[i] = c.Content
	}

	if len(docTexts) < topN {
		topN = len(docTexts)
	}

	ranked, err := e.client.Rerank(ctx, retrievalQ, docTexts, topN)
	if err != nil {
		return "", fmt.Errorf("rerank: %w", err)
	}

	if auth.LogPipeline != "" {
		log.Printf("rag.pipeline source=%s phase=rerank results=%d", auth.LogPipeline, len(ranked))
		for i, r := range ranked {
			title := ""
			if r.Index >= 0 && r.Index < len(candidates) {
				title = candidates[r.Index].Title
			}
			log.Printf("rag.pipeline source=%s rerank[%d] doc_index=%d relevance=%.4f title=%q",
				auth.LogPipeline, i, r.Index, r.RelevanceScore, title)
		}
	}

	if auth.RerankMinScore > 0 && len(ranked) > 0 && ranked[0].RelevanceScore < auth.RerankMinScore {
		system += "\n\n" + SystemPromptWeakWikiMatch
		log.Printf("rag.Answer wiki_game_filter=%q retrieve_hits=%d rerank_below_floor=1 best_score=%.4f min=%.2f user_message_bytes=%d",
			game, len(candidates), ranked[0].RelevanceScore, auth.RerankMinScore, len(userMessage))
		if auth.LogPipeline != "" {
			log.Printf("rag.pipeline source=%s phase=chat rerank_below_floor best_score=%.4f min=%.2f", auth.LogPipeline, ranked[0].RelevanceScore, auth.RerankMinScore)
		}
		return e.client.Chat(ctx, userMessage, nil, system)
	}

	topChunks := make([]SearchResult, 0, len(ranked))
	for _, r := range ranked {
		if r.Index >= 0 && r.Index < len(candidates) {
			topChunks = append(topChunks, candidates[r.Index])
		}
	}
	if len(topChunks) == 0 {
		system += "\n\n" + SystemPromptNoWikiDocuments
		log.Printf("rag.Answer wiki_game_filter=%q retrieve_hits=%d reranked_chunks=0 user_prefix_bytes=%d system_suffix_bytes=%d user_message_bytes=%d",
			game, len(candidates), len(auth.UserPrefix), len(auth.SystemSuffix), len(userMessage))
		if auth.LogPipeline != "" {
			log.Printf("rag.pipeline source=%s phase=chat rerank_empty=1 (appended SystemPromptNoWikiDocuments)", auth.LogPipeline)
		}
		return e.client.Chat(ctx, userMessage, nil, system)
	}

	log.Printf("rag.Answer wiki_game_filter=%q retrieve_hits=%d reranked_chunks=%d user_prefix_bytes=%d system_suffix_bytes=%d user_message_bytes=%d",
		game, len(candidates), len(topChunks), len(auth.UserPrefix), len(auth.SystemSuffix), len(userMessage))

	if auth.LogPipeline != "" {
		for i, ch := range topChunks {
			log.Printf("rag.pipeline source=%s phase=to_model doc[%d] title=%q category=%q game=%q content_len=%d preview=%q",
				auth.LogPipeline, i, ch.Title, ch.Category, ch.Game, len(ch.Content), truncateRunes(ch.Content, 200))
		}
		log.Printf("rag.pipeline source=%s phase=cohere_chat docs_for_command_r=%d user_message_len=%d", auth.LogPipeline, len(topChunks), len(userMessage))
	}

	return e.client.Chat(ctx, userMessage, topChunks, system)
}

// truncateRunes shortens s for logs (max runes, not bytes).
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxRunes]) + "…"
}

// SystemPrompt is the default system message for RAG-grounded chat with Command R.
const SystemPrompt = `You are Poe Herald, an expert assistant for Path of Exile game mechanics.

Rules:
- When wiki documents are provided in this turn, ground mechanics claims in them. Never invent numbers or formulas that contradict those documents.
- Documents may come from PoE1 or PoE2. Note which game the information applies to when relevant.
- When referencing specific values or formulas from a document, quote or name the source (title/category).
- When the user message includes a "Character snapshot" section, treat it as factual player data from the official API. Do not contradict it; use it together with the wiki documents.`

// SystemPromptNoWikiDocuments is appended when vector search returns nothing (or rerank yields no chunks).
const SystemPromptNoWikiDocuments = `No wiki passages were retrieved for this question (empty index, no close match, or filtered game has no chunks). There are no document citations for this turn.

Answer using your general Path of Exile knowledge. Clearly separate established mechanics from uncertainty; call out when league, patch, or PoE1 vs PoE2 matters. Do not claim text came from wiki documents.`

// SystemPromptWeakWikiMatch is appended when rerank scores are below RerankMinScore (tangential wiki hits).
const SystemPromptWeakWikiMatch = `Wiki passages were retrieved but did not strongly match this question (low relevance). Do not use tangential wiki topics as the spine of your answer.

Prioritize the player's build data in the user message (character snapshot or Path of Building XML). Quote or reference specific stats that appear there. Give practical defensive and build advice, including common baselines (e.g. resistance caps, coherent mitigation layers) when they help. Use wiki documents only to clarify a mechanic that directly supports your analysis. If a stat is not present in the player context, say it is unknown rather than inventing it.`
