package httpapi

import (
	"time"

	"github.com/labstack/echo/v5"

	"app.local/internal/modules/user/domain"
	"app.local/internal/platform/httpserver"
)

// NewRequireRecentAuth 保护 † 操作（绑定/解绑、注销等）：要求会话的 auth_time
// 在 window 之内。网关信任 header 模式没有服务端会话，返回 ErrSessionUnavailable（fail closed，且不让客户端
// 反复去重新验证）。成功的重新验证由 /v1/me/reauthenticate
// 更新 auth_time，不重新签发 token。
func NewRequireRecentAuth(window time.Duration) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			s, err := sessionBound(c)
			if err != nil {
				return err
			}
			if s.AuthTime.IsZero() || time.Since(s.AuthTime) > window {
				return domain.ErrReauthRequired
			}
			return next(c)
		}
	}
}

// sessionBound 取当前用户的服务端会话；网关信任 header 模式下没有会话（SessionID 为空），依赖会话的操作
// 返回明确的 ErrSessionUnavailable，而不是让客户端去重新验证。
func sessionBound(c *echo.Context) (httpserver.UserSession, error) {
	s, err := httpserver.UserSessionFrom(c)
	if err != nil {
		return s, err
	}
	if s.SessionID == "" {
		return s, domain.ErrSessionUnavailable
	}
	return s, nil
}
