package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The Path of Exile Wiki (poewiki.net) is built on MediaWiki.
// Instead of downloading HTML and wrestling with parsing tags, MediaWiki offers a beautiful
// native API that returns "extracts" — the pure, human-readable text of an article!

const poe1APIEndpoint = "https://www.poewiki.net/w/api.php"
const poe2APIEndpoint = "https://www.poe2wiki.net/w/api.php"

type WikiIngestor struct {
	engine     *Engine
	game       string // 'poe1' or 'poe2'
	httpClient *http.Client
}

func NewWikiIngestor(dbEngine *Engine, game string) *WikiIngestor {
	return &WikiIngestor{
		engine: dbEngine,
		game:   game,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Increased timeout for large pages like 'Essence'
		},
	}
}

// MediaWikiResponse represents the somewhat nested JSON structure MediaWiki returns.
type MediaWikiResponse struct {
	Query struct {
		Pages map[string]struct { // MediaWiki returns pages by dynamic ID keys (e.g. {"12345": {...}})
			PageID  int    `json:"pageid"`
			Title   string `json:"title"`
			Extract string `json:"extract"` // This is the goldmine: pure text content!
		} `json:"pages"`
	} `json:"query"`
}

// CategoryMembersResponse represents the JSON structure for list=categorymembers
type CategoryMembersResponse struct {
	Continue struct {
		CmContinue string `json:"cmcontinue"`
	} `json:"continue"`
	Query struct {
		CategoryMembers []struct {
			Title string `json:"title"`
		} `json:"categorymembers"`
	} `json:"query"`
}

// IngestArticle grabs a specific topic from the wiki and shoves it into our vector database.
func (w *WikiIngestor) IngestArticle(ctx context.Context, pageTitle string) error {
	// 1. Build the API URL
	// We use net/url for safe encoding of spaces and special chars (e.g. "Vaal Skill" -> "Vaal%20Skill")
	var wikiAPIEndpoint string
	if w.game == "poe1" {
		wikiAPIEndpoint = poe1APIEndpoint
	} else {
		wikiAPIEndpoint = poe2APIEndpoint
	}

	u, err := url.Parse(wikiAPIEndpoint)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("action", "query")
	q.Set("format", "json")
	q.Set("prop", "extracts")
	q.Set("explaintext", "1")
	q.Set("redirects", "1") // Important: follow redirects (e.g., Energy_Shield -> Energy_shield)
	q.Set("titles", pageTitle)
	u.RawQuery = q.Encode()

	// 2. Fetch from the Wiki
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create wiki request: %w", err)
	}

	// Like GGG's API, the Wiki maintainers appreciate a User-Agent so they see who is hitting them.
	req.Header.Set("User-Agent", "poe-herald/1.0.0 (contact: leo.cheng92@gmail.com)")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("wiki network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wiki returned status: %d", resp.StatusCode)
	}

	// 3. Decode the nested JSON
	var wikiData MediaWikiResponse
	if err := json.NewDecoder(resp.Body).Decode(&wikiData); err != nil {
		return fmt.Errorf("failed to decode wiki json: %w", err)
	}

	// Because the JSON has dynamic keys in the "pages" object, we have to loop over the map
	// to find our single page.
	var extract string
	var actualTitle string
	for _, pageInfo := range wikiData.Query.Pages {
		extract = pageInfo.Extract
		actualTitle = pageInfo.Title
		break // We only requested one title, so we only need the first iteration
	}

	if extract == "" {
		return fmt.Errorf("could not find text extract for page: %s", pageTitle)
	}

	// 4. Pass the massive text block to our RAG Engine!
	// The RAG Engine will handle chunking the text, calling Cohere for vectors, and saving to Postgres.
	var sourceURL string
	if w.game == "poe1" {
		sourceURL = fmt.Sprintf("https://www.poewiki.net/wiki/%s", url.PathEscape(actualTitle))
	} else {
		sourceURL = fmt.Sprintf("https://www.poe2wiki.net/wiki/%s", url.PathEscape(actualTitle))
	}
	fmt.Printf("[Wiki] Downloaded: %s (%d characters). Starting ingestion...\n", actualTitle, len(extract))

	err = w.engine.IngestDocument(ctx, actualTitle, "wiki-mechanics", w.game, sourceURL, extract)
	if err != nil {
		return fmt.Errorf("engine failed to ingest wiki doc: %w", err)
	}

	return nil
}

// GetCategoryMembers fetches the titles of all pages within a specific Wiki category.
func (w *WikiIngestor) GetCategoryMembers(ctx context.Context, categoryTitle string) ([]string, error) {
	var wikiAPIEndpoint string
	if w.game == "poe1" {
		wikiAPIEndpoint = poe1APIEndpoint
	} else {
		wikiAPIEndpoint = poe2APIEndpoint
	}

	u, err := url.Parse(wikiAPIEndpoint)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("action", "query")
	q.Set("list", "categorymembers")
	q.Set("cmtitle", "Category:"+categoryTitle)
	q.Set("cmlimit", "500") // Fetch up to 500 pages at once
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "poe-herald/1.0.0 (contact: leo.cheng92@gmail.com)")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var cmData CategoryMembersResponse
	if err := json.NewDecoder(resp.Body).Decode(&cmData); err != nil {
		return nil, err
	}

	var titles []string
	for _, member := range cmData.Query.CategoryMembers {
		// Filter out "Category:" or "File:" namespaces if they accidentally appear
		if !strings.Contains(member.Title, ":") {
			titles = append(titles, member.Title)
		}
	}

	return titles, nil
}
