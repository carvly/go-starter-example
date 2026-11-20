// Package httpapi 是 user 的前台 HTTP 适配器（微信登录、会话、账号）。
// handler 只做 Bind → Validate → 调 Service/编排 → 序列化；浏览器 OAuth 流程的
// 编排（state、绑定 cookie、跳转）在 flow.go。
package httpapi

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"app.local/internal/kit/secrand"
	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/httpapi/oapi"
	"app.local/internal/platform/httpserver"
	"app.local/internal/platform/oauth"
)

type Handler struct {
	service       *domain.Service
	oauth         oauth.Registry
	publicBaseURL string
	cookieOrigins map[string]bool
}

type Deps struct {
	Service *domain.Service
	// OAuth 是路由 provider 到实现的注册表（module.go 用微信配置构造）。
	OAuth                 oauth.Registry
	PublicBaseURL         string
	CookieDeliveryOrigins []string
}

func NewHandler(d Deps) *Handler {
	origins := make(map[string]bool, len(d.CookieDeliveryOrigins))
	for _, o := range d.CookieDeliveryOrigins {
		origins[o] = true
	}
	return &Handler{service: d.Service, oauth: d.OAuth, publicBaseURL: d.PublicBaseURL, cookieOrigins: origins}
}

// toExternalIdentity 把 oauth.Identity 转换成 domain 的中立类型。
func toExternalIdentity(v oauth.Identity) domain.ExternalIdentity {
	ext := domain.ExternalIdentity{
		Provider:  v.Provider,
		Subject:   v.Subject,
		Name:      v.Name,
		AvatarURL: v.AvatarURL,
	}
	if v.WeChat != nil {
		ext.WeChatAppID = v.WeChat.AppID
		ext.WeChatOpenID = v.WeChat.OpenID
	}
	return ext
}

func hashCookieValue(v string) string {
	return secrand.Hash(v)
}

// newBindingValue 生成浏览器绑定 cookie 的值（随机串，摘要入库）。
func newBindingValue() string {
	return secrand.URL(32)
}

func errUnknownProvider(provider string) error {
	return echo.NewHTTPError(http.StatusBadRequest, "unknown provider: "+provider)
}

func (h *Handler) redirectURI(provider string) string {
	return h.publicBaseURL + "/v1/auth/oauth/" + provider + "/callback"
}

// AuthorizeOAuth 发起浏览器登录授权：写一次性 state + 绑定 cookie，整页跳转到
// 第三方授权页。
func (h *Handler) AuthorizeOAuth(c *echo.Context, provider oapi.AuthorizeOAuthParamsProvider, params oapi.AuthorizeOAuthParams) error {
	entry, ok := h.oauth.Browser(string(provider))
	if !ok {
		return errUnknownProvider(string(provider))
	}
	u, err := h.authorizeURL(c, string(provider), entry, domain.IntentLogin, "", deref(params.ReturnTo))
	if err != nil {
		return err
	}
	return c.Redirect(http.StatusFound, u)
}

// OauthCallback 是第三方授权回调（GET），流程编排见 flow.go。
func (h *Handler) OauthCallback(c *echo.Context, provider string, _ oapi.OauthCallbackParams) error {
	return h.handleOAuthCallback(c, provider, c.QueryParams())
}

// OauthCallbackForm 是 response_mode=form_post 的回调（跨站表单 POST），和 GET 回调走同一个编排。
func (h *Handler) OauthCallbackForm(c *echo.Context, provider string) error {
	form, err := c.FormValues()
	if err != nil || form.Get("state") == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid callback form")
	}
	return h.handleOAuthCallback(c, provider, form)
}

