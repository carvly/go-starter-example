package httpapi

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
)

// 浏览器授权流程的绑定 cookie 名是 oauth_b_<state 前 8 位>，同时进行的多个流程
// 互不覆盖；refresh token cookie 只在 /v1/auth/session 下发送（见 docs/agents/auth.md 的"会话"）。
const (
	bindingCookiePrefix = "oauth_b_"
	refreshCookieName   = "refresh_token"
	bindingCookiePath   = "/v1/auth/oauth"
	refreshCookiePath   = "/v1/auth/session"
)

func bindingCookieName(statePrefix8 string) string {
	return bindingCookiePrefix + statePrefix8
}

func setBindingCookie(c *echo.Context, statePrefix8, value string, expires time.Duration) {
	setCookie(c, bindingCookieName(statePrefix8), value, expires)
}

func clearBindingCookie(c *echo.Context, statePrefix8 string) {
	clearCookie(c, bindingCookieName(statePrefix8), bindingCookiePath)
}

func bindingCookieValue(c *echo.Context, statePrefix8 string) (string, bool) {
	return cookieValue(c, bindingCookieName(statePrefix8))
}

// setRefreshCookie 把 refresh token 放进 httpOnly + Secure + SameSite=Strict 的
// cookie，Path 限到 /v1/auth/session，页面 JS 接触不到。
func setRefreshCookie(c *echo.Context, value string, expiresAt time.Time) {
	cookie := refreshCookie(value, expiresAt)
	c.SetCookie(cookie)
}

func clearRefreshCookie(c *echo.Context) {
	c.SetCookie(refreshCookie("", time.Unix(0, 0)))
}

func refreshCookieValue(c *echo.Context) (string, bool) {
	return cookieValue(c, refreshCookieName)
}

func refreshCookie(value string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     refreshCookieName,
		Value:    value,
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Expires:  expiresAt,
	}
}

func setCookie(c *echo.Context, name, value string, maxAge time.Duration) {
	c.SetCookie(&http.Cookie{
		Name:     name,
		Value:    value,
		Path:     bindingCookiePath,
		HttpOnly: true,
		Secure:   true,
		// 第三方跳回是跨站请求（Apple 的跨站 POST 连 Lax 都不会带），必须 None。
		SameSite: http.SameSiteNoneMode,
		MaxAge:   int(maxAge.Seconds()),
	})
}

func clearCookie(c *echo.Context, name, path string) {
	c.SetCookie(&http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
		MaxAge:   -1,
	})
}

func cookieValue(c *echo.Context, name string) (string, bool) {
	cookie, err := c.Cookie(name)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}
