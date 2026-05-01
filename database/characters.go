package database

import (
	"context"
	"fmt"
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

// ListByGuildID returns linked characters for a Discord guild (guild_id match).
func (m *CharacterModel) ListByGuildID(guildID string) ([]Character, error) {
	query := `
		SELECT id, user_id, name, game, realm, class, league, level, experience, guild_id, linked_at
		FROM characters
		WHERE guild_id = $1
	`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.Query(ctx, query, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Character
	for rows.Next() {
		var c Character
		err := rows.Scan(
			&c.ID, &c.UserID, &c.Name, &c.Game, &c.Realm, &c.Class, &c.League, &c.Level, &c.Experience,
			&c.GuildID, &c.LinkedAt,
		)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateFromGGG updates mutable character fields from the API (row keyed by id).
func (m *CharacterModel) UpdateFromGGG(c Character) error {
	if c.ID == 0 {
		return fmt.Errorf("UpdateFromGGG: character id required")
	}
	game := c.Game
	if game == "" {
		game = "poe1"
	}
	query := `
		UPDATE characters
		SET realm = $1, class = $2, league = $3, level = $4, experience = $5
		WHERE id = $6 AND game = $7
	`
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tag, err := m.DB.Exec(ctx, query, c.Realm, c.Class, c.League, c.Level, c.Experience, c.ID, game)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("UpdateFromGGG: no row updated for id=%d game=%s", c.ID, game)
	}
	return nil
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
