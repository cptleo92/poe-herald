package bot

import (
	"os"

	"github.com/bwmarrin/discordgo"
)

func OpenDiscordSession() (*discordgo.Session, error) {
	return discordgo.New("Bot " + os.Getenv("BOT_TOKEN"))
}
