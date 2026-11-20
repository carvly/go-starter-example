package queue

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// NewClient 在 workers 为 nil 时构造只入队、不消费的 client（periodic 被
// 忽略）；worker 进程传入注册好的 workers 并调用 Start。定时任务用 river 自带的
// PeriodicJob，不另接 cron 库。
func NewClient(pool *pgxpool.Pool, workers *Workers, periodic []Periodic) (*river.Client[pgx.Tx], error) {
	cfg := &river.Config{}
	if workers != nil {
		cfg.Workers = workers.river
		cfg.Queues = map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 100},
		}
		for _, p := range periodic {
			cfg.PeriodicJobs = append(cfg.PeriodicJobs, p.riverJob())
		}
		// 停止时等正在执行的任务最多这么久，之后取消它们的 ctx；要小于进程的关停超时。
		cfg.SoftStopTimeout = 10 * time.Second
	}

	client, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("queue: create river client: %w", err)
	}
	return client, nil
}
