// Package jobs 是 user 的 river 适配层：过期行清理（oauth_states、过期/已吊销
// 超过保留期的 user_sessions）和 user_deleted 的编排 worker。
package jobs

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"

	"app.local/internal/kit/logctx"
	"app.local/internal/modules/user/events"
)

// UserDeletedWorker 消费 user 自己发布的 user_deleted，但它是编排角色：一个事件
// 牵动多个模块各自的清理，所以 cleaners 由组合根注入，而不是只清 user 自己的表
// （user 自己的数据在注销事务里已经删完）。
type UserDeletedWorker struct {
	river.WorkerDefaults[events.UserDeleted]
	cleaners []func(context.Context, string) error
}

func NewUserDeletedWorker[T interface {
	DeleteUserData(context.Context, string) error
}](cleaners []T) *UserDeletedWorker {
	w := &UserDeletedWorker{cleaners: make([]func(context.Context, string) error, 0, len(cleaners))}
	for _, c := range cleaners {
		w.cleaners = append(w.cleaners, c.DeleteUserData)
	}
	return w
}

// Work 额外 recover 一次：river 自己也会把 panic 转成任务失败，这里不依赖对
// 第三方内部行为的假设。
func (w *UserDeletedWorker) Work(ctx context.Context, job *river.Job[events.UserDeleted]) (err error) {
	if id := job.Args.RequestID; id != "" {
		ctx = logctx.WithRequest(ctx, logctx.From(ctx), id)
	}
	defer func() {
		if r := recover(); r != nil {
			logctx.From(ctx).ErrorContext(ctx, "user: user_deleted worker panic recovered", "user_id", job.Args.UserID, "panic", r)
			err = fmt.Errorf("user deleted worker panic: %v", r)
		}
	}()
	for _, c := range w.cleaners {
		if err := c(ctx, job.Args.UserID); err != nil {
			return err
		}
	}
	logctx.From(ctx).InfoContext(ctx, "user: user_deleted processed", "user_id", job.Args.UserID, "cleaners", len(w.cleaners))
	return nil
}
