-- articles 增加抽取标记。已存在的历史文章 extracted_at = NULL,
-- 会被 extractor worker 自然捞起补抽。
ALTER TABLE articles ADD COLUMN extracted_at     TIMESTAMPTZ NULL;
ALTER TABLE articles ADD COLUMN extract_attempts INT         NOT NULL DEFAULT 0;

-- 部分索引:worker 取待处理批次只看这一小撮行,避免全表扫描。
-- 超过 3 次失败的文章不再重试,自然脱离索引。
CREATE INDEX IF NOT EXISTS idx_articles_pending
    ON articles (published_at DESC)
    WHERE extracted_at IS NULL AND extract_attempts < 3;

-- 实体表:人物/机构/地点/作品等
CREATE TABLE IF NOT EXISTS entities (
    id          BIGSERIAL   PRIMARY KEY,
    name        TEXT        NOT NULL,
    type        TEXT        NOT NULL,            -- person|org|location|work|other
    alias       TEXT        NOT NULL DEFAULT '', -- 逗号分隔别名,后续可拆 JSONB
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (name, type)
);
CREATE INDEX IF NOT EXISTS idx_entities_type ON entities (type);

-- 事件表:话题聚合
CREATE TABLE IF NOT EXISTS events (
    id            BIGSERIAL   PRIMARY KEY,
    title         TEXT        NOT NULL,
    fingerprint   TEXT        NOT NULL UNIQUE,   -- sha1(归一化标题)[:16]
    summary       TEXT        NOT NULL DEFAULT '',
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at  TIMESTAMPTZ NOT NULL,
    article_count INT         NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_events_last_seen ON events (last_seen_at DESC);

-- 文章 ↔ 实体 多对多
CREATE TABLE IF NOT EXISTS article_entities (
    article_id BIGINT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    entity_id  BIGINT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    PRIMARY KEY (article_id, entity_id)
);
-- 反查:某实体出现在哪些文章里
CREATE INDEX IF NOT EXISTS idx_ae_entity ON article_entities (entity_id, article_id);

-- 文章 ↔ 事件 多对多
CREATE TABLE IF NOT EXISTS article_events (
    article_id BIGINT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    event_id   BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    PRIMARY KEY (article_id, event_id)
);
-- 反查:某事件包含哪些文章(时间线)
CREATE INDEX IF NOT EXISTS idx_ae2_event ON article_events (event_id, article_id);
