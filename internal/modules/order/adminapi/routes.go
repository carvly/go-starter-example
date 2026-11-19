package adminapi

import (
	"github.com/labstack/echo/v5"

	"app.local/internal/modules/order/adminapi/oapi"
)

// Register 挂载 api/admin.yaml 中 tag=orders 的全部操作。登录和权限检查由后台 server
// 分组上的鉴权中间件按 x-permission 统一做，这里没有操作级中间件。
func Register(g *echo.Group, h *Handler) {
	oapi.RegisterHandlers(g, h)
}
