package domain

import (
	"context"
	"time"

	"app.local/internal/kit/page"
)

// Repository 由本模块的 postgres 子包实现。
type Repository interface {
	Create(ctx context.Context, p *Product) error
	GetByID(ctx context.Context, id string) (*Product, error)

	// List 按创建时间倒序分页。cursor 为空表示第一页；nextCursor 为空表示没有下一页。
	List(ctx context.Context, cursor string, limit int) (items []*Product, nextCursor string, err error)

	// Search 是后台的页码分页列表：名称包含 keyword（为空不筛选，不区分大小写），
	// 按创建时间倒序，总数封顶在 page.MaxTotal。
	Search(ctx context.Context, keyword string, p page.Request) (page.Result[*Product], error)

	// Update 在 version 仍是最新时改名称和单价并把 version 加 1，必须是单条条件 UPDATE；
	// 不存在返回 ErrNotFound，version 不是最新返回 ErrConflict。
	Update(ctx context.Context, id, name string, priceCents, version int64) (*Product, error)

	// AdjustStock 把库存加上 delta（可以为负），必须是单条原子 SQL，结果不在
	// [0, MaxStock] 内时不改并返回 ErrInsufficientStock / ErrStockTooLarge。
	// 用增减量而不是"设为某个值"：后者会覆盖同时发生的下单扣减。
	AdjustStock(ctx context.Context, id string, delta int64) (*Product, error)

	// DecreaseStock 必须是单条原子 SQL（UPDATE ... WHERE stock >= $quantity），
	// 库存不够时返回 ErrInsufficientStock。
	DecreaseStock(ctx context.Context, id string, quantity int64) error
}

// Cache 由 internal/platform/cache 实现（Redis 或 Noop），由组合根按配置选择。
type Cache interface {
	Get(ctx context.Context, id string) (*Product, bool, error)
	Set(ctx context.Context, id string, v *Product, ttl time.Duration) error
	Delete(ctx context.Context, id string) error
}
