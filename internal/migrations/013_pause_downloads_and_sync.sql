-- +goose Up
CREATE TABLE download_queue_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    track_id INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    slskd_search_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'searching', 'downloading', 'organizing', 'complete', 'failed', 'paused')),
    attempts INTEGER NOT NULL DEFAULT 0,
    last_attempt TEXT,
    error TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    next_retry_at TEXT,
    source TEXT NOT NULL DEFAULT 'user',
    last_progress_bytes INTEGER NOT NULL DEFAULT 0
);

INSERT INTO download_queue_new (id, track_id, slskd_search_id, status, attempts, last_attempt, error, created_at, next_retry_at, source, last_progress_bytes)
SELECT id, track_id, slskd_search_id, status, attempts, last_attempt, error, created_at, next_retry_at, source, last_progress_bytes FROM download_queue;

DROP TABLE download_queue;
ALTER TABLE download_queue_new RENAME TO download_queue;

CREATE INDEX idx_download_queue_status ON download_queue(status);
CREATE INDEX idx_download_queue_track_id ON download_queue(track_id);
CREATE UNIQUE INDEX idx_download_queue_track_active ON download_queue(track_id) WHERE status IN ('pending', 'searching', 'downloading', 'organizing', 'paused');

ALTER TABLE artists ADD COLUMN sync_paused INTEGER NOT NULL DEFAULT 0;

INSERT INTO settings (key, value) VALUES ('downloads_paused', 'false')
ON CONFLICT(key) DO NOTHING;

INSERT INTO settings (key, value) VALUES ('sync_discography_paused', 'false')
ON CONFLICT(key) DO NOTHING;

-- +goose Down
-- Non-destructive reversal
