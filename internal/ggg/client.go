package ggg

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const (
	baseURL               = "https://api.pathofexile.com"
	characterEndpoint     = "/character"
	characterPoe2Endpoint = "/character/poe2"
)

// Client is a reusable GGG API client that handles auth and required headers.
type Client struct {
	accessToken string
	userAgent   string
	httpClient  *http.Client
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
func (c *Client) doRequest(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("User-Agent", c.userAgent)

	return c.httpClient.Do(req)
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

// FilterLeagueCharacters filters out any character whose league contains "Standard" (covers
// "Standard", "HC Standard", "SSF Standard", etc.) and returns the rest sorted by level descending,
// capped at maxResults.
func FilterLeagueCharacters(characters []APICharacter, maxResults int) []APICharacter {
	var filtered []APICharacter
	for _, c := range characters {
		if strings.Contains(c.League, "Standard") {
			continue
		}
		filtered = append(filtered, c)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Level > filtered[j].Level
	})

	if len(filtered) > maxResults {
		filtered = filtered[:maxResults]
	}

	return filtered
}
