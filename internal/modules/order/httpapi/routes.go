package httpapi

import (
	"github.com/labstack/echo/v5"

	"app.local/internal/modules/order/httpapi/oapi"
)

// Register 挂载 tag=orders 的全部操作。演示里下单和查询都不要求登录，所以
// 没有操作级中间件；需要时按 operationId 在 OperationMiddlewares 里声明。
func Register(g *echo.Group, h *Handler) {
	oapi.RegisterHandlersWithOptions(g, h, oapi.RegisterHandlersOptions{})
}
