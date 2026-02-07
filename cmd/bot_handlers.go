package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/cptleo92/poe-herald/database"
	"github.com/jackc/pgx/v5"
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
		log.Println("Error creating channel:", err)
		s.ChannelMessageSend(
			m.ChannelID,
			"Something went wrong while sending the DM!",
		)
		return
	}

	/*
	 Check if user is already linked before everything else.
	 If no error, user is linked and we should return.
	 If error is pgx.ErrNoRows, user is not linked and we should continue with linking process.
	 If error is something else, we should return an error message.
	*/

	_, err = app.models.Users.GetUser(m.Author.ID)
	if err == nil {
		s.ChannelMessageSend(channel.ID, "You are already linked to an account. Use `!char <character name>` to link a character.")
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		log.Println("Error getting user:", err)
		s.ChannelMessageSend(channel.ID, "Something went wrong while checking if you are linked! Try again later.")
		return
	}

	message := fmt.Sprintf("Hello %s, click the link below to link your Path of Exile account to your Discord account.", m.Author.Mention())

	s.ChannelMessageSend(channel.ID, message)

	// Listens for OAuth success
	successChannel := make(chan bool, 1)

	state, link, err := generateOAuthAuthorizationLink(m.Author.ID, successChannel)
	if err != nil {
		log.Println("Error generating OAuth link:", err)
		s.ChannelMessageSend(channel.ID, "Something went wrong while generating the OAuth link! Try again later.")
		return
	}

	s.ChannelMessageSend(channel.ID, link)

	// Wait for OAuth success
	select {
	case linked := <-successChannel:
		if !linked {
			s.ChannelMessageSend(channel.ID, "Something went wrong while linking your account! Try again later.")
			return
		} else {
			s.ChannelMessageSend(channel.ID, "Your account has been linked successfully! You maybe now link characters by typing `!char <character name>`.\nExample: `!char TommyWiseOak`")
		}
	case <-time.After(10 * time.Minute):
		s.ChannelMessageSend(channel.ID, "Link expired. Use `!link` again if you still want to link.")
		OauthMutex.Lock()
		delete(OauthMap, state)
		OauthMutex.Unlock()
		return
	}
}

func (app *application) linkCharacter(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	if !strings.Contains(m.Content, "!char") {
		return
	}

	characterName := strings.TrimSpace(strings.TrimPrefix(m.Content, "!char"))
	if characterName == "" {
		s.ChannelMessageSend(m.ChannelID, "Please provide a character name.")
		return
	}

	// Fetch list of characters
	// TODO: use real API instead of mock data. See if we need account info
	var characters struct {
		Characters []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Realm      string `json:"realm"`
			Class      string `json:"class"`
			League     string `json:"league"`
			Level      int    `json:"level"`
			Experience int    `json:"experience"`
		} `json:"characters"`
	}

	file, err := os.Open("internal/mocks/characters.json")
	if err != nil {
		log.Println("Error opening characters file:", err)
		s.ChannelMessageSend(m.ChannelID, "Something went wrong while fetching the characters! Try again later.")
		return
	}
	defer file.Close()

	err = json.NewDecoder(file).Decode(&characters)
	if err != nil {
		log.Println("Error decoding characters:", err)
		s.ChannelMessageSend(m.ChannelID, "Something went wrong while fetching the characters! Try again later.")
		return
	}

	// Find character by name
	for _, character := range characters.Characters {
		if character.Name == characterName {

			// Add character to DB
			err = app.models.Characters.InsertCharacter(database.Character{
				UserID:     m.Author.ID,
				Name:       character.Name,
				Realm:      character.Realm,
				Class:      character.Class,
				League:     character.League,
				Level:      character.Level,
				Experience: character.Experience,
			})
			if err != nil {
				if isPGDuplicateError(err) {
					s.ChannelMessageSend(m.ChannelID, "Character already linked to your account.")
					return
				}
				log.Println("Error inserting character:", err)
				s.ChannelMessageSend(m.ChannelID, "Something went wrong while linking the character! Try again later.")
				return
			}

			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Character linked!\nName: %s\nClass: %s\nLeague: %s\nLevel: %d", character.Name, character.Class, character.League, character.Level))
			return
		}
	}

	s.ChannelMessageSend(m.ChannelID, "Character not found.")
}
