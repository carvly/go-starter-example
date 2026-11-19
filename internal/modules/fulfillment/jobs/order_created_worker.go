// Package jobs 是 fulfillment 的 river 适配层：只做参数转换和日志关联，业务规则
// 都在 fulfillment.Process。
package jobs

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"

	"app.local/internal/kit/logctx"
	"app.local/internal/modules/order/events"
)

type OrderCreatedHandler interface {
	OnOrderCreated(ctx context.Context, evt events.OrderCreated) error
}

type OrderCreatedWorker struct {
	river.WorkerDefaults[events.OrderCreated]
	handler OrderCreatedHandler
}

func NewOrderCreatedWorker(h OrderCreatedHandler) *OrderCreatedWorker {
	return &OrderCreatedWorker{handler: h}
}

// Work 额外 recover 一次：river 自己也会把 panic 转成任务失败，这里不依赖对
// 第三方内部行为的假设。
func (w *OrderCreatedWorker) Work(ctx context.Context, job *river.Job[events.OrderCreated]) (err error) {
	if id := job.Args.RequestID; id != "" {
		ctx = logctx.WithRequest(ctx, logctx.From(ctx), id)
	}
	defer func() {
		if r := recover(); r != nil {
			logctx.From(ctx).ErrorContext(ctx, "order created worker panic recovered", "order_id", job.Args.OrderID, "panic", r)
			err = fmt.Errorf("order created worker panic: %v", r)
		}
	}()
	return w.handler.OnOrderCreated(ctx, job.Args)
}

var _ river.Worker[events.OrderCreated] = (*OrderCreatedWorker)(nil)
