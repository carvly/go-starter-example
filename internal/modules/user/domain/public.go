package domain

import (
	"context"
	"fmt"
)

// GetPublicProfiles 批量返回存在的用户；不存在的 ID 不出现在结果里。
func (s *Service) GetPublicProfiles(ctx context.Context, ids []string) (map[string]PublicProfile, error) {
	users, err := s.users.ListByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get public profiles: %w", err)
	}
	out := make(map[string]PublicProfile, len(users))
	for _, v := range users {
		out[v.ID] = PublicProfile{
			ID:          v.ID,
			DisplayName: v.DisplayName,
			AvatarURL:   v.AvatarUrl,
			Status:      v.Status,
		}
	}
	return out, nil
}
