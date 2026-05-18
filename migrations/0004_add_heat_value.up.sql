-- 热度数值化 + 上一次热度快照,用于前端展示趋势(↑/↓ + 差值)。
-- heat 文本仍保留,展示时优先用源原文;heat_value 为后端解析出的数值,仅用于比较。
-- prev_* 字段在 UPSERT 时由旧行搬过来;首次插入保持默认 0/'',前端据此判断"无趋势可展示"。
ALTER TABLE articles
    ADD COLUMN heat_value      BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN prev_heat       TEXT   NOT NULL DEFAULT '',
    ADD COLUMN prev_heat_value BIGINT NOT NULL DEFAULT 0;
