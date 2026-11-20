package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"app.local/internal/config"
	"app.local/internal/kit/uow/uowtest"
	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/domain/domaintest"
	"app.local/internal/modules/user/httpapi"
	"app.local/internal/platform/crypto"
	"app.local/internal/platform/httpserver"
	"app.local/internal/platform/oauth"
)

// stubProvider 是 oauth.Provider 的内存实现：按 caps 声明能力，记下最近一次授权和换身份的参数。
type stubProvider struct {
	caps      oauth.BrowserCapabilities
	authorize oauth.AuthorizeParams
	exchange  oauth.ExchangeParams
}

func (p *stubProvider) Capabilities() oauth.BrowserCapabilities { return p.caps }
func (p *stubProvider) AuthorizeURL(a oauth.AuthorizeParams) string {
	p.authorize = a
	return "https://open.weixin.qq.com/connect/qrconnect?state=" + a.State
}
func (p *stubProvider) Exchange(_ context.Context, e oauth.ExchangeParams) (oauth.Identity, error) {
	p.exchange = e
	if e.Callback.Get("is_snapshotuser") == "1" {
		return oauth.Identity{}, oauth.ErrSnapshotUser
	}
	return oauth.Identity{
		Provider: oauth.ProviderWeChat,
		Subject:  "union-" + e.Code,
		Name:     "昵称",
		WeChat:   &oauth.WeChatIDs{AppID: "web-appid", OpenID: "openid-" + e.Code},
	}, nil
}

// stubNative 是 oauth.Native 的内存实现：按 caps 声明能力，记下最近一次提交的凭证。
type stubNative struct {
	caps oauth.NativeCapabilities
	cred oauth.NativeCredential
}

func (n *stubNative) Capabilities() oauth.NativeCapabilities { return n.caps }
func (n *stubNative) VerifyNative(_ context.Context, c oauth.NativeCredential) (oauth.Identity, error) {
	n.cred = c
	if c.IDToken == "" {
		return oauth.Identity{}, oauth.ErrMissingCredential
	}
	return oauth.Identity{Provider: "oidc", Subject: "sub-" + c.IDToken, Name: c.Name}, nil
}

func newFlowServer(t *testing.T) (*echo.Echo, *domain.Service, *domaintest.Fakes, *crypto.TokenIssuer) {
	t.Helper()
	return newFlowServerWith(t, oauth.Registry{"wechat-web": {Provider: &stubProvider{}}})
}

