package postgres_test

import (
	"errors"
	"testing"

	"app.local/internal/modules/product/domain"
	"app.local/internal/modules/product/postgres"
	"app.local/internal/platform/dbtx"
)

// cursor 解析失败发生在碰连接池之前，所以 nil pool 也能验证它被翻译成 4xx 的
// ErrInvalidCursor，而不是落进 500。
func TestRepository_List_InvalidCursor(t *testing.T) {
	repo := postgres.NewRepository(dbtx.NewHandle(nil))

	_, _, err := repo.List(t.Context(), "not-a-valid-cursor!!", 10)
	if !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("List() error = %v, want ErrInvalidCursor", err)
	}
}