// IssueNativeNonce 为要求 nonce 的原生 provider 签发一次性 nonce；其它 provider 返回 400，不白写一行。
func (h *Handler) IssueNativeNonce(c *echo.Context, provider string) error {
	entry, ok := h.oauth.NativeEntry(provider)
	if !ok {
		return errUnknownProvider(provider)
	}
	if !entry.Native.Capabilities().Nonce {
		return echo.NewHTTPError(http.StatusBadRequest, "provider does not use nonce: "+provider)
	}
	nonce, err := h.service.IssueNativeNonce(c.Request().Context(), provider)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, oapi.NativeNonceResponse{Nonce: nonce})
}

// ExchangeOAuth 是浏览器登录的最后一步：前端用回调带回的一次性兑换码发同站
// 请求，校验绑定 cookie 后完成登录并交付会话。
func (h *Handler) ExchangeOAuth(c *echo.Context) error {
	var body oapi.ExchangeRequest
	if err := c.Bind(&body); err != nil {
		return err
	}
	if err := c.Validate(&body); err != nil {
		return err
	}
	ctx := c.Request().Context()

	st, err := h.service.TakeOAuthResult(ctx, body.Code)
	if err != nil {
		// 兑换码无效、已用过或已过期：和回调侧的 state 一样按 400 处理。
		return echo.NewHTTPError(http.StatusBadRequest, "invalid or expired exchange code")
	}
	if err := h.checkBindingCookie(c, domain.ExchangeCodePrefix(body.Code), st.BindingHash); err != nil {
		return err
	}
	// 兑换完成，清除绑定 cookie。
	clearBindingCookie(c, domain.ExchangeCodePrefix(body.Code))
	if st.Identity == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid exchange code")
	}

	tokens, err := h.service.Login(ctx, "web", "", c.RealIP(), c.Request().UserAgent(), *st.Identity)
	if err != nil {
		return err
	}
	if tokens.ReturnTo == "" {
		tokens.ReturnTo = st.ReturnTo
	}
	return h.respondWithTokens(c, tokens)
}

// NativeLogin 是小程序 wx.login / App SDK 授权后的服务端校验登录。
func (h *Handler) NativeLogin(c *echo.Context, provider oapi.NativeLoginParamsProvider) error {
	var body oapi.NativeLoginRequest
	if err := c.Bind(&body); err != nil {
		return err
	}
	if err := c.Validate(&body); err != nil {
		return err
	}
	ext, err := h.verifyNative(c, string(provider), nativeInput{Code: body.Code, IDToken: body.IdToken, Nonce: body.Nonce, Name: body.Name})
	if err != nil {
		return err
	}
	tokens, err := h.service.Login(c.Request().Context(), string(body.Client), deref(body.DeviceName), c.RealIP(), c.Request().UserAgent(), ext)
	if err != nil {
		return err
	}
	return h.respondWithTokens(c, tokens)
}

func (h *Handler) refreshTokenFrom(c *echo.Context, bodyToken *string) string {
	if v, ok := refreshCookieValue(c); ok {
		return v
	}
	if bodyToken != nil {
		return *bodyToken
	}
	return ""
}

// RefreshSession 轮换 refresh token。Origin 在名单内时同时更新 httpOnly cookie。
func (h *Handler) RefreshSession(c *echo.Context) error {
	var body oapi.RefreshRequest
	if err := c.Bind(&body); err != nil {
		return err
	}
	tokens, err := h.service.Refresh(c.Request().Context(), h.refreshTokenFrom(c, body.RefreshToken))
	if err != nil {
		return err
	}
	return h.respondWithTokens(c, tokens)
}

// Logout 只认 refresh token：吊销会话并清除 cookie。
func (h *Handler) Logout(c *echo.Context) error {
	var body oapi.LogoutRequest
	if err := c.Bind(&body); err != nil {
		return err
	}
	if err := h.service.Logout(c.Request().Context(), h.refreshTokenFrom(c, body.RefreshToken)); err != nil {
		return err
	}
	clearRefreshCookie(c)
	return c.NoContent(http.StatusNoContent)
}

