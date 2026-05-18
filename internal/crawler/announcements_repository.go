package crawler

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AnnouncementsRepository crawler 服务对 announcements 表的写入层。
// 与 api 包里的 Repository(只读)隔离,避免相互依赖。
// 仅暴露 quotes job 需要的两个操作;运维公告由人工 SQL 处理,不走这里。
type AnnouncementsRepository struct {
	pool *pgxpool.Pool
}

func NewAnnouncementsRepository(pool *pgxpool.Pool) *AnnouncementsRepository {
	return &AnnouncementsRepository{pool: pool}
}

// InsertQuote 插入一条 level='quote'、priority=0 的公告。
// endsAt 为 nil 时存 NULL(无截止);通常应当传入"下一轮 cron 之后的时间",
// 这样万一调度漏跑也不会一直显示旧名言。
func (r *AnnouncementsRepository) InsertQuote(
	ctx context.Context, content string, endsAt *time.Time,
) (int64, error) {
	const q = `
INSERT INTO announcements (content, level, priority, starts_at, ends_at, is_active)
VALUES ($1, 'quote', 0, NOW(), $2, TRUE)
RETURNING id
`
	var id int64
	err := r.pool.QueryRow(ctx, q, content, endsAt).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// DeactivateActiveQuotes 把当前所有 is_active=TRUE 且 level='quote' 的公告软删除。
// 只动 quote 级别,人工运维公告(info/warn/critical)不受影响。
// 返回受影响行数。
func (r *AnnouncementsRepository) DeactivateActiveQuotes(ctx context.Context) (int64, error) {
	const q = `
UPDATE announcements
SET is_active = FALSE, updated_at = NOW()
WHERE level = 'quote' AND is_active = TRUE
`
	tag, err := r.pool.Exec(ctx, q)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
