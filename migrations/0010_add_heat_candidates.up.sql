-- 热度反馈候选词典:由系统自动发现的热词,达到阈值后"转正"参与正式的实体识别。
CREATE TABLE IF NOT EXISTS heat_candidates (
    id          SERIAL PRIMARY KEY,
    word        TEXT NOT NULL UNIQUE,              -- 候选词(如"豆包""免签")
    kind        TEXT NOT NULL DEFAULT 'keyword',   -- entity(名词性) / keyword(动词性)
    hit_days    INT  NOT NULL DEFAULT 1,           -- 连续命中天数
    total_hits  INT  NOT NULL DEFAULT 0,           -- 总命中文章数(累计)
    last_hit_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),-- 最近一次命中时间
    promoted_at TIMESTAMPTZ,                       -- 转正时间,NULL=未转正
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_heat_candidates_word ON heat_candidates(word);
CREATE INDEX idx_heat_candidates_promoted ON heat_candidates(promoted_at) WHERE promoted_at IS NOT NULL;
