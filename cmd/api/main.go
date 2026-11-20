// Command api 是 HTTP API 入口。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"app.local/internal/app"
	"app.local/internal/config"
	"app.local/internal/platform/healthcheck"
	"app.local/internal/platform/logger"
)

func main() {
	// 本地开发用 .env；生产环境没有这个文件，找不到就忽略。
	_ = godotenv.Load()

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := app.RunMigrateCommand(os.Args[1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "api:", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "api: load config:", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 && os.Args[1] == "queue" {
		if err := app.RunQueueCommand(context.Background(), cfg, os.Args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "api:", err)
			os.Exit(1)
		}
		return
	}

	// distroless 镜像里没有 curl，HEALTHCHECK 用这个子命令复用同一个二进制探活。
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck.Run(cfg.HTTP.Addr))
	}

	// 其余带参数的调用都是模块提供的子命令（如 api staff create），由组合根查找执行，
	// 这里不认识任何模块。
	if len(os.Args) > 1 {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		code := app.RunCommand(ctx, cfg, os.Args[1:], os.Stdout, os.Stderr)
		stop()
		os.Exit(code)
	}

	log := logger.New(cfg.LogLevel, os.Stdout)
	// 没有绑定 request 的代码路径（比如 river 执行任务时）经 logctx.From 拿到的是
	// slog.Default()，设成同一个 logger，格式和级别才一致。
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.RunAPI(ctx, cfg, log); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}
