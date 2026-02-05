CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY NOT NULL,
  ggg_account_name VARCHAR(255) NOT NULL,
  oauth_access_token TEXT NOT NULL,
  oauth_refresh_token TEXT NOT NULL,
  oauth_expires_at TIMESTAMP NOT NULL
);