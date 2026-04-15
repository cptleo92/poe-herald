package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

// This tool queries the MediaWiki API to list ALL categories on a given wiki,
// then outputs a reviewable file where you can mark each category as
// [INCLUDE] or [EXCLUDE] before running the main ingestor.

const poe1API = "https://www.poewiki.net/w/api.php"
const poe2API = "https://www.poe2wiki.net/w/api.php"

// Categories we are confident should be EXCLUDED.
// These are wiki-maintenance, cosmetic, or non-gameplay categories.
var excludeKeywords = []string{
	"template", "module", "file", "image", "icon",
	"documentation", "maintenance", "deprecated",
	"copyright", "license", "hidden",
	"cargo", "formatting", "hatnote", "infobox",
	"infocard", "cleanup", "dispute", "footnote",
	"citation", "editnotice", "experimental",
	"archival", "substitution", "content management",
	"disambiguation", "guideline", "help",
	"media", "illustration", "loading screen",
	"portrait", "screenshot", "frame image",
	"skin", "cosmetic", "microtransaction",
	"footprint", "character effect", "attachment",
	"alternate charge", "alternate skill effect",
	"armour set", "legal", "community",
	"development information", "charts and graphs",
	"historical game content", "expansion logo",
	"league banner", "league logo",
	"bug", "tonal issue", "unsourced", "unconfirmed",
	"guesswork", "missing file", "placeholder",
	"without a release", "needing update",
	"duplicate recipe", "improper modifier",
	"invalid recipe", "legacy variant",
	"table without result", "item count",
	"grinding gear", "copyrighted",
	"public domain", "documentation", "tileset", "article", "lore", "npc", "passive", "modifiers", "mods", "audio",
}

// subcategoriesResponse represents the JSON for list=categorymembers queries.
type subcategoriesResponse struct {
	Continue struct {
		CmContinue string `json:"cmcontinue"`
	} `json:"continue"`
	Query struct {
		CategoryMembers []struct {
			Title string `json:"title"`
		} `json:"categorymembers"`
	} `json:"query"`
}

// categoryInfoResponse represents the JSON for prop=categoryinfo queries.
type categoryInfoResponse struct {
	Query struct {
		Pages map[string]struct {
			Title        string `json:"title"`
			CategoryInfo struct {
				Pages   int `json:"pages"`
				Subcats int `json:"subcats"`
			} `json:"categoryinfo"`
		} `json:"pages"`
	} `json:"query"`
}

type categoryInfo struct {
	Pages   int
	Subcats int
}

func main() {
	game := flag.String("game", "poe1", "Which wiki to scan (poe1 or poe2)")
	output := flag.String("out", "", "Output file path (e.g. data/poe2_categories.txt)")
	flag.Parse()

	if *game != "poe1" && *game != "poe2" {
		log.Fatal("Invalid game. Must be 'poe1' or 'poe2'.")
	}

	apiURL := poe1API
	if *game == "poe2" {
		apiURL = poe2API
	}

	outFile := *output
	if outFile == "" {
		outFile = fmt.Sprintf("data/%s_categories.txt", *game)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	// Walk the category tree starting from the game's root category.
	// This avoids picking up wiki-maintenance categories (help, templates, etc.)
	// that live outside the game content tree.
	rootCategory := "Path of Exile 2"
	if *game == "poe1" {
		rootCategory = "Path of Exile"
	}

	fmt.Printf("Walking category tree from \"%s\" on %s wiki...\n", rootCategory, *game)

	seen := make(map[string]bool)
	var allCategories []string
	queue := []string{rootCategory}
	seen[rootCategory] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		subs := fetchSubcategories(client, apiURL, current)
		for _, sub := range subs {
			name := strings.TrimPrefix(sub, "Category:")
			if seen[name] {
				continue
			}
			seen[name] = true
			allCategories = append(allCategories, name)
			queue = append(queue, name)
		}

		fmt.Printf("  Discovered %d categories so far (queue: %d)...\n", len(allCategories), len(queue))
	}

	slices.Sort(allCategories)
	fmt.Printf("Total categories found: %d\n", len(allCategories))

	// Fetch page counts for each category (batched 50 at a time per MediaWiki limits)
	fmt.Println("Fetching page counts...")
	catInfoMap := fetchCategoryInfo(client, apiURL, allCategories)

	// Classify each category
	var lines []string
	lines = append(lines, fmt.Sprintf("# %s Wiki Category Review", strings.ToUpper(*game)))
	lines = append(lines, "#")
	lines = append(lines, "# Format: [INCLUDE/EXCLUDE] Category Name (pages: N, subcats: N)")
	lines = append(lines, "# Edit the [INCLUDE] / [EXCLUDE] tags below.")
	lines = append(lines, "# When you're happy, pass this file to the ingestor.")
	lines = append(lines, "# Lines starting with # are comments and will be ignored.")
	lines = append(lines, "#")
	lines = append(lines, fmt.Sprintf("# Total categories: %d", len(allCategories)))
	lines = append(lines, "")

	includeCount := 0
	excludeCount := 0
	totalPages := 0

	for _, cat := range allCategories {
		info := catInfoMap[cat]

		// Skip empty categories (0 pages and 0 subcategories)
		if info.Pages == 0 && info.Subcats == 0 {
			continue
		}

		label := classify(cat)
		if label == "INCLUDE" {
			includeCount++
			totalPages += info.Pages
		} else {
			excludeCount++
		}
		lines = append(lines, fmt.Sprintf("[%s] %s (pages: %d, subcats: %d)", label, cat, info.Pages, info.Subcats))
	}

	// Write the output file
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(outFile, []byte(content), 0644); err != nil {
		log.Fatalf("Failed to write output: %v", err)
	}

	fmt.Printf("\n✅ Category review file written to: %s\n", outFile)
	fmt.Printf("   %d marked INCLUDE (%d total pages), %d marked EXCLUDE\n", includeCount, totalPages, excludeCount)
	fmt.Println("   Open the file and adjust tags, then run the ingestor with --categories flag.")
}

