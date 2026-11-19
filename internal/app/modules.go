package app

import (
	"github.com/labstack/echo/v5"
	"github.com/riverqueue/river"

	"app.local/internal/modules/auth"
	"app.local/internal/modules/fulfillment"
	"app.local/internal/modules/order"
	"app.local/internal/modules/product"
	"app.local/internal/modules/staff"
	"app.local/internal/platform/cli"
	"app.local/internal/platform/queue"
)

// 新增或删除一个模块只改这个文件。约定：每个模块只占 import 一行、Modules 字段
// 一行、buildModules 里构造一行（变量名固定为 <模块>M，构造参数里引用了谁就依赖谁）、
// 返回值一行、注册切片里各一行。scripts/check-module-removal.sh 按这个约定逐行删除
// 来演练"删掉一个模块"，internal/app 的测试检查每个模块目录都在这里注册过。

var moduleRequires = map[string][]string{
	"order": {"fulfillment"},
}

type Modules struct {
	Product     *product.Module
	Order       *order.Module
	Fulfillment *fulfillment.Module
	Staff       *staff.Module
	Auth        *auth.Module
}

func buildModules(in *Infra, configs moduleConfigs) *Modules {
	productM := product.New(product.Deps{DB: in.DBTx, Redis: in.Redis, Logger: in.Logger})
	orderM := order.New(order.Deps{DB: in.DBTx, Queue: in.Queue, Products: productLookup{productM.Service}})
	fulfillmentM := fulfillment.New(fulfillment.Deps{UoW: in.DBTx, Ledger: orderM.EventLedger(), Stock: stockReserver{productM.Service}, Orders: orderM.Service})
	staffM := staff.New(staff.Deps{DB: in.DBTx, Config: configs.Staff, Argon2: in.Config.Argon2, Operations: adminOperations(), Logger: in.Logger})
	authM := auth.New(auth.Deps{DB: in.DBTx, Config: configs.Auth, Queue: in.Queue, Redis: in.Redis, Logger: in.Logger})
	return &Modules{
		Product:     productM,
		Order:       orderM,
		Fulfillment: fulfillmentM,
		Staff:       staffM,
		Auth:        authM,
	}
}

func (m *Modules) httpRegistrars() []func(*echo.Group) {
	return []func(*echo.Group){
		m.Product.RegisterHTTP,
		m.Order.RegisterHTTP,
		m.Auth.RegisterHTTP,
	}
}

// adminRegistrars 是有后台操作的模块（RegisterAdminHTTP，挂在 /admin/v1 下）。
func (m *Modules) adminRegistrars() []func(*echo.Group) {
	return []func(*echo.Group){
		m.Product.RegisterAdminHTTP,
		m.Order.RegisterAdminHTTP,
		m.Staff.RegisterAdminHTTP,
		m.Auth.RegisterAdminHTTP,
	}
}

// adminAuth 是后台的会话与权限检查，由提供员工账号的模块实现。为空时，只要有后台路由，
// 后台 server 就拒绝启动；删除演练删掉提供它的模块时，后台接入面整体去掉。
func (m *Modules) adminAuth() []echo.MiddlewareFunc {
	return []echo.MiddlewareFunc{
		m.Staff.AdminGuard(),
	}
}

func (m *Modules) jobRegistrars() []func(*river.Workers) {
	return []func(*river.Workers){
		m.Fulfillment.RegisterJobs,
		m.Staff.RegisterJobs,
		m.Auth.RegisterJobs,
	}
}

func jobKindRegistrars() []func() []string {
	return []func() []string{
		fulfillment.JobKinds,
		staff.JobKinds,
		auth.JobKinds,
	}
}

func workerKinds() []string {
	var out []string
	for _, register := range jobKindRegistrars() {
		out = append(out, register()...)
	}
	return out
}

func (m *Modules) commandRegistrars() []func() []cli.Command {
	return []func() []cli.Command{
		m.Staff.Commands,
	}
}

func (m *Modules) periodicJobRegistrars() []func() []queue.Periodic {
	return []func() []queue.Periodic{
		m.Staff.PeriodicJobs,
		m.Auth.PeriodicJobs,
	}
}

// 持有用户数据的模块在全部模块构造完成后登记清理器，每个登记独占一行：
// authM.AddUserDataCleaner(memberM.UserDataCleaner())。
// 登记行不产生构造依赖；模块经公开资料查询做每日清扫时才真实依赖 auth。
