package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"app.local/internal/modules/user/domain"
	"app.local/internal/platform/dbtx"
)

// WeChatOpenIDRepository 是 domain.WeChatOpenIDRepository 的 Postgres 实现。
type WeChatOpenIDRepository struct {
	handle *dbtx.Handle
}

func NewWeChatOpenIDRepository(handle *dbtx.Handle) *WeChatOpenIDRepository {
	return &WeChatOpenIDRepository{handle: handle}
}

func (r *WeChatOpenIDRepository) Get(ctx context.Context, userID, appid string) (string, error) {
	var openid string
	err := r.handle.Resolve(ctx).QueryRow(ctx, `SELECT openid FROM wechat_openids WHERE user_id = $1 AND appid = $2`, userID, appid).Scan(&openid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("postgres: get wechat openid: %w", err)
	}
	return openid, nil
}

// Upsert 按主键 (user_id, appid) 更新该入口的 openid；换绑/换 unionid 时调用方
// 先 DeleteByUser 再 Upsert。
func (r *WeChatOpenIDRepository) Upsert(ctx context.Context, userID, appid, openid string) error {
	_, err := r.handle.Resolve(ctx).Exec(ctx, `
		INSERT INTO wechat_openids (user_id, appid, openid, created_at, updated_at)
		VALUES ($1, $2, $3, now(), now())
		ON CONFLICT (user_id, appid) DO UPDATE SET openid = EXCLUDED.openid, updated_at = now()`,
		userID, appid, openid)
	if err != nil {
		return fmt.Errorf("postgres: upsert wechat openid: %w", err)
	}
	return nil
}

func (r *WeChatOpenIDRepository) DeleteByUser(ctx context.Context, userID string) error {
	_, err := r.handle.Resolve(ctx).Exec(ctx, `
		DELETE FROM wechat_openids WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("postgres: delete wechat openids: %w", err)
	}
	return nil
}

var _ domain.WeChatOpenIDRepository = (*WeChatOpenIDRepository)(nil)
