package order

import (
	"context"

	"app.local/internal/kit/logctx"
	"app.local/internal/modules/order/events"
	"app.local/internal/platform/queue"
)

// publisher 用 queue.Inserter 实现 domain.Publisher，并补上 request_id 供跨进程日志关联。
type publisher struct {
	q *queue.Inserter
}

func (p publisher) PublishOrderCreated(ctx context.Context, evt events.OrderCreated) error {
	evt.RequestID = logctx.RequestID(ctx)
	return p.q.InsertTx(ctx, evt)
}