// UpdateMe 修改昵称和头像。
func (h *Handler) UpdateMe(c *echo.Context) error {
	var body oapi.UpdateMeRequest
	if err := c.Bind(&body); err != nil {
		return err
	}
	if err := c.Validate(&body); err != nil {
		return err
	}
	s, err := httpserver.UserSessionFrom(c)
	if err != nil {
		return err
	}
	user, err := h.service.UpdateMe(c.Request().Context(), s.UserID, body.DisplayName, body.AvatarUrl)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toUser(user))
}

func (h *Handler) ListMyIdentities(c *echo.Context, params oapi.ListMyIdentitiesParams) error {
	s, err := httpserver.UserSessionFrom(c)
	if err != nil {
		return err
	}
	idns, next, err := h.service.ListMyIdentities(c.Request().Context(), s.UserID, deref(params.Cursor), deref(params.Limit))
	if err != nil {
		return err
	}
	resp := oapi.IdentityList{Items: make([]oapi.Identity, 0, len(idns))}
	for _, v := range idns {
		resp.Items = append(resp.Items, toIdentity(v))
	}
	if next != "" {
		resp.NextCursor = &next
	}
	return c.JSON(http.StatusOK, resp)
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// BindIdentityOAuth 发起浏览器绑定：返回 authorize_url，回调里完成绑定。
func (h *Handler) BindIdentityOAuth(c *echo.Context, provider oapi.BindIdentityOAuthParamsProvider) error {
	return h.startBoundFlow(c, string(provider), domain.IntentBind)
}

// BindIdentityNative 是原生 SDK 绑定，同步完成。
func (h *Handler) BindIdentityNative(c *echo.Context, provider oapi.BindIdentityNativeParamsProvider) error {
	var body oapi.NativeBindRequest
	if err := c.Bind(&body); err != nil {
		return err
	}
	if err := c.Validate(&body); err != nil {
		return err
	}
	s, err := sessionBound(c)
	if err != nil {
		return err
	}
	ext, err := h.verifyNative(c, string(provider), nativeInput{Code: body.Code, IDToken: body.IdToken, Nonce: body.Nonce})
	if err != nil {
		return err
	}
	bound, err := h.service.BindIdentity(c.Request().Context(), s.SessionID, ext)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toIdentity(bound))
}

