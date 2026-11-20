package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"app.local/internal/modules/user/domain"
	"app.local/internal/platform/dbtx"
	platformpg "app.local/internal/platform/postgres"
)

// SessionRepository 是 domain.SessionRepository 的 Postgres 实现。
type SessionRepository struct {
	handle      *dbtx.Handle
	idleTimeout time.Duration
}

// NewSessionRepository 的 idleTimeout 是 GetValid 的空闲超时，来自配置。
func NewSessionRepository(handle *dbtx.Handle, idleTimeout time.Duration) *SessionRepository {
	return &SessionRepository{handle: handle, idleTimeout: idleTimeout}
}

const sessionColumns = "id, user_id, client, amr, login_identity_id, auth_time, refresh_family, refresh_token_hash, previous_refresh_token_hash, rotated_at, device_name, ip, user_agent, created_at, updated_at, last_used_at, expires_at, revoked_at"

// sessionJoinedColumns 是 JOIN users 后的限定列名（id/user_id/created_at/updated_at
// 在两个表里都有，不限定会报列名歧义）。
var sessionJoinedColumns = "s." + strings.ReplaceAll(sessionColumns, ", ", ", s.")

func scanSession(row pgx.CollectableRow) (*domain.Session, error) {
	var v domain.Session
	err := row.Scan(&v.ID, &v.UserID, &v.Client, &v.AMR, &v.LoginIdentityID, &v.AuthTime,
		&v.RefreshFamily, &v.RefreshTokenHash, &v.PreviousRefreshTokenHash, &v.RotatedAt,
		&v.DeviceName, &v.IP, &v.UserAgent, &v.CreatedAt, &v.UpdatedAt, &v.LastUsedAt,
		&v.ExpiresAt, &v.RevokedAt)
	return &v, err
}

func (r *SessionRepository) Create(ctx context.Context, v *domain.Session) error {
	_, err := r.handle.Resolve(ctx).Exec(ctx, `
		INSERT INTO user_sessions (id, user_id, client, amr, login_identity_id, auth_time,
			refresh_family, refresh_token_hash, device_name, ip, user_agent,
			created_at, updated_at, last_used_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now(), now(), $12, $13)`,
		v.ID, v.UserID, v.Client, v.AMR, v.LoginIdentityID, v.AuthTime,
		v.RefreshFamily, v.RefreshTokenHash, v.DeviceName, v.IP, v.UserAgent,
		v.LastUsedAt, v.ExpiresAt)
	if err != nil {
		return fmt.Errorf("postgres: create session: %w", err)
	}
	return nil
}

// GetValid 按 sid 查会话并带 users.status：未吊销、未绝对过期、未空闲超时、账号
// active 才命中；查不到一律返回 ErrNotFound，调用方把任何失败都当拒绝处理。
func (r *SessionRepository) GetValid(ctx context.Context, sid string) (*domain.Session, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `
		SELECT `+sessionJoinedColumns+`
		FROM user_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id = $1 AND s.revoked_at IS NULL
		  AND s.expires_at > now() AND s.last_used_at > now() - $2::interval
		  AND u.status = $3`, sid, r.idleTimeout, domain.UserStatusActive)
	if err != nil {
		return nil, fmt.Errorf("postgres: get valid session: %w", err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, scanSession)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get valid session: %w", err)
	}
	return v, nil
}

func (r *SessionRepository) GetByFamily(ctx context.Context, family string) (*domain.Session, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `
		SELECT `+sessionColumns+` FROM user_sessions WHERE refresh_family = $1`, family)
	if err != nil {
		return nil, fmt.Errorf("postgres: get session by family: %w", err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, scanSession)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get session by family: %w", err)
	}
	return v, nil
}

func (r *SessionRepository) Get(ctx context.Context, sid string) (*domain.Session, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `
		SELECT `+sessionColumns+` FROM user_sessions WHERE id = $1`, sid)
	if err != nil {
		return nil, fmt.Errorf("postgres: get session: %w", err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, scanSession)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get session: %w", err)
	}
	return v, nil
}

// TryRotateRefresh 是刷新时的条件 UPDATE：命中的话轮换为新一代 token 并返回
// 会话；未命中返回 (nil, nil)，由调用方区分"普通无效"和"泄露复用"。竞态敏感的
// 判断全在这条 SQL 里，外层 WHERE 重复全部条件（Postgres 行锁后只重查外层条件）。
func (r *SessionRepository) TryRotateRefresh(ctx context.Context, family, presentedHash, newHash string, grace, idleTimeout time.Duration, now time.Time) (*domain.Session, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `
		UPDATE user_sessions
		   SET previous_refresh_token_hash = refresh_token_hash,
		       refresh_token_hash = $2,
		       rotated_at = now(),
		       last_used_at = now(),
		       updated_at = now()
		 WHERE refresh_family = $1 AND revoked_at IS NULL
		   AND expires_at > $6 AND last_used_at > $6 - $3::interval
		   AND (refresh_token_hash = $4
		        OR (previous_refresh_token_hash = $4 AND rotated_at > $6 - $5::interval))
		RETURNING `+sessionColumns,
		family, newHash, idleTimeout, presentedHash, grace, now)
	if err != nil {
		return nil, fmt.Errorf("postgres: rotate refresh: %w", err)
	}
	v, err := pgx.CollectOneRow(rows, scanSession)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: rotate refresh: %w", err)
	}
	return v, nil
}

