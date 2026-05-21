CREATE TABLE IF NOT EXISTS heat_blacklist (
    id         SERIAL PRIMARY KEY,
    word       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
