package domain

import "context"

// ProductLookup 是 order 自己定义的窄接口，只声明下单需要的信息。模块之间不能
// 互相 import，实现由 internal/app/adapters.go 提供，并负责把 product 的错误
// 翻译成 ErrProductNotFound。
type ProductLookup interface {
	// GetPriceAndName 返回商品当前的名称和单价（分）。找不到商品时返回
	// ErrProductNotFound。
	GetPriceAndName(ctx context.Context, productID string) (name string, priceCents int64, err error)
}
