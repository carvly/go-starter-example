// Package postgres 是 user 的 Repository 实现（pgx/v5，经 dbtx.Handle 取连接）。
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"app.local/internal/kit/page"
	"app.local/internal/modules/user/domain"
	"app.local/internal/platform/dbtx"
	platformpg "app.local/internal/platform/postgres"
)

type Repository struct {
	handle *dbtx.Handle
}

func NewRepository(handle *dbtx.Handle) *Repository {
	return &Repository{handle: handle}
}

const columns = `id, display_name, avatar_url, status, last_login_at, signup_client, created_at, updated_at`

func scan(row pgx.CollectableRow) (*domain.User, error) {
	var v domain.User
	err := row.Scan(&v.ID, &v.DisplayName, &v.AvatarUrl, &v.Status, &v.LastLoginAt, &v.SignupClient, &v.CreatedAt, &v.UpdatedAt)
	return &v, err
}

// mapErr 包装错误。加了唯一约束后在这里按约束名换成领域错误（platformpg.UniqueViolation）。
func mapErr(op string, err error) error {
	return fmt.Errorf("postgres: %s user: %w", op, err)
}

func (r *Repository) Create(ctx context.Context, v *domain.User) error {
	err := r.handle.Resolve(ctx).QueryRow(ctx, `
		INSERT INTO users (id, display_name, avatar_url, status, last_login_at, signup_client, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now(), now())
		RETURNING created_at, updated_at
	`, v.ID, v.DisplayName, v.AvatarUrl, v.Status, v.LastLoginAt, v.SignupClient).Scan(&v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return mapErr("create", err)
	}
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `SELECT `+columns+` FROM users WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("postgres: get user: %w", err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get user: %w", err)
	}
	return v, nil
}

func (r *Repository) ListByIDs(ctx context.Context, ids []string) ([]*domain.User, error) {
	if len(ids) == 0 {
		return []*domain.User{}, nil
	}
	rows, err := r.handle.Resolve(ctx).Query(ctx, `SELECT `+columns+` FROM users WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("postgres: list users by ids: %w", err)
	}
	v, err := pgx.CollectRows(rows, scan)
	if err != nil {
		return nil, fmt.Errorf("postgres: list users by ids: %w", err)
	}
	return v, nil
}

func (r *Repository) TouchLogin(ctx context.Context, id string) (*domain.User, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `UPDATE users SET last_login_at = now(), updated_at = now() WHERE id = $1 RETURNING `+columns, id)
	if err != nil {
		return nil, mapErr("touch login", err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, mapErr("touch login", err)
	}
	return v, nil
}

func (r *Repository) List(ctx context.Context, f domain.UserFilter, s page.Sort, p page.Request) (page.Result[*domain.User], error) {
	var w platformpg.Where
	w.Contains(f.Keyword, "display_name")
	w.In("status", strs(f.Status))
	w.Range("created_at", f.CreatedAtFrom, f.CreatedAtTo)
	w.Range("last_login_at", f.LastLoginAtFrom, f.LastLoginAtTo)
	return platformpg.ListPage(ctx, r.handle.Resolve(ctx), "users", columns, w, s, p, scan)
}

func strs[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

// Enable 是动作"启用"：条件和更新在同一条 UPDATE 里，并发下不会在条件不满足时改成功。
func (r *Repository) Enable(ctx context.Context, id string) (*domain.User, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `
		UPDATE users SET status = $2, updated_at = now() WHERE id = $1 AND status = $3 RETURNING `+columns, id, domain.UserStatusActive, "disabled")
	if err != nil {
		return nil, mapErr("enable", err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, scan)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, mapErr("enable", err)
	}
	ok, err := r.exists(ctx, id)
	if err != nil {
		return nil, err
	}
	if ok {
		return nil, domain.ErrInvalidState
	}
	return nil, domain.ErrNotFound
}

// Disable 是动作"停用"：改状态和吊销会话写在同一条语句里（数据修改的 CTE 总会执行），原子完成，
// 条件也在同一条 UPDATE 里。
func (r *Repository) Disable(ctx context.Context, id string) (*domain.User, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `
		WITH u AS (
			UPDATE users SET status = $2, updated_at = now() WHERE id = $1 AND status = $3 RETURNING `+columns+`
		), revoked AS (
			UPDATE user_sessions SET revoked_at = now(), updated_at = now()
			WHERE user_id IN (SELECT id FROM u) AND revoked_at IS NULL
		)
		SELECT `+columns+` FROM u`, id, domain.UserStatusDisabled, domain.UserStatusActive)
	if err != nil {
		return nil, mapErr("disable", err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, scan)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, mapErr("disable", err)
	}
	ok, err := r.exists(ctx, id)
	if err != nil {
		return nil, err
	}
	if ok {
		return nil, domain.ErrInvalidState
	}
	return nil, domain.ErrNotFound
}

func (r *Repository) exists(ctx context.Context, id string) (bool, error) {
	var ok bool
	err := r.handle.Resolve(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, id).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("postgres: user exists: %w", err)
	}
	return ok, nil
}

// UpdateProfile 是 PATCH /v1/me：nil 的字段用 COALESCE 保持原值。
func (r *Repository) UpdateProfile(ctx context.Context, id string, displayName, avatarURL *string) (*domain.User, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `
		UPDATE users SET display_name = COALESCE($2, display_name), avatar_url = COALESCE($3, avatar_url), updated_at = now()
		WHERE id = $1 RETURNING `+columns, id, displayName, avatarURL)
	if err != nil {
		return nil, mapErr("update profile", err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, mapErr("update profile", err)
	}
	return v, nil
}

// MarkDeleted 是注销：状态置 deleted 并清空昵称、头像（个人信息保护法要求的最小化保留）。
func (r *Repository) MarkDeleted(ctx context.Context, id string) error {
	tag, err := r.handle.Resolve(ctx).Exec(ctx, `
		UPDATE users SET status = $2, display_name = '', avatar_url = '', updated_at = now()
		WHERE id = $1 AND status <> $2`, id, domain.UserStatusDeleted)
	if err != nil {
		return mapErr("mark deleted", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

var _ domain.Repository = (*Repository)(nil)
