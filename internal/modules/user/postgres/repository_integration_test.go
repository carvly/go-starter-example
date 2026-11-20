//go:build integration

package postgres_test

import (
	"errors"
	"testing"
	"time"

	"app.local/internal/modules/user"
	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/domain/domaintest"
	"app.local/internal/modules/user/postgres"
	"app.local/internal/platform/postgres/pgtest"
)

func TestRepository_Contract(t *testing.T) {
	domaintest.RunRepositoryContract(t, func(t *testing.T) domain.Repository {
		return postgres.NewRepository(pgtest.Handle(t, user.Migrations))
	})
}

func TestIdentityRepository_Contract(t *testing.T) {
	domaintest.RunIdentityRepositoryContract(t, func(t *testing.T) domain.IdentityRepository {
		return postgres.NewIdentityRepository(pgtest.Handle(t, user.Migrations))
	})
}

func TestSessionRepository_Contract(t *testing.T) {
	domaintest.RunSessionRepositoryContract(t,
		func(t *testing.T) (domain.SessionRepository, func(*testing.T, string)) {
			// 每个子测试克隆独立数据库，仓库和 createUser 必须共用同一个 handle。
			handle := pgtest.Handle(t, user.Migrations)
			users := postgres.NewRepository(handle)
			return postgres.NewSessionRepository(handle, 7*24*time.Hour),
				func(t *testing.T, id string) {
					t.Helper()
					ctx := t.Context()
					if _, err := users.GetByID(ctx, id); err == nil {
						return
					}
					if err := users.Create(ctx, &domain.User{ID: id, DisplayName: "u", AvatarUrl: "", Status: domain.UserStatusActive, LastLoginAt: time.Now(), SignupClient: "web"}); err != nil {
						t.Fatal(err)
					}
				}
		})
}

func TestOAuthStateRepository_Contract(t *testing.T) {
	domaintest.RunOAuthStateRepositoryContract(t, func(t *testing.T) domain.OAuthStateRepository {
		return postgres.NewOAuthStateRepository(pgtest.Handle(t, user.Migrations))
	})
}

// 停用要在同一个事务里吊销该用户全部未吊销的会话：已吊销的不改吊销时间，别人的会话不受影响。
func TestRepository_DisableRevokesSessions(t *testing.T) {
	handle := pgtest.Handle(t, user.Migrations)
	repo := postgres.NewRepository(handle)
	ctx := t.Context()
	u, other, disabled := domaintest.ValidUser(), domaintest.ValidUser(), domaintest.ValidUser()
	u.Status, other.Status, disabled.Status = domain.UserStatusActive, domain.UserStatusActive, domain.UserStatusDisabled
	for _, v := range []*domain.User{u, other, disabled} {
		if err := repo.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	db := handle.Resolve(ctx)
	insert := func(id, userID string, revoked bool) {
		t.Helper()
		revokedAt := "NULL"
		if revoked {
			revokedAt = "'2026-01-01T00:00:00Z'"
		}
		if _, err := db.Exec(ctx, `INSERT INTO user_sessions (id, user_id, client, amr, auth_time, refresh_family, refresh_token_hash, ip, user_agent, expires_at, revoked_at)
			VALUES ($1, $2, 'web', '{wechat}', now(), $3, 'h', '127.0.0.1', 'test', now() + interval '1 day', `+revokedAt+`)`, id, userID, "family-"+id); err != nil {
			t.Fatal(err)
		}
	}
	const (
		active1 = "0199f1a2-0000-7000-8000-000000000001"
		active2 = "0199f1a2-0000-7000-8000-000000000002"
		revoked = "0199f1a2-0000-7000-8000-000000000003"
		others  = "0199f1a2-0000-7000-8000-000000000004"
		blocked = "0199f1a2-0000-7000-8000-000000000005"
	)
	insert(active1, u.ID, false)
	insert(active2, u.ID, false)
	insert(revoked, u.ID, true)
	insert(others, other.ID, false)
	insert(blocked, disabled.ID, false)

	if _, err := repo.Disable(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	// 条件不满足（已经停用）时什么都不改，会话也不吊销。
	if _, err := repo.Disable(ctx, disabled.ID); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("停用已停用的用户 error = %v, want ErrInvalidState", err)
	}
	rows, err := db.Query(ctx, `SELECT id::text, revoked_at IS NOT NULL, revoked_at IS NOT DISTINCT FROM '2026-01-01T00:00:00Z' FROM user_sessions`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		seen++
		var id string
		var isRevoked, untouched bool
		if err := rows.Scan(&id, &isRevoked, &untouched); err != nil {
			t.Fatal(err)
		}
		switch id {
		case active1, active2:
			if !isRevoked {
				t.Errorf("会话 %s 没有被吊销", id)
			}
		case revoked:
			if !untouched {
				t.Errorf("已吊销的会话 %s 的吊销时间被改了", id)
			}
		case others, blocked:
			if isRevoked {
				t.Errorf("不该吊销的会话 %s 被吊销了", id)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != 5 {
		t.Errorf("读到 %d 个会话，want 5", seen)
	}
}
