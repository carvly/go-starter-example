// Package httpserver 构造对外 HTTP 服务：通用中间件链、错误渲染、健康检查、
// API 文档和指标。它不认识任何业务模块，业务路由由组合根经 Deps.Routes 挂载。
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	echoprometheus "github.com/labstack/echo-prometheus"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"app.local/api"
	"app.local/internal/config"
	"app.local/internal/kit/lifecycle"
	"app.local/internal/kit/logctx"
	"app.local/internal/platform/ratelimit"
)

// APIPrefix 是全部业务路由的前缀，与 api/openapi.yaml 的 servers.url 一致。
const APIPrefix = "/v1"

type Deps struct {
	Logger  *slog.Logger
	HTTP    config.HTTPConfig
	CORS    config.CORSConfig
	Pingers []Pinger

	// RateLimitStore 是全局限流的计数存储；为 nil 时用进程内存（多实例下额度会成倍放大）。
	RateLimitStore middleware.RateLimiterStore

	// Routes 由各模块的 RegisterHTTP 组成，挂在 APIPrefix 下。
	Routes []func(g *echo.Group)
}

func healthEndpointSkipper(c *echo.Context) bool {
	switch c.Path() {
	case "/healthz", "/readyz", "/metrics":
		return true
	default:
		return false
	}
}

// New 按固定顺序注册中间件：Recover → RequestID → BodyLimit → CORS → Secure →
// 全局限流 → 指标 → Timeout；鉴权是操作级中间件，由各模块挂载。
func New(deps Deps) (*echo.Echo, error) {
	// CORS 必须排在鉴权/限流前面，否则预检请求会被拦掉。
	corsMw, err := (middleware.CORSConfig{
		Skipper:      healthEndpointSkipper,
		AllowOrigins: deps.CORS.AllowOrigins,
		AllowHeaders: []string{echo.HeaderAuthorization, echo.HeaderContentType},
		// refresh token 按 Origin 交付名单经 cookie 交付（SameSite=Strict），
		// 跨域请求必须允许携带凭据；AllowOrigins 保持精确匹配，不用 "*"。
		AllowCredentials: true,
	}).ToMiddleware()
	if err != nil {
		return nil, fmt.Errorf("configure server: CORS: %w", err)
	}

	e, registry, err := newServer(common{
		Logger:           deps.Logger,
		HTTP:             deps.HTTP,
		RateLimitStore:   deps.RateLimitStore,
		CORS:             corsMw,
		MetricsSubsystem: "echo_starter_kit",
	})
	if err != nil {
		return nil, err
	}

	v1 := e.Group(APIPrefix)
	for _, register := range deps.Routes {
		register(v1)
	}
	RegisterHealthz(e)
	RegisterReadyz(e, deps.Pingers)
	if err := registerAPIDocs(e, deps.HTTP.APIDocsEnabled, "/openapi.yaml", api.OpenAPISpec); err != nil {
		return nil, fmt.Errorf("configure server: API docs: %w", err)
	}
	e.GET("/metrics", echoprometheus.NewHandlerWithConfig(echoprometheus.HandlerConfig{Gatherer: registry}))

	return e, nil
}

// common 是前台和后台 server 共用的中间件链的配置。
type common struct {
	Logger         *slog.Logger
	HTTP           config.HTTPConfig
	RateLimitStore middleware.RateLimiterStore
	// CORS 为 nil 时不挂（后台同源部署，不开 CORS）。
	CORS echo.MiddlewareFunc
	// CSP 为空时不发 Content-Security-Policy。
	CSP              string
	MetricsSubsystem string
	// SkipRateLimit 额外跳过全局限流的请求（健康检查和指标总是跳过）。
	SkipRateLimit func(c *echo.Context) bool
}

