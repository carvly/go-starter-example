// Package fulfillment 编排"订单创建后扣库存并确认订单"这个跨模块流程。它同时
// 写 order 和 product，放进任一方都会让它依赖另一方，所以单独成模块。这里只有
// 业务规则，不依赖 river；队列适配在 jobs 子包。
package fulfillment

import (
	"context"
	"errors"
	"fmt"

	"app.local/internal/kit/logctx"
	"app.local/internal/kit/uow"
	"app.local/internal/modules/order/events"
)

// ErrStockUnavailable 表示永久性的扣库存失败（库存不足、商品已删除）：重试不会
// 成功，订单必须收口为 failed。哪些 product 错误属于这一类由组合根的适配器决定。
var ErrStockUnavailable = errors.New("fulfillment: stock unavailable")

// EventLedger 记录事件是否已处理，让非天然幂等的扣库存在至少一次投递下幂等。
type EventLedger interface {
	MarkEventProcessed(ctx context.Context, orderID string) (firstTime bool, err error)
}

type StockReserver interface {
	// Decrease 永久失败时返回 ErrStockUnavailable，其它错误视为可重试。
	Decrease(ctx context.Context, productID string, quantity int64) error
}

type OrderStatus interface {
	Confirm(ctx context.Context, orderID string) error
	MarkFailed(ctx context.Context, orderID string) error
}

type Process struct {
	uow    uow.UnitOfWork
	ledger EventLedger
	stock  StockReserver
	orders OrderStatus
}

func NewProcess(u uow.UnitOfWork, ledger EventLedger, stock StockReserver, orders OrderStatus) *Process {
	return &Process{uow: u, ledger: ledger, stock: stock, orders: orders}
}

// OnOrderCreated 在一个事务里完成去重、扣库存、更新订单状态。返回错误表示
// 应该重试；永久失败已经收口为 failed，返回 nil。
func (p *Process) OnOrderCreated(ctx context.Context, evt events.OrderCreated) error {
	log := logctx.From(ctx)
	return p.uow.Run(ctx, func(ctx context.Context) error {
		firstTime, err := p.ledger.MarkEventProcessed(ctx, evt.OrderID)
		if err != nil {
			return fmt.Errorf("mark event processed: %w", err)
		}
		if !firstTime {
			log.InfoContext(ctx, "order created event already processed, skip", "order_id", evt.OrderID)
			return nil
		}

		if err := p.stock.Decrease(ctx, evt.ProductID, evt.Quantity); err != nil {
			if !errors.Is(err, ErrStockUnavailable) {
				return fmt.Errorf("decrease stock: %w", err)
			}
			if err := p.orders.MarkFailed(ctx, evt.OrderID); err != nil {
				return fmt.Errorf("mark order failed: %w", err)
			}
			log.WarnContext(ctx, "order failed: stock unavailable",
				"order_id", evt.OrderID, "product_id", evt.ProductID, "reason", err)
			return nil
		}

		if err := p.orders.Confirm(ctx, evt.OrderID); err != nil {
			return fmt.Errorf("confirm order: %w", err)
		}
		return nil
	})
}
