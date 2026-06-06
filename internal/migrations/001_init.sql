CREATE TABLE IF NOT EXISTS apis (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    base_url     TEXT NOT NULL,
    description  TEXT,
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS apis_enabled_idx ON apis (enabled);
