package httpapi

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v5"

	"app.local/internal/kit/logctx"
	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/httpapi/oapi"
	"app.local/internal/platform/oauth"
)

// logFailedVerification 记录第三方凭证校验失败；对客户端一律是登录失败。ErrNoUnionID 是配置问题
// （应用没绑定到开放平台），按错误级别记录以便报警。
func logFailedVerification(c *echo.Context, provider string, err error) {
	ctx := c.Request().Context()
	log := logctx.From(ctx)
	if errors.Is(err, oauth.ErrNoUnionID) {
		log.ErrorContext(ctx, "user: third-party identity has no unionid, check open platform binding", "provider", provider, "error", err)
		return
	}
	log.WarnContext(ctx, "user: third-party credential verification failed", "provider", provider, "error", err)
}

// 回调跳回前端的固定路径；result 取值 ok 或错误码。
const completePath = "/auth/complete"

func completeURL(intent domain.OAuthFlowIntent, result string) string {
	q := url.Values{}
	q.Set("intent", string(intent))
	q.Set("result", result)
	return completePath + "?" + q.Encode()
}

// handleOAuthCallback 是浏览器授权回调的编排（见 docs/agents/auth.md 的"登录入口"），GET 和表单 POST
// 共用：一次性取 state → 校验绑定 cookie → 用 state 里保存的 verifier/nonce 和回调参数换身份 → 按 intent
// 分发。登录 intent 把结果写成一次性兑换（60 秒）再跳回前端；绑定/重新验证 intent 在这里直接完成。
// 任何失败都跳回前端 /auth/complete?result=<错误码>，只有 state 本身无效时才直接 400（无处可跳）。
func (h *Handler) handleOAuthCallback(c *echo.Context, provider string, callback url.Values) error {
	ctx := c.Request().Context()
	rawState := callback.Get("state")
	st, err := h.service.TakeOAuthState(ctx, rawState)
	if err != nil || st.Kind != domain.OAuthStateKindState {
		// state 无效或已过期：可能是伪造/重放的回调，也可能是用户停留太久，
		// 无处可安全跳转，直接 400。
		return echo.NewHTTPError(http.StatusBadRequest, "invalid or expired oauth state")
	}
	prefix := domain.ExchangeCodePrefix(rawState)
	if err := h.checkBindingCookie(c, prefix, st.BindingHash); err != nil {
		return err
	}
	if st.Provider != provider {
		return echo.NewHTTPError(http.StatusBadRequest, "provider mismatch")
	}
	entry, ok := h.oauth.Browser(provider)
	if !ok {
		return errUnknownProvider(provider)
	}
	if st.Intent != domain.IntentLogin {
		// 绑定 / 重新验证在回调里直接完成，绑定 cookie 随之清除；登录的留到兑换时清除。
		defer clearBindingCookie(c, prefix)
	}
	idn, err := entry.Provider.Exchange(ctx, oauth.ExchangeParams{
		Code:         callback.Get("code"),
		RedirectURI:  h.redirectURI(provider),
		CodeVerifier: st.CodeVerifier,
		Nonce:        st.Nonce,
		Callback:     callback,
	})
	if err != nil {
		if errors.Is(err, oauth.ErrSnapshotUser) {
			return c.Redirect(http.StatusFound, completeURL(st.Intent, "snapshot_user"))
		}
		logFailedVerification(c, provider, err)
		return c.Redirect(http.StatusFound, completeURL(st.Intent, "login_failed"))
	}
	switch st.Intent {
	case domain.IntentLogin:
		return h.completeLoginCallback(c, st, rawState, idn)
	case domain.IntentBind:
		if _, err := h.service.BindIdentity(ctx, st.SID, toExternalIdentity(idn)); err != nil {
			return c.Redirect(http.StatusFound, completeURL(st.Intent, resultCodeOf(err)))
		}
	case domain.IntentReauth:
		if err := h.service.Reauthenticate(ctx, st.SID, toExternalIdentity(idn)); err != nil {
			return c.Redirect(http.StatusFound, completeURL(st.Intent, resultCodeOf(err)))
		}
	}
	return c.Redirect(http.StatusFound, completeURL(st.Intent, "ok"))
}

