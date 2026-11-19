package domain

import "time"

const (
	DefaultListLimit = 20
	MaxListLimit     = 100
)

// MaxQuantity 是单次下单的上限：既是业务规则，也防止 quantity × 单价溢出 int64。
const MaxQuantity = 100_000

// Status 是订单状态机。StatusFailed 是明确的终态：异步处理永久失败后订单
// 必须收口，不能一直停在 pending。
type Status string

const (
	StatusPending   Status = "pending"
	StatusConfirmed Status = "confirmed"
	StatusFailed    Status = "failed"
)

// Valid 报告 s 是不是已知的状态，用于校验外部输入的筛选条件。
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusConfirmed, StatusFailed:
		return true
	}
	return false
}

// SearchFilter 是后台订单列表的筛选条件，零值字段表示不筛选。Statuses 是"任一"，时间是半开区间 [From, To)。
type SearchFilter struct {
	Statuses []Status
	From     time.Time
	To       time.Time
}

// Order 的 ProductName/UnitPriceCents 是下单那一刻的快照，不随商品改名/调价变化。
type Order struct {
	ID             string
	CustomerName   string
	ProductID      string
	ProductName    string
	Quantity       int64
	UnitPriceCents int64
	TotalCents     int64
	Status         Status
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
