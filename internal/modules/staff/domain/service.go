package domain

import (
	"cmp"
	"regexp"
	"slices"
	"sync"
	"time"

	"app.local/internal/kit/uow"
)

// MinPasswordLength 在 domain 层校验，CLI 和 HTTP 两个入口都受约束。
const MinPasswordLength = 12

// 登录失败退避：同一账号、同一 IP 连续失败 backoffAfter 次后开始等待，每多失败一次
// 等待时间翻倍，最长 backoffMax。不做硬锁定：只按账号锁，攻击者能把管理员锁在外面。
const (
	backoffAfter = 5
	backoffBase  = 30 * time.Second
	backoffMax   = 15 * time.Minute
)

const dummyPassword = "dummy-password-for-constant-time-comparison"

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,63}$`)

type Config struct {
	SessionIdleTimeout time.Duration
	SessionMaxAge      time.Duration
	TempPasswordTTL    time.Duration
}

type Service struct {
	accounts  AccountRepository
	sessions  SessionRepository
	roles     RoleRepository
	audit     AuditRepository
	passwords PasswordVerifier
	uow       uow.UnitOfWork
	cfg       Config
	// codes 是 admin.yaml 里出现过的全部权限码；spec 里删掉的权限码在检查和展示时忽略。
	codes     []PermissionInfo
	now       func() time.Time
	dummyHash func() string
}

// PermissionInfo 是一个可授予的权限码。Tag 用于角色页按模块分组，TagSummary 是模块的中文名，Operations 是
// 需要这个权限码的操作的说明，都来自 admin.yaml，只用于在角色权限页上展示。
type PermissionInfo struct {
	Code       string
	Tag        string
	TagSummary string
	Operations []string
}

type Option func(*Service)

// WithClock 供测试控制登录退避和临时密码过期的时间。
func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

func NewService(repo Repository, passwords PasswordVerifier, u uow.UnitOfWork, cfg Config, ops []Operation, opts ...Option) *Service {
	s := &Service{accounts: repo, sessions: repo, roles: repo, audit: repo, passwords: passwords, uow: u, cfg: cfg, now: time.Now}
	index := map[string]int{}
	for _, op := range ops {
		if !IsPermissionCode(op.Permission) {
			continue
		}
		i, ok := index[op.Permission]
		if !ok {
			i = len(s.codes)
			index[op.Permission] = i
			s.codes = append(s.codes, PermissionInfo{Code: op.Permission, Tag: op.Tag, TagSummary: op.TagSummary})
		}
		if op.Summary != "" {
			s.codes[i].Operations = append(s.codes[i].Operations, op.Summary)
		}
	}
	slices.SortFunc(s.codes, func(a, b PermissionInfo) int { return cmp.Or(cmp.Compare(a.Tag, b.Tag), cmp.Compare(a.Code, b.Code)) })
	for _, opt := range opts {
		opt(s)
	}
	s.dummyHash = sync.OnceValue(func() string {
		h, err := passwords.Hash(dummyPassword)
		if err != nil {
			return ""
		}
		return h
	})
	return s
}
