package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/cptleo92/poe-herald/database"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

type application struct {
	config config
	models database.Models
}

type config struct {
	port int
	env  string
}

const version = "1.0.0"

func main() {

	// Parse flags
	var cfg config

	flag.IntVar(&cfg.port, "port", 4000, "Port to listen on")
	flag.StringVar(&cfg.env, "env", "development", "Environment (development, production)")
	flag.Parse()

	// Load environment variables (not in prod because it's loaded from /etc/environment)
	if cfg.env != "production" {
		err := godotenv.Load()
		if err != nil {
			log.Fatal("Error loading .env file", err)
		}
	}

	// Open postgres connection
	log.Println("Connecting to postgres...")
	dbpool, err := pgxpool.New(context.Background(), os.Getenv("DB_DSN"))
	if err != nil {
		log.Fatal("Error connecting to postgres: ", err)
	}
	defer dbpool.Close()

	app := &application{
		config: cfg,
		models: database.NewModels(dbpool),
	}

	// Activate bot
	log.Println("Creating new Discord session...")
	s, err := openDiscordSession()
	if err != nil {
		log.Fatal("Error opening Discord session: ", err)
	}

	s.Open()
	defer s.Close()

	s.AddHandler(app.sendOauthLink)
	s.AddHandler(app.linkCharacter)

	log.Println("Adding commands...")
	registeredCommands := make([]*discordgo.ApplicationCommand, len(Commands))
	for i, v := range Commands {
		cmd, err := s.ApplicationCommandCreate(s.State.User.ID, "", v.Command)
		if err != nil {
			log.Panicf("Cannot create '%v' command: %v", v.Command.Name, err)
		}

		// Add handler to bot
		s.AddHandler(v.Handler)

		registeredCommands[i] = cmd
	}

	fmt.Println("Bot running...")

	// Start HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.port),
		Handler:      app.routes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	fmt.Println("Starting server on port", cfg.port)

	go func() {
		err = srv.ListenAndServe()
		if err != nil {
			log.Fatal("Error starting server: ", err)
		}
	}()

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	// Graceful shutdown

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = srv.Shutdown(ctx)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("Error shutting down server: ", err)
	}

	log.Println("Removing commands...")
	for _, v := range registeredCommands {
		err := s.ApplicationCommandDelete(s.State.User.ID, "", v.ID)
		if err != nil {
			log.Printf("Cannot delete '%v' command: %v", v.Name, err)
		}
	}

	log.Println("Bot stopped")
}
