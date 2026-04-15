package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/cptleo92/poe-herald/internal/rag"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	query := flag.String("query", "How does Armour work?", "Question to search or answer")
	topK := flag.Int("k", 5, "Number of chunks to print in search-only mode")
	game := flag.String("game", "", "Filter by game: poe1, poe2, or empty for both")
	answer := flag.Bool("answer", false, "Run full RAG pipeline (retrieve, rerank, Command R) instead of raw search")
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		log.Println("Note: No .env file found. Relying on system environment variables.")
	}

	cohereKey := os.Getenv("COHERE_API_KEY")
	if cohereKey == "" {
		log.Fatal("FATAL: COHERE_API_KEY is required")
	}
	dbDsn := os.Getenv("DB_DSN")
	if dbDsn == "" {
		log.Fatal("FATAL: DB_DSN is required")
	}

	if *game != "" && *game != "poe1" && *game != "poe2" {
		log.Fatal("FATAL: -game must be empty, poe1, or poe2")
	}

	ctx := context.Background()
	dbPool, err := pgxpool.New(ctx, dbDsn)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer dbPool.Close()

	client := rag.NewCohereClient(cohereKey)
	engine := rag.NewEngine(dbPool, client)

	if *answer {
		out, err := engine.Answer(ctx, *query, *game)
		if err != nil {
			log.Fatalf("answer: %v", err)
		}
		fmt.Println(out)
		return
	}

	results, err := engine.Search(ctx, *query, *topK, *game)
	if err != nil {
		log.Fatalf("search: %v", err)
	}
	for i, r := range results {
		fmt.Printf("--- #%d similarity=%.4f [%s] %s ---\n", i+1, r.Similarity, r.Game, r.Title)
		fmt.Printf("category: %s\nurl: %s\n\n%s\n\n", r.Category, r.SourceURL, r.Content)
	}
}
