package ggg

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GGG Headers
const (
	HeaderRateLimitAccount      = "X-Rate-Limit-Account"
	HeaderRateLimitAccountState = "X-Rate-Limit-Account-State"
	HeaderRetryAfter            = "Retry-After"
)

// rateLimitTracker stores the time when a token's rate limit expires.
// Keyed by the access token.
var (
	rateLimitTracker = make(map[string]time.Time)
	trackerMutex     sync.RWMutex
)

// RateLimitError is returned when a request is blocked by GGG rate limits.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("Rate limited. Please wait %v.", e.RetryAfter.Round(time.Second))
}

var (
	baseURL               = "https://api.pathofexile.com"
	characterEndpoint     = "/character"
	characterPoe2Endpoint = "/character/poe2"
)

// Client is a reusable GGG API client that handles auth and required headers.
type Client struct {
	accessToken string
	userAgent   string
	httpClient  *http.Client
	// OnResponse is an optional callback that exposes response headers for debugging/probing.
	OnResponse func(headers http.Header)
}

// NewClient creates a GGG API client for a given access token.
func NewClient(accessToken, userAgent string) *Client {
	return &Client{
		accessToken: accessToken,
		userAgent:   userAgent,
		httpClient:  http.DefaultClient,
	}
}

// doRequest builds and executes a GET request with the required GGG headers.
// It checks the rate limit tracker before making the request.
func (c *Client) doRequest(path string) (*http.Response, error) {
	// 1. Check if we are currently rate limited
	trackerMutex.RLock()
	expiry, exists := rateLimitTracker[c.accessToken]
	trackerMutex.RUnlock()

	if exists && time.Now().Before(expiry) {
		return nil, &RateLimitError{RetryAfter: time.Until(expiry)}
	}

	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if c.OnResponse != nil {
		c.OnResponse(resp.Header)
	}

	// 2. Handle 429 Too Many Requests
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfterStr := resp.Header.Get("Retry-After")
		retryAfterSecs, _ := strconv.Atoi(retryAfterStr)
		if retryAfterSecs == 0 {
			retryAfterSecs = 60 // Default if header is missing or invalid
		}

		duration := time.Duration(retryAfterSecs) * time.Second
		until := time.Now().Add(duration)

		trackerMutex.Lock()
		rateLimitTracker[c.accessToken] = until
		trackerMutex.Unlock()

		resp.Body.Close() // Close body as we're returning an error
		return nil, &RateLimitError{RetryAfter: duration}
	}

	return resp, nil
}

// APICharacter represents a character as returned by the GGG API.
type APICharacter struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Realm      string `json:"realm"`
	Class      string `json:"class"`
	League     string `json:"league"`
	Level      int    `json:"level"`
	Experience int    `json:"experience"`
}

type characterListResponse struct {
	Characters []APICharacter `json:"characters"`
}

// APICharacterFull represents a character with more details (returned by /character/{name}).
type APICharacterFull struct {
	Character APICharacter `json:"character"`
}

// FetchCharacters retrieves all characters (PoE1 + PoE2) for the authenticated account.
func (c *Client) FetchCharacters() ([]APICharacter, error) {
	endpoints := []string{characterEndpoint, characterPoe2Endpoint}
	var all []APICharacter

	for _, ep := range endpoints {
		chars, err := c.fetchCharactersFrom(ep)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ep, err)
		}
		all = append(all, chars...)
	}

	return all, nil
}

func (c *Client) fetchCharactersFrom(path string) ([]APICharacter, error) {
	resp, err := c.doRequest(path)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GGG API returned status %d", resp.StatusCode)
	}

	var result characterListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return result.Characters, nil
}

// FetchCharacter retrieves details for a single character by name.
func (c *Client) FetchCharacter(name string) (*APICharacter, error) {
	path := fmt.Sprintf("/character/%v", name)
	resp, err := c.doRequest(path)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GGG API returned status %d", resp.StatusCode)
	}

	var result APICharacterFull
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result.Character, nil
}

// FilterLeagueCharacters filters out any Standard or SSF characters
// and returns the rest sorted by level ascending
func FilterLeagueCharacters(characters []APICharacter, maxResults int) []APICharacter {
	var filtered []APICharacter
	for _, c := range characters {
		if strings.Contains(c.League, "Standard") || strings.Contains(c.League, "Solo") {
			continue
		}
		filtered = append(filtered, c)
	}

	slices.SortFunc(filtered, func(i, j APICharacter) int {
		return i.Level - j.Level
	})

	if len(filtered) > maxResults {
		filtered = filtered[:maxResults]
	}

	return filtered
}
