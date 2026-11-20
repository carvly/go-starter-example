package domain

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"app.local/internal/kit/secrand"
)

const (
	// OAuthStateTTL 是浏览器授权流程 state 的有效期。
	OAuthStateTTL = 10 * time.Minute
	// OAuthResultTTL 是登录兑换结果的有效期（回调跳回前端后要尽快兑换）。
	OAuthResultTTL = 60 * time.Second
	// NativeNonceTTL 是原生登录 nonce 的有效期。
	NativeNonceTTL = 5 * time.Minute
)

// OAuthStart 是发起浏览器授权的结果：明文 state，以及按 provider 要求生成的 PKCE verifier 和 nonce
// （不要求时为空）。发起方用 verifier 算 challenge、把 nonce 放进授权链接。
type OAuthStart struct {
	State        string
	CodeVerifier string
	Nonce        string
}

// OAuthStartOptions 是 provider 对流程的要求，由发起方从 provider 的能力声明转换而来（domain 不依赖 oauth 包）。
type OAuthStartOptions struct {
	PKCE  bool
	Nonce bool
}

// ValidReturnTo 报告 return_to 是否是合法的站内相对路径：以单个 / 开头，不含反斜杠和控制字符，解析后没有
// scheme 和 host（见 docs/agents/auth.md）。浏览器解析 URL 时会去掉制表符、换行并把 \ 当成 /，所以
// "/\t/evil.com"、"/\\evil.com" 这类值都会变成 //evil.com，必须整串拒绝，不能只看前缀。
func ValidReturnTo(returnTo string) bool {
	if !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") || strings.ContainsRune(returnTo, '\\') {
		return false
	}
	for _, r := range returnTo {
		if unicode.IsControl(r) {
			return false
		}
	}
	u, err := url.Parse(returnTo)
	return err == nil && u.Scheme == "" && u.Host == ""
}

// StartOAuthFlow 生成 state（以及 provider 要求的 PKCE verifier、nonce）并写入 oauth_states（10 分钟）。
// bindingHash 是浏览器绑定 cookie 值的摘要（发起方生成 cookie 值并传入它的摘要），
// 防登录 CSRF；发起方负责写 cookie 和拼授权链接。
func (s *Service) StartOAuthFlow(ctx context.Context, provider string, intent OAuthFlowIntent, sid, returnTo, bindingHash string, opts OAuthStartOptions) (*OAuthStart, error) {
	if returnTo != "" && !ValidReturnTo(returnTo) {
		return nil, fmt.Errorf("start oauth flow: %w", ErrReturnToInvalid)
	}
	start := &OAuthStart{State: secrand.Hex(32)}
	if opts.PKCE {
		// RFC 7636 要求 verifier 是 43–128 个 unreserved 字符；32 字节 base64url 正好 43 个。
		start.CodeVerifier = secrand.URL(32)
	}
	if opts.Nonce {
		start.Nonce = secrand.URL(32)
	}
	row := &OAuthState{
		IDHash:       secrand.Hash(start.State),
		Kind:         OAuthStateKindState,
		Provider:     provider,
		Intent:       intent,
		SID:          sid,
		ReturnTo:     returnTo,
		CodeVerifier: start.CodeVerifier,
		Nonce:        start.Nonce,
		BindingHash:  strPtr(bindingHash),
		ExpiresAt:    time.Now().Add(OAuthStateTTL),
	}
	if err := s.states.Create(ctx, row); err != nil {
		return nil, fmt.Errorf("start oauth flow: %w", err)
	}
	return start, nil
}

// IssueNativeNonce 为要求 nonce 的原生 provider 签发一次性 nonce（绑定 provider，NativeNonceTTL 内有效）。
func (s *Service) IssueNativeNonce(ctx context.Context, provider string) (string, error) {
	raw := secrand.URL(32)
	row := &OAuthState{
		IDHash:    secrand.Hash(raw),
		Kind:      OAuthStateKindNonce,
		Provider:  provider,
		ExpiresAt: time.Now().Add(NativeNonceTTL),
	}
	if err := s.states.Create(ctx, row); err != nil {
		return "", fmt.Errorf("issue native nonce: %w", err)
	}
	return raw, nil
}

// ConsumeNativeNonce 一次性核销 nonce：不存在、过期、已用过、不是 nonce 或签发给别的 provider 都返回 ErrLoginFailed。
func (s *Service) ConsumeNativeNonce(ctx context.Context, provider, raw string) error {
	st, err := s.states.Take(ctx, secrand.Hash(raw))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("consume native nonce: %w", ErrLoginFailed)
		}
		return fmt.Errorf("consume native nonce: %w", err)
	}
	if st.Kind != OAuthStateKindNonce || st.Provider != provider {
		return fmt.Errorf("consume native nonce: %w", ErrLoginFailed)
	}
	return nil
}

// TakeOAuthState 一次性取出 state 行（不存在或已过期返回 ErrNotFound，调用方按
// 400/登录失败处理）；发起方校验绑定 cookie 的摘要和行里的 BindingHash 一致。
func (s *Service) TakeOAuthState(ctx context.Context, rawState string) (*OAuthState, error) {
	st, err := s.states.Take(ctx, secrand.Hash(rawState))
	if err != nil {
		return nil, fmt.Errorf("take oauth state: %w", err)
	}
	return st, nil
}

// NewExchangeCode 生成登录兑换码：state 前 8 位 + 新随机串，兑换时按前 8 位
// 找到绑定 cookie（见 docs/agents/auth.md 的"必须保持的安全性质"）。
func NewExchangeCode(state string) string {
	prefix := state
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return prefix + secrand.Hex(24)
}

// ExchangeCodePrefix 返回兑换码的前 8 位（定位绑定 cookie 用）。
func ExchangeCodePrefix(code string) string {
	if len(code) <= 8 {
		return code
	}
	return code[:8]
}

// StoreOAuthResult 把登录结果写成一次性兑换（60 秒）：identity 在回调里已经换好，
// 和绑定摘要、return_to 一起暂存；兑换码明文由调用方放进跳转 URL。
func (s *Service) StoreOAuthResult(ctx context.Context, st *OAuthState, identity *ExternalIdentity, exchangeCode string) error {
	row := &OAuthState{
		IDHash:      secrand.Hash(exchangeCode),
		Kind:        OAuthStateKindResult,
		Provider:    st.Provider,
		Intent:      IntentLogin,
		ReturnTo:    st.ReturnTo,
		Identity:    identity,
		BindingHash: st.BindingHash,
		ExpiresAt:   time.Now().Add(OAuthResultTTL),
	}
	if err := s.states.Create(ctx, row); err != nil {
		return fmt.Errorf("store oauth result: %w", err)
	}
	return nil
}

// TakeOAuthResult 是兑换：一次性取出 result 行。
func (s *Service) TakeOAuthResult(ctx context.Context, code string) (*OAuthState, error) {
	st, err := s.states.Take(ctx, secrand.Hash(code))
	if err != nil {
		return nil, fmt.Errorf("take oauth result: %w", err)
	}
	return st, nil
}

func strPtr(v string) *string { return &v }
