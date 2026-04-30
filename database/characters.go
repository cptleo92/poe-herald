package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CharacterModel struct {
	DB *pgxpool.Pool
}

type Character struct {
	ID         int       `json:"id"`
	UserID     string    `json:"user_id"`
	Name       string    `json:"name"`
	Game       string    `json:"game"`
	Realm      string    `json:"realm"`
	Class      string    `json:"class"`
	League     string    `json:"league"`
	Level      int       `json:"level"`
	Experience int       `json:"experience"`
	GuildID    *string   `json:"guild_id,omitempty"`
	LinkedAt   time.Time `json:"linked_at"`
}

func (m *CharacterModel) InsertCharacter(character Character) error {
	query := `
		INSERT INTO characters (
			user_id, name, game, realm, class, league, level, experience,
			guild_id, linked_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	linkedAt := character.LinkedAt
	if linkedAt.IsZero() {
		linkedAt = time.Now().UTC()
	}

	game := character.Game
	if game == "" {
		game = "poe1"
	}

	args := []any{
		character.UserID, character.Name, game, character.Realm, character.Class, character.League,
		character.Level, character.Experience,
		character.GuildID, linkedAt,
	}

	_, err := m.DB.Exec(ctx, query, args...)
	return err
}

func (m *CharacterModel) GetByUserID(userID string) ([]Character, error) {
	query := `
		SELECT id, user_id, name, game, realm, class, league, level, experience, guild_id, linked_at
		FROM characters
		WHERE user_id = $1
		ORDER BY linked_at ASC
	`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var characters []Character
	for rows.Next() {
		var c Character
		err := rows.Scan(
			&c.ID, &c.UserID, &c.Name, &c.Game, &c.Realm, &c.Class, &c.League, &c.Level, &c.Experience,
			&c.GuildID, &c.LinkedAt,
		)
		if err != nil {
			return nil, err
		}
		characters = append(characters, c)
	}

	return characters, nil
}

func (m *CharacterModel) Delete(userID, name, game string) error {
	if game == "" {
		game = "poe1"
	}
	query := `
		DELETE FROM characters
		WHERE user_id = $1 AND name = $2 AND game = $3
	`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := m.DB.Exec(ctx, query, userID, name, game)
	return err
}
