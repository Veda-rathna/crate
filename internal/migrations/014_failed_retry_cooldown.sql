-- +goose Up
INSERT INTO settings (key, value) VALUES ('retry_delay_hours', '1')
ON CONFLICT(key) DO NOTHING;

INSERT INTO settings (key, value) VALUES ('requeue_cooldown_hours', '1')
ON CONFLICT(key) DO NOTHING;

-- Schedule any currently failed downloads to retry in 1 hour
UPDATE download_queue
SET next_retry_at = strftime('%Y-%m-%dT%H:%M:%SZ', datetime('now', '+1 hour'))
WHERE status = 'failed' AND (next_retry_at IS NULL OR next_retry_at = '');

-- +goose Down
DELETE FROM settings WHERE key IN ('retry_delay_hours', 'requeue_cooldown_hours');
