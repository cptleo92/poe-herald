package main

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

func openDiscordSession(botToken string) (*discordgo.Session, error) {
	s, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, err
	}
	s.Identify.Intents = discordgo.IntentsDirectMessages
	return s, nil
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
