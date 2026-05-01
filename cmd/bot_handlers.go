package main

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/cptleo92/poe-herald/database"
	"github.com/cptleo92/poe-herald/internal/ggg"
)

const (
	maxCharacterOptions = 25
	gggUserAgent        = "OAuth poe-herald/1.0.0 (contact: leo.cheng92@gmail.com)"
)

// existingHasCharacter reports whether the user already has this game+name linked.
func existingHasCharacter(existing []database.Character, game, name string) bool {
	for _, c := range existing {
		g := c.Game
		if g == "" {
			g = ggg.GamePoe1
		}
		if c.Name == name && g == game {
			return true
		}
	}
	return false
}

func guildIDPtrFromInteraction(i *discordgo.InteractionCreate) *string {
	if i.GuildID == "" {
		return nil
	}
	g := i.GuildID
	return &g
}

// sendCharacterSelectMenu fetches characters from the GGG API, filters them,
// and completes the deferred interaction via FollowupMessageCreate (ephemeral select menu).
func (app *application) sendCharacterSelectMenu(s *discordgo.Session, i *discordgo.InteractionCreate, accessToken string) {
	client := ggg.NewClient(accessToken, gggUserAgent)
	characters, err := client.FetchCharacters()
	if err != nil {
		log.Println("Error fetching characters:", err)
		app.handleGGGError(s, i, "", err)
		return
	}

	filtered := ggg.FilterLeagueCharacters(characters, maxCharacterOptions)

	if len(filtered) == 0 {
		sendEphemeralInteractionResponse(s, i, "No active league characters found on your account.")
		return
	}

	options := make([]discordgo.SelectMenuOption, len(filtered))
	for idx, c := range filtered {
		game := c.Game
		if game == "" {
			game = ggg.GamePoe1
		}
		options[idx] = discordgo.SelectMenuOption{
			Label:       fmt.Sprintf("%s (Lv. %d %s)", c.Name, c.Level, c.Class),
			Value:       ggg.GamePrefixedName(game, c.Name),
			Description: c.League,
		}
	}

	_, err = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: "Select a character to link:",
		Flags:   discordgo.MessageFlagsEphemeral,
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
	if err != nil {
		log.Println("Error sending character select follow-up:", err)
	}
}

// handleCharacterSelect is the component handler for when a user picks a character from the dropdown.
func (app *application) handleCharacterSelect(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) == 0 {
		return
	}

	selectedValue := data.Values[0]
	selGame, selName := ggg.ParseGamePrefixedName(selectedValue)

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
		sendEphemeralInteractionResponse(s, i, "Could not find your linked account. Use `/link-account` first.")
		return
	}

	// Fetch existing characters for this user from DB to check limit
	existing, err := app.models.Characters.GetByUserID(userID)
	if err != nil {
		log.Println("Error getting existing characters:", err)
		sendEphemeralInteractionResponse(s, i, "Something went wrong while checking your linked characters! Try again later.")
		return
	}

	if existingHasCharacter(existing, selGame, selName) {
		sendEphemeralInteractionResponse(s, i, "That character is already linked to your account.")
		return
	}

	if len(existing) >= 2 {
		app.sendUnlinkCharacterSelectMenu(s, i, existing, selectedValue)
		return
	}

	// Fetch characters again to get full details for the selected one
	client := ggg.NewClient(user.OauthAccessToken, gggUserAgent)
	characters, err := client.FetchCharacters()
	if err != nil {
		log.Println("Error fetching characters:", err)
		app.handleGGGError(s, i, "", err)
		return
	}

	// Find the selected character
	for _, c := range characters {
		cg := c.Game
		if cg == "" {
			cg = ggg.GamePoe1
		}
		if c.Name == selName && cg == selGame {
			now := time.Now().UTC()
			err = app.models.Characters.InsertCharacter(database.Character{
				UserID:     userID,
				Name:       c.Name,
				Game:       cg,
				Realm:      c.Realm,
				Class:      c.Class,
				League:     c.League,
				Level:      c.Level,
				Experience: c.Experience,
				GuildID:    guildIDPtrFromInteraction(i),
				LinkedAt:   now,
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
			app.announceCharacterLink(s, i, database.Character{
				Name:   c.Name,
				Level:  c.Level,
				League: c.League,
			})
			return
		}
	}
}

// sendUnlinkCharacterSelectMenu sends an ephemeral response asking the user to pick a character to replace.
// newPrefixedName is ggg.GamePrefixedName(game, name) for the character being linked.
func (app *application) sendUnlinkCharacterSelectMenu(s *discordgo.Session, i *discordgo.InteractionCreate, existing []database.Character, newPrefixedName string) {
	_, newPlainName := ggg.ParseGamePrefixedName(newPrefixedName)
	options := make([]discordgo.SelectMenuOption, len(existing))
	for j, c := range existing {
		g := c.Game
		if g == "" {
			g = ggg.GamePoe1
		}
		options[j] = discordgo.SelectMenuOption{
			Label:       fmt.Sprintf("Unlink %s (Lv. %d %s)", c.Name, c.Level, c.Class),
			Value:       ggg.GamePrefixedName(g, c.Name),
			Description: fmt.Sprintf("Replace with %s", newPlainName),
		}
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("You already have 2 characters linked. To link **%s**, you must first unlink one of your current characters:", newPlainName),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							CustomID:    "swap-character-select:" + newPrefixedName,
							Placeholder: "Select a character to replace...",
							Options:     options,
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Println("Error sending unlink menu:", err)
	}
}

// handleManualRemoveSelect handles the simple manual removal of a character via the /remove-character command.
func (app *application) handleManualRemoveSelect(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) == 0 {
		return
	}

	oldPrefixed := data.Values[0]
	oldGame, oldCharName := ggg.ParseGamePrefixedName(oldPrefixed)
	userID := getUserID(i)

	if err := app.performUnlink(s, i, userID, oldGame, oldCharName); err != nil {
		return
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    fmt.Sprintf("✅ **Character removed!**\n**%s** has been unlinked from your account.", oldCharName),
			Components: []discordgo.MessageComponent{},
		},
	})
	if err != nil {
		log.Println("Error sending final removal response:", err)
	}
}

