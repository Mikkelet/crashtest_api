CREATE TABLE IF NOT EXISTS intercept_rules (
    id               TEXT PRIMARY KEY,
    api_id           TEXT NOT NULL REFERENCES apis(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    method           TEXT NOT NULL,
    path_pattern     TEXT NOT NULL,
    priority         INTEGER NOT NULL DEFAULT 0,
    status_code      INTEGER NOT NULL,
    response_headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    response_body    TEXT NOT NULL DEFAULT '',
    delay_ms         INTEGER NOT NULL DEFAULT 0,
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS intercept_rules_api_lookup_idx
    ON intercept_rules (api_id, enabled, priority DESC, created_at);
