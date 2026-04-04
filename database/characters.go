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

func (m *CharacterModel) GetByUserID(userID string) ([]Character, error) {
	query := `
		SELECT id, user_id, name, realm, class, league, level, experience
		FROM characters
		WHERE user_id = $1
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
		err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.Realm, &c.Class, &c.League, &c.Level, &c.Experience)
		if err != nil {
			return nil, err
		}
		characters = append(characters, c)
	}

	return characters, nil
}

func (m *CharacterModel) Delete(userID string, name string) error {
	query := `
		DELETE FROM characters
		WHERE user_id = $1 AND name = $2
	`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := m.DB.Exec(ctx, query, userID, name)
	return err
}
