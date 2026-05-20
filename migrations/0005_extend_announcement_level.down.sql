-- 回滚:先把 quote 类型公告软删除,再收紧约束。
-- 直接 DELETE 也可以,但软删保留历史更稳妥。
UPDATE announcements SET is_active = FALSE WHERE level = 'quote';
ALTER TABLE announcements DROP CONSTRAINT announcements_level_check;
ALTER TABLE announcements ADD CONSTRAINT announcements_level_check
    CHECK (level IN ('info', 'warn', 'critical'));
