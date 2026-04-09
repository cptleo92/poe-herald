CREATE TABLE IF NOT EXISTS users (
  -- id matches discord user ID
  id TEXT PRIMARY KEY NOT NULL,
  ggg_account_name VARCHAR(255) NOT NULL,
  oauth_access_token TEXT NOT NULL,
  oauth_refresh_token TEXT NOT NULL,
  oauth_expires_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS characters (
  id INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name VARCHAR(255) NOT NULL,
  user_id TEXT NOT NULL,
  game VARCHAR(10) NOT NULL DEFAULT 'poe1',
  realm VARCHAR(255) NOT NULL,
  class VARCHAR(255) NOT NULL,
  league VARCHAR(255) NOT NULL,
  level INT NOT NULL,
  experience BIGINT NOT NULL,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  guild_id TEXT,
  linked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (user_id, name, game)
);

CREATE INDEX IF NOT EXISTS idx_characters_user_id ON characters (user_id);

CREATE OR REPLACE FUNCTION trigger_set_timestamp()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE IF NOT EXISTS guild_configs (
  -- id matches guild ID
  id TEXT PRIMARY KEY NOT NULL,
  active_channel_id BIGINT UNIQUE NOT NULL,
  active_channel_name VARCHAR(255) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE OR REPLACE TRIGGER set_timestamp
BEFORE UPDATE ON guild_configs
FOR EACH ROW
EXECUTE FUNCTION trigger_set_timestamp();

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE game_knowledge (
    id SERIAL PRIMARY KEY,
    title TEXT NOT NULL,     -- The title of the wiki page or youtube video
    category TEXT NOT NULL,  -- e.g., 'wiki-mechanics', 'youtube-zizaran'
    game TEXT NOT NULL DEFAULT 'poe1', -- 'poe1' or 'poe2'
    source_url TEXT,         -- The explicit permalink to the original source
    content TEXT,            -- The actual chunked text content
    embedding vector(1024)   -- Cohere embed-english-v3.0 produces 1024 dimensions
);

CREATE INDEX ON game_knowledge USING hnsw (embedding vector_cosine_ops);

CREATE TABLE IF NOT EXISTS passive_nodes (
    id INT PRIMARY KEY, -- The GGG hash or effect ID
    name TEXT,
    stats JSONB NOT NULL,
    reminder_text JSONB DEFAULT '[]'::jsonb
);
