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

func (m *UserModel) GetUser(id string) (User, error) {
	query := `
		SELECT * FROM users WHERE id = $1
	`

	var user User
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	args := []any{id}

	err := m.DB.QueryRow(ctx, query, args...).Scan(&user.ID, &user.GGGAccountName, &user.OauthAccessToken, &user.OauthRefreshToken, &user.OauthExpiresAt)
	if err != nil {
		return User{}, err
	}

	return user, nil
}

func (m *UserModel) GetCharacters(id string) ([]Character, error) {
	query := `
		SELECT c.id, c.user_id, c.name, c.game, c.realm, c.class, c.league, c.level, c.experience,
		       c.guild_id, c.linked_at
		FROM characters c
		INNER JOIN users u ON c.user_id = u.id
		WHERE u.id = $1
	`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	args := []any{id}

	rows, err := m.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	characters := []Character{}
	for rows.Next() {
		var character Character
		err = rows.Scan(
			&character.ID, &character.UserID, &character.Name, &character.Game, &character.Realm, &character.Class, &character.League,
			&character.Level, &character.Experience, &character.GuildID, &character.LinkedAt,
		)
		if err != nil {
			return nil, err
		}
		characters = append(characters, character)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return characters, nil
}