func newFlowServerWith(t *testing.T, registry oauth.Registry) (*echo.Echo, *domain.Service, *domaintest.Fakes, *crypto.TokenIssuer) {
	t.Helper()
	fakes := domaintest.NewFakes()
	deps := fakes.Deps()
	deps.UoW = uowtest.Immediate{}
	deps.Issuer = domaintest.Issuer{}
	deps.Config = domaintest.TestConfig()
	svc := domain.NewService(deps)

	issuer := crypto.NewTokenIssuer("flow-secret", "flow-kid")
	pass := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	h := httpapi.NewHandler(httpapi.Deps{
		Service:               svc,
		OAuth:                 registry,
		PublicBaseURL:         "https://api.example.com",
		CookieDeliveryOrigins: []string{"https://app.example.com"},
	})
	e, err := httpserver.New(httpserver.Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		HTTP:   config.HTTPConfig{BodyLimitBytes: 1 << 20, RequestTimeout: 5 * time.Second, GlobalRateLimitPerSecond: 1000},
		CORS:   config.CORSConfig{AllowOrigins: []string{"https://app.example.com", "https://evil.example.com"}},
		Routes: []func(g *echo.Group){
			func(g *echo.Group) {
				httpapi.Register(g, h, httpapi.Middlewares{
					Auth:              httpapi.NewSessionMiddleware(svc, "flow-kid", []byte("flow-secret"), nil),
					RequireRecentAuth: httpapi.NewRequireRecentAuth(10 * time.Minute),
					RateLimit:         pass,
				})
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e, svc, fakes, issuer
}

func doRequest(t *testing.T, e *echo.Echo, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func locationOf(t *testing.T, rec *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location = %q: %v", rec.Header().Get("Location"), err)
	}
	return loc
}

// 走完 authorize → callback → exchange 的完整浏览器登录流程。
func TestBrowserLoginFlow(t *testing.T) {
	e, _, fakes, _ := newFlowServer(t)

	// 1. 发起授权：302 到第三方授权页，种下绑定 cookie。
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/oauth/wechat-web/authorize?return_to=%2Forders", nil)
	rec := doRequest(t, e, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize = %d, want 302, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "https://open.weixin.qq.com/connect/qrconnect?state=") {
		t.Fatalf("Location = %q", loc)
	}
	state := strings.TrimPrefix(rec.Header().Get("Location"), "https://open.weixin.qq.com/connect/qrconnect?state=")
	cookies := rec.Result().Cookies()
	var binding *http.Cookie
	for _, c := range cookies {
		if strings.HasPrefix(c.Name, "oauth_b_") {
			binding = c
		}
	}
	if binding == nil {
		t.Fatal("authorize 没有种下绑定 cookie")
	}

	// 2. 第三方跳回：带 state + code + 绑定 cookie → 302 到 /auth/complete?code=...
	callback := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/auth/oauth/wechat-web/callback?state="+state+"&code=CODE1", nil)
		req.AddCookie(binding)
		return doRequest(t, e, req)
	}
	rec = callback()
	if rec.Code != http.StatusFound {
		t.Fatalf("callback = %d, want 302, body=%s", rec.Code, rec.Body.String())
	}
	complete := locationOf(t, rec)
	if complete.Path != "/auth/complete" || complete.Query().Get("intent") != "login" {
		t.Fatalf("complete = %q", complete.String())
	}
	code := complete.Query().Get("code")
	if code == "" || code[:8] != state[:8] {
		t.Fatalf("兑换码 = %q, want state 前 8 位开头", code)
	}
	// 登录流程的绑定 cookie 不在回调时清除。
	for _, c := range rec.Result().Cookies() {
		if c.Name == binding.Name && c.MaxAge < 0 {
			t.Error("登录的绑定 cookie 不应在回调时清除")
		}
	}

	// state 已一次性消费：重放回调 400。
	if rec := callback(); rec.Code != http.StatusBadRequest {
		t.Errorf("重放回调 = %d, want 400", rec.Code)
	}

	// 3. 兑换：名单内 Origin → refresh token 只进 cookie，响应体不含。
	exchange := func(origin string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"code": code})
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/oauth/exchange", strings.NewReader(string(body)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		for _, c := range cookies {
			req.AddCookie(c)
		}
		return doRequest(t, e, req)
	}
	rec = exchange("https://app.example.com", binding)
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var tokens map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tokens)
	if tokens["refresh_token"] != nil {
		t.Errorf("名单内 Origin 的响应体不应含 refresh_token：%v", tokens)
	}
	if tokens["refresh_delivery"] != "cookie" {
		t.Errorf("refresh_delivery = %v, want cookie", tokens["refresh_delivery"])
	}
	if tokens["return_to"] != "/orders" {
		t.Errorf("return_to = %v, want /orders", tokens["return_to"])
	}
	var refreshCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			refreshCookie = c
		}
		if strings.HasPrefix(c.Name, "oauth_b_") && c.MaxAge < 0 {
			binding = nil // 已清除
		}
	}
	if refreshCookie == nil || !refreshCookie.HttpOnly || refreshCookie.SameSite != http.SameSiteStrictMode || refreshCookie.Path != "/v1/auth/session" {
		t.Errorf("refresh cookie = %+v", refreshCookie)
	}
	if binding != nil {
		t.Error("兑换后绑定 cookie 应被清除")
	}

	// 兑换码一次性：再用一次 400。
	if rec := exchange("https://app.example.com"); rec.Code != http.StatusBadRequest {
		t.Errorf("重放兑换 = %d, want 400", rec.Code)
	}

	// 4. 换 Origin（不在名单内）重新登录一轮：refresh token 走响应体。
	req = httptest.NewRequest(http.MethodGet, "/v1/auth/oauth/wechat-web/authorize", nil)
	rec = doRequest(t, e, req)
	state2 := strings.TrimPrefix(rec.Header().Get("Location"), "https://open.weixin.qq.com/connect/qrconnect?state=")
	var binding2 *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if strings.HasPrefix(c.Name, "oauth_b_") {
			binding2 = c
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/auth/oauth/wechat-web/callback?state="+state2+"&code=CODE2", nil)
	req.AddCookie(binding2)
	rec = doRequest(t, e, req)
	code2 := locationOf(t, rec).Query().Get("code")
	body, _ := json.Marshal(map[string]string{"code": code2})
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/oauth/exchange", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("Origin", "https://evil.example.com")
	req.AddCookie(binding2)
	rec = doRequest(t, e, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange2 = %d, body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &tokens)
	if tokens["refresh_token"] == nil || tokens["refresh_delivery"] != "body" {
		t.Errorf("名单外 Origin 应走响应体：%v", tokens)
	}
	// 响应体交付时不该种 cookie。
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			t.Error("名单外 Origin 不应种 refresh cookie")
		}
	}

	// 5. 账号解析：两个 code 对应两个 unionid，得到两个账号。
	if users := fakes.Users.UsersList(); len(users) != 2 {
		t.Errorf("用户数 = %d, want 2", len(users))
	}
}

