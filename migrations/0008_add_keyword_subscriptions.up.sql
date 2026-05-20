-- 邮件订阅:用户配置一组关键词,crawler 抓到新文章命中关键词时发邮件。
-- 收件邮箱使用 .env 里的 DIGEST_TO,不存在表里(单用户场景,避免多租户复杂度)。

CREATE TABLE keyword_subscriptions (
    id         BIGSERIAL PRIMARY KEY,
    keyword    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 关键词大小写无关,LOWER 唯一索引避免 "AI" 和 "ai" 重复订阅
    CONSTRAINT keyword_not_empty CHECK (length(trim(keyword)) > 0)
);

CREATE UNIQUE INDEX idx_keyword_subscriptions_lower
    ON keyword_subscriptions (LOWER(keyword));

-- 已通知记录:同一篇文章命中同一个订阅只发一次邮件,即使后续 heat 变化重复入榜。
-- subscription 删除时级联清理;article 删除(retention purge)时也级联清理。
CREATE TABLE keyword_notifications (
    subscription_id BIGINT NOT NULL REFERENCES keyword_subscriptions(id) ON DELETE CASCADE,
    article_id      BIGINT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    notified_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (subscription_id, article_id)
);

-- 反查"这个订阅最近通知过哪些 article"用得到,但目前匹配是"article 没在表里"
-- 走主键查询足够,不另起索引。
