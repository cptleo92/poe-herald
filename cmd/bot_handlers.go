package main

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/cptleo92/poe-herald/database"
)

// SendOauthLink responds to "!link" with a link to the GGG OAuth page
func (app *application) sendOauthLink(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content != "!link" {
		return
	}

	channel, err := s.UserChannelCreate(m.Author.ID)
	if err != nil {
		fmt.Println("Error creating channel:", err)
		s.ChannelMessageSend(
			m.ChannelID,
			"Something went wrong while sending the DM!",
		)
		return
	}

	message := fmt.Sprintf("Hello %s, click the link below to link your Path of Exile account to your Discord account.", m.Author.Mention())

	s.ChannelMessageSend(channel.ID, message)

	link, state, err := generateOAuthLink()

	if err != nil {
		fmt.Println("Error generating OAuth link:", err)
		s.ChannelMessageSend(channel.ID, "Something went wrong while generating the OAuth link! Try again later.")
		return
	}

	// Upsert the user into the database
	user := database.User{
		ID:              m.Author.ID,
		DiscordUsername: m.Author.Username,
		OauthState:      state,
	}

	err = app.models.Users.UpsertUser(user)
	if err != nil {
		fmt.Println("Error upserting user:", err)
		s.ChannelMessageSend(channel.ID, "Something went wrong while linking your account!")
		return
	}

	s.ChannelMessageSend(channel.ID, link)
}

func (app *application) whoAmI(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content != "!whoami" {
		return
	}

	channel, err := s.UserChannelCreate(m.Author.ID)
	if err != nil {
		fmt.Println("Error creating channel:", err)
		s.ChannelMessageSend(
			m.ChannelID,
			"Something went wrong while sending the DM!",
		)
		return
	}

	msg := fmt.Sprintf("Your id is: %s, your discord username is: %s", m.Author.ID, m.Author.Username)

	s.ChannelMessageSend(channel.ID, msg)

}