// UnbindIdentity 解绑登录方式（†）。
func (h *Handler) UnbindIdentity(c *echo.Context, id oapi.ID) error {
	s, err := httpserver.UserSessionFrom(c)
	if err != nil {
		return err
	}
	if err := h.service.UnbindIdentity(c.Request().Context(), s.UserID, id.String(), s.SessionID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// ReauthenticateOAuth 发起浏览器重新验证：返回 authorize_url，回调里更新 auth_time。
func (h *Handler) ReauthenticateOAuth(c *echo.Context, provider oapi.ReauthenticateOAuthParamsProvider) error {
	return h.startBoundFlow(c, string(provider), domain.IntentReauth)
}

// ReauthenticateNative 是原生 SDK 重新验证：凭证必须属于当前账号。
func (h *Handler) ReauthenticateNative(c *echo.Context, provider oapi.ReauthenticateNativeParamsProvider) error {
	var body oapi.NativeBindRequest
	if err := c.Bind(&body); err != nil {
		return err
	}
	if err := c.Validate(&body); err != nil {
		return err
	}
	s, err := sessionBound(c)
	if err != nil {
		return err
	}
	ext, err := h.verifyNative(c, string(provider), nativeInput{Code: body.Code, IDToken: body.IdToken, Nonce: body.Nonce})
	if err != nil {
		return err
	}
	if err := h.service.Reauthenticate(c.Request().Context(), s.SessionID, ext); err != nil {
		return err
	}
	return c.NoContent(http.StatusOK)
}

func (h *Handler) ListMySessions(c *echo.Context, params oapi.ListMySessionsParams) error {
	s, err := httpserver.UserSessionFrom(c)
	if err != nil {
		return err
	}
	sessions, next, err := h.service.ListMySessions(c.Request().Context(), s.UserID, deref(params.Cursor), deref(params.Limit))
	if err != nil {
		return err
	}
	resp := oapi.SessionList{Items: make([]oapi.Session, 0, len(sessions))}
	for _, v := range sessions {
		resp.Items = append(resp.Items, toSession(v, v.ID == s.SessionID))
	}
	if next != "" {
		resp.NextCursor = &next
	}
	return c.JSON(http.StatusOK, resp)
}

// RevokeMySession 是当前账号踢下线自己的一个会话。
func (h *Handler) RevokeMySession(c *echo.Context, id oapi.ID) error {
	s, err := httpserver.UserSessionFrom(c)
	if err != nil {
		return err
	}
	if err := h.service.RevokeMySession(c.Request().Context(), s.UserID, id.String()); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// DeleteMe 注销账号（†）：吊销全部会话、删除登录方式和 openid、发布 user_deleted。
func (h *Handler) DeleteMe(c *echo.Context) error {
	s, err := httpserver.UserSessionFrom(c)
	if err != nil {
		return err
	}
	if err := h.service.DeleteMe(c.Request().Context(), s.UserID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// checkBindingCookie 校验绑定 cookie：没有 cookie、或摘要和流程行里登记的不一致，
// 就是登录 CSRF（别人把自己的回调链接发给了受害者），拒绝。
func (h *Handler) checkBindingCookie(c *echo.Context, statePrefix8 string, want *string) error {
	v, ok := bindingCookieValue(c, statePrefix8)
	if !ok || want == nil || *want != hashCookieValue(v) {
		return echo.NewHTTPError(http.StatusBadRequest, "oauth flow not bound to this browser")
	}
	return nil
}

// respondWithTokens 按请求来源交付 refresh token（见 docs/agents/auth.md 的"会话"）：Origin
// 在 cookie 交付名单内 → 只进 httpOnly cookie，响应体不含；否则放响应体，客户端
// 存进系统安全存储。
func (h *Handler) respondWithTokens(c *echo.Context, tokens *domain.IssuedTokens) error {
	expiresIn := int64(time.Until(tokens.AccessExpiresAt).Seconds())
	resp := oapi.TokenResponse{
		AccessToken:     tokens.AccessToken,
		ExpiresIn:       expiresIn,
		RefreshDelivery: oapi.Body,
	}
	if tokens.ReturnTo != "" {
		resp.ReturnTo = &tokens.ReturnTo
	}
	if h.cookieOrigins[c.Request().Header.Get("Origin")] {
		setRefreshCookie(c, tokens.RefreshToken, tokens.Session.ExpiresAt)
		resp.RefreshDelivery = oapi.Cookie
	} else {
		rt := tokens.RefreshToken
		resp.RefreshToken = &rt
	}
	return c.JSON(http.StatusOK, resp)
}

func toUser(v *domain.User) oapi.User {
	return oapi.User{
		Id:          v.ID,
		DisplayName: v.DisplayName,
		AvatarUrl:   v.AvatarUrl,
		CreatedAt:   v.CreatedAt,
		UpdatedAt:   v.UpdatedAt,
	}
}

func toIdentity(v *domain.Identity) oapi.Identity {
	return oapi.Identity{
		Id:          v.ID,
		Provider:    oapi.IdentityProvider(v.Provider),
		LastLoginAt: v.LastLoginAt,
		CreatedAt:   v.CreatedAt,
	}
}

func toSession(v *domain.Session, current bool) oapi.Session {
	return oapi.Session{
		Id:         v.ID,
		Client:     oapi.SessionClient(v.Client),
		DeviceName: v.DeviceName,
		Ip:         &v.IP,
		UserAgent:  &v.UserAgent,
		CreatedAt:  v.CreatedAt,
		LastUsedAt: v.LastUsedAt,
		ExpiresAt:  v.ExpiresAt,
		Current:    current,
	}
}

var _ oapi.ServerInterface = (*Handler)(nil)
