-- content 字段 trgm 索引:加速 ListArticlesByTerms 对 content 的 ILIKE 模糊搜索。
--
-- 背景:
--   migration 0010 只对 title 建了 GIN trgm 索引,但 ListArticlesByTerms
--   同时对 content 使用 ILIKE '%term%',content 仍是全表扫描。
--   对于 3 字以上实体词(如"武汉大学""特斯拉"),content 命中是有效召回,
--   应当被索引加速。
--
-- 注意:
--   · CONCURRENTLY 不阻塞写入,适合在线迁移。
--   · content 字段可能较大,索引体积约为 title 索引的 5-10x,确认磁盘充足后执行。
--   · pg_trgm 扩展在 0010 已创建,此处不需重复创建。

CREATE INDEX CONCURRENTLY IF NOT EXISTS articles_content_trgm_idx
    ON articles
    USING GIN (content gin_trgm_ops);
