package fulfillment_test

import (
	"context"
	"errors"
	"testing"

	"app.local/internal/kit/uow/uowtest"
	"app.local/internal/modules/fulfillment"
	"app.local/internal/modules/order/events"
)

type fakeLedger map[string]bool

func (l fakeLedger) MarkEventProcessed(_ context.Context, orderID string) (bool, error) {
	if l[orderID] {
		return false, nil
	}
	l[orderID] = true
	return true, nil
}

type fakeStock struct {
	stock map[string]int64
	err   error // 非 nil 时 Decrease 直接返回它
}

func (s *fakeStock) Decrease(_ context.Context, productID string, qty int64) error {
	if s.err != nil {
		return s.err
	}
	if s.stock[productID] < qty {
		return fulfillment.ErrStockUnavailable
	}
	s.stock[productID] -= qty
	return nil
}

type fakeOrders map[string]string

func (o fakeOrders) Confirm(_ context.Context, id string) error {
	o[id] = "confirmed"
	return nil
}

func (o fakeOrders) MarkFailed(_ context.Context, id string) error {
	o[id] = "failed"
	return nil
}

func newProcess(stock *fakeStock) (*fulfillment.Process, fakeOrders) {
	orders := fakeOrders{"order-1": "pending"}
	return fulfillment.NewProcess(uowtest.Immediate{}, fakeLedger{}, stock, orders), orders
}

var evt = events.OrderCreated{OrderID: "order-1", ProductID: "prod-1", Quantity: 3}

func TestProcess_OnOrderCreated(t *testing.T) {
	ctx := t.Context()

	t.Run("库存足够：扣库存并确认订单", func(t *testing.T) {
		stock := &fakeStock{stock: map[string]int64{"prod-1": 10}}
		p, orders := newProcess(stock)
		if err := p.OnOrderCreated(ctx, evt); err != nil {
			t.Fatalf("OnOrderCreated() error = %v", err)
		}
		if orders["order-1"] != "confirmed" || stock.stock["prod-1"] != 7 {
			t.Errorf("status = %s, stock = %d, want confirmed / 7", orders["order-1"], stock.stock["prod-1"])
		}
	})

	t.Run("永久失败：订单收口为 failed，不返回错误（不重试）", func(t *testing.T) {
		p, orders := newProcess(&fakeStock{stock: map[string]int64{"prod-1": 1}})
		if err := p.OnOrderCreated(ctx, evt); err != nil {
			t.Fatalf("OnOrderCreated() error = %v, want nil", err)
		}
		if orders["order-1"] != "failed" {
			t.Errorf("status = %s, want failed", orders["order-1"])
		}
	})

	t.Run("临时故障：返回错误触发重试，订单保持 pending", func(t *testing.T) {
		transient := errors.New("connection reset")
		p, orders := newProcess(&fakeStock{err: transient})
		if err := p.OnOrderCreated(ctx, evt); !errors.Is(err, transient) {
			t.Fatalf("OnOrderCreated() error = %v, want %v", err, transient)
		}
		if orders["order-1"] != "pending" {
			t.Errorf("status = %s, want pending", orders["order-1"])
		}
	})

	t.Run("重放同一事件不会重复扣库存", func(t *testing.T) {
		stock := &fakeStock{stock: map[string]int64{"prod-1": 10}}
		p, orders := newProcess(stock)
		if err := p.OnOrderCreated(ctx, evt); err != nil {
			t.Fatalf("第一次 error = %v", err)
		}
		orders["order-1"] = "pending"
		if err := p.OnOrderCreated(ctx, evt); err != nil {
			t.Fatalf("重放 error = %v", err)
		}
		if stock.stock["prod-1"] != 7 || orders["order-1"] != "pending" {
			t.Errorf("stock = %d, status = %s, want 7 / pending（重放应该直接跳过）", stock.stock["prod-1"], orders["order-1"])
		}
	})
}
