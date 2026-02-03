CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY NOT NULL,
  discord_username VARCHAR(255) NOT NULL,
  ggg_account_name VARCHAR(255),
  access_token TEXT,
  refresh_token TEXT,
  expires_at TIMESTAMP,
  oauth_state VARCHAR(255)
);