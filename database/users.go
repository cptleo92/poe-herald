package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UserModel struct {
	DB *pgxpool.Pool
}

type User struct {
	ID                string    `json:"id"`
	DiscordUsername   string    `json:"discord_username"`
	GGGAccountName    string    `json:"ggg_account_name"`
	OauthAccessToken  string    `json:"oauth_access_token"`
	OauthRefreshToken string    `json:"oauth_refresh_token"`
	OauthExpiresAt    time.Time `json:"oauth_expires_at"`
}

func (m *UserModel) UpsertUser(user User) error {
	query := `
		INSERT INTO users (id, discord_username) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET discord_username = $2
	`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	args := []any{user.ID, user.DiscordUsername}

	_, err := m.DB.Exec(ctx, query, args...)
	return err
}
