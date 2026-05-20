-- 加 source_rank 字段,记录该热搜在源平台的官方榜位(1-based,1 = 榜首)。
-- NULL 表示该源没有 rank 概念(将来可能接入的非热榜源,如普通 RSS)。
--
-- 设计动机:
-- published_at 之前一直被复用编码 rank(rank 1 = now,rank 2 = now-1s ...),
-- 但这跟"问题创建/上榜时间"语义打架 — 各 tab 想按时间序排,首页 HotPanel
-- 想按官方 rank 排,一个字段无法同时表达。独立字段后:
--   · published_at 回归"创建/上榜时间",各 tab 按 published_at DESC
--   · source_rank 反映平台官方 rank,首页 HotPanel 按 source_rank ASC
ALTER TABLE articles ADD COLUMN source_rank INT;

-- 不加 NOT NULL,允许非热搜源不填(将来扩展);
-- 不加 INDEX,因为查询场景是"过滤到最新批次后排序",批次内 ~50 行,顺序扫够快。
