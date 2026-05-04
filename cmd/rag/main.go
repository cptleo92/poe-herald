package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cptleo92/poe-herald/internal/rag"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, `Usage: %s <preview|download|ingest> [flags]

  Workflow:  download  →  ingest
             (mirror wiki to disk)  (chunk + embed into Postgres from that mirror)

  preview    Discover wiki pages; print estimated tokens, cost, and duration.
  download   Fetch wiki extracts into data/wiki_raw/{game}/{category}/{page}.txt (wiki HTTP only).
  ingest     Read only from the local mirror (default: data/wiki_raw). Requires COHERE_API_KEY, DB_DSN.

`, os.Args[0])
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "preview":
		runPreview(args)
	case "download":
		runDownload(args)
	case "ingest":
		runIngest(args)
	default:
		log.Fatalf("unknown command %q (use preview, download, or ingest)", cmd)
	}
}

type IngestTarget struct {
	Game     string
	Category string
	Page     string
}

func runPreview(args []string) {
	fs := flag.NewFlagSet("preview", flag.ExitOnError)
	gameFlag := fs.String("game", "poe1", "Game to scan (poe1, poe2, or both)")
	categoriesFile := fs.String("categories", "", "Path to categories review file (default: data/{game}_categories.txt)")
	fs.Parse(args)

	if err := validateGameFlag(*gameFlag); err != nil {
		log.Fatal(err)
	}
	gamesToRun := gamesFromFlag(*gameFlag)

	if err := godotenv.Load(); err != nil {
		log.Println("Note: No .env file found. Relying on system environment variables.")
	}

	ctx := context.Background()

	allTargets, totalCategories, err := discoverPages(ctx, gamesToRun, *categoriesFile)
	if err != nil {
		log.Fatal(err)
	}
	if len(allTargets) == 0 {
		log.Fatal("No pages discovered from any category. Nothing to estimate.")
	}

	printEstimateSummary(*gameFlag, totalCategories, len(allTargets))
}

func runDownload(args []string) {
	fs := flag.NewFlagSet("download", flag.ExitOnError)
	gameFlag := fs.String("game", "poe1", "Game to download (poe1, poe2, or both)")
	categoriesFile := fs.String("categories", "", "Path to categories review file (default: data/{game}_categories.txt)")
	outDir := fs.String("out", "data/wiki_raw", "Output directory for raw wiki extracts")
	force := fs.Bool("force", false, "Re-download and overwrite files that already exist (for wiki updates)")
	fs.Parse(args)

	if err := validateGameFlag(*gameFlag); err != nil {
		log.Fatal(err)
	}
	gamesToRun := gamesFromFlag(*gameFlag)

	ctx := context.Background()

	allTargets, totalCategories, err := discoverPages(ctx, gamesToRun, *categoriesFile)
	if err != nil {
		log.Fatal(err)
	}
	if len(allTargets) == 0 {
		log.Fatal("No pages discovered from any category. Nothing to download.")
	}

	printEstimateSummary(*gameFlag, totalCategories, len(allTargets))
	fmt.Println()
	if *force {
		fmt.Println("Force mode: existing files will be overwritten.")
	}
	fmt.Printf("Downloading %d wiki pages to %s/...\n\n", len(allTargets), *outDir)

	ingestors := make(map[string]*rag.WikiIngestor)
	for _, g := range gamesToRun {
		ingestors[g] = rag.NewWikiIngestor(nil, g)
	}

	successCount := 0
	skipCount := 0
	failCount := 0
	var failedPages []string

	for i, target := range allTargets {
		dir := fmt.Sprintf("%s/%s/%s", *outDir, target.Game, sanitizePath(target.Category))
		filePath := fmt.Sprintf("%s/%s.txt", dir, sanitizePath(target.Page))

		if !*force {
			if _, err := os.Stat(filePath); err == nil {
				fmt.Printf("[%d/%d] [%s] SKIP (exists): %s\n", i+1, len(allTargets), target.Game, target.Page)
				skipCount++
				continue
			}
		}

		actualTitle, extract, err := ingestors[target.Game].FetchArticle(ctx, target.Page)
		if err != nil {
			log.Printf("[%d/%d] [%s] ERROR: %s: %v\n", i+1, len(allTargets), target.Game, target.Page, err)
			failCount++
			failedPages = append(failedPages, fmt.Sprintf("%s|%s", target.Game, target.Page))
			continue
		}

		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("Failed to create directory %s: %v", dir, err)
		}

		if err := os.WriteFile(filePath, []byte(extract), 0o644); err != nil {
			log.Printf("[%d/%d] [%s] ERROR writing %s: %v\n", i+1, len(allTargets), target.Game, actualTitle, err)
			failCount++
			continue
		}

		fmt.Printf("[%d/%d] [%s] %s (%d chars)\n", i+1, len(allTargets), target.Game, actualTitle, len(extract))
		successCount++

		time.Sleep(500 * time.Millisecond)
	}

	fmt.Printf("\n========================================\n")
	fmt.Printf("  Wiki Download Complete!\n")
	fmt.Printf("  Downloaded: %d\n", successCount)
	fmt.Printf("  Skipped:    %d (already on disk)\n", skipCount)
	fmt.Printf("  Failed:     %d\n", failCount)
	fmt.Printf("========================================\n")

	if len(failedPages) > 0 {
		if err := writeFailedToDisk(failedPages); err != nil {
			fmt.Printf("Failed to save error list: %v\n", err)
		} else {
			fmt.Printf("List of failed pages saved to failed_ingestions.txt\n")
		}
	}
}

// sanitizePath replaces characters that are unsafe in file/directory names.
func sanitizePath(name string) string {
	r := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return r.Replace(name)
}

func runIngest(args []string) {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	gameFlag := fs.String("game", "poe1", "Game to ingest (poe1, poe2, or both)")
	categoriesFile := fs.String("categories", "", "Path to categories review file (default: data/{game}_categories.txt)")
	localDir := fs.String("local", "data/wiki_raw", "Directory with raw wiki extracts from `download` (same layout: {game}/{category}/{page}.txt)")
	fs.Parse(args)

	if err := validateGameFlag(*gameFlag); err != nil {
		log.Fatal(err)
	}
	gamesToRun := gamesFromFlag(*gameFlag)

	if fi, err := os.Stat(*localDir); err != nil || !fi.IsDir() {
		log.Fatalf("FATAL: local wiki mirror not found at %q.\n"+
			"Run from project root: go run ./cmd/rag download -game %s\n"+
			"(%v)", *localDir, *gameFlag, err)
	}

	if err := godotenv.Load(); err != nil {
		log.Println("Note: No .env file found. Relying on system environment variables.")
	}

	cohereKey := os.Getenv("COHERE_API_KEY")
	if cohereKey == "" {
		log.Fatal("FATAL: COHERE_API_KEY is missing. You need this to generate embeddings!")
	}

	dbDsn := os.Getenv("DB_DSN")
	if dbDsn == "" {
		log.Fatal("FATAL: DB_DSN is missing. Cannot connect to PostgreSQL.")
	}

	ctx := context.Background()

	dbPool, err := pgxpool.New(ctx, dbDsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbPool.Close()

	cohereClient := rag.NewCohereClient(cohereKey)
	ragEngine := rag.NewEngine(dbPool, cohereClient)

	allTargets, totalCategories, err := discoverPages(ctx, gamesToRun, *categoriesFile)
	if err != nil {
		log.Fatal(err)
	}
	if len(allTargets) == 0 {
		log.Fatal("No pages discovered from any category. Nothing to ingest.")
	}

	printEstimateSummary(*gameFlag, totalCategories, len(allTargets))
	fmt.Println()

	fmt.Printf("Ingesting from local mirror: %s\n", *localDir)
	fmt.Printf("Ingesting %d wiki pages...\n\n", len(allTargets))

	successCount := 0
	failCount := 0
	var failedPages []string

	for i, target := range allTargets {
		fmt.Printf("[%d/%d] [%s] ", i+1, len(allTargets), target.Game)

		filePath := fmt.Sprintf("%s/%s/%s/%s.txt", *localDir, target.Game, sanitizePath(target.Category), sanitizePath(target.Page))
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			log.Printf("ERROR: %s: %v\n", target.Page, readErr)
			failCount++
			failedPages = append(failedPages, fmt.Sprintf("%s|%s", target.Game, target.Page))
			continue
		}

		var sourceURL string
		if target.Game == "poe2" {
			sourceURL = "https://www.poe2wiki.net/wiki/" + url.PathEscape(target.Page)
		} else {
			sourceURL = "https://www.poewiki.net/wiki/" + url.PathEscape(target.Page)
		}

		if err := ragEngine.IngestDocument(ctx, target.Page, target.Category, target.Game, sourceURL, string(data)); err != nil {
			log.Printf("ERROR: Failed to ingest %s: %v\n", target.Page, err)
			failCount++
			failedPages = append(failedPages, fmt.Sprintf("%s|%s", target.Game, target.Page))
		} else {
			fmt.Printf("OK %s\n", target.Page)
			successCount++
		}
	}

	fmt.Printf("\n========================================\n")
	fmt.Printf("  Wiki Ingestion Complete!\n")
	fmt.Printf("  ✅ Success: %d\n", successCount)
	fmt.Printf("  ❌ Failed:  %d\n", failCount)
	fmt.Printf("========================================\n")

	if len(failedPages) > 0 {
		err := writeFailedToDisk(failedPages)
		if err != nil {
			fmt.Printf("Failed to save error list: %v\n", err)
		} else {
			fmt.Printf("List of failed pages saved to failed_ingestions.txt\n")
		}
	}
}

func validateGameFlag(game string) error {
	if game != "poe1" && game != "poe2" && game != "both" {
		return fmt.Errorf("FATAL: Invalid game. Must be 'poe1', 'poe2', or 'both'.")
	}
	return nil
}

func gamesFromFlag(gameFlag string) []string {
	if gameFlag == "both" {
		return []string{"poe1", "poe2"}
	}
	return []string{gameFlag}
}

// discoverPages walks included categories and lists wiki pages. Pass nil engine via
// NewWikiIngestor(nil, game) — only GetCategoryMembers is used; safe for preview without DB.
func discoverPages(ctx context.Context, gamesToRun []string, categoriesFile string) ([]IngestTarget, int, error) {
	var allTargets []IngestTarget
	totalCategories := 0

	for _, game := range gamesToRun {
		catFile := categoriesFile
		if catFile == "" {
			catFile = fmt.Sprintf("data/%s_categories.txt", game)
		}

		includedCategories, err := parseIncludedCategories(catFile)
		if err != nil {
			return nil, 0, err
		}

		if len(includedCategories) == 0 {
			log.Printf("Warning: No [INCLUDE] categories found in %s.", catFile)
			continue
		}

		totalCategories += len(includedCategories)
		fmt.Printf("\nDiscovering pages from %d included categories in %s (Game: %s)...\n", len(includedCategories), catFile, game)

		wikiIngestor := rag.NewWikiIngestor(nil, game)
		seen := make(map[string]bool)

		for i, cat := range includedCategories {
			fmt.Printf("  [%d/%d] [%s] Category: %s... ", i+1, len(includedCategories), game, cat)
			pages, err := wikiIngestor.GetCategoryMembers(ctx, cat)
			if err != nil {
				log.Printf("ERROR: %v (skipping)\n", err)
				continue
			}

			newCount := 0
			for _, page := range pages {
				if !seen[page] {
					seen[page] = true
					allTargets = append(allTargets, IngestTarget{Game: game, Category: cat, Page: page})
					newCount++
				}
			}
			fmt.Printf("%d pages (%d new)\n", len(pages), newCount)
		}
	}

	return allTargets, totalCategories, nil
}

func printEstimateSummary(gameFlag string, totalCategories, pageCount int) {
	estimatedSeconds := pageCount * 2
	estimatedDuration := time.Duration(estimatedSeconds) * time.Second

	fmt.Printf("\n========================================\n")
	fmt.Printf("  Game(s):    %s\n", gameFlag)
	fmt.Printf("  Categories: %d\n", totalCategories)
	fmt.Printf("  Pages:      %d (deduplicated per game)\n", pageCount)

	estTokens := pageCount * 1000
	estCost := float64(estTokens) / 1_000_000.0 * 0.10

	fmt.Printf("  Est. Tokens:~%d (1k tokens/page assumed)\n", estTokens)
	fmt.Printf("  Est. Cost:  ~$%.4f ($0.10/1M tokens)\n", estCost)
	fmt.Printf("  Est. time:  %s\n", formatDuration(estimatedDuration))
	fmt.Printf("========================================\n")
}

func writeFailedToDisk(pages []string) error {
	f, err := os.Create("failed_ingestions.txt")
	if err != nil {
		return err
	}
	defer f.Close()
	for _, p := range pages {
		fmt.Fprintln(f, p)
	}
	return nil
}

// parseIncludedCategories reads a categories review file and returns
// all category names marked with [INCLUDE].
func parseIncludedCategories(filePath string) ([]string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w\nMake sure you run this from the project root", filePath, err)
	}

	var included []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[INCLUDE]") {
			rest := strings.TrimPrefix(line, "[INCLUDE]")
			rest = strings.TrimSpace(rest)

			if idx := strings.LastIndex(rest, "("); idx > 0 {
				rest = strings.TrimSpace(rest[:idx])
			}

			if rest != "" {
				included = append(included, rest)
			}
		}
	}

	return included, nil
}

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
