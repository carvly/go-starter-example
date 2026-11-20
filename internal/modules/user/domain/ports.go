package domain

import (
	"context"
	"log/slog"
	"time"

	"app.local/internal/kit/page"
	"app.local/internal/kit/uow"
	"app.local/internal/modules/user/events"
)

// Repository 由本模块的 postgres 子包实现，管 users 表。
type Repository interface {
	Create(ctx context.Context, v *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	ListByIDs(ctx context.Context, ids []string) ([]*User, error)
	// TouchLogin 更新账号的最近登录时间。
	TouchLogin(ctx context.Context, id string) (*User, error)
	// List 是后台列表：只放有值的筛选条件，按 s 排序后再按 id 排，总数封顶在 page.MaxTotal。
	List(ctx context.Context, f UserFilter, s page.Sort, p page.Request) (page.Result[*User], error)
	// Enable 是动作"启用"：条件和更新写在同一条 SQL 里；不存在返回 ErrNotFound，条件不满足返回 ErrInvalidState。
	Enable(ctx context.Context, id string) (*User, error)
	// Disable 是动作"停用"：状态从 active 改为 disabled，同时吊销该用户全部未吊销的会话，两者原子
	// 完成；不存在返回 ErrNotFound，状态不是 active 返回 ErrInvalidState。
	Disable(ctx context.Context, id string) (*User, error)
	// UpdateProfile 是 PATCH /v1/me：nil 表示不改该字段。
	UpdateProfile(ctx context.Context, id string, displayName, avatarURL *string) (*User, error)
	// MarkDeleted 是注销：状态改为 deleted 并清空昵称、头像（密码第二期再加 password_hash）。
	MarkDeleted(ctx context.Context, id string) error
}

// IdentityRepository 管 user_identities 表。
type IdentityRepository interface {
	GetByProviderSubject(ctx context.Context, provider, subject string) (*Identity, error)
	GetByUserAndProvider(ctx context.Context, userID, provider string) (*Identity, error)
	ListByUser(ctx context.Context, userID string) ([]*Identity, error)
	// PageByUser 按 created_at、id 倒序分页；cursor 为空表示第一页，非法时返回
	// ErrInvalidCursor；nextCursor 为空表示没有下一页。
	PageByUser(ctx context.Context, userID, cursor string, limit int) (items []*Identity, nextCursor string, err error)
	Create(ctx context.Context, v *Identity) error
	// Replace 把账号在 provider 下的旧登录方式换成新的（subject/profile 不同），
	// 和写操作在同一事务里；返回新行。
	Replace(ctx context.Context, userID, provider string, v *Identity) (*Identity, error)
	Delete(ctx context.Context, id string) error
	// TouchLogin 更新 last_login_at 和 profile（第三方昵称头像可能变）。
	TouchLogin(ctx context.Context, id string, profile map[string]any) error
}

// WeChatOpenIDRepository 管 wechat_openids 表。
type WeChatOpenIDRepository interface {
	Get(ctx context.Context, userID, appid string) (string, error)
	// Upsert 按 (appid, openid) 和 (user_id, appid) 两个唯一约束 upsert：
	// 同一个 unionid 从不同入口进来时维护各入口的 openid。
	Upsert(ctx context.Context, userID, appid, openid string) error
	// DeleteByUser 删除该账号的全部 openid：解绑微信、注销、换成另一个 unionid 时用。
	DeleteByUser(ctx context.Context, userID string) error
}

// SessionRepository 管 user_sessions 表。
type SessionRepository interface {
	Create(ctx context.Context, v *Session) error
	// GetValid 按 sid 查会话并带 users.status：存在、未吊销、未过期、未空闲超时且账号
	// active 才返回；查不到或账号不是 active 返回 ErrNotFound（调用方一律当 401 处理，
	// 查询失败也拒绝）。
	GetValid(ctx context.Context, sid string) (*Session, error)
	GetByFamily(ctx context.Context, family string) (*Session, error)
	// Get 按 sid 查原始行（不校验有效性），用于踢下线时的归属检查。
	Get(ctx context.Context, sid string) (*Session, error)
	// UpdateAuthTime 是成功的重新验证：更新 auth_time 并把本次验证方式追加进 amr。
	UpdateAuthTime(ctx context.Context, sid string, authTime time.Time, method string) error
	// TryRotateRefresh 是刷新时的条件 UPDATE：命中的话更新为新一代 token 并返回会话；
	// 未命中返回 (nil, nil)。竞态敏感的判断全在这条 SQL 里。
	TryRotateRefresh(ctx context.Context, family, presentedHash, newHash string, grace, idleTimeout time.Duration, now time.Time) (*Session, error)
	Revoke(ctx context.Context, sid string) error
	RevokeAllByUser(ctx context.Context, userID string) error
	// RevokeByLoginIdentity 吊销用该登录方式建立的会话（换绑/解绑时用）；
	// exceptSID（当前会话）除外。
	RevokeByLoginIdentity(ctx context.Context, identityID, exceptSID string) error
	// RepointLoginIdentity 把当前会话的 login_identity_id 改指向换绑后的新登录方式。
	RepointLoginIdentity(ctx context.Context, sid, identityID string) error
	// ClearLoginIdentity 解除当前会话与被删登录方式的关联（解绑的最后清理）。
	ClearLoginIdentity(ctx context.Context, sid, identityID string) error
	ListByUser(ctx context.Context, userID string) ([]*Session, error)
	// PageByUser 只列未吊销的会话，按 created_at、id 倒序分页；cursor 规则同
	// IdentityRepository.PageByUser。
	PageByUser(ctx context.Context, userID, cursor string, limit int) (items []*Session, nextCursor string, err error)
	// PurgeExpired 物理删除已过期、以及吊销超过 revokedGrace 的行，返回删除数量。
	PurgeExpired(ctx context.Context, now time.Time, revokedGrace time.Duration) (int64, error)
}

// OAuthStateRepository 管 oauth_states 表。
type OAuthStateRepository interface {
	Create(ctx context.Context, v *OAuthState) error
	// Take 一次性读取：DELETE ... WHERE id_hash = $1 AND expires_at > now() RETURNING。
	// 不存在或已过期返回 ErrNotFound。
	Take(ctx context.Context, idHash string) (*OAuthState, error)
	PurgeExpired(ctx context.Context, now time.Time) (int64, error)
}

// Publisher 发布账号级事件；实现用 queue.Inserter，必须和账号写操作在同一事务里调用。
type Publisher interface {
	PublishUserDeleted(ctx context.Context, evt events.UserDeleted) error
}

// AccessTokenIssuer 签发前台用户 access token；实现是 platform/crypto 的 TokenIssuer。
// domain 不依赖 JWT 库，签名密钥不进 domain。
type AccessTokenIssuer interface {
	Issue(userID, sid string, expiresAt time.Time) (string, error)
}

// Deps 是 Service 的全部依赖，由 module.go 组装。
type Deps struct {
	Users      Repository
	Identities IdentityRepository
	OpenIDs    WeChatOpenIDRepository
	Sessions   SessionRepository
	States     OAuthStateRepository
	Publisher  Publisher
	Issuer     AccessTokenIssuer
	UoW        uow.UnitOfWork
	Logger     *slog.Logger
	Config     Config
}

// Config 是登录与会话的可调参数，来自 user.Config（module.go 转换）。
type Config struct {
	SessionMaxAge      time.Duration
	SessionIdleTimeout time.Duration
	AccessTokenTTL     time.Duration
	RefreshGracePeriod time.Duration
	ReauthWindow       time.Duration
}
