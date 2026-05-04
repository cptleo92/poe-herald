package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

// PassiveNode matches the structure of a single node in GGG's export
type PassiveNode struct {
	Name           string          `json:"name"`
	Stats          []string        `json:"stats"`
	ReminderText   []string        `json:"reminderText"`
	MasteryEffects []MasteryEffect `json:"masteryEffects"`
}

type MasteryEffect struct {
	Effect       int      `json:"effect"`
	Stats        []string `json:"stats"`
	ReminderText []string `json:"reminderText"`
}

// PassiveTree matches the top-level structure of GGG's export
type PassiveTree struct {
	Nodes map[string]PassiveNode `json:"nodes"`
}

// Parses the raw JSON from https://github.com/grindinggear/skilltree-export/blob/master/data.json
// and saves it to the provided database
func main() {
	_ = godotenv.Load() // Ignore error if .env doesn't exist

	if len(os.Args) < 2 {
		fmt.Println("Usage: go run cmd/tools/seed_passives/main.go <path_to_passives_json>")
		os.Exit(1)
	}

	filePath := os.Args[1]
	dbURL := os.Getenv("DB_DSN")
	if dbURL == "" {
		fmt.Println("Error: DB_DSN not found in .env")
		os.Exit(1)
	}

	// 1. Read and Parse JSON
	fmt.Printf("Reading %s...\n", filePath)
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	var tree PassiveTree
	if err := json.Unmarshal(data, &tree); err != nil {
		fmt.Printf("Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	// 2. Connect to Database
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		fmt.Printf("Error connecting to database: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	// 3. Perform Bulk Insert (Batch for speed)
	fmt.Printf("Parsed %d nodes. Starting seed...\n", len(tree.Nodes))

	tx, err := conn.Begin(ctx)
	if err != nil {
		fmt.Printf("Error starting transaction: %v\n", err)
		os.Exit(1)
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO passive_nodes (id, name, stats, reminder_text)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING;
	`

	count := 0
	for hashStr, node := range tree.Nodes {
		// GGG uses string keys for hashes in JSON
		id, err := strconv.Atoi(hashStr)
		if err != nil {
			// Skip "root" or other non-integer keys
			continue
		}

		// 1. Insert the main node (only if it has stats, avoiding empty mastery parents)
		if len(node.Stats) > 0 {
			reminders := node.ReminderText
			if reminders == nil {
				reminders = []string{}
			}

			_, err = tx.Exec(ctx, query, id, node.Name, node.Stats, reminders)
			if err != nil {
				fmt.Printf("Error inserting node %d: %v\n", id, err)
			} else {
				count++
			}
		}

		// 2. Insert any mastery effects as separate rows
		for _, effect := range node.MasteryEffects {
			reminders := effect.ReminderText
			if reminders == nil {
				reminders = []string{}
			}

			// Masteries will just share the parent node's name
			effectName := node.Name

			_, err = tx.Exec(ctx, query, effect.Effect, effectName, effect.Stats, reminders)
			if err != nil {
				fmt.Printf("Error inserting effect %d: %v\n", effect.Effect, err)
			} else {
				count++
			}
		}

		if count%2000 == 0 && count > 0 {
			fmt.Printf("Progress: %d entries inserted...\n", count)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		fmt.Printf("Error committing transaction: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully seeded %d entries into 'passive_nodes'!\n", count)
}
