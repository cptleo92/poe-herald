package database

import "time"

type User struct {
	ID              int       `json:"id"`
	DiscordUsername string    `json:"discord_username"`
	GGGAccountName  string    `json:"ggg_account_name"`
	AccessToken     string    `json:"access_token"`
	RefreshToken    string    `json:"refresh_token"`
	ExpiresAt       time.Time `json:"expires_at"`
}
