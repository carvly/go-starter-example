package domain

import (
	"context"
	"time"
)

// 端口按关注点拆分，Service 的各个文件只依赖自己用到的那个。本模块的 postgres 子包用一个类型实现全部端口；
// 多条语句的写操作（替换角色权限、删除角色）由 Service 包在 UnitOfWork 里。

// AccountRepository 是员工账号。
type AccountRepository interface {
	// CreateAccount 在用户名唯一约束冲突时返回 ErrUsernameTaken。
	CreateAccount(ctx context.Context, a *Account) error
	GetAccount(ctx context.Context, id string) (*Account, error)
	GetAccountByUsername(ctx context.Context, username string) (*Account, error)
	// ListAccounts 按创建时间倒序分页；cursor 不合法时返回 ErrInvalidCursor。
	ListAccounts(ctx context.Context, cursor string, limit int) ([]*Account, string, error)
	// UpdatePassword 同时设置 must_change_password、临时密码过期时间和 password_changed_at。
	UpdatePassword(ctx context.Context, id, passwordHash string, mustChange bool, tempExpiresAt *time.Time, changedAt time.Time) error
	// LockAccount 在当前事务里锁住账号行（SELECT ... FOR UPDATE），让同一账号的登录排队执行，
	// 退避的"数失败次数 → 比较密码 → 记失败"不会被并发请求绕过。
	LockAccount(ctx context.Context, id string) error
	// LockActiveSuperAdmins 在当前事务里锁住并返回全部 active 的超级管理员 ID。停用或取消
	// 超级管理员前用它检查"至少留下一个"，并发的互相停用会排队，后一个看到前一个的结果。
	LockActiveSuperAdmins(ctx context.Context) ([]string, error)
	// CountActiveSuperAdmins 不加锁，只用于展示（员工页的"只剩一个超级管理员"警告）。
	CountActiveSuperAdmins(ctx context.Context) (int, error)
	SetStatus(ctx context.Context, id string, status Status) error
	SetSuperAdmin(ctx context.Context, id string, superAdmin bool) error
}

// SessionRepository 是后台会话。
type SessionRepository interface {
	CreateSession(ctx context.Context, s *Session, tokenHash string) error
	// Authenticate 用一条查询校验会话（未吊销、未过期、未空闲超时）和账号（active），
	// 不满足时返回 ErrSessionInvalid。
	Authenticate(ctx context.Context, tokenHash string, idleTimeout time.Duration) (*Principal, error)
	// TouchSession 只在 last_used_at 早于 1 分钟前时更新，不每个请求都写。
	TouchSession(ctx context.Context, sessionID string) error
	// RevokeSession 只吊销 staffID 自己的会话；不存在或不属于它时返回 ErrSessionNotFound。
	RevokeSession(ctx context.Context, staffID, sessionID string) error
	// RevokeSessions 吊销 staffID 的全部未吊销会话，exceptSessionID 非空时保留这一个。
	RevokeSessions(ctx context.Context, staffID, exceptSessionID string) error
	// ListSessions 按登录时间倒序返回最近 limit 个会话（含已吊销的）。
	ListSessions(ctx context.Context, staffID string, limit int) ([]*Session, error)
}

// RoleRepository 是角色、权限码和员工的角色绑定。
type RoleRepository interface {
	// CreateRole / UpdateRole 在角色名冲突时返回 ErrRoleNameTaken，不写 Permissions。
	CreateRole(ctx context.Context, r *Role) error
	UpdateRole(ctx context.Context, r *Role) error
	// DeleteRole 删除角色及它的权限和绑定；不存在时返回 ErrRoleNotFound。
	DeleteRole(ctx context.Context, id string) error
	// GetRole / ListRoles 带上 Permissions（按字典序）。
	GetRole(ctx context.Context, id string) (*Role, error)
	ListRoles(ctx context.Context) ([]*Role, error)
	SetRolePermissions(ctx context.Context, roleID string, permissions []string) error
	// SetAccountRoles 用 roleIDs 替换员工的角色，返回实际存在、已绑定的角色数。
	SetAccountRoles(ctx context.Context, staffID string, roleIDs []string) (int, error)
	// AccountRoleIDs 一次查出多个员工的角色 ID（各自按 ID 排序），没有角色的员工不在结果里。
	AccountRoleIDs(ctx context.Context, staffIDs []string) (map[string][]string, error)
	HasPermission(ctx context.Context, staffID, permission string) (bool, error)
	// Permissions 返回员工经角色得到的全部权限码（去重、按字典序）。
	Permissions(ctx context.Context, staffID string) ([]string, error)
}

// AuditRepository 是操作日志，登录退避也按它计数。
type AuditRepository interface {
	RecordAudit(ctx context.Context, e *AuditEntry) error
	QueryAudit(ctx context.Context, f AuditFilter) ([]*AuditEntry, string, error)
	// LoginFailures 数 loginKey（用户名摘要）在 ip 上的 login_failed 次数，只数 since 和这个
	// loginKey 在这个 IP 上最近一次登录成功两者中较晚者之后的记录，并返回其中最近一次的时间。
	LoginFailures(ctx context.Context, loginKey, ip string, since time.Time) (count int, last time.Time, err error)
	// PurgeAudit 删除 before 之前的日志，返回删除的行数。
	PurgeAudit(ctx context.Context, before time.Time) (int64, error)
}

// Repository 是全部端口的组合，构造 Service 时一次传入。
type Repository interface {
	AccountRepository
	SessionRepository
	RoleRepository
	AuditRepository
}

// PasswordVerifier 由 internal/platform/crypto 的 Argon2id 实现满足。
type PasswordVerifier interface {
	Hash(password string) (string, error)
	Compare(hash, password string) (bool, error)
}