// completeLoginCallback 把登录结果写成一次性兑换：跳转到前端的是跨站响应，在这里种 SameSite=Strict
// 的 cookie 行为不一致，换成兑换码 + 同站请求交付。
func (h *Handler) completeLoginCallback(c *echo.Context, st *domain.OAuthState, rawState string, idn oauth.Identity) error {
	code := domain.NewExchangeCode(rawState)
	if err := h.service.StoreOAuthResult(c.Request().Context(), st, ptrIdentity(toExternalIdentity(idn)), code); err != nil {
		return err
	}
	q := url.Values{}
	q.Set("intent", string(domain.IntentLogin))
	q.Set("code", code)
	return c.Redirect(http.StatusFound, completePath+"?"+q.Encode())
}

// authorizeURL 写一次性 state（按 provider 的能力声明带上 PKCE verifier、nonce）和浏览器绑定 cookie，
// 返回授权链接。登录、绑定、重新验证的发起共用它。
func (h *Handler) authorizeURL(c *echo.Context, provider string, entry oauth.Entry, intent domain.OAuthFlowIntent, sid, returnTo string) (string, error) {
	caps := entry.Provider.Capabilities()
	binding := newBindingValue()
	start, err := h.service.StartOAuthFlow(c.Request().Context(), provider, intent, sid, returnTo, hashCookieValue(binding),
		domain.OAuthStartOptions{PKCE: caps.PKCE, Nonce: caps.Nonce})
	if err != nil {
		return "", err
	}
	setBindingCookie(c, domain.ExchangeCodePrefix(start.State), binding, domain.OAuthStateTTL)
	p := oauth.AuthorizeParams{
		State:          start.State,
		RedirectURI:    h.redirectURI(provider),
		Nonce:          start.Nonce,
		Reauthenticate: intent == domain.IntentReauth,
	}
	if start.CodeVerifier != "" {
		p.CodeChallenge = oauth.S256Challenge(start.CodeVerifier)
	}
	return entry.Provider.AuthorizeURL(p), nil
}

// startBoundFlow 是绑定/重新验证的发起（POST /v1/me/...）：当前会话做 sid、
// auth_time 由 RequireRecentAuth（†，仅绑定）或会话本身（重新验证）保证。
func (h *Handler) startBoundFlow(c *echo.Context, provider string, intent domain.OAuthFlowIntent) error {
	entry, ok := h.oauth.Browser(provider)
	if !ok {
		return errUnknownProvider(provider)
	}
	s, err := sessionBound(c)
	if err != nil {
		return err
	}
	u, err := h.authorizeURL(c, provider, entry, intent, s.SessionID, "")
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, oapi.OAuthStartResponse{AuthorizeUrl: u})
}

// nativeInput 是原生登录、绑定、重新验证共有的凭证字段。
type nativeInput struct {
	Code, IDToken, Nonce, Name *string
}

// verifyNative 是原生凭证校验的唯一入口：provider 要求 nonce 时先一次性核销，再交给 provider 校验。
// 任何失败对客户端都是登录失败，细节只进日志。
func (h *Handler) verifyNative(c *echo.Context, provider string, in nativeInput) (domain.ExternalIdentity, error) {
	entry, ok := h.oauth.NativeEntry(provider)
	if !ok {
		return domain.ExternalIdentity{}, errUnknownProvider(provider)
	}
	ctx := c.Request().Context()
	cred := oauth.NativeCredential{Code: deref(in.Code), IDToken: deref(in.IDToken), Name: deref(in.Name)}
	if entry.Native.Capabilities().Nonce {
		nonce := deref(in.Nonce)
		if nonce == "" {
			logFailedVerification(c, provider, oauth.ErrMissingCredential)
			return domain.ExternalIdentity{}, domain.ErrLoginFailed
		}
		if err := h.service.ConsumeNativeNonce(ctx, provider, nonce); err != nil {
			logFailedVerification(c, provider, err)
			return domain.ExternalIdentity{}, domain.ErrLoginFailed
		}
		cred.Nonce = nonce
	}
	idn, err := entry.Native.VerifyNative(ctx, cred)
	if err != nil {
		logFailedVerification(c, provider, err)
		return domain.ExternalIdentity{}, domain.ErrLoginFailed
	}
	return toExternalIdentity(idn), nil
}

// resultCodeOf 把绑定/重新验证的失败映射成回调跳转 result 的取值。
func resultCodeOf(err error) string {
	switch {
	case errors.Is(err, domain.ErrAlreadyBound):
		return "identity_bound"
	case errors.Is(err, domain.ErrAccountUnavailable):
		return "account_unavailable"
	case errors.Is(err, domain.ErrReauthWrongAccount):
		return "reauth_wrong_account"
	default:
		return "error"
	}
}

func ptrIdentity(v domain.ExternalIdentity) *domain.ExternalIdentity { return &v }
