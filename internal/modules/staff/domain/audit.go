package domain

import (
	"context"
	"time"
	"uuid"
)

// RecordAudit 写一条操作日志，ID 为空时生成。时间取应用时钟：登录退避用同一个时钟
// 比较"最近一次失败"和"现在"。
func (s *Service) RecordAudit(ctx context.Context, e *AuditEntry) error {
	if e.ID == "" {
		e.ID = uuid.NewV7().String()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = s.now()
	}
	return s.audit.RecordAudit(ctx, e)
}

func (s *Service) QueryAudit(ctx context.Context, f AuditFilter) ([]*AuditEntry, string, error) {
	if f.Limit <= 0 || f.Limit > MaxListLimit {
		f.Limit = DefaultListLimit
	}
	return s.audit.QueryAudit(ctx, f)
}

// PurgeAudit 删除早于保留期的操作日志。
func (s *Service) PurgeAudit(ctx context.Context, retention time.Duration) (int64, error) {
	return s.audit.PurgeAudit(ctx, s.now().Add(-retention))
}