// classify uses keyword matching to suggest whether a category is worth ingesting.
func classify(categoryName string) string {
	lower := strings.ToLower(categoryName)

	if slices.ContainsFunc(excludeKeywords, func(kw string) bool {
		return strings.Contains(lower, kw)
	}) {
		return "EXCLUDE"
	}

	return "INCLUDE"
}

// fetchCategoryInfo queries the MediaWiki API in batches of 50 to get page/subcategory counts.
func fetchCategoryInfo(client *http.Client, apiURL string, categories []string) map[string]categoryInfo {
	result := make(map[string]categoryInfo)

	// MediaWiki allows up to 50 titles per request
	batchSize := 50
	for i := 0; i < len(categories); i += batchSize {
		end := i + batchSize
		if end > len(categories) {
			end = len(categories)
		}
		batch := categories[i:end]

		// Build pipe-separated "Category:X|Category:Y|..." titles
		var titles []string
		for _, cat := range batch {
			titles = append(titles, "Category:"+cat)
		}
		titlesParam := strings.Join(titles, "|")

		reqURL := fmt.Sprintf("%s?action=query&prop=categoryinfo&format=json&titles=%s",
			apiURL, url.QueryEscape(titlesParam))

		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			log.Printf("Warning: failed to create request for batch %d: %v", i/batchSize, err)
			continue
		}
		req.Header.Set("User-Agent", "poe-herald/1.0.0 (contact: leo.cheng92@gmail.com)")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Warning: batch %d request failed: %v", i/batchSize, err)
			continue
		}

		var data categoryInfoResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			resp.Body.Close()
			log.Printf("Warning: batch %d decode failed: %v", i/batchSize, err)
			continue
		}
		resp.Body.Close()

		for _, pageInfo := range data.Query.Pages {
			// Strip the "Category:" prefix to match our internal names
			name := strings.TrimPrefix(pageInfo.Title, "Category:")
			result[name] = categoryInfo{
				Pages:   pageInfo.CategoryInfo.Pages,
				Subcats: pageInfo.CategoryInfo.Subcats,
			}
		}

		fmt.Printf("  Page counts: %d/%d categories processed...\n", end, len(categories))
		time.Sleep(500 * time.Millisecond)
	}

	return result
}

// fetchSubcategories returns all subcategory titles (prefixed with "Category:") for the given category.
func fetchSubcategories(client *http.Client, apiURL string, category string) []string {
	var results []string
	continueToken := ""

	for {
		reqURL := fmt.Sprintf("%s?action=query&list=categorymembers&cmtitle=Category:%s&cmtype=subcat&cmlimit=500&format=json",
			apiURL, url.QueryEscape(category))
		if continueToken != "" {
			reqURL += "&cmcontinue=" + url.QueryEscape(continueToken)
		}

		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			log.Printf("Warning: failed to create subcategory request for %s: %v", category, err)
			return results
		}
		req.Header.Set("User-Agent", "poe-herald/1.0.0 (contact: leo.cheng92@gmail.com)")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Warning: subcategory request failed for %s: %v", category, err)
			return results
		}

		var data subcategoriesResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			resp.Body.Close()
			log.Printf("Warning: subcategory decode failed for %s: %v", category, err)
			return results
		}
		resp.Body.Close()

		for _, member := range data.Query.CategoryMembers {
			results = append(results, member.Title)
		}

		if data.Continue.CmContinue == "" {
			break
		}
		continueToken = data.Continue.CmContinue
		time.Sleep(500 * time.Millisecond)
	}

	time.Sleep(300 * time.Millisecond) // Be polite between categories
	return results
}
