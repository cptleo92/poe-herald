package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type GuildConfigModel struct {
	DB *pgxpool.Pool
}

type GuildConfig struct {
	ID                string `json:"id"`
	ActiveChannelID   string `json:"active_channel_id"`
	ActiveChannelName string `json:"active_channel_name"`
}

func (m *GuildConfigModel) UpsertGuildConfig(gc GuildConfig) error {
	query := `
		INSERT INTO guild_configs (id, active_channel_id, active_channel_name)
		VALUES ($1, $2, $3)
		ON CONFLICT(id)
		DO UPDATE SET 
			active_channel_id = EXCLUDED.active_channel_id,
			active_channel_name = EXCLUDED.active_channel_name;
	`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	args := []any{gc.ID, gc.ActiveChannelID, gc.ActiveChannelName}

	_, err := m.DB.Exec(ctx, query, args...)
	return err
}