// 登录 CSRF：没有绑定 cookie 的回调失败，且绑定 cookie 值不对的兑换失败。
func TestBrowserLogin_LoginCSRFRejected(t *testing.T) {
	e, _, fakes, _ := newFlowServer(t)

	// 正常走完回调，拿到兑换码。
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/oauth/wechat-web/authorize", nil)
	rec := doRequest(t, e, req)
	state := strings.TrimPrefix(rec.Header().Get("Location"), "https://open.weixin.qq.com/connect/qrconnect?state=")
	var binding *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if strings.HasPrefix(c.Name, "oauth_b_") {
			binding = c
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/auth/oauth/wechat-web/callback?state="+state+"&code=CODE", nil)
	req.AddCookie(binding)
	rec = doRequest(t, e, req)
	code := locationOf(t, rec).Query().Get("code")

	// 换一个浏览器（没有绑定 cookie）直接兑换 → 400。
	body, _ := json.Marshal(map[string]string{"code": code})
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/oauth/exchange", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = doRequest(t, e, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("无 cookie 兑换 = %d, want 400", rec.Code)
	}

	// 绑定 cookie 值不对的兑换 → 400（登录 CSRF：别人把自己的链接发给受害者）。
	forged := *binding
	forged.Value = "forged-by-attacker"
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/oauth/exchange", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.AddCookie(&forged)
	rec = doRequest(t, e, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("伪造 cookie 兑换 = %d, want 400", rec.Code)
	}

	// 两次攻击都没有建立任何会话。
	if n := len(fakes.Sessions.ListAll()); n != 0 {
		t.Errorf("会话数 = %d, want 0", n)
	}
}

