package database

import "github.com/jackc/pgx/v5/pgxpool"

type Models struct {
	Users UserModel
}

func NewModels(db *pgxpool.Pool) Models {
	return Models{
		Users: UserModel{DB: db},
	}
}
