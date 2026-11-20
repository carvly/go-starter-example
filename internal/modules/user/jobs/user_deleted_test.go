package jobs_test

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"

	"app.local/internal/kit/logctx"
	"app.local/internal/modules/user/events"
	"app.local/internal/modules/user/jobs"
)

// fakeCleaner 记录调用并按脚本返回错误。
type fakeCleaner struct {
	calls   []string
	userIDs []string
	err     error
}

func (f *fakeCleaner) DeleteUserData(_ context.Context, userID string) error {
	f.calls = append(f.calls, "delete")
	f.userIDs = append(f.userIDs, userID)
	return f.err
}

// user_deleted 的 worker 逐个调用组合根注入的清理端口，任一失败整体重试，
// 每个实现必须幂等（见 docs/agents/queue.md）。
func TestUserDeletedWorker(t *testing.T) {
	t.Run("没有登记清理器时也成功", func(t *testing.T) {
		w := jobs.NewUserDeletedWorker([]*fakeCleaner(nil))
		job := &river.Job[events.UserDeleted]{Args: events.UserDeleted{UserID: "u1"}}
		if err := w.Work(t.Context(), job); err != nil {
			t.Errorf("Work() = %v, want nil", err)
		}
	})

	t.Run("逐个调用全部端口", func(t *testing.T) {
		a, b := &fakeCleaner{}, &fakeCleaner{}
		w := jobs.NewUserDeletedWorker([]*fakeCleaner{a, b})
		job := &river.Job[events.UserDeleted]{Args: events.UserDeleted{UserID: "u1"}}
		if err := w.Work(t.Context(), job); err != nil {
			t.Fatal(err)
		}
		if len(a.calls) != 1 || len(b.calls) != 1 || a.userIDs[0] != "u1" || b.userIDs[0] != "u1" {
			t.Errorf("calls = %v/%v %v/%v", a.calls, a.userIDs, b.calls, b.userIDs)
		}
	})

	t.Run("中间失败返回错误，river 整体重试（后面的端口在重试时再调）", func(t *testing.T) {
		boom := errors.New("boom")
		a, b := &fakeCleaner{err: boom}, &fakeCleaner{}
		w := jobs.NewUserDeletedWorker([]*fakeCleaner{a, b})
		job := &river.Job[events.UserDeleted]{Args: events.UserDeleted{UserID: "u1"}}
		if err := w.Work(t.Context(), job); !errors.Is(err, boom) {
			t.Fatalf("Work() = %v, want boom", err)
		}
		if len(b.calls) != 0 {
			t.Errorf("失败后不应继续调用后面的端口：%v", b.calls)
		}
		// 重试（幂等）：a 不再失败时整体成功。
		a.err = nil
		if err := w.Work(t.Context(), job); err != nil {
			t.Fatalf("重试 Work() = %v, want nil", err)
		}
		if len(a.calls) != 2 || len(b.calls) != 1 {
			t.Errorf("重试后的调用 = %v/%v, want 2/1（a 被重试、两个端口各清一次）", a.calls, b.calls)
		}
	})
}

type cleanerFunc func(context.Context, string) error

func (f cleanerFunc) DeleteUserData(ctx context.Context, userID string) error { return f(ctx, userID) }

func TestUserDeletedWorker_PropagatesRequestID(t *testing.T) {
	var got string
	w := jobs.NewUserDeletedWorker([]cleanerFunc{func(ctx context.Context, _ string) error {
		got = logctx.RequestID(ctx)
		return nil
	}})
	job := &river.Job[events.UserDeleted]{Args: events.UserDeleted{UserID: "u1", RequestID: "req-1"}}
	if err := w.Work(t.Context(), job); err != nil {
		t.Fatalf("Work() error = %v", err)
	}
	if got != "req-1" {
		t.Errorf("request_id = %q, want req-1", got)
	}
}

func TestUserDeletedWorker_RecoversPanic(t *testing.T) {
	w := jobs.NewUserDeletedWorker([]cleanerFunc{func(context.Context, string) error {
		panic("boom")
	}})
	if err := w.Work(t.Context(), &river.Job[events.UserDeleted]{Args: events.UserDeleted{UserID: "u1"}}); err == nil {
		t.Error("panic 应该被转成错误返回")
	}
}

// Kind 是存量任务找 worker 的依据，改名会让队列里已有的任务永远没人消费。
func TestUserDeletedKind_IsFrozen(t *testing.T) {
	if got := (events.UserDeleted{}).Kind(); got != "user_deleted" {
		t.Fatalf("Kind() = %q, 不能修改，必须保持 user_deleted", got)
	}
}
