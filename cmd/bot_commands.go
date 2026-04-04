package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/cptleo92/poe-herald/database"
)

var (
	guildConfigUpsert    func(database.GuildConfig) error
	characterGetByUserID func(string) ([]database.Character, error)
	userGet              func(string) (database.User, error)
	oauthLinkGenerate    func(string, chan bool) (string, string, error)
	sendCharSelectMenu   func(*discordgo.Session, string, string)
	guildConfigGet       func(string) (database.GuildConfig, error)
)

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
		customID := i.MessageComponentData().CustomID
		// 1. Try exact match
		if h, ok := componentsHandlers[customID]; ok {
			h(s, i)
			return
		}

		// 2. Try prefix match for dynamic IDs (e.g. "prefix:args")
		parts := strings.SplitN(customID, ":", 2)
		if len(parts) > 1 {
			if h, ok := componentsHandlers[parts[0]]; ok {
				h(s, i)
				return
			}
		}
	}
}

var componentsHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
	"set-channel": setChannelComponentHandler,
}

// TODO: permissions
var CommandHandlers = map[string]Command{
	"set-channel": {
		command: &discordgo.ApplicationCommand{
			Name:        "set-channel",
			Description: "Set a channel for your bot to spam everyone in",
		},
		handler: setChannelHandler,
	},
	"remove-character": {
		command: &discordgo.ApplicationCommand{
			Name:        "remove-character",
			Description: "Unlink a character from your account",
		},
		handler: removeCharacterHandler,
	},
	"link-account": {
		command: &discordgo.ApplicationCommand{
			Name:        "link-account",
			Description: "Link your Path of Exile account via OAuth",
		},
		handler: linkAccountHandler,
	},
	"link-character": {
		command: &discordgo.ApplicationCommand{
			Name:        "link-character",
			Description: "Link a specific character for tracking",
		},
		handler: linkCharacterHandler,
	},
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

func removeCharacterHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	var userID string
	if i.Member != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}

	chars, err := characterGetByUserID(userID)
	if err != nil {
		log.Println("Error fetching characters for removal:", err)
		sendEphemeralChannelMessage(s, i, "Something went wrong! Try again later.")
		return
	}

	if len(chars) == 0 {
		sendEphemeralChannelMessage(s, i, "You have no linked characters.")
		return
	}

	options := make([]discordgo.SelectMenuOption, len(chars))
	for idx, c := range chars {
		options[idx] = discordgo.SelectMenuOption{
			Label:       fmt.Sprintf("%s (Lv. %d %s)", c.Name, c.Level, c.Class),
			Value:       c.Name,
			Description: c.League,
		}
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Select a character to remove from your account:",
			Flags:   discordgo.MessageFlagsEphemeral,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							CustomID:    "remove-character-select",
							Placeholder: "Choose a character...",
							Options:     options,
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Println("Error sending remove-character menu:", err)
	}
}

func linkAccountHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	var userID string
	var mention string
	if i.Member != nil {
		userID = i.Member.User.ID
		mention = i.Member.User.Mention()
	} else if i.User != nil {
		userID = i.User.ID
		mention = i.User.Mention()
	}

	// Check if already linked
	_, err := userGet(userID)
	if err == nil {
		sendEphemeralChannelMessage(s, i, "You are already linked to an account. Use `/link-character` to link a character.")
		return
	}

	successChannel := make(chan bool, 1)
	state, link, err := oauthLinkGenerate(userID, successChannel)
	if err != nil {
		log.Println("Error generating OAuth link:", err)
		sendEphemeralChannelMessage(s, i, "Something went wrong! Try again later.")
		return
	}
	_ = state

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("Click the link below to link your Path of Exile account:\n\n%s", link),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Println("Error responding to link-account:", err)
		return
	}

	// Handle OAuth flow in background
	go func() {
		linked := <-successChannel
		if linked {
			channel, err := s.UserChannelCreate(userID)
			if err != nil {
				log.Println("Error creating DM channel for link success:", err)
				return
			}
			s.ChannelMessageSend(channel.ID, fmt.Sprintf("✅ %s, your Path of Exile account has been successfully linked! Use `/link-character` to track a character.", mention))
		}
	}()
}

func linkCharacterHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := getUserID(i)

	user, err := userGet(userID)
	if err != nil {
		sendEphemeralChannelMessage(s, i, "You haven't linked your account yet. Use `/link-account` first.")
		return
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Println(err)
		return
	}

	sendCharSelectMenu(s, i.ChannelID, user.OauthAccessToken)
}
