package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/cptleo92/poe-herald/cmd/bot"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	bot.OpenDiscordSession()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}
