CREATE TABLE IF NOT EXISTS articles (
    id           BIGSERIAL PRIMARY KEY,
    source_key   TEXT        NOT NULL,
    url          TEXT        NOT NULL UNIQUE,
    title        TEXT        NOT NULL DEFAULT '',
    content      TEXT        NOT NULL DEFAULT '',
    author       TEXT        NOT NULL DEFAULT '',
    published_at TIMESTAMPTZ NOT NULL,
    fetched_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_articles_published_at ON articles (published_at DESC);
CREATE INDEX IF NOT EXISTS idx_articles_source_key   ON articles (source_key);
