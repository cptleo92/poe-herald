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
	GGGAccountName    string    `json:"ggg_account_name"`
	OauthAccessToken  string    `json:"oauth_access_token"`
	OauthRefreshToken string    `json:"oauth_refresh_token"`
	OauthExpiresAt    time.Time `json:"oauth_expires_at"`
}

func (m *UserModel) InsertUser(user User) error {
	query := `
		INSERT INTO users (id, ggg_account_name, oauth_access_token, oauth_refresh_token, oauth_expires_at) VALUES ($1, $2, $3, $4, $5)
	`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	args := []any{user.ID, user.GGGAccountName, user.OauthAccessToken, user.OauthRefreshToken, user.OauthExpiresAt}

	_, err := m.DB.Exec(ctx, query, args...)
	return err
}
