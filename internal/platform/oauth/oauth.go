// Package oauth 是第三方授权登录的接入层：浏览器 OAuth 跳转和原生 SDK
// 两条路径，不认识任何业务模块。user 模块把校验通过的 Identity 转换成自己的登录方式。
// 各 provider 的测试用 httptest 模拟对方的响应，包括错误响应。
package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
)

// ProviderWeChat 是微信身份的 provider 名（Identity.Provider）：微信各入口的身份都以 unionid 为 subject，
// 同属一个 provider。
const ProviderWeChat = "wechat"

// Identity 是第三方身份校验通过后的结果。Subject 一律用 provider 文档里最稳定的
// 唯一 ID（微信是 unionid），不是入口级的 openid。
type Identity struct {
	Provider  string     // 登录方式 provider（wechat），不是路由里的入口名
	Subject   string     // 该 provider 下稳定的唯一 ID
	Name      string     // 昵称；微信可能为空
	AvatarURL string     // 头像；微信可能为空
	WeChat    *WeChatIDs // 仅微信：本次登录入口的 appid + openid
}

// WeChatIDs 是微信各入口的应用标识和该入口的 openid。
type WeChatIDs struct {
	AppID  string
	OpenID string
}

// 校验失败的哨兵错误。对客户端一律是登录失败；ErrNoUnionID 是配置问题（应用没绑定开放平台），调用方
// 应按错误级别记录以便报警，其余按警告记录。ErrSnapshotUser 有单独的回调结果码。
var (
	// ErrMissingCredential：原生凭证缺少该 provider 需要的字段（如只给了 id_token 没给 code）。
	ErrMissingCredential = errors.New("oauth: missing credential")
	// ErrNoUnionID：应用没绑定到开放平台或没获得 userinfo 授权时拿不到 unionid，
	// 登录失败并报警，不退化成用 openid。
	ErrNoUnionID = errors.New("wechat: unionid missing")
	// ErrSnapshotUser：服务号网页快照页的虚拟账号（is_snapshotuser=1），一律拒绝。
	ErrSnapshotUser = errors.New("wechat: snapshot user")
	// ErrHighRiskUser：code2session 返回 40226，高风险用户被微信拦截。
	ErrHighRiskUser = errors.New("wechat: high risk user intercepted")
)

// BrowserCapabilities 是浏览器路径的 provider 对通用流程的要求；零值表示都不需要（微信）。
type BrowserCapabilities struct {
	// PKCE：流程生成 code_verifier，授权链接带 S256 challenge，换 token 时带 verifier。
	PKCE bool
	// Nonce：流程生成 nonce 随 state 保存，授权链接带上，换 token 时交给 provider 校验 id_token。
	Nonce bool
}

// NativeCapabilities 是原生路径的要求。两条路径各用自己的类型：原生没有 PKCE，共用一个结构体会让
// "原生声明 PKCE"这种无效声明被静默忽略。
type NativeCapabilities struct {
	// Nonce：要求客户端提交服务端签发的一次性 nonce（POST /v1/auth/native/{provider}/nonce），流程核销后
	// 交给 provider 比对 id_token。截获的 id_token 因此无法重放。
	Nonce bool
}

// Provider 是浏览器跳转路径：拼授权链接、code 换身份。防登录 CSRF 靠一次性 state + 绑定 cookie，
// PKCE 和 nonce 由流程按 Capabilities 生成、保存、回传，provider 只负责放进请求和校验。
type Provider interface {
	Capabilities() BrowserCapabilities
	// AuthorizeURL 拼授权链接；response_mode 等 provider 专有参数由实现自己加。
	AuthorizeURL(p AuthorizeParams) string
	Exchange(ctx context.Context, p ExchangeParams) (Identity, error)
}

// AuthorizeParams 是发起授权所需的参数。
type AuthorizeParams struct {
	State         string
	RedirectURI   string
	CodeChallenge string // BrowserCapabilities.PKCE 时非空，方法固定 S256
	Nonce         string // BrowserCapabilities.Nonce 时非空
	// Reauthenticate 为真时是重新验证身份，provider 可以要求重新选择账号或重新登录（如 prompt=select_account）。
	Reauthenticate bool
}

// ExchangeParams 是回调里换身份所需的参数。
type ExchangeParams struct {
	Code         string
	RedirectURI  string
	CodeVerifier string // 与 AuthorizeParams.CodeChallenge 对应
	Nonce        string // 发起时生成的 nonce，provider 校验 id_token 的 nonce claim
	// Callback 是回调的全部参数（GET 查询串或 POST 表单），provider 从中读自己的专有字段。
	Callback url.Values
}

// Native 是 App / 小程序原生 SDK 路径：客户端提交 SDK 返回的凭证，服务端校验。
type Native interface {
	Capabilities() NativeCapabilities
	VerifyNative(ctx context.Context, cred NativeCredential) (Identity, error)
}

// NativeCredential 是原生 SDK 登录提交的凭证；需要哪些字段由 provider 决定，缺字段返回 ErrMissingCredential。
type NativeCredential struct {
	Code    string // 授权码（微信 wx.login / SDK 授权）
	IDToken string // OIDC id_token
	Nonce   string // 已由流程核销的服务端 nonce 原值；provider 比对 id_token 的 nonce claim
	Name    string // 客户端提交的姓名（部分 provider 只在首次授权时给出），只作昵称参考，由 provider 决定是否采用
}

// S256Challenge 按 RFC 7636 计算 code_challenge：BASE64URL(SHA256(verifier))，不带填充。
func S256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Entry 是路由里的 provider 名（wechat-web、wechat-miniprogram……）对应的实现，Provider 与 Native 二选一。
type Entry struct {
	Provider Provider // 浏览器路径的实现；nil 表示走 Native
	Native   Native   // 原生路径的实现；nil 表示走浏览器
}

// Registry 是路由 provider 名到实现的注册表，由组合方（user 的 module.go）用配置构造。
type Registry map[string]Entry

func (r Registry) Browser(name string) (Entry, bool) {
	e, ok := r[name]
	return e, ok && e.Provider != nil
}

func (r Registry) NativeEntry(name string) (Entry, bool) {
	e, ok := r[name]
	return e, ok && e.Native != nil
}
