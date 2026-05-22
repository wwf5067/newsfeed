-- 热词发现评估报告:每小时定时跑评估任务,把当前算法 + 多组候选阈值的指标
-- 写入此表,供运维观察哪组阈值在最近数据上最优。
--
-- 设计动机:
-- collectHeatDiscoveredWords 的几个常数(minArticles/minSources/minHanLen 等)
-- 是经验值,可能跟 prod 实际数据分布不匹配。需要一个反馈回路:
--   1. 用 heat_blacklist(用户手动删的"不该被识别为热词")作为 negative 样本
--   2. 当前算法 + 邻近候选阈值各跑一遍,算 precision(发现的词 NOT 命中黑名单的比例)
--   3. 跟 candidate_count(召回信号)综合给一个 score
--   4. 写入此表,如果某候选显著优于当前,suggestion 字段写建议
--   5. 运维定期查询 / 手动决定是否调整代码常数
--
-- 不自动改算法,因为参数自调容易跑偏(垃圾词涌入或召回归零),需要人工把关。

CREATE TABLE heat_eval_reports (
    id              BIGSERIAL PRIMARY KEY,
    evaluated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 评估用的文章窗口(小时)。固定 24 比较稳定,跨小时变化不大。
    window_hours    INT NOT NULL,
    -- 当时窗口内的文章数 / 黑名单大小,用于解读报告
    articles_count  INT NOT NULL,
    blacklist_count INT NOT NULL,
    -- baseline = 当前线上算法的表现 {params, discovered_count, blacklist_hit_count, precision, score}
    baseline        JSONB NOT NULL,
    -- candidates = 候选阈值组合数组 [{params, discovered_count, blacklist_hit_count, precision, score}, ...]
    candidates      JSONB NOT NULL,
    -- 最优组合的 params 描述(如 "minArticles=3,minSources=2"),最优 = candidates 中 score 最高
    best_variant    TEXT,
    -- 建议文本(空字符串表示当前已最优、无需调整)
    suggestion      TEXT NOT NULL DEFAULT ''
);

-- 按时间倒序查询最近报告
CREATE INDEX idx_heat_eval_reports_evaluated_at ON heat_eval_reports (evaluated_at DESC);
