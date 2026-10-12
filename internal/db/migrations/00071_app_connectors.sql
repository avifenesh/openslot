-- +goose Up
CREATE TABLE app_integrations (
 provider TEXT PRIMARY KEY CHECK(provider IN ('gmail','notion')),
 generation TEXT NOT NULL,
 credentials_enc TEXT NOT NULL,
 config TEXT NOT NULL DEFAULT '{}',
 connected_by TEXT REFERENCES users(id) ON DELETE SET NULL,
 connected_at TEXT NOT NULL,
 last_success_at TEXT,
 retry_after_at TEXT,
 last_error TEXT NOT NULL DEFAULT ''
);
CREATE TABLE app_oauth_states (
 state_hash TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 verifier TEXT NOT NULL,
 expires_at TEXT NOT NULL
);
CREATE TABLE notion_booking_pages (
 generation TEXT NOT NULL,
 booking_id TEXT NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
 page_id TEXT NOT NULL,
 PRIMARY KEY(generation, booking_id)
);
-- +goose StatementBegin
CREATE TRIGGER notion_booking_insert AFTER INSERT ON bookings
WHEN EXISTS(SELECT 1 FROM app_integrations WHERE provider='notion')
BEGIN
 INSERT INTO jobs(id,type,payload,run_at)
 SELECT lower(hex(randomblob(16))), 'notion.sync',
 json_object('booking_id',NEW.id,'generation',generation,'event',lower(hex(randomblob(16)))),
 strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM app_integrations WHERE provider='notion';
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER notion_booking_update AFTER UPDATE OF start_at,end_at,status,payment_status ON bookings
WHEN EXISTS(SELECT 1 FROM app_integrations WHERE provider='notion')
 AND (NEW.start_at != OLD.start_at OR NEW.end_at != OLD.end_at OR NEW.status != OLD.status OR NEW.payment_status != OLD.payment_status)
BEGIN
 INSERT INTO jobs(id,type,payload,run_at)
 SELECT lower(hex(randomblob(16))), 'notion.sync',
 json_object('booking_id',NEW.id,'generation',generation,'event',lower(hex(randomblob(16)))),
 strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM app_integrations WHERE provider='notion';
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER notion_booking_update;
DROP TRIGGER notion_booking_insert;
DROP TABLE notion_booking_pages;
DROP TABLE app_oauth_states;
DROP TABLE app_integrations;
