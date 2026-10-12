-- +goose Up
CREATE TABLE inbox_settings (
 id INTEGER PRIMARY KEY CHECK(id=1), enabled INTEGER NOT NULL DEFAULT 0,
 address TEXT NOT NULL DEFAULT '', event_slug TEXT NOT NULL DEFAULT '', timezone TEXT NOT NULL DEFAULT 'UTC',
 generation TEXT NOT NULL, started_at INTEGER NOT NULL DEFAULT 0, scanned_at INTEGER NOT NULL DEFAULT 0,
 last_poll TEXT, last_error TEXT NOT NULL DEFAULT '', worker_seen TEXT, worker_engine TEXT NOT NULL DEFAULT ''
);
CREATE TABLE inbox_requests (
 id TEXT PRIMARY KEY, generation TEXT NOT NULL, gmail_generation TEXT NOT NULL DEFAULT '', mailbox TEXT NOT NULL, message_id TEXT NOT NULL,
 email_enc TEXT NOT NULL, preview INTEGER NOT NULL DEFAULT 0,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','sending','sent','previewed','ignored','cancelled','uncertain')),
 lease_hash TEXT NOT NULL DEFAULT '', lease_until INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL, finished_at TEXT, result TEXT NOT NULL DEFAULT '',
 UNIQUE(mailbox,message_id)
);
CREATE INDEX inbox_requests_queue ON inbox_requests(status,created_at);
-- +goose Down
DROP TABLE inbox_requests;
DROP TABLE inbox_settings;
