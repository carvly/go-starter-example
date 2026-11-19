package domain

import "time"

// 列表接口从 v1 起就带 cursor 分页：上线后再补会破坏客户端兼容性。
const (
	DefaultListLimit = 20
	MaxListLimit     = 100
)

// MaxStock 是单个商品的库存上限：调整库存是"加减一个量"，有上限才不会在数据库里溢出。
const MaxStock = 1_000_000_000

// Product 的 PriceCents 用最小货币单位（分）的整数，避免浮点精度误差。
type Product struct {
	ID         string
	Name       string
	PriceCents int64
	Stock      int64
	// Version 只在后台编辑（Update）时校验并加 1；调库存、扣库存是单个增减量，不会覆盖别人的修改，不动它。
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}
