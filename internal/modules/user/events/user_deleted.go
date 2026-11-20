// Package events 是 user 模块对外公开的事件契约：只有纯数据类型，不依赖
// river 或任何基础设施（river.JobArgs 只要求 Kind()），组合根可以直接 import。
package events

import "time"

// UserDeletedKind 一旦发布就不能改名：队列里的存量任务按它找 worker，
// user/jobs 的 TestUserDeletedKind_IsFrozen 锁死了这个值。
const UserDeletedKind = "user_deleted"

// UserDeleted 在账号注销的同一个事务里入队，通知各模块匿名化或删除该用户的
// 数据。JSON 字段名同样是存量任务的序列化格式，只能新增可选字段。
type UserDeleted struct {
	UserID string `json:"user_id"`
	// OccurredAt 是注销发生的时间。
	OccurredAt time.Time `json:"occurred_at"`
	// RequestID 用于跨进程日志关联：worker 用它重建带 request_id 的 logger。
	RequestID string `json:"request_id,omitempty"`
}

func (UserDeleted) Kind() string { return UserDeletedKind }
