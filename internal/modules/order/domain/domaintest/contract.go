package domaintest

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"app.local/internal/kit/page"
	"app.local/internal/modules/order/domain"
)

// RunRepositoryContract 是 domain.Repository 的行为契约，内存 fake 和 Postgres
// 实现跑同一套用例。newRepo 返回一个空仓库和一个供订单引用的商品 ID。
func RunRepositoryContract(t *testing.T, newRepo func(t *testing.T) (repo domain.Repository, productID string)) {
	ctx := t.Context()
	create := func(t *testing.T, repo domain.Repository, productID string) *domain.Order {
		t.Helper()
		o := &domain.Order{
			ID: uuid.NewV7().String(), CustomerName: "张三", ProductID: productID, ProductName: "p",
			Quantity: 2, UnitPriceCents: 100, TotalCents: 200, Status: domain.StatusPending,
		}
		if err := repo.Create(ctx, o); err != nil {
			t.Fatal(err)
		}
		return o
	}

	t.Run("Create 后 GetByID 能读回", func(t *testing.T) {
		repo, pid := newRepo(t)
		o := create(t, repo, pid)
		got, err := repo.GetByID(ctx, o.ID)
		if err != nil || got.TotalCents != 200 || got.Status != domain.StatusPending || got.ProductID != pid {
			t.Fatalf("GetByID = %+v, %v", got, err)
		}
	})

	t.Run("GetByID 不存在返回 ErrNotFound", func(t *testing.T) {
		repo, _ := newRepo(t)
		if _, err := repo.GetByID(ctx, uuid.NewV7().String()); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("List 分页：cursor 能翻到最后一页", func(t *testing.T) {
		repo, pid := newRepo(t)
		for range 3 {
			create(t, repo, pid)
		}
		page1, next, err := repo.List(ctx, "", 2)
		if err != nil || len(page1) != 2 || next == "" {
			t.Fatalf("第一页 = %d 条, next = %q, err = %v", len(page1), next, err)
		}
		page2, next2, err := repo.List(ctx, next, 2)
		if err != nil || len(page2) != 1 || next2 != "" {
			t.Fatalf("第二页 = %d 条, next = %q, err = %v", len(page2), next2, err)
		}
	})

	t.Run("UpdateStatus 只允许从 pending 转出", func(t *testing.T) {
		repo, pid := newRepo(t)
		o := create(t, repo, pid)
		if err := repo.UpdateStatus(ctx, o.ID, domain.StatusConfirmed); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpdateStatus(ctx, o.ID, domain.StatusFailed); !errors.Is(err, domain.ErrInvalidStatusTransition) {
			t.Errorf("error = %v, want ErrInvalidStatusTransition", err)
		}
		if err := repo.UpdateStatus(ctx, uuid.NewV7().String(), domain.StatusFailed); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("MarkEventProcessed 第二次返回 firstTime=false", func(t *testing.T) {
		repo, pid := newRepo(t)
		o := create(t, repo, pid)
		first, err := repo.MarkEventProcessed(ctx, o.ID)
		if err != nil || !first {
			t.Fatalf("第一次 = %v, %v", first, err)
		}
		again, err := repo.MarkEventProcessed(ctx, o.ID)
		if err != nil || again {
			t.Fatalf("第二次 = %v, %v, want false", again, err)
		}
	})

	t.Run("Search 按状态和时间半开区间筛选、按创建倒序分页、带总数", func(t *testing.T) {
		repo, pid := newRepo(t)
		var orders []*domain.Order
		for range 4 {
			orders = append(orders, create(t, repo, pid))
			time.Sleep(2 * time.Millisecond) // 保证 created_at 互不相同
		}
		if err := repo.UpdateStatus(ctx, orders[1].ID, domain.StatusConfirmed); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpdateStatus(ctx, orders[3].ID, domain.StatusConfirmed); err != nil {
			t.Fatal(err)
		}
		req := func(n, size int) page.Request {
			r, err := page.New(n, size)
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		ids := func(r page.Result[*domain.Order]) []string {
			out := make([]string, len(r.Items))
			for i, o := range r.Items {
				out[i] = o.ID
			}
			return out
		}

		all, err := repo.Search(ctx, domain.SearchFilter{}, req(1, 3))
		if err != nil || all.Total != 4 || !slices.Equal(ids(all), []string{orders[3].ID, orders[2].ID, orders[1].ID}) {
			t.Fatalf("不筛选第一页 = %v / 总数 %d, %v", ids(all), all.Total, err)
		}
		if p2, _ := repo.Search(ctx, domain.SearchFilter{}, req(2, 3)); !slices.Equal(ids(p2), []string{orders[0].ID}) {
			t.Errorf("不筛选第二页 = %v", ids(p2))
		}
		confirmed, _ := repo.Search(ctx, domain.SearchFilter{Statuses: []domain.Status{domain.StatusConfirmed}}, req(1, 10))
		if confirmed.Total != 2 || !slices.Equal(ids(confirmed), []string{orders[3].ID, orders[1].ID}) {
			t.Errorf("confirmed = %v / 总数 %d", ids(confirmed), confirmed.Total)
		}
		if multi, _ := repo.Search(ctx, domain.SearchFilter{Statuses: []domain.Status{domain.StatusConfirmed, domain.StatusPending}}, req(1, 10)); multi.Total != 4 {
			t.Errorf("confirmed 或 pending 总数 = %d, want 4", multi.Total)
		}
		got, err := repo.GetByID(ctx, orders[1].ID)
		if err != nil {
			t.Fatal(err)
		}
		end, err := repo.GetByID(ctx, orders[3].ID)
		if err != nil {
			t.Fatal(err)
		}
		ranged, _ := repo.Search(ctx, domain.SearchFilter{From: got.CreatedAt, To: end.CreatedAt}, req(1, 10))
		if ranged.Total != 2 || !slices.Equal(ids(ranged), []string{orders[2].ID, orders[1].ID}) {
			t.Errorf("[orders[1], orders[3]) = %v / 总数 %d, want orders[2], orders[1]", ids(ranged), ranged.Total)
		}
		both, _ := repo.Search(ctx, domain.SearchFilter{Statuses: []domain.Status{domain.StatusConfirmed}, From: got.CreatedAt, To: end.CreatedAt}, req(1, 10))
		if !slices.Equal(ids(both), []string{orders[1].ID}) {
			t.Errorf("状态 + 时间 = %v, want 只有 orders[1]", ids(both))
		}
	})

	t.Run("非法 cursor 返回 ErrInvalidCursor", func(t *testing.T) {
		repo := first(newRepo(t))
		if _, _, err := repo.List(ctx, "not-a-valid-cursor!!", 10); !errors.Is(err, domain.ErrInvalidCursor) {
			t.Fatalf("error = %v, want ErrInvalidCursor", err)
		}
	})
}

func first(repo domain.Repository, _ string) domain.Repository { return repo }
