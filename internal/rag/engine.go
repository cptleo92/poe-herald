package rag

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// The Engine is the coordinator of our RAG system.
// It holds our Cohere API client to generate embeddings, and our Postgres Pool to store them.
type Engine struct {
	db     *pgxpool.Pool
	client *EmbedClient
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

func NewEngine(db *pgxpool.Pool, client *EmbedClient) *Engine {
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
func (e *Engine) Search(ctx context.Context, userQuery string, topK int) ([]SearchResult, error) {
	// 1. Convert the user's question into completely identical Math format
	// Example: user says "league start builds". Cohere understands this concept and generates [0.55, -1.2, ...]
	queryVec, err := e.client.EmbedQuery(ctx, userQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to embed search query: %w", err)
	}

	// 2. Perform a "vector similarity search" using pgvector's built-in operators.
	// The operator <=> calculates "Cosine Distance".
	// The smaller the Cosine Distance, the MORE similar the texts are.
	// So we ORDER BY the distance ascending, and LIMIT to our topK chunks.
	sql := `
		SELECT title, category, game, source_url, content, 1 - (embedding <=> $1) AS similarity 
		FROM game_knowledge 
		ORDER BY embedding <=> $1
		LIMIT $2
	`

	vec := pgvector.NewVector(queryVec)
	rows, err := e.db.Query(ctx, sql, vec, topK)
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
