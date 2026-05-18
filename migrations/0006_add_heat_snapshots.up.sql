-- 热度时间序列:每次 UPSERT articles 时多写一行,前端用于画 sparkline。
-- ON DELETE CASCADE 让 retention 删 article 时,snapshot 自动级联清理,无需另起 purge job。
CREATE TABLE article_heat_snapshots (
    article_id  BIGINT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    heat_value  BIGINT NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (article_id, captured_at)
);

-- 反向时间序拉取(最近 N 条)用,主键已是 (article_id, captured_at) 升序
-- 但 sparkline 需要倒序,显式建一个降序索引避免规划器走全表扫
CREATE INDEX idx_heat_snapshots_article_time
    ON article_heat_snapshots (article_id, captured_at DESC);
