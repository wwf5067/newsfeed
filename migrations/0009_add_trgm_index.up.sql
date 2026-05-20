-- pg_trgm 索引:加速 title 的 ILIKE 模糊搜索。
--
-- 背景:
--   ListArticlesByTerms 和文章列表搜索当前用 ILIKE '%keyword%',
--   无法命中 B-tree 索引,数据量增长后会触发全表扫描。
--   pg_trgm 的 GIN 索引将三元组(trigram)作为索引键,能直接加速 LIKE/ILIKE。
--
-- 适用场景:
--   · /trackers/storyline?term=... 的多别名 ILIKE OR 查询
--   · /articles?q=... 的标题模糊搜索
--
-- 注意:
--   · CREATE EXTENSION IF NOT EXISTS 是幂等的,重复执行无副作用。
--   · 索引创建时会短暂锁表(非 CONCURRENTLY),迁移期间建议暂停写入或在维护窗口执行。
--     个人项目数据量级下锁表时间 < 1s,可接受。

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS articles_title_trgm_idx
    ON articles
    USING GIN (title gin_trgm_ops);
