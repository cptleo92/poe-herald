package main

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/cptleo92/poe-herald/database"
	"github.com/cptleo92/poe-herald/internal/ggg"
	"github.com/robfig/cron/v3"
)

const dailyLeaderboardMax = 20

// maxRateLimitRetries is how many times we re-sleep and retry after GGG RateLimitError per character.
const maxRateLimitRetries = 5

const dailyLeaderboardTime = "0 0 9 * * *" // 9:00 AM Eastern

func (app *application) initializeCron() *cron.Cron {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		log.Fatalf("cron location: %v", err)
	}
	cr := cron.New(cron.WithSeconds(), cron.WithLocation(loc))

	// Every day at 9:00 US Eastern, per-guild leaderboard (top 20 by level then experience).
	if _, err := cr.AddFunc(dailyLeaderboardTime, app.broadcastDailyLeaderboard); err != nil {
		log.Fatalf("cron add daily leaderboard: %v", err)
	}

	return cr
}

func fetchCharacterWithRetry(client *ggg.Client, name, game string) (*ggg.APICharacter, error) {
	var rlHits int
	for {
		c, err := client.FetchCharacter(name, game)
		if err == nil {
			return c, nil
		}
		var rl *ggg.RateLimitError
		if errors.As(err, &rl) && rlHits < maxRateLimitRetries {
			rlHits++
			time.Sleep(rl.RetryAfter)
			continue
		}
		return nil, err
	}
}

func (app *application) broadcastDailyLeaderboard() {
	if app.session == nil {
		log.Println("broadcastDailyLeaderboard: no Discord session")
		return
	}

	guildIDs, err := app.models.GuildConfigs.ListIDs()
	if err != nil {
		log.Printf("broadcastDailyLeaderboard ListIDs: %v", err)
		return
	}

	for _, guildID := range guildIDs {
		app.broadcastDailyLeaderboardForGuild(guildID)
	}
}

func (app *application) broadcastDailyLeaderboardForGuild(guildID string) {
	cfg, err := app.models.GuildConfigs.GetByID(guildID)
	if err != nil || cfg.ActiveChannelID == "" {
		return
	}

	chars, err := app.models.Characters.ListByGuildID(guildID)
	if err != nil {
		log.Printf("broadcastDailyLeaderboard ListByGuildID guild=%s: %v", guildID, err)
		return
	}
	if len(chars) == 0 {
		return
	}

	refreshed := make([]database.Character, 0, len(chars))
	for _, ch := range chars {
		game := ch.Game
		if game == "" {
			game = ggg.GamePoe1
		}

		user, err := app.models.Users.GetUser(ch.UserID)
		if err != nil {
			log.Printf("broadcastDailyLeaderboard GetUser user=%s char=%s: %v", ch.UserID, ch.Name, err)
			continue
		}

		client := ggg.NewClient(user.OauthAccessToken, gggUserAgent)
		apiChar, err := fetchCharacterWithRetry(client, ch.Name, game)
		if err != nil {
			log.Printf("broadcastDailyLeaderboard FetchCharacter guild=%s char=%s: %v", guildID, ch.Name, err)
			continue
		}

		updated := ch
		updated.Realm = apiChar.Realm
		updated.Class = apiChar.Class
		updated.League = apiChar.League
		updated.Level = apiChar.Level
		updated.Experience = apiChar.Experience

		if err := app.models.Characters.UpdateFromGGG(updated); err != nil {
			log.Printf("broadcastDailyLeaderboard UpdateFromGGG id=%d: %v", ch.ID, err)
			continue
		}
		refreshed = append(refreshed, updated)
	}

	if len(refreshed) == 0 {
		return
	}

	slices.SortFunc(refreshed, func(a, b database.Character) int {
		if a.Level != b.Level {
			return b.Level - a.Level
		}
		if a.Experience != b.Experience {
			return b.Experience - a.Experience
		}
		return strings.Compare(a.Name, b.Name)
	})

	n := len(refreshed)
	if n > dailyLeaderboardMax {
		n = dailyLeaderboardMax
	}
	top := refreshed[:n]

	var b strings.Builder
	b.WriteString("🏆 **Daily leaderboard** — top characters by level\n")
	for i := range top {
		c := top[i]
		g := c.Game
		if g == "" {
			g = ggg.GamePoe1
		}
		gameTag := ""
		if g == ggg.GamePoe2 {
			gameTag = " (PoE2)"
		}
		fmt.Fprintf(&b, "%d. **%s**%s — Lv.%d %s — %s\n", i+1, c.Name, gameTag, c.Level, c.Class, c.League)
	}

	if _, err := app.session.ChannelMessageSend(cfg.ActiveChannelID, strings.TrimSuffix(b.String(), "\n")); err != nil {
		log.Printf("broadcastDailyLeaderboard ChannelMessageSend guild=%s: %v", guildID, err)
	}
}
