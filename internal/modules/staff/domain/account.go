package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"app.local/internal/kit/secrand"
)

func (s *Service) GetAccount(ctx context.Context, id string) (*Account, error) {
	return s.accounts.GetAccount(ctx, id)
}

// AccountRoleIDs 返回这些员工各自绑定的角色 ID，没有角色的员工不在结果里。
func (s *Service) AccountRoleIDs(ctx context.Context, ids []string) (map[string][]string, error) {
	if len(ids) == 0 {
		return map[string][]string{}, nil
	}
	return s.roles.AccountRoleIDs(ctx, ids)
}

// CountActiveSuperAdmins 供员工页提示"只剩一个超级管理员"。
func (s *Service) CountActiveSuperAdmins(ctx context.Context) (int, error) {
	return s.accounts.CountActiveSuperAdmins(ctx)
}

func (s *Service) ListAccounts(ctx context.Context, cursor string, limit int) ([]*Account, string, error) {
	if limit <= 0 || limit > MaxListLimit {
		limit = DefaultListLimit
	}
	return s.accounts.ListAccounts(ctx, cursor, limit)
}

// ChangePassword 改自己的密码：校验当前密码，清除临时密码状态，吊销除当前会话以外的会话。
func (s *Service) ChangePassword(ctx context.Context, p *Principal, current, next string) error {
	if utf8.RuneCountInString(next) < MinPasswordLength || next == current {
		return ErrInvalidInput
	}
	a, err := s.accounts.GetAccount(ctx, p.StaffID)
	if err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	ok, err := s.passwords.Compare(a.PasswordHash, current)
	if err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	if !ok {
		return ErrWrongPassword
	}
	hash, err := s.passwords.Hash(next)
	if err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	return s.uow.Run(ctx, func(ctx context.Context) error {
		if err := s.accounts.UpdatePassword(ctx, a.ID, hash, false, nil, s.now()); err != nil {
			return err
		}
		return s.sessions.RevokeSessions(ctx, a.ID, p.SessionID)
	})
}

// CreateAccountInput 的 ActorID 为空表示由 CLI 创建。
type CreateAccountInput struct {
	ActorID     string
	Username    string
	DisplayName string
	SuperAdmin  bool
}

// CreateAccount 创建员工并设一个临时密码（返回明文，只出现这一次），首次登录后必须改密。
func (s *Service) CreateAccount(ctx context.Context, in CreateAccountInput) (*Account, string, error) {
	username := normalizeUsername(in.Username)
	display := strings.TrimSpace(in.DisplayName)
	if display == "" {
		display = username
	}
	if !usernamePattern.MatchString(username) || utf8.RuneCountInString(display) > 100 {
		return nil, "", ErrInvalidInput
	}
	temp, hash, expires, err := s.newTempPassword()
	if err != nil {
		return nil, "", fmt.Errorf("create account: %w", err)
	}
	a := &Account{
		ID: uuid.NewV7().String(), Username: username, DisplayName: display, PasswordHash: hash,
		SuperAdmin: in.SuperAdmin, MustChangePassword: true, TempPasswordExpiresAt: &expires,
		PasswordChangedAt: s.now(), Status: StatusActive, CreatedBy: in.ActorID,
	}
	if err := s.accounts.CreateAccount(ctx, a); err != nil {
		if errors.Is(err, ErrUsernameTaken) {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("create account: %w", err)
	}
	return a, temp, nil
}

// ResetPassword 设一个新的临时密码并吊销对方全部会话，返回临时密码明文和它的过期时间。
func (s *Service) ResetPassword(ctx context.Context, id string) (string, time.Time, error) {
	return s.resetPassword(ctx, id, false)
}

// RecoverAccount 是 CLI 的恢复手段：重设密码并启用账号，在一个事务里完成，不会出现
// "密码已改、账号仍停用、临时密码没拿到"的中间状态。
func (s *Service) RecoverAccount(ctx context.Context, id string) (string, time.Time, error) {
	return s.resetPassword(ctx, id, true)
}

func (s *Service) resetPassword(ctx context.Context, id string, enable bool) (string, time.Time, error) {
	temp, hash, expires, err := s.newTempPassword()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("reset password: %w", err)
	}
	err = s.uow.Run(ctx, func(ctx context.Context) error {
		if err := s.accounts.UpdatePassword(ctx, id, hash, true, &expires, s.now()); err != nil {
			return err
		}
		if enable {
			if err := s.accounts.SetStatus(ctx, id, StatusActive); err != nil {
				return err
			}
		}
		return s.sessions.RevokeSessions(ctx, id, "")
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return temp, expires, nil
}

func (s *Service) newTempPassword() (plain, hash string, expires time.Time, err error) {
	plain = secrand.URL(16)
	hash, err = s.passwords.Hash(plain)
	if err != nil {
		return "", "", time.Time{}, err
	}
	return plain, hash, s.now().Add(s.cfg.TempPasswordTTL), nil
}

// Disable 停用员工并吊销它的全部会话。不能停用自己；停用超级管理员时必须还剩至少一个
// active 的超级管理员（在锁里检查，并发的互相停用不会把超级管理员全部停掉）。
func (s *Service) Disable(ctx context.Context, actorID, id string) error {
	if actorID == id {
		return ErrCannotModifySelf
	}
	return s.uow.Run(ctx, func(ctx context.Context) error {
		if err := s.keepOneSuperAdmin(ctx, id); err != nil {
			return err
		}
		if err := s.accounts.SetStatus(ctx, id, StatusDisabled); err != nil {
			return err
		}
		return s.sessions.RevokeSessions(ctx, id, "")
	})
}

func (s *Service) Enable(ctx context.Context, id string) error {
	return s.accounts.SetStatus(ctx, id, StatusActive)
}

// SetSuperAdmin 不能取消自己的标记；取消别人的标记时同样检查至少留下一个超级管理员。
func (s *Service) SetSuperAdmin(ctx context.Context, actorID, id string, superAdmin bool) error {
	if actorID == id && !superAdmin {
		return ErrCannotModifySelf
	}
	if superAdmin {
		return s.accounts.SetSuperAdmin(ctx, id, true)
	}
	return s.uow.Run(ctx, func(ctx context.Context) error {
		if err := s.keepOneSuperAdmin(ctx, id); err != nil {
			return err
		}
		return s.accounts.SetSuperAdmin(ctx, id, false)
	})
}

// keepOneSuperAdmin 在事务里锁住全部 active 超级管理员，id 是其中最后一个时拒绝。
func (s *Service) keepOneSuperAdmin(ctx context.Context, id string) error {
	ids, err := s.accounts.LockActiveSuperAdmins(ctx)
	if err != nil {
		return err
	}
	if len(ids) == 1 && ids[0] == id {
		return ErrLastSuperAdmin
	}
	return nil
}

// GetAccountByUsername 供 CLI 按用户名定位账号。
func (s *Service) GetAccountByUsername(ctx context.Context, username string) (*Account, error) {
	return s.accounts.GetAccountByUsername(ctx, normalizeUsername(username))
}

func normalizeUsername(u string) string {
	return strings.ToLower(strings.TrimSpace(u))
}
