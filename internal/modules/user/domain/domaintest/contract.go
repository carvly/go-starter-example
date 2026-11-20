package domaintest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
	"uuid"

	"app.local/internal/kit/page"
	"app.local/internal/modules/user/domain"
)

// RunRepositoryContract 是 domain.Repository 的行为契约，内存 fake 和 Postgres 实现跑同一套用例。
// newRepo 每次返回一个空仓库。
func RunRepositoryContract(t *testing.T, newRepo func(t *testing.T) domain.Repository) {
	ctx := context.Background()
	create := func(t *testing.T, repo domain.Repository, v *domain.User) *domain.User {
		t.Helper()
		if err := repo.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	t.Run("ValidUser 的每个字段都有值，Create 后 GetByID 读回相同的值", func(t *testing.T) {
		v := ValidUser()
		rv := reflect.ValueOf(v).Elem()
		for i := range rv.NumField() {
			if name := rv.Type().Field(i).Name; !generated[name] && rv.Field(i).IsZero() {
				t.Errorf("ValidUser 的 %s 是零值：加了字段之后在 domaintest/sample.go 里补上", name)
			}
		}
		repo := newRepo(t)
		create(t, repo, v)
		got, err := repo.GetByID(ctx, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertSame(t, v, got)
		if _, err := repo.GetByID(ctx, uuid.NewV7().String()); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("不存在时 error = %v, want ErrNotFound", err)
		}
	})

	t.Run("List 分页、总数和排序", func(t *testing.T) {
		repo := newRepo(t)
		for range 3 {
			create(t, repo, ValidUser())
		}
		p, err := page.New(1, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range domain.SortFields {
			for _, desc := range []bool{false, true} {
				r, err := repo.List(ctx, domain.UserFilter{}, page.Sort{Field: field, Desc: desc}, p)
				if err != nil || r.Total != 3 || r.TotalCapped || len(r.Items) != 2 {
					t.Fatalf("按 %s 排序 = %d 条 / 总数 %d，err = %v", field, len(r.Items), r.Total, err)
				}
				if c := compare(r.Items[0], r.Items[1], field); (desc && c < 0) || (!desc && c > 0) {
					t.Errorf("按 %s 排序（倒序 %v）的顺序不对", field, desc)
				}
			}
		}
	})

	t.Run("ListByIDs 只返回存在的用户，TouchLogin 更新最近登录时间", func(t *testing.T) {
		repo := newRepo(t)
		v := ValidUser()
		v.LastLoginAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		create(t, repo, v)
		old := v.LastLoginAt
		got, err := repo.ListByIDs(ctx, []string{v.ID, uuid.NewV7().String()})
		if err != nil || len(got) != 1 || got[0].ID != v.ID {
			t.Fatalf("ListByIDs = %+v, %v", got, err)
		}
		updated, err := repo.TouchLogin(ctx, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		if updated.LastLoginAt.Before(old) {
			t.Errorf("last_login_at = %v, want >= %v", updated.LastLoginAt, old)
		}
		if _, err := repo.TouchLogin(ctx, uuid.NewV7().String()); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("TouchLogin 不存在 error = %v, want ErrNotFound", err)
		}
	})

	t.Run("动作停用", func(t *testing.T) { runDisableContract(t, newRepo) })

	t.Run("动作启用", func(t *testing.T) {
		repo := newRepo(t)
		v := ValidUser()
		v.Status = domain.UserStatusDisabled
		create(t, repo, v)
		got, err := repo.Enable(ctx, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != domain.UserStatusActive {
			t.Errorf("status = %v, want %v", got.Status, domain.UserStatusActive)
		}
		w := ValidUser()
		w.Status = domain.UserStatusActive
		create(t, repo, w)
		if _, err := repo.Enable(ctx, w.ID); !errors.Is(err, domain.ErrInvalidState) {
			t.Fatalf("条件不满足时 error = %v, want ErrInvalidState", err)
		}
		if _, err := repo.Enable(ctx, uuid.NewV7().String()); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("不存在时 error = %v, want ErrNotFound", err)
		}
	})
}

// runDisableContract 是动作"停用"的契约：只停用 active 的用户。
func runDisableContract(t *testing.T, newRepo func(t *testing.T) domain.Repository) {
	ctx := context.Background()
	repo := newRepo(t)
	v := ValidUser()
	v.Status = domain.UserStatusActive
	if err := repo.Create(ctx, v); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Disable(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.UserStatusDisabled {
		t.Errorf("status = %v, want %v", got.Status, domain.UserStatusDisabled)
	}
	for _, s := range []domain.UserStatus{domain.UserStatusDisabled, domain.UserStatusDeleted} {
		w := ValidUser()
		w.Status = s
		if err := repo.Create(ctx, w); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Disable(ctx, w.ID); !errors.Is(err, domain.ErrInvalidState) {
			t.Fatalf("状态是 %v 时 error = %v, want ErrInvalidState", s, err)
		}
	}
	if _, err := repo.Disable(ctx, uuid.NewV7().String()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("不存在时 error = %v, want ErrNotFound", err)
	}
}

// generated 是由 Repository 填写的字段，夹具里可以是零值，比较时也跳过。
var generated = map[string]bool{"CreatedAt": true, "UpdatedAt": true, "Version": true}

// assertSame 逐个字段比较；时间按时刻比较（读回的时区可能不同）。
func assertSame(t *testing.T, want, got *domain.User) {
	t.Helper()
	w, g := reflect.ValueOf(want).Elem(), reflect.ValueOf(got).Elem()
	for i := range w.NumField() {
		name := w.Type().Field(i).Name
		if generated[name] {
			continue
		}
		if a, b := w.Field(i).Interface(), g.Field(i).Interface(); !same(a, b) {
			t.Errorf("%s = %v, want %v", name, b, a)
		}
	}
}

func same(a, b any) bool {
	switch a := a.(type) {
	case time.Time:
		return a.Equal(b.(time.Time))
	case *time.Time:
		b := b.(*time.Time)
		return (a == nil) == (b == nil) && (a == nil || a.Equal(*b))
	}
	return reflect.DeepEqual(a, b)
}
