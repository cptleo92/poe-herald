package main

import (
	"github.com/robfig/cron/v3"
)

func (app *application) initializeCron() *cron.Cron {
	cr := cron.New(cron.WithSeconds())

	// Every day at 9am EST, broadcast a list of linked characters ranked by level and experience (cap at 20)
	// cr.AddFunc("@every 1d 09:00:00", app.broadcastDailyLeaderboard)

	return cr
}

// func discordDisplayName(u *discordgo.User) string {
// 	if u == nil {
// 		return "Unknown"
// 	}
// 	if u.GlobalName != "" {
// 		return u.GlobalName
// 	}
// 	return u.Username
// }

// func (app *application) announceToGuild(guildID *string, message string) {
// 	if guildID == nil || *guildID == "" {
// 		return
// 	}
// 	if app.session == nil {
// 		return
// 	}
// 	cfg, err := app.models.GuildConfigs.GetByID(*guildID)
// 	if err != nil {
// 		return
// 	}
// 	if cfg.ActiveChannelID == "" {
// 		return
// 	}
// 	if _, err := app.session.ChannelMessageSend(cfg.ActiveChannelID, message); err != nil {
// 		log.Printf("announceToGuild ChannelMessageSend: %v", err)
// 	}
// }
