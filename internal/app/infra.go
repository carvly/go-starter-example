// Package app 是组合根：唯一知道"有哪些模块、它们怎么连起来"的地方。
// infra.go 构造共享基础设施，modules.go 构造模块，api.go / worker.go 组装两个进程。
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"app.local/internal/config"
	"app.local/internal/platform/dbtx"
	"app.local/internal/platform/httpserver"
	"app.local/internal/platform/postgres"
	"app.local/internal/platform/queue"
	"app.local/internal/platform/redis"
)

// Infra 持有连接类资源，按配置决定是否创建（没配 REDIS_ADDR 就不连 Redis）。
// 模块构造只是结构体赋值，昂贵的东西都在这里，由 Close 统一释放。
type Infra struct {
	Config *config.Config
	Logger *slog.Logger

	Pool *pgxpool.Pool
	DBTx *dbtx.Handle
	// Redis 为 nil 表示没有配置 Redis。
	Redis *goredis.Client
	// Queue 只入队、不消费；worker 进程另建消费用的 client。
	Queue *queue.Inserter

	// Pingers 是 /readyz 检查的依赖。
	Pingers []httpserver.Pinger
}

func NewInfra(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*Infra, error) {
	pool, err := postgres.NewPool(ctx, postgres.PoolConfig{
		DSN:               cfg.DB.DSN,
		MinConns:          cfg.DB.MinConns,
		MaxConns:          cfg.DB.MaxConns,
		MaxConnLifetime:   cfg.DB.MaxConnLifetime,
		HealthCheckPeriod: cfg.DB.HealthCheckPeriod,
	})
	if err != nil {
		return nil, fmt.Errorf("app: build infra: %w", err)
	}
	handle := dbtx.NewHandle(pool)

	insertClient, err := queue.NewClient(pool, nil, nil)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("app: build infra: %w", err)
	}

	in := &Infra{
		Config:  cfg,
		Logger:  logger,
		Pool:    pool,
		DBTx:    handle,
		Queue:   queue.NewInserter(insertClient, handle),
		Pingers: []httpserver.Pinger{handle},
	}
	if cfg.Redis.Addr != "" {
		in.Redis = redis.NewClient(cfg.Redis.Addr, cfg.Redis.Password)
		in.Pingers = append(in.Pingers, redis.NewPinger(in.Redis))
	} else {
		logger.Warn("REDIS_ADDR 未配置：缓存退回 Noop，限流按进程内存计数，多实例部署时实际额度 = 配置值 × 实例数")
	}
	return in, nil
}

// Close 逆序释放资源。
func (in *Infra) Close() {
	if in.Redis != nil {
		if err := in.Redis.Close(); err != nil {
			in.Logger.Error("close redis client", "error", err)
		}
	}
	if in.Pool != nil {
		in.Pool.Close()
	}
}
