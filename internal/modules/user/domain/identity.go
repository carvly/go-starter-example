package domain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"uuid"
)

func (s *Service) upsertOpenID(ctx context.Context, userID string, ext ExternalIdentity) error {
	if ext.WeChatAppID == "" {
		return nil
	}
	return s.openIDs.Upsert(ctx, userID, ext.WeChatAppID, ext.WeChatOpenID)
}

// WeChatOpenID 返回用户在指定微信应用下的 openid。
func (s *Service) WeChatOpenID(ctx context.Context, userID, appid string) (string, error) {
	openid, err := s.openIDs.Get(ctx, userID, appid)
	if err != nil {
		return "", fmt.Errorf("get wechat openid: %w", err)
	}
	return openid, nil
}

func (s *Service) ListMyIdentities(ctx context.Context, userID, cursor string, limit int) ([]*Identity, string, error) {
	v, next, err := s.identities.PageByUser(ctx, userID, cursor, normalizeLimit(limit))
	if err != nil {
		return nil, "", fmt.Errorf("list my identities: %w", err)
	}
	return v, next, nil
}

func (s *Service) ListIdentities(ctx context.Context, userID string) ([]*Identity, error) {
	v, err := s.identities.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	return v, nil
}

// ResolveBind 是绑定分支：同一账号只更新痕迹；属于其它账号则拒绝；换绑时替换
// 登录方式并吊销旧方式建立的其它会话。
func (s *Service) ResolveBind(ctx context.Context, sess *Session, ext ExternalIdentity) (*Identity, error) {
	user, err := s.users.GetByID(ctx, sess.UserID)
	if err != nil {
		return nil, fmt.Errorf("resolve bind: %w", err)
	}
	if user.Status != UserStatusActive {
		return nil, fmt.Errorf("resolve bind: %w", ErrAccountUnavailable)
	}

	idn, err := s.identities.GetByProviderSubject(ctx, ext.Provider, ext.Subject)
	if err != nil && !errors.Is(err, ErrIdentityNotFound) {
		return nil, fmt.Errorf("resolve bind: %w", err)
	}
	if idn != nil {
		if idn.UserID != sess.UserID {
			return nil, fmt.Errorf("resolve bind: %w", ErrAlreadyBound)
		}
		if err := s.identities.TouchLogin(ctx, idn.ID, profileOf(ext)); err != nil {
			return nil, fmt.Errorf("resolve bind: %w", err)
		}
		if err := s.upsertOpenID(ctx, user.ID, ext); err != nil {
			return nil, fmt.Errorf("resolve bind: %w", err)
		}
		return idn, nil
	}

	now := time.Now()
	next := &Identity{
		ID:          uuid.NewV7().String(),
		UserID:      user.ID,
		Provider:    ext.Provider,
		Subject:     ext.Subject,
		Profile:     profileOf(ext),
		LastLoginAt: &now,
	}

	cur, err := s.identities.GetByUserAndProvider(ctx, user.ID, ext.Provider)
	if err != nil && !errors.Is(err, ErrIdentityNotFound) {
		return nil, fmt.Errorf("resolve bind: %w", err)
	}
	if cur != nil {
		if ext.WeChatAppID != "" {
			if err := s.openIDs.DeleteByUser(ctx, user.ID); err != nil {
				return nil, fmt.Errorf("resolve bind: %w", err)
			}
		}
		replaced, err := s.identities.Replace(ctx, user.ID, ext.Provider, next)
		if err != nil {
			return nil, fmt.Errorf("resolve bind: %w", err)
		}
		if err := s.sessions.RevokeByLoginIdentity(ctx, cur.ID, sess.ID); err != nil {
			return nil, fmt.Errorf("resolve bind: %w", err)
		}
		if err := s.sessions.RepointLoginIdentity(ctx, sess.ID, replaced.ID); err != nil {
			return nil, fmt.Errorf("resolve bind: %w", err)
		}
		if err := s.upsertOpenID(ctx, user.ID, ext); err != nil {
			return nil, fmt.Errorf("resolve bind: %w", err)
		}
		return replaced, nil
	}

	if err := s.identities.Create(ctx, next); err != nil {
		return nil, fmt.Errorf("resolve bind: %w", err)
	}
	if err := s.upsertOpenID(ctx, user.ID, ext); err != nil {
		return nil, fmt.Errorf("resolve bind: %w", err)
	}
	return next, nil
}

func (s *Service) BindIdentity(ctx context.Context, sid string, ext ExternalIdentity) (*Identity, error) {
	var idn *Identity
	err := s.uow.Run(ctx, func(ctx context.Context) error {
		sess, err := s.sessions.GetValid(ctx, sid)
		if err != nil {
			return fmt.Errorf("bind identity: %w", ErrInvalidToken)
		}
		idn, err = s.ResolveBind(ctx, sess, ext)
		return err
	})
	if err != nil {
		return nil, err
	}
	return idn, nil
}

// UnbindIdentity 拒绝解绑最后一种登录方式，并吊销被解绑方式建立的其它会话。
func (s *Service) UnbindIdentity(ctx context.Context, userID, identityID, currentSID string) error {
	return s.uow.Run(ctx, func(ctx context.Context) error {
		idns, err := s.identities.ListByUser(ctx, userID)
		if err != nil {
			return fmt.Errorf("unbind identity: %w", err)
		}
		var target *Identity
		for _, v := range idns {
			if v.ID == identityID {
				target = v
			}
		}
		if target == nil {
			return fmt.Errorf("unbind identity: %w", ErrIdentityNotFound)
		}
		if len(idns) == 1 {
			return fmt.Errorf("unbind identity: %w", ErrLastIdentity)
		}
		if err := s.identities.Delete(ctx, identityID); err != nil {
			return fmt.Errorf("unbind identity: %w", err)
		}
		if target.Provider == ProviderWeChat {
			if err := s.openIDs.DeleteByUser(ctx, userID); err != nil {
				return fmt.Errorf("unbind identity: %w", err)
			}
		}
		if err := s.sessions.RevokeByLoginIdentity(ctx, identityID, currentSID); err != nil {
			return fmt.Errorf("unbind identity: %w", err)
		}
		return s.sessions.ClearLoginIdentity(ctx, currentSID, identityID)
	})
}
