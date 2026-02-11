package main

import (
	"log"
	"os"

	"github.com/bwmarrin/discordgo"
)

func openDiscordSession() (*discordgo.Session, error) {
	return discordgo.New("Bot " + os.Getenv("BOT_TOKEN"))
}

func sendEphemeralChannelMessage(s *discordgo.Session, i *discordgo.InteractionCreate, errMessage string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: errMessage,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})

	if err != nil {
		log.Println(err)
	}
}
