package domain

import "time"

// UserStatus 是用户的状态。
type UserStatus string

const (
	UserStatusActive   UserStatus = "active"   // 正常
	UserStatusDisabled UserStatus = "disabled" // 已停用
	UserStatusDeleted  UserStatus = "deleted"  // 已注销
)

// Valid 报告是不是合法的取值。
func (v UserStatus) Valid() bool {
	switch v {
	case UserStatusActive, UserStatusDisabled, UserStatusDeleted:
		return true
	}
	return false
}

// User 是用户：前台用户。账号由登录创建，后台只能查看、停用和启用。
type User struct {
	ID           string
	DisplayName  string     // 昵称
	AvatarUrl    string     // 头像
	Status       UserStatus // 状态
	LastLoginAt  time.Time
	SignupClient string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// PublicProfile 是业务模块可以读取的用户公开资料和状态。
type PublicProfile struct {
	ID          string
	DisplayName string
	AvatarURL   string
	Status      UserStatus
}

// UserFilter 是后台列表的筛选条件，零值表示不筛选。
type UserFilter struct {
	Keyword                        string // 搜昵称
	Status                         []UserStatus
	CreatedAtFrom, CreatedAtTo     *time.Time // 半开区间 [From, To)
	LastLoginAtFrom, LastLoginAtTo *time.Time // 半开区间 [From, To)
}

// ExternalIdentity 是校验通过的第三方身份：oauth.Identity 在 user 的 module.go
// 里转换成本类型再进 domain，domain 不依赖 platform/oauth。
type ExternalIdentity struct {
	Provider     string // 登录方式 provider（wechat）
	Subject      string // provider 下稳定的唯一 ID（微信是 unionid）
	Name         string // 昵称，可能为空
	AvatarURL    string // 头像，可能为空
	WeChatAppID  string // 仅微信：本次登录入口的 appid
	WeChatOpenID string // 仅微信：该 appid 下的 openid
}

// Identity 是登录方式：一个账号可绑定多个，(provider, subject) 唯一定位，
// 每种 provider 每个账号最多一个，绑定新的就替换旧的。
type Identity struct {
	ID          string
	UserID      string
	Provider    string
	Subject     string
	Profile     map[string]any // 第三方的昵称、头像，只作展示
	LastLoginAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Session 是登录会话。RefreshTokenHash/PreviousRefreshTokenHash 只存 refresh
// token 随机串部分的摘要，明文只出现在签发响应和 cookie/请求体里。
type Session struct {
	ID                       string
	UserID                   string
	Client                   string
	AMR                      []string
	LoginIdentityID          *string
	AuthTime                 time.Time
	RefreshFamily            string
	RefreshTokenHash         string
	PreviousRefreshTokenHash *string
	RotatedAt                *time.Time
	DeviceName               string
	IP                       string
	UserAgent                string
	CreatedAt                time.Time
	UpdatedAt                time.Time
	LastUsedAt               time.Time
	ExpiresAt                time.Time
	RevokedAt                *time.Time
}

// ValidAt 报告会话在 now 是否可用：未吊销、未绝对过期、未空闲超时。账号状态
// 由查询方一并检查（GetValid 带 users.status）。
func (s *Session) ValidAt(now time.Time, idleTimeout time.Duration) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt) && now.Sub(s.LastUsedAt) < idleTimeout
}

// OAuthStateKind 是 oauth_states.kind 的取值（目前只用 state/result）。
type OAuthStateKind string

const (
	OAuthStateKindState  OAuthStateKind = "state"
	OAuthStateKindResult OAuthStateKind = "result"
	OAuthStateKindNonce  OAuthStateKind = "nonce" // 原生登录的一次性 nonce
)

// OAuthFlowIntent 是浏览器授权流程的 intent。
type OAuthFlowIntent string

const (
	IntentLogin  OAuthFlowIntent = "login"
	IntentBind   OAuthFlowIntent = "bind"
	IntentReauth OAuthFlowIntent = "reauth"
)

// OAuthState 是浏览器授权流程的临时数据，一次性读取（DELETE ... RETURNING）。
// Identity 只出现在 kind=result 的行里：回调完成 code 换身份后暂存，兑换端点
// 凭兑换码 + 绑定 cookie 取走并登录。
type OAuthState struct {
	IDHash   string
	Kind     OAuthStateKind
	Provider string // 路由里的 provider（wechat-web、wechat-mp）
	Intent   OAuthFlowIntent
	SID      string // bind/reauth：发起时的会话；回调时重新校验
	ReturnTo string // login：发起时的站内跳转路径
	Identity *ExternalIdentity
	// CodeVerifier、Nonce 是浏览器授权发起时按 provider 要求生成的 PKCE verifier 和 nonce，回调时取回。
	// 明文存放：行在回调时即删除，且 verifier 只有配合一次性授权码才有用，与 state 本身的保护级别一致。
	CodeVerifier string
	Nonce        string
	BindingHash  *string
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

// IssuedTokens 是登录/刷新成功后的结果。RefreshToken 是完整值，交付方式
// （httpOnly cookie 还是响应体）由调用方按 Origin 决定，Service 不关心。
type IssuedTokens struct {
	AccessToken     string
	AccessExpiresAt time.Time
	RefreshToken    string
	Session         Session
	ReturnTo        string // 浏览器登录时带回的站内路径
}
