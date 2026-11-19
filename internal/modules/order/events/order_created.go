// Package events 是 order 模块对外公开的事件契约：只有纯数据类型，不依赖
// river 或任何基础设施（river.JobArgs 只要求 Kind()），fulfillment 等其它
// 模块可以直接 import。
package events

// OrderCreatedKind 一旦发布就不能改名：队列里的存量任务按它找 worker，
// 改名后这些任务永远没人消费。fulfillment/jobs 的 TestOrderCreatedKind_IsFrozen
// 锁死了这个值。
const OrderCreatedKind = "order_created"

// OrderCreated 在订单落库的同一个事务里入队。JSON 字段名同样是存量任务的
// 序列化格式，只能新增可选字段，不能改名或删除。
type OrderCreated struct {
	OrderID   string `json:"order_id"`
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
	// RequestID 用于跨进程日志关联：worker 用它重建带 request_id 的 logger。
	RequestID string `json:"request_id,omitempty"`
}

func (OrderCreated) Kind() string { return OrderCreatedKind }
