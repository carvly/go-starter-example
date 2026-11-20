// Package user 是前台用户模块：账号的后台管理、微信登录、会话和
// 前台鉴权中间件。module.go 是它对组合根暴露的唯一入口。
package user

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v5"
	echoMiddleware "github.com/labstack/echo/v5/middleware"
	goredis "github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"

	"app.local/internal/modules/user/adminapi"
	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/httpapi"
	"app.local/internal/modules/user/jobs"
	"app.local/internal/modules/user/postgres"
	"app.local/internal/platform/crypto"
	"app.local/internal/platform/dbtx"
	"app.local/internal/platform/oauth"
	"app.local/internal/platform/queue"
	"app.local/internal/platform/ratelimit"
)

// Deps 是 user 模块的全部依赖，由组合根注入。
type Deps struct {
	DB     *dbtx.Handle
	Config Config
	Queue  *queue.Inserter
	// Redis 为 nil 时限流退回进程内存（额度按实例数放大）。
	Redis  *goredis.Client
	Logger *slog.Logger
}

type Module struct {
	Service      *domain.Service
	cfg          Config
	oauth        oauth.Registry
	redis        *goredis.Client
	logger       *slog.Logger
	dataCleaners []DataCleaner
}

func New(d Deps) *Module {
	issuer := crypto.NewTokenIssuer(d.Config.JWTSigningKey, d.Config.JWTKeyID)
	svc := domain.NewService(domain.Deps{
		Users:      postgres.NewRepository(d.DB),
		Identities: postgres.NewIdentityRepository(d.DB),
		OpenIDs:    postgres.NewWeChatOpenIDRepository(d.DB),
		Sessions:   postgres.NewSessionRepository(d.DB, d.Config.SessionIdleTimeout),
		States:     postgres.NewOAuthStateRepository(d.DB),
		Publisher:  publisher{q: d.Queue},
		Issuer:     issuer,
		UoW:        d.DB,
		Logger:     d.Logger,
		Config: domain.Config{
			SessionMaxAge:      d.Config.SessionMaxAge,
			SessionIdleTimeout: d.Config.SessionIdleTimeout,
			AccessTokenTTL:     d.Config.AccessTokenTTL,
			RefreshGracePeriod: d.Config.RefreshGracePeriod,
			ReauthWindow:       d.Config.ReauthWindow,
		},
	})
	return &Module{
		Service: svc,
		cfg:     d.Config,
		oauth:   wechatRegistry(d.Config.WeChat),
		redis:   d.Redis,
		logger:  d.Logger,
	}
}

// Middleware 返回其它模块保护前台操作用的鉴权中间件。默认验签 → 按 sid 查会话，吊销立即生效；
// 配置了 USER_TRUSTED_IDENTITY_HEADER 时改为信任网关透传的用户 ID（部署前提见
// httpapi.NewTrustedIdentityMiddleware），此时没有会话信息，† 操作一律拒绝。
func (m *Module) Middleware() echo.MiddlewareFunc {
	if m.cfg.TrustedIdentityHeader != "" {
		return httpapi.NewTrustedIdentityMiddleware(m.cfg.TrustedIdentityHeader)
	}
	return httpapi.NewSessionMiddleware(m.Service, m.cfg.JWTKeyID, []byte(m.cfg.JWTSigningKey), m.cfg.JWTAdditionalKeys)
}

// RequireRecentAuth 返回保护 † 操作（改绑登录方式、注销等）的中间件：会话的
// auth_time 要在 USER_REAUTH_WINDOW 之内，挂在 Middleware() 之后。
func (m *Module) RequireRecentAuth() echo.MiddlewareFunc {
	return httpapi.NewRequireRecentAuth(m.cfg.ReauthWindow)
}

// RegisterHTTP 挂载 api/openapi.yaml 中 tag=user 的全部操作。
func (m *Module) RegisterHTTP(g *echo.Group) {
	httpapi.Register(g, httpapi.NewHandler(httpapi.Deps{
		Service:               m.Service,
		OAuth:                 m.oauth,
		PublicBaseURL:         m.cfg.PublicBaseURL,
		CookieDeliveryOrigins: m.cfg.CookieDeliveryOrigin,
	}), httpapi.Middlewares{
		Auth:              m.Middleware(),
		RequireRecentAuth: m.RequireRecentAuth(),
		RateLimit: echoMiddleware.RateLimiterWithConfig(echoMiddleware.RateLimiterConfig{
			Store: ratelimit.NewStore(m.redis, "auth", ratelimit.PerSecond(authRatePerSecond), m.logger),
		}),
	})
}

// authRatePerSecond 是 /v1/auth/* 按 IP 限流的默认值（全局限流之外的一道防线）。
const authRatePerSecond = 5

// RegisterAdminHTTP 挂载 api/admin.yaml 中 tag=users 的全部操作。
func (m *Module) RegisterAdminHTTP(g *echo.Group) {
	adminapi.Register(g, adminapi.NewHandler(m.Service))
}

// RegisterJobs 注册 river worker：过期行清理，以及 user_deleted 的编排 worker
// （消费 user 自己发布的事件，清理其它模块的数据，cleaners 由组合根注入）。
func (m *Module) RegisterJobs(workers *queue.Workers) {
	queue.AddWorker(workers, jobs.NewCleanupWorker(m.Service))
	queue.AddWorker(workers, jobs.NewUserDeletedWorker(m.dataCleaners))
}

// PeriodicJobs 每小时清理一次 oauth_states 和 user_sessions 的过期行，
// 进程启动时先跑一次。
func (m *Module) PeriodicJobs() []queue.Periodic {
	return []queue.Periodic{{
		Schedule: river.PeriodicInterval(time.Hour),
		Args:     jobs.CleanupArgs{},
		Opts:     &river.PeriodicJobOpts{RunOnStart: true},
	}}
}
