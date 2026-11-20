package domain

import (
	"context"
)

// RevokeSessions 吊销员工的全部会话（踢下线）。
func (s *Service) RevokeSessions(ctx context.Context, id string) error {
	if _, err := s.accounts.GetAccount(ctx, id); err != nil {
		return err
	}
	return s.sessions.RevokeSessions(ctx, id, "")
}

// maxListedSessions 是"我的登录设备"列出的最近会话数。
const maxListedSessions = 50

// ListedSession 是"我的登录设备"里的一行。
type ListedSession struct {
	*Session
	Status SessionStatus
}

func (s *Service) ListSessions(ctx context.Context, staffID string) ([]ListedSession, error) {
	sessions, err := s.sessions.ListSessions(ctx, staffID, maxListedSessions)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]ListedSession, len(sessions))
	for i, sess := range sessions {
		out[i] = ListedSession{Session: sess, Status: sess.Status(now, s.cfg.SessionIdleTimeout)}
	}
	return out, nil
}

func (s *Service) RevokeSession(ctx context.Context, staffID, sessionID string) error {
	return s.sessions.RevokeSession(ctx, staffID, sessionID)
}
