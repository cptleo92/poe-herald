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
	"github.com/cptleo92/poe-herald/internal/rag"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

type application struct {
	config    config
	models    database.Models
	session   *discordgo.Session
	ragEngine *rag.Engine
}

type config struct {
	port         int
	env          string
	botToken     string
	dbDSN        string
	clientID     string
	clientSecret string
	redirectURI  string
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

	requiredEnvVars := []string{"BOT_TOKEN", "DB_DSN", "CLIENT_ID", "CLIENT_SECRET", "REDIRECT_URI"}
	for _, envVar := range requiredEnvVars {
		if os.Getenv(envVar) == "" {
			log.Fatalf("Missing required environment variable: %s", envVar)
		}
	}

	cfg.botToken = os.Getenv("BOT_TOKEN")
	cfg.dbDSN = os.Getenv("DB_DSN")
	cfg.clientID = os.Getenv("CLIENT_ID")
	cfg.clientSecret = os.Getenv("CLIENT_SECRET")
	cfg.redirectURI = os.Getenv("REDIRECT_URI")

	// Open postgres connection
	log.Println("Connecting to postgres...")
	dbpool, err := pgxpool.New(context.Background(), cfg.dbDSN)
	if err != nil {
		log.Fatal("Error connecting to postgres: ", err)
	}
	defer dbpool.Close()

	var ragEngine *rag.Engine
	if cohereKey := os.Getenv("COHERE_API_KEY"); cohereKey != "" {
		ragEngine = rag.NewEngine(dbpool, rag.NewCohereClient(cohereKey))
		log.Println("RAG engine initialized (DM wiki Q&A enabled)")
	} else {
		log.Println("COHERE_API_KEY not set; wiki Q&A via DM disabled")
	}

	app := &application{
		config:    cfg,
		models:    database.NewModels(dbpool),
		ragEngine: ragEngine,
	}

	guildConfigUpsert = app.models.GuildConfigs.UpsertGuildConfig
	characterGetByUserID = app.models.Characters.GetByUserID
	userGet = app.models.Users.GetUser
	oauthLinkGenerate = app.generateOAuthAuthorizationLink
	sendCharSelectMenu = app.sendCharacterSelectMenu
	guildConfigGet = app.models.GuildConfigs.GetByID
	componentsHandlers["link-character-select"] = app.handleCharacterSelect
	componentsHandlers["remove-character-select"] = app.handleManualRemoveSelect
	componentsHandlers["swap-character-select"] = app.handleSwapLinkCharSelect

	// Activate bot
	log.Println("Creating new Discord session...")
	s, err := openDiscordSession(cfg.botToken)
	if err != nil {
		log.Fatal("Error opening Discord session: ", err)
	}

	s.Open()
	defer s.Close()

	app.session = s

	// Commands
	s.AddHandler(commandRouter)
	s.AddHandler(app.handleDirectMessage)

	log.Println("Adding commands...")
	var registeredCommands []*discordgo.ApplicationCommand
	for _, v := range CommandHandlers {
		cmd, err := s.ApplicationCommandCreate(s.State.User.ID, "", v.command)
		if err != nil {
			log.Panicf("Cannot create '%v' command: %v", v.command.Name, err)
		}
		registeredCommands = append(registeredCommands, cmd)
	}

	log.Println("Bot running...")

	// Start HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.port),
		Handler:      app.routes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Println("Starting server on port", cfg.port)

	go func() {
		err = srv.ListenAndServe()
		if err != nil {
			log.Fatal("Error starting server: ", err)
		}
	}()

	cr := app.initializeCron()
	cr.Start()

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	// Graceful shutdown
	log.Println("Shutting down...")
	cr.Stop()
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
