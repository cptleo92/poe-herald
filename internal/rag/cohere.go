package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	cohereEmbedModel = "embed-english-v3.0"
	cohereEmbedURL   = "https://api.cohere.ai/v1/embed"

	cohereRerankModel = "rerank-english-v3.0"
	cohereRerankURL   = "https://api.cohere.ai/v2/rerank"

	cohereChatModel = "command-r-08-2024"
	cohereChatURL   = "https://api.cohere.ai/v2/chat"
)

// CohereClient talks to Cohere embed, rerank, and chat endpoints (same API key).
type CohereClient struct {
	apiKey         string
	httpClient     *http.Client // embed + rerank (fast)
	httpClientChat *http.Client // chat generation can be slow; separate long timeout
}

func NewCohereClient(apiKey string) *CohereClient {
	return &CohereClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 3 * time.Minute,
		},
		// Command R with RAG documents may sit in queue or stream slowly; 120s is often too short.
		httpClientChat: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

// --- Embed (v1) ---

type embedRequest struct {
	Texts     []string `json:"texts"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func (c *CohereClient) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return c.callEmbed(ctx, texts, "search_document")
}

func (c *CohereClient) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	embeddings, err := c.callEmbed(ctx, []string{query}, "search_query")
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("cohere returned 0 embeddings for query")
	}
	return embeddings[0], nil
}

func (c *CohereClient) callEmbed(ctx context.Context, texts []string, inputType string) ([][]float32, error) {
	bodyBytes, err := json.Marshal(embedRequest{
		Texts:     texts,
		Model:     cohereEmbedModel,
		InputType: inputType,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cohereEmbedURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	c.setAuthJSONHeaders(req)

	fmt.Printf("[Cohere] Sending %d text chunks for embedding...\n", len(texts))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error calling cohere embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readAPIError(resp, "embed")
	}

	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode embed response: %w", err)
	}
	return out.Embeddings, nil
}

// --- Rerank (v2) ---

type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
}

type rerankResponse struct {
	Results []struct {
		Index           int     `json:"index"`
		RelevanceScore  float64 `json:"relevance_score"`
	} `json:"results"`
}

// RerankResult maps a reranked position back to the original documents slice.
type RerankResult struct {
	Index           int
	RelevanceScore  float64
}

// Rerank reorders document strings by relevance to the query (cross-attention).
func (c *CohereClient) Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	if topN > len(documents) {
		topN = len(documents)
	}

	bodyBytes, err := json.Marshal(rerankRequest{
		Model:     cohereRerankModel,
		Query:     query,
		Documents: documents,
		TopN:      topN,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rerank request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cohereRerankURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	c.setAuthJSONHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error calling cohere rerank: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readAPIError(resp, "rerank")
	}

	var out rerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode rerank response: %w", err)
	}

	results := make([]RerankResult, 0, len(out.Results))
	for _, r := range out.Results {
		results = append(results, RerankResult{
			Index:          r.Index,
			RelevanceScore: r.RelevanceScore,
		})
	}
	return results, nil
}

// --- Chat (v2) ---

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatDocument struct {
	Data map[string]string `json:"data"`
}

type chatRequest struct {
	Model     string           `json:"model"`
	Messages  []chatMessage    `json:"messages"`
	Documents []chatDocument   `json:"documents"`
}

// Chat runs Command R with RAG documents and returns the assistant text.
func (c *CohereClient) Chat(ctx context.Context, userQuestion string, chunks []SearchResult, systemPrompt string) (string, error) {
	docs := make([]chatDocument, 0, len(chunks))
	for _, ch := range chunks {
		docs = append(docs, chatDocument{
			Data: map[string]string{
				"text":        ch.Content,
				"title":       ch.Title,
				"source_url":  ch.SourceURL,
				"category":    ch.Category,
				"game":        ch.Game,
			},
		})
	}

	body := chatRequest{
		Model: cohereChatModel,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userQuestion},
		},
		Documents: docs,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cohereChatURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	c.setAuthJSONHeaders(req)

	resp, err := c.httpClientChat.Do(req)
	if err != nil {
		return "", fmt.Errorf("network error calling cohere chat: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cohere chat: %s", string(respBody))
	}

	text, err := extractAssistantText(respBody)
	if err != nil {
		return "", fmt.Errorf("parse chat response: %w (body: %s)", err, truncateForErr(respBody))
	}
	return text, nil
}

func (c *CohereClient) setAuthJSONHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}

func (c *CohereClient) readAPIError(resp *http.Response, endpoint string) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return fmt.Errorf("cohere %s: status %d: %s", endpoint, resp.StatusCode, string(b))
}

func extractAssistantText(body []byte) (string, error) {
	var outer struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &outer); err != nil {
		return "", err
	}
	if len(outer.Message.Content) == 0 {
		return "", fmt.Errorf("empty assistant message")
	}

	var s string
	if err := json.Unmarshal(outer.Message.Content, &s); err == nil && s != "" {
		return s, nil
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(outer.Message.Content, &blocks); err == nil {
		var b strings.Builder
		for _, bl := range blocks {
			if bl.Type == "text" && bl.Text != "" {
				b.WriteString(bl.Text)
			}
		}
		if b.Len() > 0 {
			return b.String(), nil
		}
	}

	return "", fmt.Errorf("could not parse assistant content")
}

func truncateForErr(b []byte) string {
	const max = 500
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
