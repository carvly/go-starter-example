package httpapi

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"app.local/internal/platform/httpserver"
)

// DefaultTrustedIdentityHeader 是 NewTrustedIdentityMiddleware 在
// headerName 留空时使用的默认 header 名。
const DefaultTrustedIdentityHeader = "X-User-ID"

// NewTrustedIdentityMiddleware 是 NewSessionMiddleware 的并列替代：服务部署在已经
// 校验过身份的网关后面，网关把用户 ID 放进约定的 header 透传过来。
//
// 这个中间件本身不做任何签名校验，安全性完全依赖部署拓扑：
//   - 客户端必须无法绕过网关直连本服务（防火墙/服务网格 mTLS 只放行网关）；
//     否则任何人都能自己带上这个 header 冒充任意用户；
//   - 网关转发前必须先清掉客户端带来的同名 header，只保留自己校验后设置的值。
//
// 配错不会在代码层面报错，只会让鉴权悄悄失效，上线前必须确认前提成立。
func NewTrustedIdentityMiddleware(headerName string) echo.MiddlewareFunc {
	// 只有空白的 header 名永远匹配不到任何 header，会让所有请求 401 且很难排查。
	headerName = strings.TrimSpace(headerName)
	if headerName == "" {
		headerName = DefaultTrustedIdentityHeader
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			// 空白身份不能被当成合法用户放行。
			userID := strings.TrimSpace(c.Request().Header.Get(headerName))
			if userID == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing "+headerName+" header")
			}
			httpserver.SetUserSession(c, httpserver.UserSession{UserID: userID})
			return next(c)
		}
	}
}
