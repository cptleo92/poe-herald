package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/cptleo92/poe-herald/database"
	"github.com/cptleo92/poe-herald/internal/ggg"
	"github.com/jackc/pgx/v5"
)

const (
	maxCharacterOptions = 25
	gggUserAgent        = "OAuth poe-herald/1.0.0 (contact: leo.cheng92@gmail.com)"
)

// sendOauthLink responds to "!link" with a link to the GGG OAuth page.
// After successful OAuth, it fetches the user's characters and presents a dropdown.
func (app *application) sendOauthLink(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID || m.Content != "!link" || m.GuildID != "" {
		return
	}

	// Global cap to prevent abuse
	// TODO: limit links to one per user
	if len(OauthMap) > 100 {
		s.ChannelMessageSend(
			m.ChannelID,
			"Too many active links at the moment! Please try again later.",
		)
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
		s.ChannelMessageSend(channel.ID, "You are already linked to an account. Use `!char` to link a character.")
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

	state, link, err := app.generateOAuthAuthorizationLink(m.Author.ID, successChannel)
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
		}

		s.ChannelMessageSend(channel.ID, "Your account has been linked successfully! Fetching your characters...")

		// Fetch user to get access token
		user, err := app.models.Users.GetUser(m.Author.ID)
		if err != nil {
			log.Println("Error getting user after link:", err)
			s.ChannelMessageSend(channel.ID, "Account linked, but could not fetch your characters. Use `!char` to try again.")
			return
		}

		app.sendCharacterSelectMenu(s, channel.ID, user.OauthAccessToken)

	case <-time.After(30 * time.Minute):
		s.ChannelMessageSend(channel.ID, "Link expired. Use `!link` again if you still want to link.")
		OauthMutex.Lock()
		delete(OauthMap, state)
		OauthMutex.Unlock()
		return
	}
}

// linkCharacter responds to "!char" by fetching the user's characters from the GGG API
// and presenting a dropdown select menu.
func (app *application) linkCharacter(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID || m.Content != "!char" || m.GuildID != "" {
		return
	}

	user, err := app.models.Users.GetUser(m.Author.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.ChannelMessageSend(m.ChannelID, "You haven't linked your account yet. Use `!link` first.")
		} else {
			log.Println("Error getting user:", err)
			s.ChannelMessageSend(m.ChannelID, "Something went wrong! Try again later.")
		}
		return
	}

	app.sendCharacterSelectMenu(s, m.ChannelID, user.OauthAccessToken)
}

// sendCharacterSelectMenu fetches characters from the GGG API, filters them,
// and sends a Discord select menu to the given channel.
func (app *application) sendCharacterSelectMenu(s *discordgo.Session, channelID string, accessToken string) {
	client := ggg.NewClient(accessToken, gggUserAgent)
	characters, err := client.FetchCharacters()
	if err != nil {
		log.Println("Error fetching characters:", err)
		s.ChannelMessageSend(channelID, "Could not fetch characters from Path of Exile. Your token may have expired — try `!link` again.")
		return
	}

	filtered := ggg.FilterLeagueCharacters(characters, maxCharacterOptions)

	if len(filtered) == 0 {
		s.ChannelMessageSend(channelID, "No active league characters found on your account.")
		return
	}

	// Build select menu options
	options := make([]discordgo.SelectMenuOption, len(filtered))
	for i, c := range filtered {
		options[i] = discordgo.SelectMenuOption{
			Label:       fmt.Sprintf("%s (Lv. %d %s)", c.Name, c.Level, c.Class),
			Value:       c.Name,
			Description: c.League,
		}
	}

	s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: "Select a character to link:",
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.SelectMenu{
						CustomID:    "link-character-select",
						Placeholder: "Choose a character...",
						Options:     options,
					},
				},
			},
		},
	})
}

// handleCharacterSelect is the component handler for when a user picks a character from the dropdown.
func (app *application) handleCharacterSelect(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) == 0 {
		return
	}

	selectedName := data.Values[0]

	// Determine the user ID — works in both DMs and guilds
	var userID string
	if i.Member != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}

	user, err := app.models.Users.GetUser(userID)
	if err != nil {
		log.Println("Error getting user for character select:", err)
		sendEphemeralInteractionResponse(s, i, "Could not find your linked account. Use `!link` first.")
		return
	}

	// Fetch characters again to get full details for the selected one
	client := ggg.NewClient(user.OauthAccessToken, gggUserAgent)
	characters, err := client.FetchCharacters()
	if err != nil {
		log.Println("Error fetching characters:", err)
		sendEphemeralInteractionResponse(s, i, "Could not fetch characters. Your token may have expired — try `!link` again.")
		return
	}

	// Find the selected character
	for _, c := range characters {
		if c.Name == selectedName {
			err = app.models.Characters.InsertCharacter(database.Character{
				UserID:     userID,
				Name:       c.Name,
				Realm:      c.Realm,
				Class:      c.Class,
				League:     c.League,
				Level:      c.Level,
				Experience: c.Experience,
			})
			if err != nil {
				if isPGDuplicateError(err) {
					sendEphemeralInteractionResponse(s, i, "That character is already linked to your account.")
					return
				}
				log.Println("Error inserting character:", err)
				sendEphemeralInteractionResponse(s, i, "Something went wrong while linking the character! Try again later.")
				return
			}

			sendEphemeralInteractionResponse(s, i, fmt.Sprintf("✅ Character linked!\n**%s** — Level %d %s (%s)", c.Name, c.Level, c.Class, c.League))
			return
		}
	}

	sendEphemeralInteractionResponse(s, i, "Character not found. It may have been deleted or renamed.")
}

// sendEphemeralInteractionResponse sends an ephemeral response to an interaction (component handler).
func sendEphemeralInteractionResponse(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Println("Error sending interaction response:", err)
	}
}
