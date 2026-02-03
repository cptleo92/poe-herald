package main

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

type Command struct {
	Command *discordgo.ApplicationCommand
	Handler func(s *discordgo.Session, i *discordgo.InteractionCreate)
}

var Commands = []Command{
	{
		Command: &discordgo.ApplicationCommand{
			Name:        "rips",
			Description: "How many deaths your character has",
		},
		Handler: ripsHandler,
	},
}

func ripsHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Hello, world!",
		},
	})
	if err != nil {
		fmt.Println(err)
	}
}
