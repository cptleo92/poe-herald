package database

import "github.com/jackc/pgx/v5/pgxpool"

type Models struct {
	Users        UserModel
	Characters   CharacterModel
	GuildConfigs GuildConfigModel
}

func NewModels(db *pgxpool.Pool) Models {
	return Models{
		Users:        UserModel{DB: db},
		Characters:   CharacterModel{DB: db},
		GuildConfigs: GuildConfigModel{DB: db},
	}
}
