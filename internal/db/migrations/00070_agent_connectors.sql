-- +goose Up
ALTER TABLE api_keys ADD COLUMN connector_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE api_keys DROP COLUMN connector_id;
