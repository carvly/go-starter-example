package domain

import (
	"context"

	"app.local/internal/kit/page"

	"app.local/internal/modules/order/events"
)

// Repository 由本模块的 postgres 子包实现。
type Repository interface {
	Create(ctx context.Context, o *Order) error
	GetByID(ctx context.Context, id string) (*Order, error)

	// List 按创建时间倒序分页，语义同 product 的 Repository.List。
	List(ctx context.Context, cursor string, limit int) (items []*Order, nextCursor string, err error)

	// Search 是后台的页码分页列表：按 SearchFilter 筛选、按创建时间倒序，总数封顶在
	// page.MaxTotal。
	Search(ctx context.Context, f SearchFilter, p page.Request) (page.Result[*Order], error)

	// UpdateStatus 只允许 pending → confirmed/failed，其它转换返回
	// ErrInvalidStatusTransition；判断和更新必须是同一条原子 SQL。
	UpdateStatus(ctx context.Context, id string, status Status) error

	EventLedger
}

// EventLedger 是 order_created 事件的去重记录，经 Module.EventLedger() 单独暴露
// 给 fulfillment，让"扣库存"这种非天然幂等的操作在至少一次投递下变得幂等。
type EventLedger interface {
	// MarkEventProcessed 在当前事务里插入去重记录（INSERT ... ON CONFLICT DO
	// NOTHING）；firstTime 为 false 表示这个事件之前已经处理过。
	MarkEventProcessed(ctx context.Context, orderID string) (firstTime bool, err error)
}

// Publisher 必须在 UnitOfWork.Run 内调用，保证订单落库和事件入队同事务提交。
type Publisher interface {
	PublishOrderCreated(ctx context.Context, evt events.OrderCreated) error
}
