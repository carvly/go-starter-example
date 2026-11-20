package httpapi_test

import (
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
	"app.local/internal/modules/user/domain/domaintest"
	"app.local/internal/modules/user/httpapi"
	"app.local/internal/platform/httpserver"
	"app.local/internal/platform/oauth"
)

// 第三方登录通用链路：provider 声明的能力一定被流程生成、传递并在回调时取回；未声明时什么都不生成。

// startLogin 发起浏览器登录，返回 state 和绑定 cookie。
func startLogin(t *testing.T, e *echo.Echo, provider string) (string, *http.Cookie) {
	t.Helper()
	rec := doRequest(t, e, httptest.NewRequest(http.MethodGet, "/v1/auth/oauth/"+provider+"/authorize", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize = %d, body=%s", rec.Code, rec.Body.String())
	}
	state := strings.TrimPrefix(rec.Header().Get("Location"), "https://open.weixin.qq.com/connect/qrconnect?state=")
	for _, c := range rec.Result().Cookies() {
		if strings.HasPrefix(c.Name, "oauth_b_") {
			return state, c
		}
	}
	t.Fatal("authorize 没有种下绑定 cookie")
	return "", nil
}

func getCallback(t *testing.T, e *echo.Echo, provider, query string, binding *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/oauth/"+provider+"/callback?"+query, nil)
	if binding != nil {
		req.AddCookie(binding)
	}
	return doRequest(t, e, req)
}

func postCallback(t *testing.T, e *echo.Echo, provider string, form url.Values, binding *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/oauth/"+provider+"/callback", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	if binding != nil {
		req.AddCookie(binding)
	}
	return doRequest(t, e, req)
}

func TestChain_PKCEAndNonceRoundTrip(t *testing.T) {
	stub := &stubProvider{caps: oauth.BrowserCapabilities{PKCE: true, Nonce: true}}
	e, _, _, _ := newFlowServerWith(t, oauth.Registry{"oidc-web": {Provider: stub}})

	state, binding := startLogin(t, e, "oidc-web")
	if stub.authorize.CodeChallenge == "" || stub.authorize.Nonce == "" {
		t.Fatalf("声明了 PKCE 和 nonce，授权参数却没有：%+v", stub.authorize)
	}
	if stub.authorize.Reauthenticate {
		t.Error("登录不是重新验证")
	}
	rec := getCallback(t, e, "oidc-web", "state="+state+"&code=C1", binding)
	if rec.Code != http.StatusFound || locationOf(t, rec).Query().Get("code") == "" {
		t.Fatalf("callback = %d, Location = %s", rec.Code, rec.Header().Get("Location"))
	}
	if got := oauth.S256Challenge(stub.exchange.CodeVerifier); got != stub.authorize.CodeChallenge {
		t.Errorf("回调取回的 verifier 和发起时的 challenge 对不上：S256(%q) = %q, want %q", stub.exchange.CodeVerifier, got, stub.authorize.CodeChallenge)
	}
	if n := len(stub.exchange.CodeVerifier); n < 43 || n > 128 {
		t.Errorf("verifier 长度 = %d, RFC 7636 要求 43–128", n)
	}
	if stub.exchange.Nonce != stub.authorize.Nonce {
		t.Errorf("回调取回的 nonce = %q, want %q", stub.exchange.Nonce, stub.authorize.Nonce)
	}
	if stub.exchange.RedirectURI != "https://api.example.com/v1/auth/oauth/oidc-web/callback" {
		t.Errorf("RedirectURI = %q", stub.exchange.RedirectURI)
	}
	// state（连同 verifier、nonce）只能用一次。
	if rec := getCallback(t, e, "oidc-web", "state="+state+"&code=C1", binding); rec.Code != http.StatusBadRequest {
		t.Errorf("重放回调 = %d, want 400", rec.Code)
	}
}

func TestChain_NoCapabilitiesGeneratesNothing(t *testing.T) {
	stub := &stubProvider{}
	e, _, _, _ := newFlowServerWith(t, oauth.Registry{"plain": {Provider: stub}})
	state, binding := startLogin(t, e, "plain")
	if stub.authorize.CodeChallenge != "" || stub.authorize.Nonce != "" {
		t.Fatalf("没声明能力却生成了：%+v", stub.authorize)
	}
	if rec := getCallback(t, e, "plain", "state="+state+"&code=C1", binding); rec.Code != http.StatusFound {
		t.Fatalf("callback = %d", rec.Code)
	}
	if stub.exchange.CodeVerifier != "" || stub.exchange.Nonce != "" {
		t.Fatalf("没声明能力，Exchange 却收到了 verifier/nonce：%+v", stub.exchange)
	}
}

