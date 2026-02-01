package bot

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// newMessage is a handler for new messages
func NewMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	switch {
	case strings.Contains(m.Content, "!help"):
		s.ChannelMessageSend(m.ChannelID, "Hello")
	case strings.Contains(m.Content, "!bye"):
		s.ChannelMessageSend(m.ChannelID, "Good bye?")
	}
}
