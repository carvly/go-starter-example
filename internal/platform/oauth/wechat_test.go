package oauth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app.local/internal/platform/oauth"
)

const testAppID = "wx-test-appid"

// wechatAPI 用 httptest 模拟微信 API：path -> 响应 JSON。
func wechatAPI(t *testing.T, responses map[string]string) (*httptest.Server, *int) {
	t.Helper()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, ok := responses[r.URL.Path+"?"+r.URL.RawQuery]
		if !ok {
			// 没命中的请求按参数无关匹配一次，方便断言只看 path。
			body, ok = responses[r.URL.Path]
		}
		if !ok {
			t.Errorf("未预期的微信请求：%s?%s", r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

const tokenResp = `{"access_token":"AT","expires_in":7200,"refresh_token":"RT","openid":"o1","scope":"snsapi_userinfo","unionid":"UNION-1"}`

func userInfoResp(extra string) string {
	return `{"openid":"o1","nickname":"昵称","headimgurl":"https://a/1.png","unionid":"UNION-1"` + extra + `}`
}

func TestWeChatWeb_Exchange(t *testing.T) {
	srv, _ := wechatAPI(t, map[string]string{
		"/sns/oauth2/access_token": tokenResp,
		"/sns/userinfo":            userInfoResp(""),
	})
	p := oauth.WeChatWeb(oauth.WeChatApp{AppID: testAppID, Secret: "s"}, srv.URL)
	if p.Capabilities() != (oauth.BrowserCapabilities{}) {
		t.Error("微信不支持 PKCE 和 nonce，Capabilities 应为零值")
	}
	idn, err := p.Exchange(context.Background(), oauth.ExchangeParams{Code: "code-1"})
	if err != nil {
		t.Fatal(err)
	}
	if idn.Provider != oauth.ProviderWeChat || idn.Subject != "UNION-1" || idn.Name != "昵称" {
		t.Errorf("identity = %+v", idn)
	}
	if idn.WeChat == nil || idn.WeChat.AppID != testAppID || idn.WeChat.OpenID != "o1" {
		t.Errorf("wechat ids = %+v", idn.WeChat)
	}
}

func TestWeChatMP_AuthorizeURL(t *testing.T) {
	p := oauth.WeChatMP(oauth.WeChatApp{AppID: testAppID, Secret: "s"}, "")
	u := p.AuthorizeURL(oauth.AuthorizeParams{State: "abc123", RedirectURI: "https://api.example.com/v1/auth/oauth/wechat-mp/callback"})
	if !strings.HasPrefix(u, "https://open.weixin.qq.com/connect/oauth2/authorize?") {
		t.Errorf("authorize url = %q", u)
	}
	for _, want := range []string{"appid=" + testAppID, "redirect_uri=https%3A%2F%2Fapi.example.com", "response_type=code", "scope=snsapi_userinfo", "state=abc123"} {
		if !strings.Contains(u, want) {
			t.Errorf("authorize url 缺少 %q：%q", want, u)
		}
	}
	if !strings.HasSuffix(u, "#wechat_redirect") {
		t.Errorf("authorize url 应以 #wechat_redirect 结尾：%q", u)
	}
	// 服务号的 state 只能是 a-zA-Z0-9：发起方生成 hex state，这里确保拼接不引入其它字符。
	stateValue := u[strings.Index(u, "state=")+len("state="):]
	if i := strings.IndexAny(stateValue, "&#"); i >= 0 {
		stateValue = stateValue[:i]
	}
	if stateValue == "" || strings.ContainsAny(stateValue, `-_.%`) {
		t.Errorf("state 值 %q 含非法字符", stateValue)
	}
}

func TestWeChatWeb_AuthorizeURL(t *testing.T) {
	p := oauth.WeChatWeb(oauth.WeChatApp{AppID: testAppID, Secret: "s"}, "")
	u := p.AuthorizeURL(oauth.AuthorizeParams{State: "s", RedirectURI: "https://api.example.com/cb"})
	if !strings.Contains(u, "connect/qrconnect?") || !strings.Contains(u, "scope=snsapi_login") {
		t.Errorf("authorize url = %q", u)
	}
}

func TestWeChatMiniprogram_VerifyNative(t *testing.T) {
	srv, _ := wechatAPI(t, map[string]string{
		"/sns/jscode2session": `{"openid":"om","session_key":"sk","unionid":"UNION-MINI"}`,
	})
	n := oauth.WeChatMiniprogram(oauth.WeChatApp{AppID: "mini-appid", Secret: "s"}, srv.URL)
	idn, err := n.VerifyNative(context.Background(), oauth.NativeCredential{Code: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if idn.Subject != "UNION-MINI" || idn.WeChat.OpenID != "om" || idn.Name != "" {
		t.Errorf("identity = %+v", idn)
	}
}

func TestWeChatAppNative_VerifyNative(t *testing.T) {
	srv, _ := wechatAPI(t, map[string]string{
		"/sns/oauth2/access_token": tokenResp,
		"/sns/userinfo":            userInfoResp(""),
	})
	n := oauth.WeChatAppNative(oauth.WeChatApp{AppID: "app-appid", Secret: "s"}, srv.URL)
	idn, err := n.VerifyNative(context.Background(), oauth.NativeCredential{Code: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if idn.Subject != "UNION-1" || idn.WeChat.AppID != "app-appid" {
		t.Errorf("identity = %+v", idn)
	}
}

func TestWeChat_Errors(t *testing.T) {
	t.Run("没有 unionid：ErrNoUnionID，不退化成 openid", func(t *testing.T) {
		srv, _ := wechatAPI(t, map[string]string{
			"/sns/oauth2/access_token": `{"access_token":"AT","openid":"o1"}`,
			"/sns/userinfo":            `{"openid":"o1","nickname":"x"}`,
		})
		p := oauth.WeChatWeb(oauth.WeChatApp{AppID: testAppID, Secret: "s"}, srv.URL)
		_, err := p.Exchange(context.Background(), oauth.ExchangeParams{Code: "c"})
		if !errors.Is(err, oauth.ErrNoUnionID) {
			t.Errorf("error = %v, want ErrNoUnionID", err)
		}
	})

	t.Run("网页快照页：ErrSnapshotUser", func(t *testing.T) {
		srv, _ := wechatAPI(t, map[string]string{
			"/sns/oauth2/access_token": tokenResp,
			"/sns/userinfo":            userInfoResp(`,"is_snapshotuser":1`),
		})
		p := oauth.WeChatWeb(oauth.WeChatApp{AppID: testAppID, Secret: "s"}, srv.URL)
		_, err := p.Exchange(context.Background(), oauth.ExchangeParams{Code: "c"})
		if !errors.Is(err, oauth.ErrSnapshotUser) {
			t.Errorf("error = %v, want ErrSnapshotUser", err)
		}
	})

	// 官方文档里 is_snapshotuser 是 sns/oauth2/access_token 的返回字段，userinfo 不一定带它。
	t.Run("换 token 的返回标记了快照页：ErrSnapshotUser，不再查 userinfo", func(t *testing.T) {
		srv, calls := wechatAPI(t, map[string]string{
			"/sns/oauth2/access_token": `{"access_token":"AT","openid":"o1","unionid":"UNION-1","is_snapshotuser":1}`,
			"/sns/userinfo":            userInfoResp(""),
		})
		p := oauth.WeChatMP(oauth.WeChatApp{AppID: testAppID, Secret: "s"}, srv.URL)
		_, err := p.Exchange(context.Background(), oauth.ExchangeParams{Code: "c"})
		if !errors.Is(err, oauth.ErrSnapshotUser) {
			t.Errorf("error = %v, want ErrSnapshotUser", err)
		}
		if *calls != 1 {
			t.Errorf("微信请求次数 = %d, want 1（换 token 之后就拒绝）", *calls)
		}
	})

	t.Run("code2session 40226：ErrHighRiskUser", func(t *testing.T) {
		srv, _ := wechatAPI(t, map[string]string{
			"/sns/jscode2session": `{"errcode":40226,"errmsg":"high risk"}`,
		})
		n := oauth.WeChatMiniprogram(oauth.WeChatApp{AppID: "mini", Secret: "s"}, srv.URL)
		_, err := n.VerifyNative(context.Background(), oauth.NativeCredential{Code: "c"})
		if !errors.Is(err, oauth.ErrHighRiskUser) {
			t.Errorf("error = %v, want ErrHighRiskUser", err)
		}
	})

	t.Run("code 无效：errcode 原样带出", func(t *testing.T) {
		srv, _ := wechatAPI(t, map[string]string{
			"/sns/oauth2/access_token": `{"errcode":40029,"errmsg":"invalid code"}`,
		})
		p := oauth.WeChatWeb(oauth.WeChatApp{AppID: testAppID, Secret: "s"}, srv.URL)
		_, err := p.Exchange(context.Background(), oauth.ExchangeParams{Code: "bad"})
		if err == nil || !strings.Contains(err.Error(), "40029") {
			t.Errorf("error = %v, want 含 errcode=40029", err)
		}
	})
}

// 快照页虚拟账号在回调参数里就能识别，不用换 token。
func TestWeChatBrowser_RejectsSnapshotCallback(t *testing.T) {
	p := oauth.WeChatMP(oauth.WeChatApp{AppID: "a", Secret: "s"}, "http://127.0.0.1:1")
	_, err := p.Exchange(context.Background(), oauth.ExchangeParams{Code: "c", Callback: url.Values{"is_snapshotuser": {"1"}}})
	if !errors.Is(err, oauth.ErrSnapshotUser) {
		t.Fatalf("err = %v, want ErrSnapshotUser", err)
	}
}

func TestWeChat_MissingCode(t *testing.T) {
	app := oauth.WeChatApp{AppID: "a", Secret: "s"}
	if _, err := oauth.WeChatWeb(app, "http://127.0.0.1:1").Exchange(context.Background(), oauth.ExchangeParams{}); !errors.Is(err, oauth.ErrMissingCredential) {
		t.Errorf("浏览器缺 code: err = %v", err)
	}
	for name, n := range map[string]oauth.Native{"mini": oauth.WeChatMiniprogram(app, "http://127.0.0.1:1"), "app": oauth.WeChatAppNative(app, "http://127.0.0.1:1")} {
		if _, err := n.VerifyNative(context.Background(), oauth.NativeCredential{IDToken: "x"}); !errors.Is(err, oauth.ErrMissingCredential) {
			t.Errorf("%s 缺 code: err = %v", name, err)
		}
	}
}

// 网络错误不能把带 secret 的请求 URL 带进错误文字（错误会进日志）。
func TestWeChat_NetworkErrorHidesSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // 连接被拒绝
	p := oauth.WeChatWeb(oauth.WeChatApp{AppID: "wx-app", Secret: "TOP-SECRET-VALUE"}, srv.URL)
	_, err := p.Exchange(context.Background(), oauth.ExchangeParams{Code: "c"})
	if err == nil {
		t.Fatal("连不上时应返回错误")
	}
	if strings.Contains(err.Error(), "TOP-SECRET-VALUE") {
		t.Fatalf("错误文字泄露了 secret：%v", err)
	}
}
