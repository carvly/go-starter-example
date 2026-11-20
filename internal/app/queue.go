package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"

	"app.local/internal/config"
	"app.local/internal/platform/logger"
	"app.local/internal/platform/queue"
)

// RunQueueCommand 执行 api queue cancel-orphans：取消当前二进制没有 worker 的非终态任务。
// 它和 worker 进程一样构造模块并注册 worker，kind 清单只来自实际注册（newWorkers），不另写声明；
// 部署环境本来就有完整配置。日志写 stderr，stdout 只输出结果。
func RunQueueCommand(ctx context.Context, cfg *config.Config, args []string, stdout, stderr io.Writer) error {
	if len(args) != 2 || args[1] != "cancel-orphans" {
		return fmt.Errorf("usage: api queue cancel-orphans")
	}
	configs, err := loadModuleConfigs(cfg.Source)
	if err != nil {
		return err
	}
	log := logger.New(cfg.LogLevel, stderr)
	slog.SetDefault(log)
	in, err := NewInfra(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer in.Close()
	cancelled, err := queue.CancelOrphans(ctx, in.Pool, newWorkers(buildModules(in, configs)).Kinds())
	if err != nil {
		return err
	}
	for _, kind := range slices.Sorted(maps.Keys(cancelled)) {
		_, _ = fmt.Fprintf(stdout, "%s\t%d\n", kind, cancelled[kind])
	}
	return nil
}