// newServer 构造 Echo 并注册共用的中间件链：Recover → RequestID → BodyLimit →
// [CORS] → Secure → 全局限流 → 指标 → Timeout。返回的 Registry 供 /metrics 使用。
func newServer(d common) (*echo.Echo, *prometheus.Registry, error) {
	e := echo.New()
	e.Logger = d.Logger
	e.Validator = newRequestValidator()
	e.HTTPErrorHandler = newHTTPErrorHandler()

	// IPExtractor 必须匹配真实部署拓扑，否则按 IP 的限流可以被伪造 header 绕过。
	if d.HTTP.TrustedProxy {
		e.IPExtractor = echo.ExtractIPFromXFFHeader(echo.TrustLoopback(true), echo.TrustPrivateNet(true))
	} else {
		e.IPExtractor = echo.ExtractIPDirect()
	}

	e.Use(middleware.Recover())

	e.Use(middleware.RequestIDWithConfig(middleware.RequestIDConfig{
		RequestIDHandler: func(c *echo.Context, requestID string) {
			ctx := logctx.WithRequest(c.Request().Context(), d.Logger, requestID)
			c.SetRequest(c.Request().WithContext(ctx))
			c.SetLogger(logctx.From(ctx))
		},
	}))

	e.Use(middleware.BodyLimit(d.HTTP.BodyLimitBytes))

	if d.CORS != nil {
		e.Use(d.CORS)
	}

	secureMw, err := (middleware.SecureConfig{
		XSSProtection:         "1; mode=block",
		ContentTypeNosniff:    "nosniff",
		XFrameOptions:         "SAMEORIGIN",
		HSTSMaxAge:            int(d.HTTP.HSTSMaxAge.Seconds()),
		ContentSecurityPolicy: d.CSP,
	}).ToMiddleware()
	if err != nil {
		return nil, nil, fmt.Errorf("configure server: Secure: %w", err)
	}
	e.Use(secureMw)

	if d.HTTP.GlobalRateLimitPerSecond <= 0 || d.HTTP.RequestTimeout <= 0 {
		return nil, nil, errors.New("configure server: GlobalRateLimitPerSecond and RequestTimeout must be > 0 (validated by config)")
	}
	store := d.RateLimitStore
	if store == nil {
		store = ratelimit.NewStore(nil, "global", ratelimit.PerSecond(d.HTTP.GlobalRateLimitPerSecond), d.Logger)
	}
	rateLimitMw, err := (middleware.RateLimiterConfig{
		Skipper: func(c *echo.Context) bool {
			return healthEndpointSkipper(c) || (d.SkipRateLimit != nil && d.SkipRateLimit(c))
		},
		Store: store,
	}).ToMiddleware()
	if err != nil {
		return nil, nil, fmt.Errorf("configure server: RateLimiter: %w", err)
	}
	e.Use(rateLimitMw)

	// 独立 Registry：同一进程里多次构造 server（测试、前台加后台）用全局 Registry 会重复注册而 panic。
	registry := prometheus.NewRegistry()
	metricsMw, err := (echoprometheus.MiddlewareConfig{
		Subsystem:  d.MetricsSubsystem,
		Registerer: registry,
		// 默认 404 用原始请求路径当 url 标签，扫描器乱扫的路径会各占一条时间序列。
		DoNotUseRequestPathFor404: true,
	}).ToMiddleware()
	if err != nil {
		return nil, nil, fmt.Errorf("configure server: prometheus middleware: %w", err)
	}
	e.Use(metricsMw)

	// Timeout 只让客户端提前拿到超时响应；释放服务端资源要靠 ctx 一路透传。
	timeoutMw, err := (middleware.ContextTimeoutConfig{
		Skipper: healthEndpointSkipper,
		Timeout: d.HTTP.RequestTimeout,
	}).ToMiddleware()
	if err != nil {
		return nil, nil, fmt.Errorf("configure server: ContextTimeout: %w", err)
	}
	e.Use(timeoutMw)

	return e, registry, nil
}

// NewInternal 构造 worker 用的内部 server：只有 /healthz 和 /metrics，
// 绑在不对外发布的内部端口上。
func NewInternal(logger *slog.Logger) *echo.Echo {
	e := echo.New()
	e.Logger = logger
	RegisterHealthz(e)
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))
	return e
}

// Component 把一个 *echo.Echo 包成 lifecycle.Component。ctx 取消时 Echo 自己
// 走优雅关闭（最多 15s），所以 Stop 不需要做事。
func Component(name string, e *echo.Echo, addr string) lifecycle.Component {
	return &serverComponent{name: name, e: e, addr: addr}
}

type serverComponent struct {
	name string
	e    *echo.Echo
	addr string
}

func (s *serverComponent) Name() string { return s.name }

func (s *serverComponent) Start(ctx context.Context) error {
	sc := echo.StartConfig{
		Address:         s.addr,
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: 15 * time.Second,
		OnShutdownError: func(err error) {
			s.e.Logger.Error("graceful shutdown error", "server", s.name, "error", err)
		},
		BeforeServeFunc: func(srv *http.Server) error {
			// 连接级读写超时防慢速连接，与 handler 级的 ContextTimeout 互补。
			srv.ReadTimeout = 30 * time.Second
			srv.WriteTimeout = 30 * time.Second
			return nil
		},
	}
	s.e.Logger.Info("http server listening", "server", s.name, "addr", s.addr)
	return sc.Start(ctx, s.e)
}

func (s *serverComponent) Stop(context.Context) error { return nil }
