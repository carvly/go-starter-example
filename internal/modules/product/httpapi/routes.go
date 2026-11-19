package httpapi

import (
	"github.com/labstack/echo/v5"

	"app.local/internal/modules/product/httpapi/oapi"
)

// Register 挂载 api/openapi.yaml 中 tag=products 的全部操作，都是公开的只读接口。
func Register(g *echo.Group, h *Handler) {
	oapi.RegisterHandlers(g, h)
}
