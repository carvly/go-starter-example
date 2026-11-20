package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"app.local/internal/kit/secrand"
)

// Authenticate 是每个请求的会话校验（鉴权中间件用）：按 sid 查会话（带
// users.status），已吊销、已过期、空闲超时、账号非 active 或查询失败一律拒绝。
func (s *Service) Authenticate(ctx context.Context, sid string) (*Session, error) {
	sess, err := s.sessions.GetValid(ctx, sid)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("authenticate: %w", ErrInvalidToken)
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}
	return sess, nil
}

// newSession 建一行会话；返回的 refreshRandom 是 refresh token 的随机串部分
// （明文只出现在签发响应里，库里只存它的摘要）。
func (s *Service) newSession(ctx context.Context, userID string, loginIdentityID *string, client, deviceName, ip, userAgent, method string) (*Session, string, error) {
	refreshRandom := secrand.URL(32)
	now := time.Now()
	sess := &Session{
		ID:               uuid.NewV7().String(),
		UserID:           userID,
		Client:           client,
		AMR:              []string{method},
		LoginIdentityID:  loginIdentityID,
		AuthTime:         now,
		RefreshFamily:    secrand.URL(16),
		RefreshTokenHash: secrand.Hash(refreshRandom),
		DeviceName:       deviceName,
		IP:               ip,
		UserAgent:        userAgent,
		CreatedAt:        now,
		UpdatedAt:        now,
		LastUsedAt:       now,
		ExpiresAt:        now.Add(s.cfg.SessionMaxAge),
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, "", fmt.Errorf("new session: %w", err)
	}
	return sess, refreshRandom, nil
}

// issueTokens 签发 access token 并组装结果；refresh token 的完整值是
// "<family>.<random>"，交付方式（cookie 还是响应体）由调用方决定。
func (s *Service) issueTokens(sess *Session, refreshRandom, returnTo string) (*IssuedTokens, error) {
	expiresAt := time.Now().Add(s.cfg.AccessTokenTTL)
	access, err := s.issuer.Issue(sess.UserID, sess.ID, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("issue tokens: %w", err)
	}
	return &IssuedTokens{
		AccessToken:     access,
		AccessExpiresAt: expiresAt,
		RefreshToken:    sess.RefreshFamily + "." + refreshRandom,
		Session:         *sess,
		ReturnTo:        returnTo,
	}, nil
}

// splitRefreshToken 把 "<family>.<random>" 拆成两部分。
func splitRefreshToken(presented string) (family, random string, ok bool) {
	family, random, found := strings.Cut(presented, ".")
	return family, random, found && family != "" && random != ""
}

// Refresh 轮换 refresh token：命中的话返回新一代 token；会话有效但出示的既不是
// 当前值、也不是宽限期内的上一代时，判定为泄露并吊销整个会话（按 refresh_family
// 定位，只知道 sid 的人吊销不了别人的会话）。
func (s *Service) Refresh(ctx context.Context, presented string) (*IssuedTokens, error) {
	family, random, ok := splitRefreshToken(presented)
	if !ok {
		return nil, fmt.Errorf("refresh: %w", ErrInvalidToken)
	}
	newRandom := secrand.URL(32)
	now := time.Now()
	sess, err := s.sessions.TryRotateRefresh(ctx, family, secrand.Hash(random), secrand.Hash(newRandom), s.cfg.RefreshGracePeriod, s.cfg.SessionIdleTimeout, now)
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	if sess != nil {
		return s.issueTokens(sess, newRandom, "")
	}

	// 条件 UPDATE 未命中：区分"普通的无效 token"和"泄露复用"。
	old, err := s.sessions.GetByFamily(ctx, family)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("refresh: %w", ErrInvalidToken)
	}
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	if old.ValidAt(now, s.cfg.SessionIdleTimeout) {
		// 会话有效但出示的不是当前/宽限期内的上一代：旧 token 被人用过，吊销会话。
		if err := s.sessions.Revoke(ctx, old.ID); err != nil {
			return nil, fmt.Errorf("refresh: revoke reused session: %w", err)
		}
		s.log().WarnContext(ctx, "user: refresh token reuse detected, session revoked", "sid", old.ID, "user_id", old.UserID)
	}
	return nil, fmt.Errorf("refresh: %w", ErrInvalidToken)
}

// Logout 登出：摘要匹配当前 refresh token 才吊销（幂等；access token 过期了
// 也能登出，因为只认 refresh token）。
func (s *Service) Logout(ctx context.Context, presented string) error {
	family, random, ok := splitRefreshToken(presented)
	if !ok {
		return nil
	}
	sess, err := s.sessions.GetByFamily(ctx, family)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	if sess.RefreshTokenHash != secrand.Hash(random) {
		return nil
	}
	if err := s.sessions.Revoke(ctx, sess.ID); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

// ListMySessions 分页列出当前账号未吊销的会话（设备列表）。
func (s *Service) ListMySessions(ctx context.Context, userID, cursor string, limit int) ([]*Session, string, error) {
	v, next, err := s.sessions.PageByUser(ctx, userID, cursor, normalizeLimit(limit))
	if err != nil {
		return nil, "", fmt.Errorf("list my sessions: %w", err)
	}
	return v, next, nil
}

// RevokeMySession 是当前账号踢下线自己的一个会话。
func (s *Service) RevokeMySession(ctx context.Context, userID, sid string) error {
	return s.KickSession(ctx, userID, sid)
}

func (s *Service) ListSessions(ctx context.Context, userID string) ([]*Session, error) {
	v, err := s.sessions.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return v, nil
}

// KickSession 只吊销属于该用户的会话，不暴露他人会话的存在。
func (s *Service) KickSession(ctx context.Context, userID, sid string) error {
	sess, err := s.sessions.Get(ctx, sid)
	if err != nil {
		return fmt.Errorf("kick session: %w", err)
	}
	if sess.UserID != userID {
		return fmt.Errorf("kick session: %w", ErrSessionNotFound)
	}
	if err := s.sessions.Revoke(ctx, sid); err != nil {
		return fmt.Errorf("kick session: %w", err)
	}
	return nil
}

// revokedSessionRetention 是已吊销会话行的物理删除保留期：列表页还能看到最近
// 吊销的会话，超过后由 user_cleanup 删除。
const revokedSessionRetention = 7 * 24 * time.Hour

// PurgeExpired 删除 oauth_states 的过期行、user_sessions 的过期行和吊销超过
// revokedSessionRetention 的行。由 user_cleanup 定时任务调用。
func (s *Service) PurgeExpired(ctx context.Context) (int64, int64, error) {
	now := time.Now()
	states, err := s.states.PurgeExpired(ctx, now)
	if err != nil {
		return 0, 0, fmt.Errorf("purge expired: %w", err)
	}
	sessions, err := s.sessions.PurgeExpired(ctx, now, revokedSessionRetention)
	if err != nil {
		return 0, 0, fmt.Errorf("purge expired: %w", err)
	}
	return states, sessions, nil
}
