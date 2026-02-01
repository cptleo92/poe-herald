package bot

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// newMessage is a handler for new messages
func NewMessage(session *discordgo.Session, message *discordgo.MessageCreate) {
	if message.Author.ID == session.State.User.ID {
		return
	}

	switch {
	case strings.Contains(message.Content, "!help"):
		session.ChannelMessageSend(message.ChannelID, "Hello")
	case strings.Contains(message.Content, "!bye"):
		session.ChannelMessageSend(message.ChannelID, "Good bye")
	}
}
