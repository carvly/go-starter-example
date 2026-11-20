// Package jobs 是 user 的 river 适配层：过期行清理（oauth_states、过期/已吊销
// 超过保留期的 user_sessions）。
package jobs

import (
	"context"

	"github.com/riverqueue/river"

	"app.local/internal/kit/logctx"
)

// CleanupArgs 是定时任务的参数，每次入队都相同；截止时间在 Work 里按当前时间计算。
type CleanupArgs struct{}

func (CleanupArgs) Kind() string { return "user_cleanup" }

// Purger 由 domain.Service 实现。
type Purger interface {
	PurgeExpired(ctx context.Context) (states, sessions int64, err error)
}

type CleanupWorker struct {
	river.WorkerDefaults[CleanupArgs]
	purger Purger
}

func NewCleanupWorker(p Purger) *CleanupWorker {
	return &CleanupWorker{purger: p}
}

func (w *CleanupWorker) Work(ctx context.Context, _ *river.Job[CleanupArgs]) error {
	states, sessions, err := w.purger.PurgeExpired(ctx)
	if err != nil {
		return err
	}
	logctx.From(ctx).InfoContext(ctx, "user: purged expired rows", "oauth_states", states, "user_sessions", sessions)
	return nil
}
