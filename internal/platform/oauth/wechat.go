package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// WeChatApp 是一个微信登录入口的应用凭据：网站应用、服务号、小程序、移动应用。
type WeChatApp struct {
	AppID  string
	Secret string
}

const defaultWeChatAPIBase = "https://api.weixin.qq.com"

// wechatClient 是微信 API 的底层客户端。apiBase 可换成 httptest 服务器的地址做测试。
type wechatClient struct {
	apiBase string
	http    *http.Client
}

func newWeChatClient(apiBase string) *wechatClient {
	if apiBase == "" {
		apiBase = defaultWeChatAPIBase
	}
	return &wechatClient{
		apiBase: apiBase,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *wechatClient) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.apiBase + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("wechat: build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// *url.Error 的文字带完整 URL（含 appid 和 secret），错误会进日志：只保留路径和内层原因。
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("wechat: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("wechat: %s: decode response: %w", path, err)
	}
	return nil
}

// wechatError 是微信的出错返回：HTTP 状态恒为 200，错误在 body 的 errcode/errmsg 里。
type wechatError struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

func (e wechatError) err() error {
	if e.ErrCode == 0 {
		return nil
	}
	switch e.ErrCode {
	case 40226:
		return fmt.Errorf("%w (errcode=%d)", ErrHighRiskUser, e.ErrCode)
	default:
		return fmt.Errorf("wechat: errcode=%d errmsg=%s", e.ErrCode, e.ErrMsg)
	}
}

// snsToken 是 sns/oauth2/access_token 的返回。官方文档把 is_snapshotuser 列为这个接口的返回
// 字段（userinfo 不一定带），所以换完 token 就要检查。
type snsToken struct {
	wechatError
	AccessToken    string `json:"access_token"`
	OpenID         string `json:"openid"`
	UnionID        string `json:"unionid"`
	IsSnapshotUser int    `json:"is_snapshotuser"`
}

// jscode2Session 是 sns/jscode2session 的返回。
type jscode2Session struct {
	wechatError
	OpenID     string `json:"openid"`
	UnionID    string `json:"unionid"`
	SessionKey string `json:"session_key"`
}

// snsUserInfo 是 sns/userinfo 的返回。is_snapshotuser=1 是网页快照页的虚拟账号，
// 一律拒绝；nickname 可能为空（用户没设昵称）。
type snsUserInfo struct {
	wechatError
	OpenID         string `json:"openid"`
	Nickname       string `json:"nickname"`
	HeadImgURL     string `json:"headimgurl"`
	UnionID        string `json:"unionid"`
	IsSnapshotUser int    `json:"is_snapshotuser"`
}

func (c *wechatClient) oauthAccessToken(ctx context.Context, app WeChatApp, code string) (snsToken, error) {
	var t snsToken
	q := url.Values{
		"appid":      {app.AppID},
		"secret":     {app.Secret},
		"code":       {code},
		"grant_type": {"authorization_code"},
	}
	err := c.get(ctx, "/sns/oauth2/access_token", q, &t)
	return t, err
}

func (c *wechatClient) code2Session(ctx context.Context, app WeChatApp, code string) (jscode2Session, error) {
	var s jscode2Session
	q := url.Values{
		"appid":      {app.AppID},
		"secret":     {app.Secret},
		"js_code":    {code},
		"grant_type": {"authorization_code"},
	}
	err := c.get(ctx, "/sns/jscode2session", q, &s)
	return s, err
}

func (c *wechatClient) snsUserInfo(ctx context.Context, accessToken, openid string) (snsUserInfo, error) {
	var u snsUserInfo
	q := url.Values{
		"access_token": {accessToken},
		"openid":       {openid},
		"lang":         {"zh_CN"},
	}
	err := c.get(ctx, "/sns/userinfo", q, &u)
	return u, err
}

// requireUnionID 把"没有 unionid"统一成 ErrNoUnionID：上线前的开放平台绑定没完成时
// 这里最容易漏，登录必须失败并留下可报警的错误，不能静默退化成 openid。
func requireUnionID(unionid string) error {
	if unionid == "" {
		return ErrNoUnionID
	}
	return nil
}

// checkSnapshot 拒绝网页快照页的虚拟账号。
func checkSnapshot(isSnapshotUser int) error {
	if isSnapshotUser == 1 {
		return ErrSnapshotUser
	}
	return nil
}

// wechatBrowser 是网站应用扫码（snsapi_login）和服务号网页授权（snsapi_userinfo）
// 共用的浏览器路径：code 换 sns access_token，再拉 userinfo 取昵称头像。
type wechatBrowser struct {
	app    WeChatApp
	client *wechatClient
	// authorize 拼授权链接；服务号的 state 只能是 a-zA-Z0-9、不超过 128 字节，发起方
	// 按此约束生成 state。参数顺序按官方文档：appid、redirect_uri、response_type、
	// scope、state。
	authorize func(app WeChatApp, p AuthorizeParams) string
}

// WeChatWeb 是开放平台网站应用的扫码登录（PC 浏览器）。
func WeChatWeb(app WeChatApp, apiBase string) Provider {
	return &wechatBrowser{
		app:    app,
		client: newWeChatClient(apiBase),
		authorize: func(app WeChatApp, p AuthorizeParams) string {
			q := url.Values{}
			q.Set("appid", app.AppID)
			q.Set("redirect_uri", p.RedirectURI)
			q.Set("response_type", "code")
			q.Set("scope", "snsapi_login")
			q.Set("state", p.State)
			return "https://open.weixin.qq.com/connect/qrconnect?" + q.Encode() + "#wechat_redirect"
		},
	}
}

// WeChatMP 是服务号的网页授权（微信内置浏览器），仅限已认证服务号，scope 必须
// 是 snsapi_userinfo——snsapi_base 不返回 unionid。授权必须由用户点击触发，否则
// 页面落入网页快照页。
func WeChatMP(app WeChatApp, apiBase string) Provider {
	return &wechatBrowser{
		app:    app,
		client: newWeChatClient(apiBase),
		authorize: func(app WeChatApp, p AuthorizeParams) string {
			q := url.Values{}
			q.Set("appid", app.AppID)
			q.Set("redirect_uri", p.RedirectURI)
			q.Set("response_type", "code")
			q.Set("scope", "snsapi_userinfo")
			q.Set("state", p.State)
			return "https://open.weixin.qq.com/connect/oauth2/authorize?" + q.Encode() + "#wechat_redirect"
		},
	}
}

// Capabilities：微信不支持 PKCE 和 nonce。
func (w *wechatBrowser) Capabilities() BrowserCapabilities { return BrowserCapabilities{} }

func (w *wechatBrowser) AuthorizeURL(p AuthorizeParams) string {
	return w.authorize(w.app, p)
}

func (w *wechatBrowser) Exchange(ctx context.Context, p ExchangeParams) (Identity, error) {
	// 服务号网页快照页的回调带 is_snapshotuser=1，不用换 token 就能拒绝。
	if p.Callback.Get("is_snapshotuser") == "1" {
		return Identity{}, ErrSnapshotUser
	}
	if p.Code == "" {
		return Identity{}, ErrMissingCredential
	}
	t, err := w.client.oauthAccessToken(ctx, w.app, p.Code)
	if err != nil {
		return Identity{}, err
	}
	if err := t.err(); err != nil {
		return Identity{}, err
	}
	if err := checkSnapshot(t.IsSnapshotUser); err != nil {
		return Identity{}, err
	}
	u, err := w.client.snsUserInfo(ctx, t.AccessToken, t.OpenID)
	if err != nil {
		return Identity{}, err
	}
	if err := u.err(); err != nil {
		return Identity{}, err
	}
	if err := checkSnapshot(u.IsSnapshotUser); err != nil {
		return Identity{}, err
	}
	unionid := u.UnionID
	if unionid == "" {
		unionid = t.UnionID
	}
	if err := requireUnionID(unionid); err != nil {
		return Identity{}, err
	}
	return Identity{
		Provider:  ProviderWeChat,
		Subject:   unionid,
		Name:      u.Nickname,
		AvatarURL: u.HeadImgURL,
		WeChat:    &WeChatIDs{AppID: w.app.AppID, OpenID: t.OpenID},
	}, nil
}

// wechatMiniprogram 是小程序的 wx.login 路径：code2session 直接换 openid + unionid。
// session_key 不下发也不保存：不解密开放数据（手机号用 code 换号）。
type wechatMiniprogram struct {
	app    WeChatApp
	client *wechatClient
}

// WeChatMiniprogram 是小程序登录。
func WeChatMiniprogram(app WeChatApp, apiBase string) Native {
	return &wechatMiniprogram{app: app, client: newWeChatClient(apiBase)}
}

func (w *wechatMiniprogram) Capabilities() NativeCapabilities { return NativeCapabilities{} }

func (w *wechatMiniprogram) VerifyNative(ctx context.Context, cred NativeCredential) (Identity, error) {
	if cred.Code == "" {
		return Identity{}, ErrMissingCredential
	}
	s, err := w.client.code2Session(ctx, w.app, cred.Code)
	if err != nil {
		return Identity{}, err
	}
	if err := s.err(); err != nil {
		return Identity{}, err
	}
	if err := requireUnionID(s.UnionID); err != nil {
		return Identity{}, err
	}
	return Identity{
		Provider: ProviderWeChat,
		Subject:  s.UnionID,
		WeChat:   &WeChatIDs{AppID: w.app.AppID, OpenID: s.OpenID},
	}, nil
}

// wechatAppNative 是移动应用 SDK 授权（scope snsapi_userinfo）后的服务端校验，
// 和浏览器路径共用 code 换 token + userinfo。
type wechatAppNative struct {
	app    WeChatApp
	client *wechatClient
}

// WeChatAppNative 是 Android/iOS App 的微信登录。
func WeChatAppNative(app WeChatApp, apiBase string) Native {
	return &wechatAppNative{app: app, client: newWeChatClient(apiBase)}
}

func (w *wechatAppNative) Capabilities() NativeCapabilities { return NativeCapabilities{} }

func (w *wechatAppNative) VerifyNative(ctx context.Context, cred NativeCredential) (Identity, error) {
	if cred.Code == "" {
		return Identity{}, ErrMissingCredential
	}
	t, err := w.client.oauthAccessToken(ctx, w.app, cred.Code)
	if err != nil {
		return Identity{}, err
	}
	if err := t.err(); err != nil {
		return Identity{}, err
	}
	if err := checkSnapshot(t.IsSnapshotUser); err != nil {
		return Identity{}, err
	}
	u, err := w.client.snsUserInfo(ctx, t.AccessToken, t.OpenID)
	if err != nil {
		return Identity{}, err
	}
	if err := u.err(); err != nil {
		return Identity{}, err
	}
	if err := checkSnapshot(u.IsSnapshotUser); err != nil {
		return Identity{}, err
	}
	unionid := u.UnionID
	if unionid == "" {
		unionid = t.UnionID
	}
	if err := requireUnionID(unionid); err != nil {
		return Identity{}, err
	}
	return Identity{
		Provider:  ProviderWeChat,
		Subject:   unionid,
		Name:      u.Nickname,
		AvatarURL: u.HeadImgURL,
		WeChat:    &WeChatIDs{AppID: w.app.AppID, OpenID: t.OpenID},
	}, nil
}
