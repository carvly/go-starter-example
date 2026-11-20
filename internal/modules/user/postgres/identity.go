package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"app.local/internal/modules/user/domain"
	"app.local/internal/platform/dbtx"
	platformpg "app.local/internal/platform/postgres"
)

// IdentityRepository 是 domain.IdentityRepository 的 Postgres 实现。
type IdentityRepository struct {
	handle *dbtx.Handle
}

func NewIdentityRepository(handle *dbtx.Handle) *IdentityRepository {
	return &IdentityRepository{handle: handle}
}

const identityColumns = `id, user_id, provider, subject, profile, last_login_at, created_at, updated_at`

func scanIdentity(row pgx.CollectableRow) (*domain.Identity, error) {
	var v domain.Identity
	err := row.Scan(&v.ID, &v.UserID, &v.Provider, &v.Subject, &v.Profile, &v.LastLoginAt, &v.CreatedAt, &v.UpdatedAt)
	return &v, err
}

func (r *IdentityRepository) GetByProviderSubject(ctx context.Context, provider, subject string) (*domain.Identity, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `
		SELECT `+identityColumns+` FROM user_identities WHERE provider = $1 AND subject = $2`, provider, subject)
	if err != nil {
		return nil, fmt.Errorf("postgres: get identity: %w", err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, scanIdentity)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrIdentityNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get identity: %w", err)
	}
	return v, nil
}

func (r *IdentityRepository) GetByUserAndProvider(ctx context.Context, userID, provider string) (*domain.Identity, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `
		SELECT `+identityColumns+` FROM user_identities WHERE user_id = $1 AND provider = $2`, userID, provider)
	if err != nil {
		return nil, fmt.Errorf("postgres: get identity by user: %w", err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, scanIdentity)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrIdentityNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get identity by user: %w", err)
	}
	return v, nil
}

func (r *IdentityRepository) ListByUser(ctx context.Context, userID string) ([]*domain.Identity, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `
		SELECT `+identityColumns+` FROM user_identities WHERE user_id = $1 ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list identities: %w", err)
	}
	return pgx.CollectRows(rows, scanIdentity)
}

func (r *IdentityRepository) PageByUser(ctx context.Context, userID, cursor string, limit int) ([]*domain.Identity, string, error) {
	createdAt, id, hasCursor, err := platformpg.DecodeCursor(cursor)
	if err != nil {
		return nil, "", domain.ErrInvalidCursor
	}
	q := r.handle.Resolve(ctx)
	var rows pgx.Rows
	if hasCursor {
		rows, err = q.Query(ctx, `
			SELECT `+identityColumns+` FROM user_identities
			WHERE user_id = $1 AND (created_at, id) < ($2, $3)
			ORDER BY created_at DESC, id DESC LIMIT $4`, userID, createdAt, id, limit+1)
	} else {
		rows, err = q.Query(ctx, `
			SELECT `+identityColumns+` FROM user_identities
			WHERE user_id = $1
			ORDER BY created_at DESC, id DESC LIMIT $2`, userID, limit+1)
	}
	if err != nil {
		return nil, "", fmt.Errorf("postgres: page identities: %w", err)
	}
	items, err := pgx.CollectRows(rows, scanIdentity)
	if err != nil {
		return nil, "", fmt.Errorf("postgres: page identities: %w", err)
	}
	if len(items) <= limit {
		return items, "", nil
	}
	last := items[limit-1]
	return items[:limit], platformpg.EncodeCursor(last.CreatedAt, last.ID), nil
}

func (r *IdentityRepository) Create(ctx context.Context, v *domain.Identity) error {
	profile, err := json.Marshal(v.Profile)
	if err != nil {
		return fmt.Errorf("postgres: create identity: marshal profile: %w", err)
	}
	return r.handle.Resolve(ctx).QueryRow(ctx, `
		INSERT INTO user_identities (id, user_id, provider, subject, profile, last_login_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now(), now())
		RETURNING created_at, updated_at`,
		v.ID, v.UserID, v.Provider, v.Subject, profile, v.LastLoginAt,
	).Scan(&v.CreatedAt, &v.UpdatedAt)
}

// Replace 删掉账号在该 provider 下的旧行、插入新行（新 ID）。和写操作在同一事务里，
// 调用方负责吊销用旧行建立的会话（旧行 ID 在 Replace 之前取）。
func (r *IdentityRepository) Replace(ctx context.Context, userID, provider string, v *domain.Identity) (*domain.Identity, error) {
	conn := r.handle.Resolve(ctx)
	tag, err := conn.Exec(ctx, `
		DELETE FROM user_identities WHERE user_id = $1 AND provider = $2`, userID, provider)
	if err != nil {
		return nil, fmt.Errorf("postgres: replace identity: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("postgres: replace identity: %w", domain.ErrIdentityNotFound)
	}
	if err := r.Create(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

func (r *IdentityRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.handle.Resolve(ctx).Exec(ctx, `
		DELETE FROM user_identities WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres: delete identity: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: delete identity: %w", domain.ErrIdentityNotFound)
	}
	return nil
}

func (r *IdentityRepository) TouchLogin(ctx context.Context, id string, profile map[string]any) error {
	data, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("postgres: touch identity: marshal profile: %w", err)
	}
	tag, err := r.handle.Resolve(ctx).Exec(ctx, `
		UPDATE user_identities SET last_login_at = now(), profile = $2, updated_at = now() WHERE id = $1`, id, data)
	if err != nil {
		return fmt.Errorf("postgres: touch identity: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: touch identity: %w", domain.ErrIdentityNotFound)
	}
	return nil
}

var _ domain.IdentityRepository = (*IdentityRepository)(nil)
