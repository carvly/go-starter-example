package domaintest

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"
	"uuid"

	"app.local/internal/modules/user/domain"
)

// RunIdentityRepositoryContract 是 domain.IdentityRepository 的行为契约。
func RunIdentityRepositoryContract(t *testing.T, newRepo func(t *testing.T) domain.IdentityRepository) {
	ctx := context.Background()

	t.Run("唯一约束：provider+subject 与 user+provider 各只能有一行", func(t *testing.T) {
		repo := newRepo(t)
		v := ValidIdentity()
		if err := repo.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetByProviderSubject(ctx, v.Provider, v.Subject)
		if err != nil || got.ID != v.ID {
			t.Fatalf("GetByProviderSubject = %v, %v", got, err)
		}
		got, err = repo.GetByUserAndProvider(ctx, v.UserID, v.Provider)
		if err != nil || got.ID != v.ID {
			t.Fatalf("GetByUserAndProvider = %v, %v", got, err)
		}

		dupSubject := ValidIdentity()
		dupSubject.Provider, dupSubject.Subject = v.Provider, v.Subject
		if err := repo.Create(ctx, dupSubject); err == nil {
			t.Error("重复 (provider, subject) 应该失败")
		}
		dupProvider := ValidIdentity()
		dupProvider.UserID, dupProvider.Provider = v.UserID, v.Provider
		if err := repo.Create(ctx, dupProvider); err == nil {
			t.Error("同一账号重复 (provider) 应该失败")
		}
		if _, err := repo.GetByProviderSubject(ctx, v.Provider, "no-such"); !errors.Is(err, domain.ErrIdentityNotFound) {
			t.Errorf("不存在 error = %v, want ErrIdentityNotFound", err)
		}
	})

	t.Run("Replace 换成新行：旧行消失、新 subject 可查", func(t *testing.T) {
		repo := newRepo(t)
		v := ValidIdentity()
		if err := repo.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		next := ValidIdentity()
		next.UserID, next.Provider = v.UserID, v.Provider
		replaced, err := repo.Replace(ctx, v.UserID, v.Provider, next)
		if err != nil {
			t.Fatal(err)
		}
		if replaced.ID != next.ID {
			t.Errorf("Replace 返回 %v, want 新行 %v", replaced.ID, next.ID)
		}
		if _, err := repo.GetByProviderSubject(ctx, v.Provider, v.Subject); !errors.Is(err, domain.ErrIdentityNotFound) {
			t.Errorf("旧 subject 还能查到：%v", err)
		}
		if _, err := repo.GetByProviderSubject(ctx, next.Provider, next.Subject); err != nil {
			t.Errorf("新 subject 查不到：%v", err)
		}
		if _, err := repo.Replace(ctx, uuid.NewV7().String(), v.Provider, ValidIdentity()); !errors.Is(err, domain.ErrIdentityNotFound) {
			t.Errorf("Replace 不存在的旧行 error = %v, want ErrIdentityNotFound", err)
		}
	})

	t.Run("TouchLogin 更新 last_login_at 和 profile；Delete 后查不到", func(t *testing.T) {
		repo := newRepo(t)
		v := ValidIdentity()
		if err := repo.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		if err := repo.TouchLogin(ctx, v.ID, map[string]any{"name": "新昵称"}); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetByProviderSubject(ctx, v.Provider, v.Subject)
		if err != nil {
			t.Fatal(err)
		}
		if got.Profile["name"] != "新昵称" {
			t.Errorf("profile = %v", got.Profile)
		}
		if got.LastLoginAt == nil || !got.LastLoginAt.After(v.LastLoginAt.Add(-time.Second)) {
			t.Errorf("last_login_at = %v", got.LastLoginAt)
		}
		if err := repo.Delete(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.GetByProviderSubject(ctx, v.Provider, v.Subject); !errors.Is(err, domain.ErrIdentityNotFound) {
			t.Errorf("Delete 后还能查到：%v", err)
		}
		if err := repo.Delete(ctx, v.ID); !errors.Is(err, domain.ErrIdentityNotFound) {
			t.Errorf("重复 Delete error = %v, want ErrIdentityNotFound", err)
		}
	})
	t.Run("PageByUser 按创建倒序分页，不漏不重，只含本账号", func(t *testing.T) {
		repo := newRepo(t)
		userID := uuid.NewV7().String()
		var want []string
		for i := range 5 {
			v := ValidIdentity()
			v.UserID, v.Provider = userID, "p"+strconv.Itoa(i)
			if err := repo.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
			want = append([]string{v.ID}, want...)
		}
		if err := repo.Create(ctx, ValidIdentity()); err != nil {
			t.Fatal(err)
		}
		got := collectPages(t, func(cursor string) ([]string, string, error) {
			items, next, err := repo.PageByUser(ctx, userID, cursor, 2)
			return identityIDs(items), next, err
		})
		if !slices.Equal(got, want) {
			t.Errorf("分页结果 = %v, want %v", got, want)
		}
		if _, _, err := repo.PageByUser(ctx, userID, "not-a-valid-cursor!!", 2); !errors.Is(err, domain.ErrInvalidCursor) {
			t.Errorf("非法 cursor error = %v, want ErrInvalidCursor", err)
		}
	})
}

// collectPages 从第一页翻到最后一页，返回全部 ID；翻页超过 20 次视为死循环。
func collectPages(t *testing.T, page func(cursor string) ([]string, string, error)) []string {
	t.Helper()
	var all []string
	cursor := ""
	for range 20 {
		ids, next, err := page(cursor)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, ids...)
		if next == "" {
			return all
		}
		cursor = next
	}
	t.Fatal("翻页没有结束")
	return nil
}

