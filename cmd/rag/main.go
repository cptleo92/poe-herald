package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/cptleo92/poe-herald/internal/rag"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	// First, load our local environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("Note: No .env file found. Relying on system environment variables.")
	}

	// Make sure we have the required API key for embeddings
	cohereKey := os.Getenv("COHERE_API_KEY")
	if cohereKey == "" {
		log.Fatal("FATAL: COHERE_API_KEY is missing. You need this to generate embeddings!")
	}

	dbDsn := os.Getenv("DB_DSN")
	if dbDsn == "" {
		log.Fatal("FATAL: DB_DSN is missing. Cannot connect to PostgreSQL.")
	}

	ctx := context.Background()

	// Connect to our Database using pgxpool (the postgres driver)
	dbPool, err := pgxpool.New(ctx, dbDsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbPool.Close()

	// Initialize our RAG pipeline
	embedClient := rag.NewEmbedClient(cohereKey)
	ragEngine := rag.NewEngine(dbPool, embedClient)
	// TODO: support poe2 pipeline
	wikiIngestor := rag.NewWikiIngestor(ragEngine, "poe1")

	// Read the list of pages from our text file
	pagesData, err := os.ReadFile("data/wiki_pages.txt")
	if err != nil {
		log.Fatalf("Failed to read wiki_pages.txt: %v\nMake sure you run this from the project root.", err)
	}

	var keyTopics []string
	for line := range strings.SplitSeq(string(pagesData), "\n") {
		cleanLine := strings.TrimSpace(line)
		if cleanLine != "" && !strings.HasPrefix(cleanLine, "#") {
			keyTopics = append(keyTopics, cleanLine)
		}
	}

	fmt.Printf("Started Poe Herald RAG Ingestion Pipeline\n")
	fmt.Printf("Targeting %d Wiki pages...\n", len(keyTopics))

	for _, topic := range keyTopics {
		err := wikiIngestor.IngestArticle(ctx, topic)
		if err != nil {
			log.Printf("ERROR: Failed to ingest %s: %v\n", topic, err)
		} else {
			fmt.Printf("✅ Successfully ingested: %s\n", topic)
		}

		// A small delay to respect Wiki and Cohere Trial rate limits.
		// Without this, we can hit 429 Too Many Requests if we hammer them too fast.
		time.Sleep(time.Second)
	}

	fmt.Println("\nWiki Ingestion Complete! You can test searches next.")
}
