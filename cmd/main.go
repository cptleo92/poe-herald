package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/cptleo92/poe-herald/cmd/bot"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file", err)
	}

	// Open postgres connection
	log.Println("Connecting to postgres...")
	dbpool, err := pgxpool.New(context.Background(), os.Getenv("dsn"))
	if err != nil {
		log.Fatal("Error connecting to postgres: ", err)
	}
	defer dbpool.Close()

	// Activate bot
	log.Println("Creating new Discord session...")
	s, err := bot.OpenDiscordSession()
	if err != nil {
		log.Fatal("Error opening Discord session: ", err)
	}

	s.Open()
	defer s.Close()

	s.AddHandler(bot.NewMessage)
	for _, v := range bot.Commands {
		s.AddHandler(v.Handler)
	}

	log.Println("Adding commands...")
	registeredCommands := make([]*discordgo.ApplicationCommand, len(bot.Commands))
	for i, v := range bot.Commands {
		cmd, err := s.ApplicationCommandCreate(s.State.User.ID, "", v.Command)
		if err != nil {
			log.Panicf("Cannot create '%v' command: %v", v.Command.Name, err)
		}
		registeredCommands[i] = cmd
	}

	fmt.Println("Bot running...")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	// Cleanup

	log.Println("Removing commands...")
	for _, v := range registeredCommands {
		err := s.ApplicationCommandDelete(s.State.User.ID, "", v.ID)
		if err != nil {
			log.Panicf("Cannot delete '%v' command: %v", v.Name, err)
		}
	}

	log.Println("Bot stopped")
}
