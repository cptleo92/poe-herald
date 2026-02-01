package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/cptleo92/poe-herald/cmd/bot"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	log.Println("Creating new Discord session...")
	s, err := discordgo.New("Bot " + os.Getenv("BOT_TOKEN"))
	if err != nil {
		log.Fatal(err)
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

	log.Println("Removing commands...")
	for _, v := range registeredCommands {
		err := s.ApplicationCommandDelete(s.State.User.ID, "", v.ID)
		if err != nil {
			log.Panicf("Cannot delete '%v' command: %v", v.Name, err)
		}
	}

	log.Println("Bot stopped")
}
