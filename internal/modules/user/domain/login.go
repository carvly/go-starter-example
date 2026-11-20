package domain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"uuid"
)

// ResolveLogin 是"解析账号"的登录分支：所有登录入口都只能经过它。
func (s *Service) ResolveLogin(ctx context.Context, ext ExternalIdentity, client string) (*User, *Identity, error) {
	idn, err := s.identities.GetByProviderSubject(ctx, ext.Provider, ext.Subject)
	if err != nil && !errors.Is(err, ErrIdentityNotFound) {
		return nil, nil, fmt.Errorf("resolve login: %w", err)
	}
	if idn != nil {
		user, err := s.users.GetByID(ctx, idn.UserID)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve login: %w", err)
		}
		if user.Status != UserStatusActive {
			return nil, nil, fmt.Errorf("resolve login: %w", ErrAccountUnavailable)
		}
		if err := s.identities.TouchLogin(ctx, idn.ID, profileOf(ext)); err != nil {
			return nil, nil, fmt.Errorf("resolve login: %w", err)
		}
		user, err = s.users.TouchLogin(ctx, user.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve login: %w", err)
		}
		if err := s.upsertOpenID(ctx, user.ID, ext); err != nil {
			return nil, nil, fmt.Errorf("resolve login: %w", err)
		}
		return user, idn, nil
	}

	now := time.Now()
	user := &User{
		ID:           uuid.NewV7().String(),
		DisplayName:  normalizeDisplayName(ext.Name),
		AvatarUrl:    normalizeAvatarURL(ext.AvatarURL),
		Status:       UserStatusActive,
		LastLoginAt:  now,
		SignupClient: client,
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, nil, fmt.Errorf("resolve login: create user: %w", err)
	}
	idn = &Identity{
		ID:          uuid.NewV7().String(),
		UserID:      user.ID,
		Provider:    ext.Provider,
		Subject:     ext.Subject,
		Profile:     profileOf(ext),
		LastLoginAt: &now,
	}
	if err := s.identities.Create(ctx, idn); err != nil {
		return nil, nil, fmt.Errorf("resolve login: create identity: %w", err)
	}
	if err := s.upsertOpenID(ctx, user.ID, ext); err != nil {
		return nil, nil, fmt.Errorf("resolve login: %w", err)
	}
	return user, idn, nil
}

// Login 在同一个事务里解析账号和建会话，access token 在事务外签发。
func (s *Service) Login(ctx context.Context, client, deviceName, ip, userAgent string, ext ExternalIdentity) (*IssuedTokens, error) {
	var sess *Session
	var refreshRandom string
	err := s.uow.Run(ctx, func(ctx context.Context) error {
		user, idn, err := s.ResolveLogin(ctx, ext, client)
		if err != nil {
			return err
		}
		sess, refreshRandom, err = s.newSession(ctx, user.ID, &idn.ID, client, deviceName, ip, userAgent, ext.Provider)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.issueTokens(sess, refreshRandom, "")
}

// Reauthenticate 校验第三方身份属于当前账号，成功后更新会话的 auth_time 和 amr。
func (s *Service) Reauthenticate(ctx context.Context, sid string, ext ExternalIdentity) error {
	return s.uow.Run(ctx, func(ctx context.Context) error {
		sess, err := s.sessions.GetValid(ctx, sid)
		if err != nil {
			return fmt.Errorf("reauthenticate: %w", ErrInvalidToken)
		}
		idn, err := s.identities.GetByProviderSubject(ctx, ext.Provider, ext.Subject)
		if err != nil {
			return fmt.Errorf("reauthenticate: %w", ErrReauthWrongAccount)
		}
		if idn.UserID != sess.UserID {
			return fmt.Errorf("reauthenticate: %w", ErrReauthWrongAccount)
		}
		return s.sessions.UpdateAuthTime(ctx, sid, time.Now(), ext.Provider)
	})
}
