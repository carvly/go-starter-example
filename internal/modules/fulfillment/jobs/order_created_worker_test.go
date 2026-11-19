package jobs_test

import (
	"context"
	"testing"

	"github.com/riverqueue/river"

	"app.local/internal/kit/logctx"
	"app.local/internal/modules/fulfillment/jobs"
	"app.local/internal/modules/order/events"
)

type handlerFunc func(context.Context, events.OrderCreated) error

func (f handlerFunc) OnOrderCreated(ctx context.Context, evt events.OrderCreated) error {
	return f(ctx, evt)
}

func TestOrderCreatedWorker_PropagatesRequestID(t *testing.T) {
	var got string
	w := jobs.NewOrderCreatedWorker(handlerFunc(func(ctx context.Context, _ events.OrderCreated) error {
		got = logctx.RequestID(ctx)
		return nil
	}))
	job := &river.Job[events.OrderCreated]{Args: events.OrderCreated{OrderID: "o", RequestID: "req-1"}}
	if err := w.Work(t.Context(), job); err != nil {
		t.Fatalf("Work() error = %v", err)
	}
	if got != "req-1" {
		t.Errorf("request_id = %q, want req-1", got)
	}
}

func TestOrderCreatedWorker_RecoversPanic(t *testing.T) {
	w := jobs.NewOrderCreatedWorker(handlerFunc(func(context.Context, events.OrderCreated) error {
		panic("boom")
	}))
	if err := w.Work(t.Context(), &river.Job[events.OrderCreated]{}); err == nil {
		t.Error("panic 应该被转成错误返回")
	}
}

// Kind 是存量任务找 worker 的依据，改名会让队列里已有的任务永远没人消费。
func TestOrderCreatedKind_IsFrozen(t *testing.T) {
	if got := (events.OrderCreated{}).Kind(); got != "order_created" {
		t.Fatalf("Kind() = %q, 不能修改，必须保持 order_created", got)
	}
}
