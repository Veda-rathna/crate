-- +goose Up
INSERT INTO settings (key, value) VALUES ('max_concurrent_downloads', '3')
ON CONFLICT(key) DO NOTHING;

-- +goose Down
DELETE FROM settings WHERE key = 'max_concurrent_downloads';
