package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// In RAG (Retrieval-Augmented Generation), an "Embedding" is simply an array of floating-point numbers.
// These numbers mathematically represent the semantic meaning of a piece of text.
// If two pieces of text have similar meanings (e.g. "dog" vs "hound"), their floating-point arrays
// will mathematically point in roughly the same direction (known as cosine similarity).

const (
	// The specific Cohere model we are using. "embed-english-v3.0" outputs an array of 1024 floats.
	cohereModel    = "embed-english-v3.0"
	cohereEndpoint = "https://api.cohere.ai/v1/embed"
)

// EmbedClient is responsible for communicating with the Cohere API.
// We use a custom struct so we don't need to pass the API key or HTTP client into every function.
// This is a common Go pattern: defining a client struct with its dependencies.
type EmbedClient struct {
	apiKey     string
	httpClient *http.Client
}

func NewEmbedClient(apiKey string) *EmbedClient {
	return &EmbedClient{
		apiKey: apiKey,
		// It is crucial to always configure timeouts for HTTP clients in Go.
		// The default http.Client has NO TIMEOUT and can hang your app forever if the remote server stalls.
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// ------ API Request & Response Shapes ------

// cohereRequest represents the JSON body we send to Cohere.
type cohereRequest struct {
	Texts     []string `json:"texts"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type"`
	// Note: in Cohere v3 models, you must specify input_type.
	// Use "search_document" for texts you are saving to your DB.
	// Use "search_query" for the question the user is asking in Discord.
}

// cohereResponse represents the JSON we expect back from Cohere.
type cohereResponse struct {
	Id         string      `json:"id"`
	Texts      []string    `json:"texts"`
	Embeddings [][]float32 `json:"embeddings"` // A slice of float slices: one array of floats per text string we sent
}

// -------------------------------------------

// EmbedDocuments asks Cohere to convert a list of text strings into vectors (embeddings)
// for the purpose of STORING them in our database.
func (c *EmbedClient) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return c.callCohereAPI(ctx, texts, "search_document")
}

// EmbedQuery asks Cohere to convert a single user question (from Discord) into a vector (embedding)
// for the purpose of SEARCHING our database to find matching documents.
func (c *EmbedClient) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	// Cohere API always takes an array of texts, so we wrap our single query in a slice of length 1.
	embeddings, err := c.callCohereAPI(ctx, []string{query}, "search_query")
	if err != nil {
		return nil, err
	}

	// Safety check: Make sure we actually got the 1 embedding back.
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("cohere returned 0 embeddings for query")
	}

	return embeddings[0], nil
}

// callCohereAPI is the private helper function that actually executes the HTTP POST request to Cohere.
func (c *EmbedClient) callCohereAPI(ctx context.Context, texts []string, inputType string) ([][]float32, error) {
	// 1. Build the payload
	reqData := cohereRequest{
		Texts:     texts,
		Model:     cohereModel,
		InputType: inputType,
	}

	// 2. Marshal (convert) our Go struct into JSON bytes
	bodyBytes, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cohere request: %w", err)
	}

	// 3. Create the HTTP request
	// We use http.NewRequestWithContext so that if the parent function cancels the context
	// (e.g. the user closes their discord app, or a system timeout hits), the HTTP request aborts early.
	req, err := http.NewRequestWithContext(ctx, "POST", cohereEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	// 4. Attach required Headers
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	// Highly recommended to set an Accept header for APIs that return JSON
	req.Header.Set("Accept", "application/json")

	// 5. Execute the request over the network
	fmt.Printf("[Cohere] Sending %d text chunks for embedding...\n", len(texts))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error calling cohere: %w", err)
	}
	// Important: Always ensure the response body is closed so we don't leak memory.
	// 'defer' ensures this runs right before this function returns, regardless of errors.
	defer resp.Body.Close()

	// 6. Check for non-200 OK status codes
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cohere returned non-200 status: %d", resp.StatusCode)
	}

	// 7. Parse the JSON response back into a Go struct
	var cohereResp cohereResponse
	if err := json.NewDecoder(resp.Body).Decode(&cohereResp); err != nil {
		return nil, fmt.Errorf("failed to decode cohere response: %w", err)
	}

	// 8. Return the embedded vectors!
	return cohereResp.Embeddings, nil
}
