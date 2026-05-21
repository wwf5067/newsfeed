package api

import (
	"context"
	"fmt"

	"github.com/wwf5067/newsfeed/internal/model"
)

// ListEntitiesOptions 查询实体的选项
type ListEntitiesOptions struct {
	Name   string
	Type   string
	Limit  int
	Offset int
}

// ListEntities 查询实体列表,包含相关文章/事件数
func (r *Repository) ListEntities(ctx context.Context, opts ListEntitiesOptions) ([]model.EntityResponse, error) {
	if opts.Limit == 0 {
		opts.Limit = 10
	}
	if opts.Limit > 100 {
		opts.Limit = 100
	}

	var where string
	var args []any
	argIdx := 1

	if opts.Name != "" {
		args = append(args, "%"+opts.Name+"%")
		where += fmt.Sprintf("AND e.name ILIKE $%d ", argIdx)
		argIdx++
	}
	if opts.Type != "" {
		args = append(args, opts.Type)
		where += fmt.Sprintf("AND e.type = $%d ", argIdx)
		argIdx++
	}

	args = append(args, opts.Limit, opts.Offset)
	limitIdx := argIdx
	offsetIdx := argIdx + 1

	const q = `
SELECT 
    e.id,
    e.name,
    e.type,
    e.alias,
    COUNT(DISTINCT ae.article_id) as article_count,
    COUNT(DISTINCT ae_event.event_id) as event_count,
    e.created_at,
    e.updated_at
FROM entities e
LEFT JOIN article_entities ae ON e.id = ae.entity_id
LEFT JOIN (
    SELECT DISTINCT entity_id, event_id
    FROM article_entities ae2
    INNER JOIN article_events ae3 ON ae2.article_id = ae3.article_id
) ae_event ON e.id = ae_event.entity_id
WHERE 1=1 %s
GROUP BY e.id, e.name, e.type, e.alias, e.created_at, e.updated_at
ORDER BY article_count DESC, e.name
LIMIT $%d OFFSET $%d
`

	query := fmt.Sprintf(q, where, limitIdx, offsetIdx)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []model.EntityResponse
	for rows.Next() {
		var entity model.EntityResponse
		if err := rows.Scan(
			&entity.ID,
			&entity.Name,
			&entity.Type,
			&entity.Alias,
			&entity.ArticleCount,
			&entity.EventCount,
			&entity.CreatedAt,
			&entity.UpdatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, entity)
	}

	return result, rows.Err()
}

// GetEntity 获取单个实体详情
func (r *Repository) GetEntity(ctx context.Context, entityID int64) (*model.EntityResponse, error) {
	const q = `
SELECT 
    e.id,
    e.name,
    e.type,
    e.alias,
    COUNT(DISTINCT ae.article_id) as article_count,
    COUNT(DISTINCT ae_event.event_id) as event_count,
    e.created_at,
    e.updated_at
FROM entities e
LEFT JOIN article_entities ae ON e.id = ae.entity_id
LEFT JOIN (
    SELECT DISTINCT entity_id, event_id
    FROM article_entities ae2
    INNER JOIN article_events ae3 ON ae2.article_id = ae3.article_id
) ae_event ON e.id = ae_event.entity_id
WHERE e.id = $1
GROUP BY e.id, e.name, e.type, e.alias, e.created_at, e.updated_at
`

	var entity model.EntityResponse
	err := r.pool.QueryRow(ctx, q, entityID).Scan(
		&entity.ID,
		&entity.Name,
		&entity.Type,
		&entity.Alias,
		&entity.ArticleCount,
		&entity.EventCount,
		&entity.CreatedAt,
		&entity.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &entity, nil
}

// ListEntityArticles 获取一个实体相关的文章列表
func (r *Repository) ListEntityArticles(ctx context.Context, entityID int64, limit, offset int) ([]model.Article, error) {
	if limit == 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	const q = `
SELECT
    a.id,
    a.title,
    a.url,
    a.source_key,
    a.published_at,
    a.content,
    a.heat_value
FROM articles a
INNER JOIN article_entities ae ON a.id = ae.article_id
WHERE ae.entity_id = $1
ORDER BY a.published_at DESC
LIMIT $2 OFFSET $3
`

	rows, err := r.pool.Query(ctx, q, entityID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []model.Article
	for rows.Next() {
		var article model.Article
		if err := rows.Scan(
			&article.ID,
			&article.Title,
			&article.URL,
			&article.SourceKey,
			&article.PublishedAt,
			&article.Content,
			&article.HeatValue,
		); err != nil {
			return nil, err
		}
		result = append(result, article)
	}

	return result, rows.Err()
}
