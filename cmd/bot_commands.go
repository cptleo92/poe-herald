package main

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/cptleo92/poe-herald/database"
)

// Simplest way to pass this function from `app` to here
var guildConfigUpsert func(database.GuildConfig) error

type Command struct {
	command *discordgo.ApplicationCommand
	handler func(s *discordgo.Session, i *discordgo.InteractionCreate)
}

// Prevents behavior where invoking a command handles all of them
func commandRouter(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		if command, ok := CommandHandlers[i.ApplicationCommandData().Name]; ok {
			command.handler(s, i)
		}
	case discordgo.InteractionMessageComponent:
		if h, ok := componentsHandlers[i.MessageComponentData().CustomID]; ok {
			h(s, i)
		}
	}
}

var componentsHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
	"set-channel": setChannelComponentHandler,
}

// TODO: permissions
var CommandHandlers = map[string]Command{
	"rips": {
		command: &discordgo.ApplicationCommand{
			Name:                     "rips",
			Description:              "How many deaths your character has",
			DefaultMemberPermissions: nil,
		},
		handler: ripsHandler,
	},
	"set-channel": {
		command: &discordgo.ApplicationCommand{
			Name:        "set-channel",
			Description: "Set a channel for your bot to spam everyone in",
		},
		handler: setChannelHandler,
	},
}

func ripsHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Coming soon!",
		},
	})
	if err != nil {
		log.Println(err)
	}
}

// Displays channel select. Choice gets sent to component handler below
func setChannelHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	log.Println(i.Member.Permissions)

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Select a channel for POE Herald to message in.",
			Flags:   discordgo.MessageFlagsEphemeral,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							MenuType:     discordgo.ChannelSelectMenu,
							CustomID:     "set-channel",
							ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildText},
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Println(err)
	}
}

func setChannelComponentHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if data.CustomID != "set-channel" || len(data.Values) == 0 {
		return
	}

	channelID := data.Values[0]
	channel, ok := data.Resolved.Channels[channelID]
	if !ok {
		sendEphemeralChannelMessage(s, i, "Unable to find channel!")
		return
	}

	gC := database.GuildConfig{
		ID:                channel.GuildID,
		ActiveChannelID:   channelID,
		ActiveChannelName: channel.Name,
	}

	err := guildConfigUpsert(gC)
	if err != nil {
		sendEphemeralChannelMessage(s, i, "Unable to set channel!")
		log.Println(err)
		return
	}

	sendEphemeralChannelMessage(s, i, fmt.Sprintf("Channel `%v` has been set as your active channel.", channel.Name))
}
