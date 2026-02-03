package main

import (
	"os"

	"github.com/bwmarrin/discordgo"
)

func openDiscordSession() (*discordgo.Session, error) {
	return discordgo.New("Bot " + os.Getenv("BOT_TOKEN"))
}
