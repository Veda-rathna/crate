-- +goose Up
-- Default to highest FLAC quality available when tiers are still the initial default.
UPDATE settings
SET value = '[{"format":"flac","label":"FLAC"}]'
WHERE key = 'quality_tiers'
  AND value = '[{"format":"flac","label":"FLAC"},{"format":"mp3","min_bitrate":320,"label":"MP3 320kbps"},{"format":"mp3","min_bitrate":256,"label":"MP3 256kbps"}]';

INSERT INTO settings (key, value) VALUES ('quality_fallback_enabled', 'false')
ON CONFLICT(key) DO NOTHING;

-- +goose Down
-- Non-destructive setting update.

