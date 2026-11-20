// Package staff 是后台员工模块：账号、会话、角色权限、操作日志，以及后台 server 的鉴权
// 中间件。module.go 是它对组合根暴露的唯一入口。
package staff

import (
	"log/slog"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/labstack/echo/v5"
	"github.com/riverqueue/river"

	"app.local/internal/config"
	"app.local/internal/modules/staff/adminapi"
	"app.local/internal/modules/staff/domain"
	"app.local/internal/modules/staff/jobs"
	"app.local/internal/modules/staff/postgres"
	"app.local/internal/platform/cli"
	"app.local/internal/platform/crypto"
	"app.local/internal/platform/dbtx"
	"app.local/internal/platform/httpserver"
	"app.local/internal/platform/queue"
)

type Deps struct {
	DB     *dbtx.Handle
	Config Config
	// Argon2 是基础设施配置里的密码哈希参数（按部署硬件调优，不属于某个模块）。
	Argon2 config.Argon2Params
	// Operations 来自 api/admin.yaml，用于权限映射和可授予的权限码清单。
	Operations []httpserver.AdminOperation
	Logger     *slog.Logger
}

type Module struct {
	Service *domain.Service
	cfg     Config
	ops     []domain.Operation
}

func New(d Deps) *Module {
	passwords := crypto.NewPasswordVerifier(&argon2id.Params{
		Memory:      d.Argon2.MemoryKiB,
		Iterations:  d.Argon2.Iterations,
		Parallelism: d.Argon2.Parallelism,
		SaltLength:  d.Argon2.SaltLength,
		KeyLength:   d.Argon2.KeyLength,
	})
	ops := make([]domain.Operation, 0, len(d.Operations))
	for _, op := range d.Operations {
		ops = append(ops, domain.Operation{
			Method: op.Method, Route: op.Route, Permission: op.Permission, Tag: op.Tag,
			Summary: op.Summary, TagSummary: op.TagSummary,
		})
	}
	svc := domain.NewService(postgres.NewRepository(d.DB), passwords, d.DB, domain.Config{
		SessionIdleTimeout: d.Config.SessionIdleTimeout,
		SessionMaxAge:      d.Config.SessionMaxAge,
		TempPasswordTTL:    d.Config.TempPasswordTTL,
	}, ops)
	return &Module{Service: svc, cfg: d.Config, ops: ops}
}

// AdminGuard 返回后台 server 的鉴权中间件（会话校验 → 改密拦截 → 权限检查 → 操作日志），
// 由组合根注入 httpserver.AdminDeps.Auth。
func (m *Module) AdminGuard() echo.MiddlewareFunc {
	return adminapi.NewGuard(m.Service, m.ops)
}

// RegisterAdminHTTP 挂载 api/admin.yaml 中 tag=staff 的全部操作。
func (m *Module) RegisterAdminHTTP(g *echo.Group) {
	adminapi.Register(g, adminapi.NewHandler(m.Service, m.cfg.SessionMaxAge))
}

// RegisterJobs 注册操作日志清理任务的 worker。
func (m *Module) RegisterJobs(workers *queue.Workers) {
	queue.AddWorker(workers, jobs.NewAuditCleanupWorker(m.Service, m.cfg.AuditRetention))
}

// PeriodicJobs 每天清理一次超过保留期的操作日志，进程启动时先跑一次。
func (m *Module) PeriodicJobs() []queue.Periodic {
	return []queue.Periodic{{
		Schedule: river.PeriodicInterval(24 * time.Hour),
		Args:     jobs.AuditCleanupArgs{},
		Opts:     &river.PeriodicJobOpts{RunOnStart: true},
	}}
}

// Commands 是 api 二进制的 staff 子命令。
func (m *Module) Commands() []cli.Command {
	return commands(m.Service)
}
