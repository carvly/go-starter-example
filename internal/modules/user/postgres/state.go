package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"app.local/internal/modules/user/domain"
	"app.local/internal/platform/dbtx"
)

// OAuthStateRepository 是 domain.OAuthStateRepository 的 Postgres 实现。
// 表里的 payload JSONB 存流程上下文（intent、sid、return_to、兑换结果、PKCE verifier、nonce），其它字段是独立的列。
type OAuthStateRepository struct {
	handle *dbtx.Handle
}

func NewOAuthStateRepository(handle *dbtx.Handle) *OAuthStateRepository {
	return &OAuthStateRepository{handle: handle}
}

type statePayload struct {
	Intent   domain.OAuthFlowIntent   `json:"intent"`
	SID      string                   `json:"sid,omitempty"`
	ReturnTo string                   `json:"return_to,omitempty"`
	Identity *domain.ExternalIdentity `json:"identity,omitempty"`
	Verifier string                   `json:"code_verifier,omitempty"`
	Nonce    string                   `json:"nonce,omitempty"`
}

const stateColumns = `id_hash, kind, provider, payload, binding_hash, expires_at, created_at`

func scanState(row pgx.CollectableRow) (*domain.OAuthState, error) {
	var (
		v       domain.OAuthState
		payload statePayload
		raw     []byte
	)
	if err := row.Scan(&v.IDHash, &v.Kind, &v.Provider, &raw, &v.BindingHash, &v.ExpiresAt, &v.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("postgres: scan oauth state: unmarshal payload: %w", err)
	}
	v.Intent, v.SID, v.ReturnTo, v.Identity = payload.Intent, payload.SID, payload.ReturnTo, payload.Identity
	v.CodeVerifier, v.Nonce = payload.Verifier, payload.Nonce
	return &v, nil
}

func (r *OAuthStateRepository) Create(ctx context.Context, v *domain.OAuthState) error {
	payload, err := json.Marshal(statePayload{Intent: v.Intent, SID: v.SID, ReturnTo: v.ReturnTo, Identity: v.Identity, Verifier: v.CodeVerifier, Nonce: v.Nonce})
	if err != nil {
		return fmt.Errorf("postgres: create oauth state: marshal payload: %w", err)
	}
	_, err = r.handle.Resolve(ctx).Exec(ctx, `
		INSERT INTO oauth_states (id_hash, kind, provider, payload, binding_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())`,
		v.IDHash, v.Kind, v.Provider, payload, v.BindingHash, v.ExpiresAt)
	if err != nil {
		return fmt.Errorf("postgres: create oauth state: %w", err)
	}
	return nil
}

// Take 一次性读取：DELETE ... WHERE id_hash = $1 AND expires_at > now() RETURNING。
// 不存在或已过期返回 ErrNotFound。
func (r *OAuthStateRepository) Take(ctx context.Context, idHash string) (*domain.OAuthState, error) {
	rows, err := r.handle.Resolve(ctx).Query(ctx, `
		DELETE FROM oauth_states WHERE id_hash = $1 AND expires_at > now()
		RETURNING `+stateColumns, idHash)
	if err != nil {
		return nil, fmt.Errorf("postgres: take oauth state: %w", err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, scanState)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: take oauth state: %w", err)
	}
	return v, nil
}

func (r *OAuthStateRepository) PurgeExpired(ctx context.Context, now time.Time) (int64, error) {
	tag, err := r.handle.Resolve(ctx).Exec(ctx, `
		DELETE FROM oauth_states WHERE expires_at < $1`, now)
	if err != nil {
		return 0, fmt.Errorf("postgres: purge oauth states: %w", err)
	}
	return tag.RowsAffected(), nil
}

var _ domain.OAuthStateRepository = (*OAuthStateRepository)(nil)