func TestChain_ReauthenticateFlag(t *testing.T) {
	stub := &stubProvider{}
	e, _, fakes, issuer := newFlowServerWith(t, oauth.Registry{"plain": {Provider: stub}})
	sess := domaintest.ValidSession()
	sess.UserID = "user-reauth"
	sess.AuthTime = time.Now()
	if err := fakes.Sessions.Create(t.Context(), sess); err != nil {
		t.Fatal(err)
	}
	token, err := issuer.Issue("user-reauth", sess.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/me/reauthenticate/oauth/plain", strings.NewReader("{}"))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	if rec := doRequest(t, e, req); rec.Code != http.StatusOK {
		t.Fatalf("发起重新验证 = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !stub.authorize.Reauthenticate {
		t.Error("重新验证的授权参数应 Reauthenticate = true")
	}
}

// 表单 POST 回调和 GET 回调走同一个编排：校验相同，额外的表单字段原样交给 provider。
func TestChain_FormPostCallback(t *testing.T) {
	stub := &stubProvider{}
	e, _, _, _ := newFlowServerWith(t, oauth.Registry{"form": {Provider: stub}})

	// 缺绑定 cookie（跨站表单只能带来自己浏览器的 cookie）：GET 和 POST 都拒绝，provider 不会被调用。
	for _, send := range []func(state string) *httptest.ResponseRecorder{
		func(state string) *httptest.ResponseRecorder {
			return getCallback(t, e, "form", "state="+state+"&code=C", nil)
		},
		func(state string) *httptest.ResponseRecorder {
			return postCallback(t, e, "form", url.Values{"state": {state}, "code": {"C"}}, nil)
		},
	} {
		state, _ := startLogin(t, e, "form")
		stub.exchange = oauth.ExchangeParams{}
		if rec := send(state); rec.Code != http.StatusBadRequest {
			t.Errorf("缺绑定 cookie = %d, want 400, body=%s", rec.Code, rec.Body.String())
		}
		if stub.exchange.Code != "" {
			t.Error("缺绑定 cookie 时不能调用 provider 换身份")
		}
	}

	state, binding := startLogin(t, e, "form")
	rec := postCallback(t, e, "form", url.Values{"state": {state}, "code": {"C2"}, "user": {`{"name":{"firstName":"三"}}`}}, binding)
	if rec.Code != http.StatusFound || locationOf(t, rec).Query().Get("intent") != "login" {
		t.Fatalf("POST 回调 = %d, Location = %s, body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if stub.exchange.Code != "C2" || stub.exchange.Callback.Get("user") == "" {
		t.Errorf("provider 没有收到表单字段：%+v", stub.exchange)
	}
	if rec := postCallback(t, e, "form", url.Values{"code": {"C"}}, binding); rec.Code != http.StatusBadRequest {
		t.Errorf("表单缺 state = %d, want 400", rec.Code)
	}
}

// provider 专有的回调参数由 provider 处理，流程只把它的哨兵错误映射成结果码。
func TestChain_SnapshotUserViaProvider(t *testing.T) {
	e, _, _, _ := newFlowServer(t)
	state, binding := startLogin(t, e, "wechat-web")
	rec := getCallback(t, e, "wechat-web", "state="+state+"&code=C&is_snapshotuser=1", binding)
	if rec.Code != http.StatusFound || locationOf(t, rec).Query().Get("result") != "snapshot_user" {
		t.Fatalf("快照页回调 = %d, Location = %s", rec.Code, rec.Header().Get("Location"))
	}
}

func issueNonce(t *testing.T, e *echo.Echo, provider string) (int, string) {
	t.Helper()
	rec := doRequest(t, e, httptest.NewRequest(http.MethodPost, "/v1/auth/native/"+provider+"/nonce", nil))
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body["nonce"]
}

func nativeLogin(t *testing.T, e *echo.Echo, provider string, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/native/"+provider+"/login", strings.NewReader(string(data)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	return doRequest(t, e, req)
}

// 原生 nonce 由服务端签发、绑定 provider、只能核销一次。
func TestChain_NativeNonce(t *testing.T) {
	strict := &stubNative{caps: oauth.NativeCapabilities{Nonce: true}}
	other := &stubNative{caps: oauth.NativeCapabilities{Nonce: true}}
	plain := &stubNative{}
	e, _, _, _ := newFlowServerWith(t, oauth.Registry{"oidc-app": {Native: strict}, "oidc-other": {Native: other}, "plain-app": {Native: plain}})

	status, nonce := issueNonce(t, e, "oidc-app")
	if status != http.StatusOK || nonce == "" {
		t.Fatalf("签发 nonce = %d %q", status, nonce)
	}
	rec := nativeLogin(t, e, "oidc-app", map[string]string{"client": "app", "id_token": "T1", "nonce": nonce, "name": "张三"})
	if rec.Code != http.StatusOK {
		t.Fatalf("带 nonce 登录 = %d, body=%s", rec.Code, rec.Body.String())
	}
	if strict.cred.Nonce != nonce || strict.cred.IDToken != "T1" || strict.cred.Name != "张三" {
		t.Errorf("provider 收到的凭证 = %+v", strict.cred)
	}
	failed := func(rec *httptest.ResponseRecorder) bool {
		return rec.Code == http.StatusUnauthorized && strings.Contains(rec.Body.String(), "login_failed")
	}
	if rec := nativeLogin(t, e, "oidc-app", map[string]string{"client": "app", "id_token": "T1", "nonce": nonce}); !failed(rec) {
		t.Errorf("nonce 第二次使用 = %d %s, want 统一的登录失败", rec.Code, rec.Body.String())
	}
	_, forOther := issueNonce(t, e, "oidc-other")
	if rec := nativeLogin(t, e, "oidc-app", map[string]string{"client": "app", "id_token": "T1", "nonce": forOther}); !failed(rec) {
		t.Errorf("用签发给别的 provider 的 nonce = %d, want 登录失败", rec.Code)
	}
	if rec := nativeLogin(t, e, "oidc-app", map[string]string{"client": "app", "id_token": "T1"}); !failed(rec) {
		t.Errorf("要求 nonce 却没带 = %d, want 登录失败", rec.Code)
	}
	if status, _ := issueNonce(t, e, "plain-app"); status != http.StatusBadRequest {
		t.Errorf("不要求 nonce 的 provider 签发 = %d, want 400", status)
	}
	// 缺 provider 需要的字段：ErrMissingCredential 对客户端也是统一的登录失败。
	if rec := nativeLogin(t, e, "plain-app", map[string]string{"client": "app", "code": "c"}); !failed(rec) {
		t.Errorf("缺 id_token = %d, want 登录失败", rec.Code)
	}
}

// 网关信任 header 模式没有服务端会话：† 操作、发起重新验证都返回明确的 session_unavailable，
// 不能返回 recent_auth_required 让客户端去重新验证（重新验证在这个模式下永远不会成功）。
func TestTrustedIdentity_SessionOperationsUnavailable(t *testing.T) {
	stub := &stubProvider{}
	h := httpapi.NewHandler(httpapi.Deps{OAuth: oauth.Registry{"plain": {Provider: stub}}, PublicBaseURL: "https://api.example.com"})
	pass := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	e, err := httpserver.New(httpserver.Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		HTTP:   config.HTTPConfig{BodyLimitBytes: 1 << 20, RequestTimeout: 5 * time.Second, GlobalRateLimitPerSecond: 1000},
		CORS:   config.CORSConfig{AllowOrigins: []string{"https://app.example.com"}},
		Routes: []func(g *echo.Group){func(g *echo.Group) {
			httpapi.Register(g, h, httpapi.Middlewares{
				Auth:              httpapi.NewTrustedIdentityMiddleware("X-Gateway-User"),
				RequireRecentAuth: httpapi.NewRequireRecentAuth(10 * time.Minute),
				RateLimit:         pass,
			})
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []struct{ method, path string }{
		{http.MethodDelete, "/v1/me"},
		{http.MethodPost, "/v1/me/reauthenticate/oauth/plain"},
		{http.MethodPost, "/v1/me/identities/oauth/plain"},
	} {
		req := httptest.NewRequest(op.method, op.path, strings.NewReader("{}"))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set("X-Gateway-User", "u-1")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "session_unavailable") {
			t.Errorf("%s %s = %d %s, want 403 session_unavailable", op.method, op.path, rec.Code, rec.Body.String())
		}
	}
	if stub.authorize.State != "" {
		t.Error("没有会话时不能发起授权流程")
	}
}
