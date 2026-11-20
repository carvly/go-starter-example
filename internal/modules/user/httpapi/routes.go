package httpapi

import (
	"github.com/labstack/echo/v5"

	"app.local/internal/modules/user/httpapi/oapi"
)

// Middlewares 是 Register 需要的操作级中间件：Auth 是会话校验，RequireRecentAuth
// 保护 † 操作，RateLimit 是 /v1/auth/* 的按 IP 限流（全局限流之外的一道）。
type Middlewares struct {
	Auth              echo.MiddlewareFunc
	RequireRecentAuth echo.MiddlewareFunc
	RateLimit         echo.MiddlewareFunc
}

// Register 挂载 api/openapi.yaml 中 tag=user 的全部操作。公开操作（登录、回调、
// 兑换、刷新、登出）不挂 Auth，但和 /v1/auth/* 下其它操作一样挂 RateLimit；
// † 操作在 Auth 之后再挂 RequireRecentAuth。
func Register(g *echo.Group, h *Handler, mw Middlewares) {
	// 带请求体的操作只接受 application/json（部署约束）；没有请求体的操作不挂。
	jsonOnly := NewJSONOnly()
	oapi.RegisterHandlersWithOptions(g, h, oapi.RegisterHandlersOptions{
		OperationMiddlewares: map[string][]echo.MiddlewareFunc{
			// 登录与会话：未登录可用，按 IP 限流。
			"authorizeOAuth": {mw.RateLimit, jsonOnly},
			"oauthCallback":  {mw.RateLimit, jsonOnly},
			// 唯一不限定 JSON 的写操作：response_mode=form_post 的跨站表单回调，防护由一次性 state + 绑定 cookie 承担。
			"oauthCallbackForm": {mw.RateLimit},
			"issueNativeNonce":  {mw.RateLimit, jsonOnly},
			"exchangeOAuth":     {mw.RateLimit, jsonOnly},
			"nativeLogin":       {mw.RateLimit, jsonOnly},
			"refreshSession":    {mw.RateLimit, jsonOnly},
			"logout":            {mw.RateLimit, jsonOnly},
			// 已登录。
			"updateMe":             {jsonOnly, mw.Auth},
			"listMyIdentities":     {mw.Auth},
			"listMySessions":       {mw.Auth},
			"revokeMySession":      {mw.Auth},
			"reauthenticateOAuth":  {mw.Auth},
			"reauthenticateNative": {jsonOnly, mw.Auth},
			// †：要求最近重新验证过身份。
			"bindIdentityOAuth":  {mw.Auth, mw.RequireRecentAuth},
			"bindIdentityNative": {jsonOnly, mw.Auth, mw.RequireRecentAuth},
			"unbindIdentity":     {mw.Auth, mw.RequireRecentAuth},
			"deleteMe":           {mw.Auth, mw.RequireRecentAuth},
		},
	})
}
