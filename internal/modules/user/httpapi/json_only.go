package httpapi

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
)

// NewJSONOnly 限定带请求体的写操作只接受 application/json（见 docs/agents/auth.md 的"登录接口的防护"，
// 挂在哪些操作上见 routes.go）：跨站页面只能发不触发预检的表单请求，限定 JSON 后它们到不了
// handler。Apple 的 form_post 回调是第三期的事，到那一步时这个中间件不挂在
// 回调路由上。
func NewJSONOnly() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			switch req.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				if req.ContentLength != 0 && !strings.HasPrefix(req.Header.Get(echo.HeaderContentType), "application/json") {
					return echo.NewHTTPError(http.StatusUnsupportedMediaType, "content type must be application/json")
				}
			}
			return next(c)
		}
	}
}