func identityIDs(v []*domain.Identity) []string {
	out := make([]string, 0, len(v))
	for _, x := range v {
		out = append(out, x.ID)
	}
	return out
}

// RunSessionRepositoryContract 是 domain.SessionRepository 的行为契约。users 表
// 由调用方保证存在：newRepo 拿到的 handle 上用户可能不存在，GetValid 带
// users.status 检查，契约用例先建用户行（同一 handle）。
// newRepo 返回仓库和 createUser；createUser 确保 users 表里有该用户（GetValid 带
// users.status 检查）。两个回调必须用同一个 handle，Postgres 侧每个子测试克隆
// 独立数据库。
func RunSessionRepositoryContract(t *testing.T, newRepo func(t *testing.T) (domain.SessionRepository, func(t *testing.T, id string))) {
	ctx := context.Background()

	t.Run("GetValid：未吊销、未过期、未空闲超时且账号 active 才命中", func(t *testing.T) {
		repo, createUser := newRepo(t)
		base := ValidSession()
		createUser(t, base.UserID)

		ok := *base
		if err := repo.Create(ctx, &ok); err != nil {
			t.Fatal(err)
		}
		if got, err := repo.GetValid(ctx, ok.ID); err != nil || got.ID != ok.ID {
			t.Fatalf("正常会话 = %v, %v", got, err)
		}

		revoked := ValidSession()
		revoked.UserID = base.UserID
		createUser(t, revoked.UserID)
		if err := repo.Create(ctx, revoked); err != nil {
			t.Fatal(err)
		}
		if err := repo.Revoke(ctx, revoked.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.GetValid(ctx, revoked.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("已吊销 error = %v, want ErrNotFound", err)
		}

		expired := ValidSession()
		expired.UserID = base.UserID
		expired.ExpiresAt = time.Now().Add(-time.Minute)
		if err := repo.Create(ctx, expired); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.GetValid(ctx, expired.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("已过期 error = %v, want ErrNotFound", err)
		}

		idle := ValidSession()
		idle.UserID = base.UserID
		idle.LastUsedAt = time.Now().Add(-8 * 24 * time.Hour)
		if err := repo.Create(ctx, idle); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.GetValid(ctx, idle.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("空闲超时 error = %v, want ErrNotFound", err)
		}

		if _, err := repo.GetValid(ctx, uuid.NewV7().String()); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("不存在 error = %v, want ErrNotFound", err)
		}
	})

	t.Run("TryRotateRefresh：当前值命中、宽限期内上一代命中、其它不命中", func(t *testing.T) {
		repo, createUser := newRepo(t)
		v := ValidSession()
		createUser(t, v.UserID)
		if err := repo.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		const idle = 7 * 24 * time.Hour

		// 当前值：轮换成功，旧 hash 变成 previous。
		got, err := repo.TryRotateRefresh(ctx, v.RefreshFamily, v.RefreshTokenHash, "new-hash-1", 10*time.Second, idle, time.Now())
		if err != nil || got == nil {
			t.Fatalf("轮换当前值 = %v, %v", got, err)
		}
		if got.RefreshTokenHash != "new-hash-1" || got.PreviousRefreshTokenHash == nil || *got.PreviousRefreshTokenHash != v.RefreshTokenHash {
			t.Errorf("轮换后的哈希不对：%+v", got)
		}

		// 上一代在 10 秒宽限期内：允许，再轮换一代。
		got2, err := repo.TryRotateRefresh(ctx, v.RefreshFamily, v.RefreshTokenHash, "new-hash-2", 10*time.Second, idle, time.Now())
		if err != nil || got2 == nil {
			t.Fatalf("宽限期内轮换上一代 = %v, %v", got2, err)
		}

		// 上上代（已不在宽限期）：不命中，行也不再变化。
		miss, err := repo.TryRotateRefresh(ctx, v.RefreshFamily, v.RefreshTokenHash, "new-hash-3", 10*time.Second, idle, time.Now())
		if err != nil || miss != nil {
			t.Errorf("上上代应该不命中：%v, %v", miss, err)
		}
		cur, err := repo.GetByFamily(ctx, v.RefreshFamily)
		if err != nil {
			t.Fatal(err)
		}
		if cur.RefreshTokenHash != "new-hash-2" {
			t.Errorf("未命中的轮换改动了行：%v", cur.RefreshTokenHash)
		}

		// 宽限期外 presenting 上一代：不命中。
		old := ValidSession()
		createUser(t, old.UserID)
		if err := repo.Create(ctx, old); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.TryRotateRefresh(ctx, old.RefreshFamily, old.RefreshTokenHash, "h", 10*time.Second, idle, time.Now()); err != nil {
			t.Fatal(err)
		}
		later := time.Now().Add(11 * time.Second)
		if miss, err := repo.TryRotateRefresh(ctx, old.RefreshFamily, old.RefreshTokenHash, "h2", 10*time.Second, idle, later); err != nil || miss != nil {
			t.Errorf("宽限期外的上一代应该不命中：%v, %v", miss, err)
		}
	})

	t.Run("吊销语义：RevokeByLoginIdentity 除外当前会话，UpdateAuthTime 追加 amr", func(t *testing.T) {
		repo, createUser := newRepo(t)
		idn := uuid.NewV7().String()
		owner, other := ValidSession(), ValidSession()
		owner.LoginIdentityID, other.LoginIdentityID = &idn, &idn
		createUser(t, owner.UserID)
		createUser(t, other.UserID)
		for _, v := range []*domain.Session{owner, other} {
			if err := repo.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.RevokeByLoginIdentity(ctx, idn, owner.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.GetValid(ctx, other.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("其它会话应该被吊销：%v", err)
		}
		if _, err := repo.GetValid(ctx, owner.ID); err != nil {
			t.Errorf("当前会话不应被吊销：%v", err)
		}

		before := len(owner.AMR)
		if err := repo.UpdateAuthTime(ctx, owner.ID, time.Now(), "wechat"); err != nil {
			t.Fatal(err)
		}
		got, err := repo.Get(ctx, owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.AMR) != before+1 || got.AMR[len(got.AMR)-1] != "wechat" {
			t.Errorf("amr = %v, want 追加 wechat", got.AMR)
		}

		if err := repo.ClearLoginIdentity(ctx, owner.ID, idn); err != nil {
			t.Fatal(err)
		}
		got, err = repo.Get(ctx, owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.LoginIdentityID != nil {
			t.Errorf("login_identity_id = %v, want nil", *got.LoginIdentityID)
		}
	})

	t.Run("PageByUser 只列未吊销的，按创建倒序分页，不漏不重", func(t *testing.T) {
		repo, createUser := newRepo(t)
		userID := uuid.NewV7().String()
		createUser(t, userID)
		base := time.Now().Add(-time.Hour)
		var want []string
		for i := range 5 {
			v := ValidSession()
			v.UserID = userID
			v.CreatedAt = base.Add(time.Duration(i) * time.Minute)
			if err := repo.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
			want = append([]string{v.ID}, want...)
		}
		revoked := ValidSession()
		revoked.UserID = userID
		if err := repo.Create(ctx, revoked); err != nil {
			t.Fatal(err)
		}
		if err := repo.Revoke(ctx, revoked.ID); err != nil {
			t.Fatal(err)
		}
		other := ValidSession()
		createUser(t, other.UserID)
		if err := repo.Create(ctx, other); err != nil {
			t.Fatal(err)
		}
		got := collectPages(t, func(cursor string) ([]string, string, error) {
			items, next, err := repo.PageByUser(ctx, userID, cursor, 2)
			ids := make([]string, 0, len(items))
			for _, x := range items {
				ids = append(ids, x.ID)
			}
			return ids, next, err
		})
		if !slices.Equal(got, want) {
			t.Errorf("分页结果 = %v, want %v", got, want)
		}
		if _, _, err := repo.PageByUser(ctx, userID, "not-a-valid-cursor!!", 2); !errors.Is(err, domain.ErrInvalidCursor) {
			t.Errorf("非法 cursor error = %v, want ErrInvalidCursor", err)
		}
	})

	t.Run("PurgeExpired 删除过期和吊销超保留期的行", func(t *testing.T) {
		repo, createUser := newRepo(t)
		expired := ValidSession()
		expired.ExpiresAt = time.Now().Add(-time.Minute)
		createUser(t, expired.UserID)
		if err := repo.Create(ctx, expired); err != nil {
			t.Fatal(err)
		}
		revokedOld := ValidSession()
		createUser(t, revokedOld.UserID)
		if err := repo.Create(ctx, revokedOld); err != nil {
			t.Fatal(err)
		}
		if err := repo.Revoke(ctx, revokedOld.ID); err != nil {
			t.Fatal(err)
		}
		alive := ValidSession()
		createUser(t, alive.UserID)
		if err := repo.Create(ctx, alive); err != nil {
			t.Fatal(err)
		}
		// fake 的吊销时间是 now，把截止时间拨后一秒再删：刚吊销的行
		// （revoked_at < now+1s - 0）和过期行被删，正常行保留。
		n, err := repo.PurgeExpired(ctx, time.Now().Add(time.Second), 0)
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Errorf("删除 %d 行, want 2（过期 + 刚吊销且保留期 0）", n)
		}
		if _, err := repo.Get(ctx, alive.ID); err != nil {
			t.Errorf("正常会话被误删：%v", err)
		}
	})
}

// RunOAuthStateRepositoryContract 是 domain.OAuthStateRepository 的行为契约。
func RunOAuthStateRepositoryContract(t *testing.T, newRepo func(t *testing.T) domain.OAuthStateRepository) {
	ctx := context.Background()

	t.Run("PKCE verifier 和 nonce 随 state 保存、原样取回", func(t *testing.T) {
		repo := newRepo(t)
		v := ValidState()
		v.CodeVerifier = "verifier-0123456789-0123456789-0123456789"
		v.Nonce = "nonce-1"
		if err := repo.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		got, err := repo.Take(ctx, v.IDHash)
		if err != nil || got.CodeVerifier != v.CodeVerifier || got.Nonce != v.Nonce {
			t.Fatalf("Take = %+v, %v", got, err)
		}
	})

	t.Run("Take 一次性：第二次取不到，过期行也取不到", func(t *testing.T) {
		repo := newRepo(t)
		v := ValidState()
		if err := repo.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		got, err := repo.Take(ctx, v.IDHash)
		if err != nil || got.IDHash != v.IDHash || got.ReturnTo != v.ReturnTo || got.Intent != v.Intent {
			t.Fatalf("Take = %+v, %v", got, err)
		}
		if _, err := repo.Take(ctx, v.IDHash); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("第二次 Take error = %v, want ErrNotFound", err)
		}

		expired := ValidState()
		expired.ExpiresAt = time.Now().Add(-time.Minute)
		if err := repo.Create(ctx, expired); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Take(ctx, expired.IDHash); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("过期行 Take error = %v, want ErrNotFound", err)
		}

		// 前面 Take 失败的过期行不会被消费（Postgres 的 DELETE 匹配 0 行，
		// 内存 fake 同样保留），和这里新建的行一起被清理。
		expired2 := ValidState()
		expired2.ExpiresAt = time.Now().Add(-time.Minute)
		if err := repo.Create(ctx, expired2); err != nil {
			t.Fatal(err)
		}
		if n, err := repo.PurgeExpired(ctx, time.Now()); err != nil || n != 2 {
			t.Errorf("PurgeExpired = %d, %v, want 2", n, err)
		}
	})
}
