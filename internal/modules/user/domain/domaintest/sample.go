package domaintest

import (
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"uuid"

	"app.local/internal/modules/user/domain"
)

var seq atomic.Int64

// ValidUser 返回一个满足全部约束、每个字段都有值的 User，唯一字段每次调用都不同。
// 加字段后在这里补上：契约测试检查没有字段是零值。
func ValidUser() *domain.User {
	n := seq.Add(1)
	return &domain.User{
		ID:           uuid.NewV7().String(),
		DisplayName:  Text(n*100+0, 8),
		AvatarUrl:    Text(n*100+1, 8),
		Status:       domain.UserStatusDeleted,
		LastLoginAt:  time.Now().Truncate(time.Microsecond), // Postgres 存微秒，读回来要相等
		SignupClient: "web",
	}
}

// Text 返回长度为 size、随 n 变化的字符串，用于满足长度约束和唯一约束。
func Text(n int64, size int) string {
	s := strconv.FormatInt(n, 36)
	if len(s) < size {
		s = strings.Repeat("a", size-len(s)) + s
	}
	return s[len(s)-size:]
}

// Int 返回 [lo, hi] 内随 n 变化的整数，用于满足范围约束和唯一约束。
func Int(n, lo, hi int64) int64 {
	return lo + n%(hi-lo+1)
}

// ValidIdentity 返回一个满足全部约束的登录方式，唯一字段每次调用都不同。
func ValidIdentity() *domain.Identity {
	n := seq.Add(1)
	now := time.Now().Truncate(time.Microsecond)
	return &domain.Identity{
		ID:          uuid.NewV7().String(),
		UserID:      uuid.NewV7().String(),
		Provider:    domain.ProviderWeChat,
		Subject:     "union-" + Text(n, 24),
		Profile:     map[string]any{"name": Text(n*100+2, 8)},
		LastLoginAt: &now,
	}
}

// ValidSession 返回一个满足全部约束的会话，唯一字段每次调用都不同。
func ValidSession() *domain.Session {
	n := seq.Add(1)
	now := time.Now().Truncate(time.Microsecond)
	idn := uuid.NewV7().String()
	return &domain.Session{
		ID:               uuid.NewV7().String(),
		UserID:           uuid.NewV7().String(),
		Client:           "web",
		AMR:              []string{domain.ProviderWeChat},
		LoginIdentityID:  &idn,
		AuthTime:         now,
		RefreshFamily:    "family-" + Text(n, 24),
		RefreshTokenHash: "hash-" + Text(n, 24),
		DeviceName:       Text(n*100+3, 8),
		IP:               "203.0.113." + strconv.FormatInt(n%256, 10),
		UserAgent:        Text(n*100+4, 8),
		CreatedAt:        now,
		UpdatedAt:        now,
		LastUsedAt:       now,
		ExpiresAt:        now.Add(30 * 24 * time.Hour),
	}
}

// ValidState 返回一个满足全部约束的 oauth_states 行，唯一字段每次调用都不同。
func ValidState() *domain.OAuthState {
	n := seq.Add(1)
	return &domain.OAuthState{
		IDHash:      "hash-" + Text(n, 24),
		Kind:        domain.OAuthStateKindState,
		Provider:    "wechat-web",
		Intent:      domain.IntentLogin,
		ReturnTo:    "/orders",
		BindingHash: ptr("binding-" + Text(n, 16)),
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}
}

func ptr(v string) *string { return &v }