func (r *SessionRepository) UpdateAuthTime(ctx context.Context, sid string, authTime time.Time, method string) error {
	tag, err := r.handle.Resolve(ctx).Exec(ctx, `
		UPDATE user_sessions SET auth_time = $2, amr = amr || $3::text, updated_at = now()
		WHERE id = $1`, sid, authTime, method)
	if err != nil {
		return fmt.Errorf("postgres: update auth time: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: update auth time: %w", domain.ErrNotFound)
	}
	return nil
}

func (r *SessionRepository) Revoke(ctx context.Context, sid string) error {
	_, err := r.handle.Resolve(ctx).Exec(ctx, `
		UPDATE user_sessions SET revoked_at = now(), updated_at = now()
		WHERE id = $1 AND revoked_at IS NULL`, sid)
	if err != nil {
		return fmt.Errorf("postgres: revoke session: %w", err)
	}
	return nil
}

func (r *SessionRepository) RevokeAllByUser(ctx context.Context, userID string) error {
	_, err := r.handle.Resolve(ctx).Exec(ctx, `
		UPDATE user_sessions SET revoked_at = now(), updated_at = now()
		WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return fmt.Errorf("postgres: revoke user sessions: %w", err)
	}
	return nil
}

func (r *SessionRepository) RevokeByLoginIdentity(ctx context.Context, identityID, exceptSID string) error {
	_, err := r.handle.Resolve(ctx).Exec(ctx, `
		UPDATE user_sessions SET revoked_at = now(), updated_at = now()
		WHERE login_identity_id = $1 AND revoked_at IS NULL AND id <> $2`, identityID, exceptSID)
	if err != nil {
		return fmt.Errorf("postgres: revoke sessions by identity: %w", err)
	}
	return nil
}

func (r *SessionRepository) RepointLoginIdentity(ctx context.Context, sid, identityID string) error {
	_, err := r.handle.Resolve(ctx).Exec(ctx, `
		UPDATE user_sessions SET login_identity_id = $2, updated_at = now() WHERE id = $1`, sid, identityID)
	if err != nil {
		return fmt.Errorf("postgres: repoint login identity: %w", err)
	}
	return nil
}

func (r *SessionRepository) ClearLoginIdentity(ctx context.Context, sid, identityID string) error {
	_, err := r.handle.Resolve(ctx).Exec(ctx, `
		UPDATE user_sessions SET login_identity_id = NULL, updated_at = now()
		WHERE id = $1 AND login_identity_id = $2`, sid, identityID)
	if err != nil {
		return fmt.Errorf("postgres: clear login identity: %w", err)
	}
	return nil
}

func (r *SessionRepository) ListByUser(ctx context.Context, userID string) ([]*domain.Session, error) {
	// 列表只展示有效会话；已吊销的行保留到 PurgeExpired 物理删除。
	rows, err := r.handle.Resolve(ctx).Query(ctx, `
		SELECT `+sessionColumns+` FROM user_sessions WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY last_used_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list sessions: %w", err)
	}
	return pgx.CollectRows(rows, scanSession)
}

func (r *SessionRepository) PageByUser(ctx context.Context, userID, cursor string, limit int) ([]*domain.Session, string, error) {
	createdAt, id, hasCursor, err := platformpg.DecodeCursor(cursor)
	if err != nil {
		return nil, "", domain.ErrInvalidCursor
	}
	q := r.handle.Resolve(ctx)
	var rows pgx.Rows
	if hasCursor {
		rows, err = q.Query(ctx, `
			SELECT `+sessionColumns+` FROM user_sessions
			WHERE user_id = $1 AND revoked_at IS NULL AND (created_at, id) < ($2, $3)
			ORDER BY created_at DESC, id DESC LIMIT $4`, userID, createdAt, id, limit+1)
	} else {
		rows, err = q.Query(ctx, `
			SELECT `+sessionColumns+` FROM user_sessions
			WHERE user_id = $1 AND revoked_at IS NULL
			ORDER BY created_at DESC, id DESC LIMIT $2`, userID, limit+1)
	}
	if err != nil {
		return nil, "", fmt.Errorf("postgres: page sessions: %w", err)
	}
	items, err := pgx.CollectRows(rows, scanSession)
	if err != nil {
		return nil, "", fmt.Errorf("postgres: page sessions: %w", err)
	}
	if len(items) <= limit {
		return items, "", nil
	}
	last := items[limit-1]
	return items[:limit], platformpg.EncodeCursor(last.CreatedAt, last.ID), nil
}

// PurgeExpired 物理删除已过期、以及吊销超过 revokedGrace 的行，返回删除数量。
func (r *SessionRepository) PurgeExpired(ctx context.Context, now time.Time, revokedGrace time.Duration) (int64, error) {
	tag, err := r.handle.Resolve(ctx).Exec(ctx, `
		DELETE FROM user_sessions
		WHERE expires_at < $1 OR (revoked_at IS NOT NULL AND revoked_at < $1 - $2::interval)`,
		now, revokedGrace)
	if err != nil {
		return 0, fmt.Errorf("postgres: purge sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

var _ domain.SessionRepository = (*SessionRepository)(nil)
