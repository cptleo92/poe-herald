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
	ID         int    `json:"id"`
	UserID     string `json:"user_id"`
	Name       string `json:"name"`
	Realm      string `json:"realm"`
	Class      string `json:"class"`
	League     string `json:"league"`
	Level      int    `json:"level"`
	Experience int    `json:"experience"`
}

func (m *CharacterModel) InsertCharacter(character Character) error {
	query := `
		INSERT INTO characters ( user_id, name, realm, class, league, level, experience) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	args := []any{character.UserID, character.Name, character.Realm, character.Class, character.League, character.Level, character.Experience}

	_, err := m.DB.Exec(ctx, query, args...)
	return err
}
