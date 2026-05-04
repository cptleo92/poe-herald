package database

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ChatSlashSessionModel struct {
	DB *pgxpool.Pool
}

type ChatSlashSession struct {
	ID            string     `json:"id"`
	DiscordUserID string     `json:"discord_user_id"`
	Question      string     `json:"question"`
	GuildID       *string    `json:"guild_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (m *ChatSlashSessionModel) Upsert(discordUserID, question string, guildID *string, ttl time.Duration) error {
	query := `
		INSERT INTO chat_slash_sessions (id, discord_user_id, question, guild_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (discord_user_id)
		DO UPDATE SET question = EXCLUDED.question, guild_id = EXCLUDED.guild_id, created_at = NOW()
	`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := m.DB.Exec(ctx, `DELETE FROM chat_slash_sessions WHERE created_at < $1`, time.Now().Add(-ttl)); err != nil {
		return err
	}

	_, err := m.DB.Exec(ctx, query, newUUIDString(), discordUserID, question, guildID)
	return err
}

func (m *ChatSlashSessionModel) ConsumeLatest(discordUserID string, cutoff time.Time) (ChatSlashSession, error) {
	query := `
		DELETE FROM chat_slash_sessions
		WHERE id = (
			SELECT id
			FROM chat_slash_sessions
			WHERE discord_user_id = $1 AND created_at >= $2
			ORDER BY created_at DESC
			LIMIT 1
		)
		RETURNING id, discord_user_id, question, guild_id, created_at
	`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var out ChatSlashSession
	err := m.DB.QueryRow(ctx, query, discordUserID, cutoff).Scan(
		&out.ID, &out.DiscordUserID, &out.Question, &out.GuildID, &out.CreatedAt,
	)
	if err != nil {
		return ChatSlashSession{}, err
	}
	return out, nil
}

func newUUIDString() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16],
	)
}
