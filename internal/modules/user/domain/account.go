package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"app.local/internal/kit/page"
	"app.local/internal/modules/user/events"
)

// fallbackDisplayName 是建号时的兜底昵称：小程序入口拿不到微信昵称、或昵称
// 为空时使用（见 docs/agents/auth.md 的"前台用户"）。
const fallbackDisplayName = "微信用户"

// ProviderWeChat 是 user_identities.provider 的取值（微信，含四个入口）。
const ProviderWeChat = "wechat"

func normalizeDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return fallbackDisplayName
	}
	if utf8.RuneCountInString(name) > 64 {
		r := []rune(name)
		name = string(r[:64])
	}
	return name
}

func normalizeAvatarURL(avatar string) string {
	if utf8.RuneCountInString(avatar) > 1024 {
		return ""
	}
	return avatar
}

// profileOf 只收第三方的昵称和头像；第三方邮箱不落库。
func profileOf(ext ExternalIdentity) map[string]any {
	m := map[string]any{}
	if ext.Name != "" {
		m["name"] = ext.Name
	}
	if ext.AvatarURL != "" {
		m["avatar_url"] = ext.AvatarURL
	}
	return m
}

// SortFields 是列表允许排序的字段，DefaultSort 是默认排序。
var (
	SortFields  = []string{"created_at", "last_login_at"}
	DefaultSort = page.Sort{Field: "created_at", Desc: true}
)

func (s *Service) List(ctx context.Context, f UserFilter, sort page.Sort, p page.Request) (page.Result[*User], error) {
	f.Keyword = strings.TrimSpace(f.Keyword)
	r, err := s.users.List(ctx, f, sort, p)
	if err != nil {
		return page.Result[*User]{}, fmt.Errorf("list users: %w", err)
	}
	return r, nil
}

func (s *Service) Get(ctx context.Context, id string) (*User, error) {
	v, err := s.users.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return v, nil
}

// Disable 在同一个事务里停用账号并吊销全部会话。
func (s *Service) Disable(ctx context.Context, id string) (*User, error) {
	v, err := s.users.Disable(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("disable user: %w", err)
	}
	return v, nil
}

func (s *Service) Enable(ctx context.Context, id string) (*User, error) {
	v, err := s.users.Enable(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("enable user: %w", err)
	}
	return v, nil
}

func (s *Service) UpdateMe(ctx context.Context, id string, displayName, avatarURL *string) (*User, error) {
	if displayName == nil && avatarURL == nil {
		return nil, fmt.Errorf("update me: %w", ErrNothingToUpdate)
	}
	var name, avatar *string
	if displayName != nil {
		v := normalizeDisplayName(*displayName)
		name = &v
	}
	if avatarURL != nil {
		v := normalizeAvatarURL(*avatarURL)
		avatar = &v
	}
	v, err := s.users.UpdateProfile(ctx, id, name, avatar)
	if err != nil {
		return nil, fmt.Errorf("update me: %w", err)
	}
	return v, nil
}

// DeleteMe 是账号注销（†，auth_time 已由中间件检查）：状态置 deleted、清空昵称
// 头像、吊销全部会话、删除全部登录方式和微信 openid，并在同一事务里发布
// user_deleted 让各模块清理数据。
func (s *Service) DeleteMe(ctx context.Context, userID string) error {
	return s.uow.Run(ctx, func(ctx context.Context) error {
		if err := s.users.MarkDeleted(ctx, userID); err != nil {
			return fmt.Errorf("delete me: %w", err)
		}
		if err := s.sessions.RevokeAllByUser(ctx, userID); err != nil {
			return fmt.Errorf("delete me: %w", err)
		}
		idns, err := s.identities.ListByUser(ctx, userID)
		if err != nil {
			return fmt.Errorf("delete me: %w", err)
		}
		for _, idn := range idns {
			if err := s.identities.Delete(ctx, idn.ID); err != nil {
				return fmt.Errorf("delete me: %w", err)
			}
		}
		if err := s.openIDs.DeleteByUser(ctx, userID); err != nil {
			return fmt.Errorf("delete me: %w", err)
		}
		return s.publisher.PublishUserDeleted(ctx, events.UserDeleted{
			UserID:     userID,
			OccurredAt: time.Now(),
		})
	})
}
