package main

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

func openDiscordSession(botToken string) (*discordgo.Session, error) {
	return discordgo.New("Bot " + botToken)
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
