//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	"app.local/internal/modules/user"
	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/postgres"
	"app.local/internal/platform/postgres/pgtest"
)

func TestWeChatOpenIDRepository_Get(t *testing.T) {
	repo := postgres.NewWeChatOpenIDRepository(pgtest.Handle(t, user.Migrations))
	ctx := t.Context()
	if err := repo.Upsert(ctx, "0199f1a2-3b4c-7d5e-8f60-718293a4b5c6", "mini", "openid-1"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "0199f1a2-3b4c-7d5e-8f60-718293a4b5c6", "mini")
	if err != nil || got != "openid-1" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if _, err := repo.Get(ctx, "0199f1a2-3b4c-7d5e-8f60-718293a4b5c6", "unknown"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("不存在 error = %v, want ErrNotFound", err)
	}
}
