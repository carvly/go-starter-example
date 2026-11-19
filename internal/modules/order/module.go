// Package order 是订单模块。module.go 是它对组合根暴露的唯一入口。
package order

import (
	"github.com/labstack/echo/v5"

	"app.local/internal/modules/order/adminapi"
	"app.local/internal/modules/order/domain"
	"app.local/internal/modules/order/httpapi"
	"app.local/internal/modules/order/postgres"
	"app.local/internal/platform/dbtx"
	"app.local/internal/platform/queue"
)

type Deps struct {
	DB    *dbtx.Handle
	Queue *queue.Inserter
	// Products 由 internal/app/adapters.go 提供。
	Products domain.ProductLookup
}

type Module struct {
	Service *domain.Service
	repo    domain.Repository
}

func New(d Deps) *Module {
	repo := postgres.NewRepository(d.DB)
	return &Module{
		Service: domain.NewService(repo, d.Products, publisher{q: d.Queue}, d.DB),
		repo:    repo,
	}
}

// RegisterHTTP 挂载 api/openapi.yaml 中 tag=orders 的全部操作。
func (m *Module) RegisterHTTP(g *echo.Group) {
	httpapi.Register(g, httpapi.NewHandler(m.Service))
}

// RegisterAdminHTTP 挂载 api/admin.yaml 中 tag=orders 的全部操作（只读）。
func (m *Module) RegisterAdminHTTP(g *echo.Group) {
	adminapi.Register(g, adminapi.NewHandler(m.Service))
}

// EventLedger 只把事件去重能力暴露给 fulfillment，不暴露整个 Repository。
func (m *Module) EventLedger() domain.EventLedger { return m.repo }