// handleSwapLinkCharSelect handles the unlinking of an old character and automatic linking of a new one.
func (app *application) handleSwapLinkCharSelect(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) == 0 {
		return
	}

	oldPrefixed := data.Values[0]
	oldGame, oldCharName := ggg.ParseGamePrefixedName(oldPrefixed)
	// CustomID: "swap-character-select:" + GamePrefixedName(game, name)
	const swapPrefix = "swap-character-select:"
	if !strings.HasPrefix(data.CustomID, swapPrefix) {
		sendEphemeralInteractionResponse(s, i, "Invalid swap state. Run `/link-character` again.")
		return
	}
	newGame, newCharName := ggg.ParseGamePrefixedName(strings.TrimPrefix(data.CustomID, swapPrefix))

	userID := getUserID(i)

	// 1. Remove the old character
	if err := app.performUnlink(s, i, userID, oldGame, oldCharName); err != nil {
		return
	}

	// 2. Link the new character
	user, err := app.models.Users.GetUser(userID)
	if err != nil {
		log.Println("Error getting user for swap:", err)
		sendEphemeralInteractionResponse(s, i, "Your account link was not found. Use `/link-account` first.")
		return
	}

	client := ggg.NewClient(user.OauthAccessToken, gggUserAgent)
	characters, err := client.FetchCharacters()
	if err != nil {
		log.Println("Error fetching characters for swap:", err)
		app.handleGGGError(s, i, "", err)
		return
	}

	for _, c := range characters {
		cg := c.Game
		if cg == "" {
			cg = ggg.GamePoe1
		}
		if c.Name == newCharName && cg == newGame {
			now := time.Now().UTC()
			err = app.models.Characters.InsertCharacter(database.Character{
				UserID:     userID,
				Name:       c.Name,
				Game:       cg,
				Realm:      c.Realm,
				Class:      c.Class,
				League:     c.League,
				Level:      c.Level,
				Experience: c.Experience,
				GuildID:    guildIDPtrFromInteraction(i),
				LinkedAt:   now,
			})
			if err != nil {
				log.Println("Error inserting character during swap:", err)
				msg := "Removed the old character, but failed to link the new one. Try `/link-character` again."
				if isPGDuplicateError(err) {
					msg = "Removed the old character, but that character is already linked to your account."
				}
				sendEphemeralInteractionResponse(s, i, msg)
				return
			}

			// Respond with swap success
			err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseUpdateMessage,
				Data: &discordgo.InteractionResponseData{
					Content:    fmt.Sprintf("🔄 **Swap complete!**\nUnlinked **%s** and linked **%s** (Lv. %d %s).", oldCharName, c.Name, c.Level, c.Class),
					Components: []discordgo.MessageComponent{},
				},
			})
			if err != nil {
				log.Println("Error sending final swap response:", err)
			}
			app.announceCharacterLink(s, i, database.Character{
				Name:   c.Name,
				Level:  c.Level,
				League: c.League,
			})
			return
		}
	}

	sendEphemeralInteractionResponse(s, i, "The new character was not found. It may have been deleted.")
}

// performUnlink abstracts the database deletion and error reporting for character removal.
func (app *application) performUnlink(s *discordgo.Session, i *discordgo.InteractionCreate, userID, game, charName string) error {
	err := app.models.Characters.Delete(userID, charName, game)
	if err != nil {
		log.Println("Error deleting character in performUnlink:", err)
		sendEphemeralInteractionResponse(s, i, "Something went wrong while removing the character! Try again later.")
		return err
	}
	return nil
}

// announceCharacterLink sends a public message to the guild's active channel when a character is linked.
func (app *application) announceCharacterLink(s *discordgo.Session, i *discordgo.InteractionCreate, char database.Character) {
	if i.GuildID == "" {
		return
	}

	config, err := guildConfigGet(i.GuildID)
	if err != nil || config.ActiveChannelID == "" {
		return
	}

	var username string
	if i.Member != nil {
		username = i.Member.User.Username
	} else if i.User != nil {
		username = i.User.Username
	}

	message := fmt.Sprintf("📢 **%s** linked **%s** - Level %d (%s)!", username, char.Name, char.Level, char.League)
	s.ChannelMessageSend(config.ActiveChannelID, message)
}

// getUserID is a helper to extract the user ID regardless of whether the interaction was in a DM or Server.
func getUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
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
		// If we already responded (e.g. deferred), try following up
		_, err = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		})
		if err != nil {
			log.Println("Error sending interaction response/followup:", err)
		}
	}
}

// handleGGGError provides a unified way to handle and report errors from the GGG API.
func (app *application) handleGGGError(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string, err error) {
	var rateErr *ggg.RateLimitError
	message := "Could not fetch characters from Path of Exile. Your token may have expired — try `/link-account` again."

	if errors.As(err, &rateErr) {
		message = fmt.Sprintf("⚠️ **Rate limited!**\n\nThe Path of Exile API is requesting a cooldown. Please try again in **%v**.", rateErr.RetryAfter.Round(time.Second))
	}

	if i != nil {
		sendEphemeralInteractionResponse(s, i, message)
	} else if channelID != "" {
		s.ChannelMessageSend(channelID, message)
	}
}
