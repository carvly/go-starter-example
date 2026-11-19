// Package product 是商品模块。module.go 是它对组合根暴露的唯一入口。
package product

import (
	"log/slog"

	"github.com/labstack/echo/v5"
	goredis "github.com/redis/go-redis/v9"

	"app.local/internal/modules/product/adminapi"
	"app.local/internal/modules/product/domain"
	"app.local/internal/modules/product/httpapi"
	"app.local/internal/modules/product/postgres"
	"app.local/internal/platform/cache"
	"app.local/internal/platform/dbtx"
)

type Deps struct {
	DB *dbtx.Handle
	// Redis 为 nil 时不缓存商品（NoopCache）。
	Redis  *goredis.Client
	Logger *slog.Logger
}

type Module struct {
	Service *domain.Service
}

func New(d Deps) *Module {
	var c domain.Cache = cache.NewNoopCache[domain.Product]()
	if d.Redis != nil {
		c = cache.NewRedisCache[domain.Product](d.Redis, "product", d.Logger)
	}
	return &Module{
		Service: domain.NewService(postgres.NewRepository(d.DB), domain.WithCache(c)),
	}
}

// RegisterHTTP 挂载 api/openapi.yaml 中 tag=products 的全部操作（只读）。
func (m *Module) RegisterHTTP(g *echo.Group) {
	httpapi.Register(g, httpapi.NewHandler(m.Service))
}

// RegisterAdminHTTP 挂载 api/admin.yaml 中 tag=products 的全部操作。
func (m *Module) RegisterAdminHTTP(g *echo.Group) {
	adminapi.Register(g, adminapi.NewHandler(m.Service))
}
