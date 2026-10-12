-- +goose Up
ALTER TABLE server_settings ADD COLUMN google_client_type TEXT NOT NULL DEFAULT 'web' CHECK(google_client_type IN ('web','desktop'));
CREATE TABLE google_oauth_states (
 state_hash TEXT PRIMARY KEY,
 purpose TEXT NOT NULL CHECK(purpose IN ('login','calendar')),
 user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
 verifier TEXT NOT NULL,
 expires_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE google_oauth_states;
ALTER TABLE server_settings DROP COLUMN google_client_type;
