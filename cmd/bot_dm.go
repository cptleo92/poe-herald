package main

import (
	"context"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

const discordMessageMax = 1900

func (app *application) handleDirectMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}
	if m.GuildID != "" {
		return
	}
	if app.ragEngine == nil {
		return
	}

	q := strings.TrimSpace(m.Content)
	if q == "" {
		return
	}

	answer, err := app.ragEngine.Answer(context.Background(), q, "")
	if err != nil {
		log.Println("RAG Answer error:", err)
		_, _ = s.ChannelMessageSend(m.ChannelID, "Something went wrong processing your question. Try again later.")
		return
	}

	for _, chunk := range splitDiscordMessage(answer, discordMessageMax) {
		if _, err := s.ChannelMessageSend(m.ChannelID, chunk); err != nil {
			log.Println("DM send error:", err)
			return
		}
	}
}

func splitDiscordMessage(s string, maxRunes int) []string {
	if s == "" {
		return nil
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return []string{s}
	}
	var out []string
	for i := 0; i < len(r); i += maxRunes {
		end := i + maxRunes
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[i:end]))
	}
	return out
}
