package database

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UserModel struct {
	DB *pgxpool.Pool
}

type User struct {
	ID              int       `json:"id"`
	DiscordUsername string    `json:"discord_username"`
	GGGAccountName  string    `json:"ggg_account_name"`
	AccessToken     string    `json:"access_token"`
	RefreshToken    string    `json:"refresh_token"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// func (m *UserModel) GetUserByDiscordUsername(discordUsername string) (User, error) {

// 	query := `
// 		SELECT * FROM users
// 		WHERE discord_username = $1
// 	`

// 	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
// 	defer cancel()

// 	var user User
// 	err := m.DB.QueryRow(ctx, query, discordUsername).Scan(&user.ID, &user.DiscordUsername, &user.GGGAccountName, &user.AccessToken, &user.RefreshToken, &user.ExpiresAt)
// 	if err != nil {
// 		return User{}, err
// 	}
// 	return user, nil
// }
