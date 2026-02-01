package bot

import (
	"fmt"
	"log"
	"os"

	"github.com/bwmarrin/discordgo"
)

func OpenDiscordSession() *discordgo.Session {
	session, err := discordgo.New("Bot " + os.Getenv("BOT_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}

	session.AddHandler(NewMessage)

	session.Open()
	defer session.Close()

	fmt.Println("Bot running...")

	return session
}
