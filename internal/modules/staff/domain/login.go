package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"app.local/internal/kit/secrand"

	"app.local/internal/kit/logctx"
)

// LoginInput 的 IP、UserAgent、RequestID 来自请求，用于会话记录、退避计数和操作日志。
type LoginInput struct {
	Username  string
	Password  string
	IP        string
	UserAgent string
	RequestID string
}

// Login 校验用户名和密码，成功后建会话并返回会话 token（只在这里出现一次，库里存摘要）。
// 用户名不存在、密码错误、账号停用对外一律是 ErrInvalidCredentials，并且都做一次密码比较
// （不存在时是哑比较），响应时间不暴露账号是否存在。
//
// 退避按"用户名摘要 + IP"计数，不管账号是否存在：不存在的用户名一样会进入等待，所以等待期
// 可以单独返回 ErrLoginThrottled（带剩余时间），让输错的人知道要等多久，又不暴露账号是否
// 存在。等待期内也做一次哑比较。
//
// 存在的账号在账号行锁上排队："数失败次数 → 比较密码 → 记失败"在一个事务里完成，并发请求
// 不能一起读到"还没到阈值"而绕过退避。
func (s *Service) Login(ctx context.Context, in LoginInput) (string, *Account, error) {
	username := normalizeUsername(in.Username)
	if username == "" || in.Password == "" {
		return "", nil, ErrInvalidCredentials
	}
	key := loginKey(username)

	found, err := s.accounts.GetAccountByUsername(ctx, username)
	if err != nil && !errors.Is(err, ErrAccountNotFound) {
		return "", nil, fmt.Errorf("login: %w", err)
	}

	var token string
	var a *Account
	var denied error // 登录被拒绝时事务照常提交，失败记录要留下来供退避计数。
	err = s.uow.Run(ctx, func(ctx context.Context) error {
		var staffID string
		var since time.Time // 存在的账号只数上次设置密码之后的失败
		if found != nil {
			if err := s.accounts.LockAccount(ctx, found.ID); err != nil {
				return err
			}
			var err error
			if a, err = s.accounts.GetAccount(ctx, found.ID); err != nil {
				return err
			}
			staffID, since = a.ID, a.PasswordChangedAt
		}
		audit := func(action, result string) error {
			return s.RecordAudit(ctx, &AuditEntry{StaffID: staffID, LoginKey: key, Action: action, Result: result, IP: in.IP, RequestID: in.RequestID})
		}
		failures, last, err := s.audit.LoginFailures(ctx, key, in.IP, since)
		if err != nil {
			return err
		}
		if until := last.Add(backoff(failures)); failures >= backoffAfter && s.now().Before(until) {
			_, _ = s.passwords.Compare(s.dummyHash(), in.Password)
			denied = ErrLoginThrottled.WithRetryAfter(until.Sub(s.now()))
			return audit(ActionLoginThrottled, "throttled")
		}
		if a == nil {
			_, _ = s.passwords.Compare(s.dummyHash(), in.Password)
			denied = ErrInvalidCredentials
			return audit(ActionLoginFailed, "invalid_credentials")
		}
		ok, err := s.passwords.Compare(a.PasswordHash, in.Password)
		if err != nil {
			return err
		}
		switch {
		case !ok:
			denied = ErrInvalidCredentials
			return audit(ActionLoginFailed, "invalid_credentials")
		case a.Status != StatusActive:
			denied = ErrInvalidCredentials
			return audit(ActionLoginFailed, "disabled")
		case a.MustChangePassword && a.TempPasswordExpiresAt != nil && !s.now().Before(*a.TempPasswordExpiresAt):
			// 密码正确之后才返回，不泄露账号是否存在。
			denied = ErrTemporaryPasswordExpired
			return audit(ActionLoginFailed, "temporary_password_expired")
		}

		token = secrand.URL(32)
		sess := &Session{ID: uuid.NewV7().String(), StaffID: a.ID, ExpiresAt: s.now().Add(s.cfg.SessionMaxAge), IP: in.IP, UserAgent: in.UserAgent}
		if err := s.sessions.CreateSession(ctx, sess, secrand.Hash(token)); err != nil {
			return err
		}
		return audit(ActionLoginSucceeded, "ok")
	})
	if err != nil {
		return "", nil, fmt.Errorf("login: %w", err)
	}
	if denied != nil {
		return "", nil, denied
	}
	return token, a, nil
}

// backoff 返回连续失败 n 次后需要等待的时间。
func backoff(n int) time.Duration {
	if n < backoffAfter {
		return 0
	}
	shift := min(n-backoffAfter, 10)
	return min(backoffBase<<shift, backoffMax)
}

// Authenticate 校验会话 token，每个后台请求执行一次。不缓存：吊销、停用立即生效；
// 查询失败也拒绝（返回错误）。
func (s *Service) Authenticate(ctx context.Context, token string) (*Principal, error) {
	if token == "" {
		return nil, ErrSessionInvalid
	}
	p, err := s.sessions.Authenticate(ctx, secrand.Hash(token), s.cfg.SessionIdleTimeout)
	if err != nil {
		return nil, err
	}
	if s.now().Sub(p.LastUsedAt) > time.Minute {
		if err := s.sessions.TouchSession(ctx, p.SessionID); err != nil {
			// 只影响空闲超时的精度，不拒绝这次请求。
			logctx.From(ctx).WarnContext(ctx, "staff: touch session failed", "error", err)
		}
	}
	return p, nil
}

func (s *Service) Logout(ctx context.Context, p *Principal) error {
	return s.sessions.RevokeSession(ctx, p.StaffID, p.SessionID)
}

// loginKey 是退避计数用的用户名摘要：账号不存在时也要计数，日志里不存输入的原文。
func loginKey(normalizedUsername string) string {
	return secrand.Hash(normalizedUsername)
}
