-- 允许 level='quote' 用于区分机器生成的每日名言公告(由 crawler quotes job 写入)。
-- 这类公告与 info/warn/critical 在前端样式上有区分,后台轮询时也只软删 quote 类型,
-- 避免误伤运维手动 INSERT 的公告。
ALTER TABLE announcements DROP CONSTRAINT announcements_level_check;
ALTER TABLE announcements ADD CONSTRAINT announcements_level_check
    CHECK (level IN ('info', 'warn', 'critical', 'quote'));
