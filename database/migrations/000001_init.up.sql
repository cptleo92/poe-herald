CREATE TABLE IF NOT EXISTS users (
  id SERIAL PRIMARY KEY,
  discord_username VARCHAR(255) NOT NULL,
  ggg_account_name VARCHAR(255) NOT NULL,
  access_token VARCHAR(255) NOT NULL,
  refresh_token VARCHAR(255) NOT NULL,
  expires_at TIMESTAMP NOT NULL
);