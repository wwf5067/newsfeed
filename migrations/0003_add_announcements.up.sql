-- 顶部公告栏。Stage 1 由开发者直接 SQL 写入,后续再做管理页。
-- 同时支持多条公告,按 priority 降序展示;到期或软关后自动消失。
CREATE TABLE IF NOT EXISTS announcements (
    id         BIGSERIAL   PRIMARY KEY,
    content    TEXT        NOT NULL,                -- 纯文本(Stage 1 不引入 markdown)
    level      TEXT        NOT NULL DEFAULT 'info', -- info | warn | critical
    priority   INT         NOT NULL DEFAULT 0,      -- 数字越大越靠前
    starts_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ends_at    TIMESTAMPTZ NULL,                    -- NULL = 无截止
    is_active  BOOLEAN     NOT NULL DEFAULT TRUE,   -- 软开关,可临时下线
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (level IN ('info', 'warn', 'critical'))
);

-- 部分索引:列表查询永远只看活跃公告,索引体积小、命中率高。
-- 注意 NOW() 不能进 partial index 的 WHERE,所以时间窗口过滤交给 SQL,
-- 索引只过滤 is_active 这个静态条件。
CREATE INDEX IF NOT EXISTS idx_announcements_active
    ON announcements (priority DESC, created_at DESC)
    WHERE is_active = TRUE;
