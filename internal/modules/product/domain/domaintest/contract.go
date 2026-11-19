package domaintest

import (
	"errors"
	"sync"
	"testing"
	"uuid"

	"app.local/internal/kit/page"
	"app.local/internal/modules/product/domain"
)

// RunRepositoryContract 是 domain.Repository 的行为契约。内存 fake 和 Postgres
// 实现跑同一套用例，保证 domain 测试里用的 fake 不说谎。newRepo 每次返回一个空仓库。
func RunRepositoryContract(t *testing.T, newRepo func(t *testing.T) domain.Repository) {
	ctx := t.Context()
	mk := func(name string, stock int64) *domain.Product {
		return &domain.Product{ID: uuid.NewV7().String(), Name: name, PriceCents: 100, Stock: stock}
	}

	t.Run("Create 后 GetByID 能读回", func(t *testing.T) {
		repo := newRepo(t)
		p := mk("键盘", 3)
		if err := repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetByID(ctx, p.ID)
		if err != nil || got.Name != "键盘" || got.Stock != 3 || got.PriceCents != 100 {
			t.Fatalf("GetByID = %+v, %v", got, err)
		}
	})

	t.Run("GetByID 不存在返回 ErrNotFound", func(t *testing.T) {
		if _, err := newRepo(t).GetByID(ctx, uuid.NewV7().String()); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("List 按创建倒序分页，cursor 能翻到最后一页", func(t *testing.T) {
		repo := newRepo(t)
		var ids []string
		for _, n := range []string{"a", "b", "c"} {
			p := mk(n, 1)
			if err := repo.Create(ctx, p); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, p.ID)
		}
		page1, next, err := repo.List(ctx, "", 2)
		if err != nil || len(page1) != 2 || next == "" {
			t.Fatalf("第一页 = %d 条, next = %q, err = %v", len(page1), next, err)
		}
		if page1[0].ID != ids[2] || page1[1].ID != ids[1] {
			t.Errorf("第一页顺序不是创建倒序")
		}
		page2, next2, err := repo.List(ctx, next, 2)
		if err != nil || len(page2) != 1 || page2[0].ID != ids[0] || next2 != "" {
			t.Fatalf("第二页 = %+v, next = %q, err = %v", page2, next2, err)
		}
	})

	t.Run("DecreaseStock 区分库存不足和商品不存在", func(t *testing.T) {
		repo := newRepo(t)
		p := mk("p", 5)
		if err := repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		if err := repo.DecreaseStock(ctx, p.ID, 2); err != nil {
			t.Fatal(err)
		}
		if err := repo.DecreaseStock(ctx, p.ID, 4); !errors.Is(err, domain.ErrInsufficientStock) {
			t.Errorf("error = %v, want ErrInsufficientStock", err)
		}
		if err := repo.DecreaseStock(ctx, uuid.NewV7().String(), 1); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
		if got, _ := repo.GetByID(ctx, p.ID); got.Stock != 3 {
			t.Errorf("stock = %d, want 3", got.Stock)
		}
	})

	t.Run("并发扣库存不会超卖", func(t *testing.T) {
		repo := newRepo(t)
		p := mk("p", 10)
		if err := repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		succeeded := 0
		for range 25 {
			wg.Go(func() {
				if err := repo.DecreaseStock(ctx, p.ID, 1); err == nil {
					mu.Lock()
					succeeded++
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		got, _ := repo.GetByID(ctx, p.ID)
		if succeeded != 10 || got.Stock != 0 {
			t.Errorf("成功 %d 次, 剩余库存 %d, want 10 / 0", succeeded, got.Stock)
		}
	})

	t.Run("Search 按名称筛选（不区分大小写、% 和 _ 按字面匹配）、按创建倒序分页、带总数", func(t *testing.T) {
		repo := newRepo(t)
		var ids []string
		for _, n := range []string{"Keyboard 50%", "keyboard pro", "鼠标", "KEYBOARD_mini", "keyboard 500"} {
			p := mk(n, 1)
			if err := repo.Create(ctx, p); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, p.ID)
		}
		req := func(n, size int) page.Request {
			r, err := page.New(n, size)
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		got, err := repo.Search(ctx, "keyboard", req(1, 2))
		if err != nil || got.Total != 4 || got.TotalCapped || len(got.Items) != 2 ||
			got.Items[0].ID != ids[4] || got.Items[1].ID != ids[3] {
			t.Fatalf("第一页 = %+v, %v", got, err)
		}
		got, err = repo.Search(ctx, "keyboard", req(2, 2))
		if err != nil || got.Total != 4 || len(got.Items) != 2 || got.Items[0].ID != ids[1] || got.Items[1].ID != ids[0] {
			t.Fatalf("第二页 = %+v, %v", got, err)
		}
		if got, _ := repo.Search(ctx, "50%", req(1, 10)); got.Total != 1 || got.Items[0].ID != ids[0] {
			t.Errorf("搜索 50%% = %+v, want 只有 Keyboard 50%%", got)
		}
		if got, _ := repo.Search(ctx, "d_m", req(1, 10)); got.Total != 1 || got.Items[0].ID != ids[3] {
			t.Errorf("搜索 d_m = %+v, want 只有 KEYBOARD_mini", got)
		}
		if got, _ := repo.Search(ctx, "", req(1, 10)); got.Total != 5 || len(got.Items) != 5 {
			t.Errorf("不筛选 = %d 条 / 总数 %d, want 5 / 5", len(got.Items), got.Total)
		}
	})

	t.Run("Update 改名称和单价并把 version 加 1，库存不变", func(t *testing.T) {
		repo := newRepo(t)
		p := mk("旧名字", 7)
		if err := repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		if p.Version != 1 {
			t.Fatalf("Create 后 Version = %d, want 1", p.Version)
		}
		got, err := repo.Update(ctx, p.ID, "新名字", 2500, 1)
		if err != nil || got.Name != "新名字" || got.PriceCents != 2500 || got.Stock != 7 || got.Version != 2 {
			t.Fatalf("Update = %+v, %v", got, err)
		}
		if again, _ := repo.GetByID(ctx, p.ID); again.Name != "新名字" || again.PriceCents != 2500 || again.Version != 2 {
			t.Errorf("读回 = %+v", again)
		}
		if _, err := repo.Update(ctx, uuid.NewV7().String(), "x", 1, 1); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("不存在 error = %v, want ErrNotFound", err)
		}
	})

	t.Run("Update 带的 version 不是最新时返回 ErrConflict，不改", func(t *testing.T) {
		repo := newRepo(t)
		p := mk("原名", 1)
		if err := repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Update(ctx, p.ID, "先保存的", 200, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Update(ctx, p.ID, "后保存的", 300, 1); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("旧 version error = %v, want ErrConflict", err)
		}
		if again, _ := repo.GetByID(ctx, p.ID); again.Name != "先保存的" || again.Version != 2 {
			t.Errorf("冲突后读回 = %+v, want 保留先保存的", again)
		}
	})

	t.Run("调库存和扣库存不改 version", func(t *testing.T) {
		repo := newRepo(t)
		p := mk("p", 5)
		if err := repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		if got, err := repo.AdjustStock(ctx, p.ID, 3); err != nil || got.Version != 1 {
			t.Fatalf("AdjustStock = %+v, %v, want Version 1", got, err)
		}
		if err := repo.DecreaseStock(ctx, p.ID, 1); err != nil {
			t.Fatal(err)
		}
		if got, err := repo.Update(ctx, p.ID, "p2", 100, 1); err != nil || got.Version != 2 {
			t.Errorf("调库存后用原 version 编辑 = %+v, %v, want 成功", got, err)
		}
	})

	t.Run("AdjustStock 加减库存，结果越界时不改", func(t *testing.T) {
		repo := newRepo(t)
		p := mk("p", 5)
		if err := repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		if got, err := repo.AdjustStock(ctx, p.ID, 10); err != nil || got.Stock != 15 {
			t.Fatalf("+10 = %+v, %v", got, err)
		}
		if got, err := repo.AdjustStock(ctx, p.ID, -15); err != nil || got.Stock != 0 {
			t.Fatalf("-15 = %+v, %v", got, err)
		}
		if _, err := repo.AdjustStock(ctx, p.ID, -1); !errors.Is(err, domain.ErrInsufficientStock) {
			t.Errorf("减到负数 error = %v, want ErrInsufficientStock", err)
		}
		if _, err := repo.AdjustStock(ctx, p.ID, domain.MaxStock+1); !errors.Is(err, domain.ErrStockTooLarge) {
			t.Errorf("超过上限 error = %v, want ErrStockTooLarge", err)
		}
		if got, _ := repo.GetByID(ctx, p.ID); got.Stock != 0 {
			t.Errorf("越界后库存 = %d, want 0（不能改）", got.Stock)
		}
		if _, err := repo.AdjustStock(ctx, uuid.NewV7().String(), 1); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("不存在 error = %v, want ErrNotFound", err)
		}
	})

	t.Run("调库存与下单扣减并发不丢更新", func(t *testing.T) {
		repo := newRepo(t)
		p := mk("p", 20)
		if err := repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		var added, decreased int64
		for i := range 60 {
			wg.Go(func() {
				if i%3 == 0 {
					if _, err := repo.AdjustStock(ctx, p.ID, 5); err == nil {
						mu.Lock()
						added += 5
						mu.Unlock()
					}
					return
				}
				if err := repo.DecreaseStock(ctx, p.ID, 1); err == nil {
					mu.Lock()
					decreased++
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		got, _ := repo.GetByID(ctx, p.ID)
		if got.Stock != 20+added-decreased || got.Stock < 0 {
			t.Errorf("最终库存 %d, want 20 + %d - %d = %d", got.Stock, added, decreased, 20+added-decreased)
		}
		if added != 100 {
			t.Errorf("加库存成功 %d, want 100（加库存不会失败）", added)
		}
	})

	t.Run("非法 cursor 返回 ErrInvalidCursor", func(t *testing.T) {
		repo := newRepo(t)
		if _, _, err := repo.List(ctx, "not-a-valid-cursor!!", 10); !errors.Is(err, domain.ErrInvalidCursor) {
			t.Fatalf("error = %v, want ErrInvalidCursor", err)
		}
	})
}
