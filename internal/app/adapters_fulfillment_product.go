package app

import (
	"context"
	"errors"
	"fmt"

	"app.local/internal/modules/fulfillment"
	productdomain "app.local/internal/modules/product/domain"
)

// stockReserver 实现 fulfillment 的 StockReserver。"库存不足"和"商品已不存在"
// 重试都不会成功，翻译成 ErrStockUnavailable 让订单收口为 failed；其它错误原样
// 返回，交给队列重试。
type stockReserver struct {
	products *productdomain.Service
}

func (a stockReserver) Decrease(ctx context.Context, productID string, quantity int64) error {
	err := a.products.DecreaseStock(ctx, productID, quantity)
	if errors.Is(err, productdomain.ErrInsufficientStock) || errors.Is(err, productdomain.ErrNotFound) {
		return fmt.Errorf("%w: %w", fulfillment.ErrStockUnavailable, err)
	}
	return err
}

var _ fulfillment.StockReserver = stockReserver{}
