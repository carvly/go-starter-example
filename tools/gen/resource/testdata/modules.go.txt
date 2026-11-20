package app

import (
	"github.com/labstack/echo/v5"

	"app.local/internal/modules/staff"
	"app.local/internal/modules/user"
	"app.local/internal/platform/cli"
	"app.local/internal/platform/queue"
)

// 新增或删除一个模块改组合根的 modules.go（本文件）、migrate.go、module_configs.go 三个文件（跨模块调用
// 另加 adapters_*.go）。约定：每个模块只占 import 一行、Modules 字段一行、buildModules 里构造一行（变量名
// 固定为 <模块>M，构造参数里引用了谁就依赖谁）、返回值一行、注册切片里各一行。tools/starter 的删除执行器
// （modules_go.go）按这个约定逐行删除，internal/app 的测试检查每个模块目录都在这里注册过。
//
// 构造行推导不出的依赖写在可选的 moduleRequires 里，例如 order 的事件只有 fulfillment 消费，删掉
// fulfillment 时 order 也要删：var moduleRequires = map[string][]string{"order": {"fulfillment"}}

type Modules struct {
	Staff *staff.Module
	User  *user.Module
}

func buildModules(in *Infra, configs moduleConfigs) *Modules {
	staffM := staff.New(staff.Deps{DB: in.DBTx, Config: configs.Staff, Argon2: in.Config.Argon2, Operations: adminOperations(), Logger: in.Logger})
	userM := user.New(user.Deps{DB: in.DBTx, Config: configs.User, Queue: in.Queue, Redis: in.Redis, Logger: in.Logger})
	return &Modules{
		Staff: staffM,
		User:  userM,
	}
}

func (m *Modules) httpRegistrars() []func(*echo.Group) {
	return []func(*echo.Group){
		m.User.RegisterHTTP,
	}
}

// adminRegistrars 是有后台操作的模块（RegisterAdminHTTP，挂在 /admin/v1 下）。
func (m *Modules) adminRegistrars() []func(*echo.Group) {
	return []func(*echo.Group){
		m.Staff.RegisterAdminHTTP,
		m.User.RegisterAdminHTTP,
	}
}

// adminAuth 是后台的会话与权限检查，由提供员工账号的模块实现。为空时，只要有后台路由，
// 后台 server 就拒绝启动；删除演练删掉提供它的模块时，后台接入面整体去掉。
func (m *Modules) adminAuth() []echo.MiddlewareFunc {
	return []echo.MiddlewareFunc{
		m.Staff.AdminGuard(),
	}
}

func (m *Modules) jobRegistrars() []func(*queue.Workers) {
	return []func(*queue.Workers){
		m.Staff.RegisterJobs,
		m.User.RegisterJobs,
	}
}

func (m *Modules) commandRegistrars() []func() []cli.Command {
	return []func() []cli.Command{
		m.Staff.Commands,
	}
}

func (m *Modules) periodicJobRegistrars() []func() []queue.Periodic {
	return []func() []queue.Periodic{
		m.Staff.PeriodicJobs,
		m.User.PeriodicJobs,
	}
}

// 持有用户数据的模块在全部模块构造完成后登记清理器，每个登记独占一行：
// userM.AddDataCleaner(memberM.UserDataCleaner())。
// 登记行不产生构造依赖；模块经公开资料查询做每日清扫时才真实依赖 user。