// JSON-only：带请求体的操作只接受 application/json，跨站表单到不了 handler。
// 清单是 api/openapi.yaml 里 tag=user、有 requestBody 的全部操作。
func TestJSONOnly(t *testing.T) {
	e, _, _, _ := newFlowServer(t)
	for _, op := range []struct{ method, path string }{
		{http.MethodPost, "/v1/auth/oauth/exchange"},
		{http.MethodPost, "/v1/auth/native/wechat-web/login"},
		{http.MethodPost, "/v1/auth/session/refresh"},
		{http.MethodPost, "/v1/auth/session/logout"},
		{http.MethodPatch, "/v1/me"},
		{http.MethodPost, "/v1/me/identities/native/wechat-web"},
		{http.MethodPost, "/v1/me/reauthenticate/native/wechat-web"},
	} {
		req := httptest.NewRequest(op.method, op.path, strings.NewReader("code=abc"))
		req.Header.Set(echo.HeaderContentType, "application/x-www-form-urlencoded")
		rec := doRequest(t, e, req)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s %s 表单提交 = %d, want 415, body=%s", op.method, op.path, rec.Code, rec.Body.String())
		}
	}
}

// 已登录 + 最近验证过的会话才能发起浏览器绑定。
func TestBindFlow(t *testing.T) {
	e, _, fakes, issuer := newFlowServer(t)
	// 直接造一个会话行（原生登录的覆盖在 domain 层契约里）。
	sess := domaintest.ValidSession()
	sess.UserID = "user-bind"
	sess.AuthTime = time.Now()
	if err := fakes.Sessions.Create(t.Context(), sess); err != nil {
		t.Fatal(err)
	}
	token, err := issuer.Issue("user-bind", sess.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	// 带合法 token 发起绑定：200 + authorize_url + 绑定 cookie。
	req := httptest.NewRequest(http.MethodPost, "/v1/me/identities/oauth/wechat-web", strings.NewReader("{}"))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	rec := doRequest(t, e, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("发起绑定 = %d, body=%s", rec.Code, rec.Body.String())
	}
	var start map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &start)
	if !strings.HasPrefix(start["authorize_url"], "https://open.weixin.qq.com/") {
		t.Errorf("authorize_url = %q", start["authorize_url"])
	}
	hasCookie := false
	for _, c := range rec.Result().Cookies() {
		if strings.HasPrefix(c.Name, "oauth_b_") {
			hasCookie = true
		}
	}
	if !hasCookie {
		t.Error("发起绑定应种绑定 cookie")
	}

	// 未登录 → 401。
	req = httptest.NewRequest(http.MethodPost, "/v1/me/identities/oauth/wechat-web", strings.NewReader("{}"))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = doRequest(t, e, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("未登录发起绑定 = %d, want 401", rec.Code)
	}
}

// 我的会话列表按 cursor 分页：limit 生效、next_cursor 翻到底、非法 cursor 返回 400。
func TestListMySessions_Paging(t *testing.T) {
	e, _, fakes, issuer := newFlowServer(t)
	base := time.Now().Add(-time.Hour)
	var current string
	for i := range 3 {
		sess := domaintest.ValidSession()
		sess.UserID = "user-page"
		sess.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		if err := fakes.Sessions.Create(t.Context(), sess); err != nil {
			t.Fatal(err)
		}
		current = sess.ID
	}
	token, err := issuer.Issue("user-page", current, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	list := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/me/sessions"+query, nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
		return doRequest(t, e, req)
	}

	var page struct {
		Items []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	rec := list("?limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("第一页 = %d, body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if len(page.Items) != 2 || page.NextCursor == "" || !page.Items[0].Current {
		t.Fatalf("第一页 = %+v, want 2 条、最新的是当前会话、有 next_cursor", page)
	}
	rec = list("?limit=2&cursor=" + url.QueryEscape(page.NextCursor))
	page.NextCursor = ""
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if rec.Code != http.StatusOK || len(page.Items) != 1 || page.NextCursor != "" {
		t.Fatalf("第二页 = %d %+v, want 1 条、没有 next_cursor", rec.Code, page)
	}
	if rec := list("?cursor=bogus"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_cursor") {
		t.Errorf("非法 cursor = %d %s, want 400 invalid_cursor", rec.Code, rec.Body.String())
	}
}
