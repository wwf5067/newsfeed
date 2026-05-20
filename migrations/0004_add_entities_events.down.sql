-- 顺序:先删依赖的表,再删被依赖的
DROP TABLE IF EXISTS article_events;
DROP TABLE IF EXISTS article_entities;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS entities;

DROP INDEX IF EXISTS idx_articles_pending;
ALTER TABLE articles DROP COLUMN IF EXISTS extract_attempts;
ALTER TABLE articles DROP COLUMN IF EXISTS extracted_at;
