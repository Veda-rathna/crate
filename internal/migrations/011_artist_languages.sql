-- +goose Up
ALTER TABLE artists ADD COLUMN languages TEXT DEFAULT '[]';

-- +goose Down
ALTER TABLE artists DROP COLUMN languages;

