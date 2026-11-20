package app

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/labstack/echo/v5"

	"app.local/internal/config"
	"app.local/internal/kit/lifecycle"
	"app.local/internal/platform/httpserver"
	"app.local/internal/platform/ratelimit"
)

// shutdownTimeout 要大于 HTTP 优雅关闭的 15s，给它留出完成的时间。
const shutdownTimeout = 20 * time.Second

// RunAPI 运行 HTTP API 进程，阻塞到 ctx 取消或组件出错。
func RunAPI(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	configs, err := loadModuleConfigs(cfg.Source)
	if err != nil {
		return err
	}
	logger.Info("starting api", "config", configs.Redacted(cfg))
	in, err := NewInfra(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer in.Close()

	components, err := apiComponents(in, buildModules(in, configs))
	if err != nil {
		return err
	}
	return lifecycle.Run(ctx, shutdownTimeout, components...)
}

// apiComponents 是 api 进程的 HTTP server：前台总是启动；后台只在配置了地址时启动，
// 未配置时不监听后台端口。
func apiComponents(in *Infra, mods *Modules) ([]lifecycle.Component, error) {
	e, err := httpserver.New(apiServerDeps(in, mods))
	if err != nil {
		return nil, fmt.Errorf("app: build http server: %w", err)
	}
	components := []lifecycle.Component{httpserver.Component("api", e, in.Config.HTTP.Addr)}

	if adminEnabled(in, mods.adminRegistrars()) {
		admin, err := httpserver.NewAdmin(adminServerDeps(in, mods))
		if err != nil {
			return nil, fmt.Errorf("app: build admin server: %w", err)
		}
		components = append(components, httpserver.Component("admin", admin, in.Config.HTTP.AdminAddr))
	}
	return components, nil
}

func adminEnabled(in *Infra, routes []func(*echo.Group)) bool {
	return in.Config.HTTP.AdminAddr != "" && len(routes) > 0
}

func apiServerDeps(in *Infra, mods *Modules) httpserver.Deps {
	return httpserver.Deps{
		Logger:         in.Logger,
		HTTP:           in.Config.HTTP,
		CORS:           in.Config.CORS,
		Pingers:        in.Pingers,
		RateLimitStore: ratelimit.NewStore(in.Redis, "global", globalRate(in), in.Logger),
		Routes:         mods.httpRegistrars(),
	}
}

func adminServerDeps(in *Infra, mods *Modules) httpserver.AdminDeps {
	return httpserver.AdminDeps{
		Logger:         in.Logger,
		HTTP:           in.Config.HTTP,
		Origin:         in.Config.HTTP.AdminOrigin,
		RateLimitStore: ratelimit.NewStore(in.Redis, "admin", globalRate(in), in.Logger),
		Routes:         mods.adminRegistrars(),
		Auth:           mods.adminAuth(),
		Static:         adminStatic,
	}
}

// adminStatic 是后台前端的构建产物，由 admin_ui.go 设置；没有那个文件时为 nil，不托管前端。
var adminStatic fs.FS

// globalRate 是按 IP 的全局限流额度，前台和后台各自计数、额度相同；取值由 config 校验为正数。
func globalRate(in *Infra) ratelimit.Limit {
	return ratelimit.PerSecond(in.Config.HTTP.GlobalRateLimitPerSecond)
}
