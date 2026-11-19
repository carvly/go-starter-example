package app

import (
	"context"
	"errors"
	"fmt"

	orderdomain "app.local/internal/modules/order/domain"
	productdomain "app.local/internal/modules/product/domain"
)

// 跨模块适配器按"adapters_<消费方>_<提供方>.go"分文件：模块之间不能互相 import，
// 消费方定义窄接口，这里用提供方的 Service 实现它，并把提供方的错误翻译成消费方
// 自己的错误。删除模块时连同名字里带它的适配器文件一起删除。

// productLookup 实现 order 的 ProductLookup。
type productLookup struct {
	products *productdomain.Service
}

func (a productLookup) GetPriceAndName(ctx context.Context, productID string) (string, int64, error) {
	p, err := a.products.Get(ctx, productID)
	if err != nil {
		if errors.Is(err, productdomain.ErrNotFound) {
			return "", 0, orderdomain.ErrProductNotFound
		}
		return "", 0, fmt.Errorf("lookup product: %w", err)
	}
	return p.Name, p.PriceCents, nil
}

var _ orderdomain.ProductLookup = productLookup{}
