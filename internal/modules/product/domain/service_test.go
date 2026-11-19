package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	product "app.local/internal/modules/product/domain"
	"app.local/internal/modules/product/domain/domaintest"
)

func TestService_Create(t *testing.T) {
	svc := product.NewService(domaintest.NewRepository())
	ctx := t.Context()

	t.Run("成功创建", func(t *testing.T) {
		p, err := svc.Create(ctx, "机械键盘", 19900, 10)
		if err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		if p.ID == "" {
			t.Error("Create() 应该生成非空 ID")
		}
		if p.PriceCents != 19900 || p.Stock != 10 {
			t.Errorf("Create() = %+v, 字段不匹配", p)
		}
	})

	t.Run("名称为空拒绝", func(t *testing.T) {
		_, err := svc.Create(ctx, "  ", 100, 1)
		if !errors.Is(err, product.ErrInvalidName) {
			t.Errorf("Create() error = %v, want ErrInvalidName", err)
		}
	})

	t.Run("价格非正拒绝", func(t *testing.T) {
		_, err := svc.Create(ctx, "商品", 0, 1)
		if !errors.Is(err, product.ErrInvalidPrice) {
			t.Errorf("Create() error = %v, want ErrInvalidPrice", err)
		}
	})

	t.Run("库存为负拒绝", func(t *testing.T) {
		_, err := svc.Create(ctx, "商品", 100, -1)
		if !errors.Is(err, product.ErrInvalidStock) {
			t.Errorf("Create() error = %v, want ErrInvalidStock", err)
		}
	})
}

func TestService_DecreaseStock(t *testing.T) {
	repo := domaintest.NewRepository()
	svc := product.NewService(repo)
	ctx := t.Context()

	p, err := svc.Create(ctx, "机械键盘", 19900, 5)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	t.Run("库存充足时扣减成功", func(t *testing.T) {
		if err := svc.DecreaseStock(ctx, p.ID, 3); err != nil {
			t.Fatalf("DecreaseStock() error = %v, want nil", err)
		}
		got, _ := svc.Get(ctx, p.ID)
		if got.Stock != 2 {
			t.Errorf("Stock = %d, want 2", got.Stock)
		}
	})

	t.Run("库存不足时返回 ErrInsufficientStock", func(t *testing.T) {
		err := svc.DecreaseStock(ctx, p.ID, 100)
		if !errors.Is(err, product.ErrInsufficientStock) {
			t.Errorf("DecreaseStock() error = %v, want ErrInsufficientStock", err)
		}
	})

	t.Run("数量非正拒绝", func(t *testing.T) {
		err := svc.DecreaseStock(ctx, p.ID, 0)
		if !errors.Is(err, product.ErrInvalidStock) {
			t.Errorf("DecreaseStock() error = %v, want ErrInvalidStock", err)
		}
	})
}

func TestService_List(t *testing.T) {
	repo := domaintest.NewRepository()
	svc := product.NewService(repo)
	ctx := t.Context()

	for range 25 {
		if _, err := svc.Create(ctx, "商品", 100, 1); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	t.Run("超过上限时回落到默认 limit", func(t *testing.T) {
		items, next, err := svc.List(ctx, "", 1000)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(items) != product.DefaultListLimit {
			t.Errorf("len(items) = %d, want %d", len(items), product.DefaultListLimit)
		}
		if next == "" {
			t.Error("next cursor 不应该为空——还有下一页")
		}
	})

	t.Run("翻到最后一页 next cursor 为空", func(t *testing.T) {
		_, next, err := svc.List(ctx, "", 1000)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		_, next2, err := svc.List(ctx, next, 1000)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if next2 != "" {
			t.Errorf("next2 = %q, want empty (已经是最后一页)", next2)
		}
	})
}

// fakeCache 用来验证"缓存命中不查库"和"写库后删缓存"两条路径。
type fakeCache struct {
	data map[string]*product.Product
	gets int
	sets int
	dels int
}

func newFakeCache() *fakeCache {
	return &fakeCache{data: make(map[string]*product.Product)}
}

func (c *fakeCache) Get(_ context.Context, id string) (*product.Product, bool, error) {
	c.gets++
	p, ok := c.data[id]
	return p, ok, nil
}

func (c *fakeCache) Set(_ context.Context, id string, v *product.Product, _ time.Duration) error {
	c.sets++
	c.data[id] = v
	return nil
}

func (c *fakeCache) Delete(_ context.Context, id string) error {
	c.dels++
	delete(c.data, id)
	return nil
}

// countingRepository 数一下 GetByID 被真的调用
// 了几次——用来证明缓存命中之后 Service 不会再去查 Repository。
type countingRepository struct {
	*domaintest.Repository
	getByIDCalls int
}

func (r *countingRepository) GetByID(ctx context.Context, id string) (*product.Product, error) {
	r.getByIDCalls++
	return r.Repository.GetByID(ctx, id)
}

func TestService_Get_CacheAside(t *testing.T) {
	ctx := t.Context()
	repo := &countingRepository{Repository: domaintest.NewRepository()}
	cache := newFakeCache()
	svc := product.NewService(repo, product.WithCache(cache))

	p, err := svc.Create(ctx, "机械键盘", 19900, 10)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// 第一次 Get：缓存里还没有，应该回源查 Repository，并且把结果写进
	// 缓存。
	got, err := svc.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != p.ID {
		t.Fatalf("Get() = %+v, want ID=%s", got, p.ID)
	}
	if repo.getByIDCalls != 1 {
		t.Errorf("第一次 Get() 之后 repo.getByIDCalls = %d, want 1", repo.getByIDCalls)
	}
	if cache.sets != 1 {
		t.Errorf("第一次 Get() 之后 cache.sets = %d, want 1（应该回填缓存）", cache.sets)
	}

	// 第二次 Get：应该命中缓存，不再查 Repository。
	if _, err := svc.Get(ctx, p.ID); err != nil {
		t.Fatalf("第二次 Get() error = %v", err)
	}
	if repo.getByIDCalls != 1 {
		t.Errorf("第二次 Get() 之后 repo.getByIDCalls = %d, want 仍然是 1（应该命中缓存，不查库）", repo.getByIDCalls)
	}
}

func TestService_DecreaseStock_InvalidatesCache(t *testing.T) {
	ctx := t.Context()
	repo := domaintest.NewRepository()
	cache := newFakeCache()
	svc := product.NewService(repo, product.WithCache(cache))

	p, err := svc.Create(ctx, "机械键盘", 19900, 10)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := svc.Get(ctx, p.ID); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, hit, _ := cache.Get(ctx, p.ID); !hit {
		t.Fatal("Get() 之后缓存里应该已经有这条记录")
	}

	if err := svc.DecreaseStock(ctx, p.ID, 3); err != nil {
		t.Fatalf("DecreaseStock() error = %v", err)
	}
	if _, hit, _ := cache.Get(ctx, p.ID); hit {
		t.Error("DecreaseStock() 之后缓存应该被删除（先写 DB 再删缓存），不能让下一次 Get 读到扣减前的旧库存")
	}
}

func TestService_Get_NilCache_WorksWithoutCaching(t *testing.T) {
	// 没有配 Option（对应本地默认的 NoopCache/根本没配 Redis 场景）时，
	// Service 不能因为 s.cache == nil 就 panic 或者行为不一致。
	ctx := t.Context()
	svc := product.NewService(domaintest.NewRepository())

	p, err := svc.Create(ctx, "机械键盘", 19900, 10)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := svc.Get(ctx, p.ID); err != nil {
		t.Fatalf("Get() error = %v, want nil（没有缓存时应该直接回源，不报错）", err)
	}
	if err := svc.DecreaseStock(ctx, p.ID, 1); err != nil {
		t.Fatalf("DecreaseStock() error = %v, want nil", err)
	}
}

func TestService_Create_StockAboveMax(t *testing.T) {
	svc := product.NewService(domaintest.NewRepository())
	if _, err := svc.Create(t.Context(), "商品", 100, product.MaxStock+1); !errors.Is(err, product.ErrInvalidStock) {
		t.Errorf("Create() error = %v, want ErrInvalidStock", err)
	}
}

func TestService_Update(t *testing.T) {
	cache := newFakeCache()
	svc := product.NewService(domaintest.NewRepository(), product.WithCache(cache))
	ctx := t.Context()
	p, _ := svc.Create(ctx, "旧名字", 100, 1)
	_, _ = svc.Get(ctx, p.ID)

	got, err := svc.Update(ctx, p.ID, "  新名字  ", 250, p.Version)
	if err != nil || got.Name != "新名字" || got.PriceCents != 250 {
		t.Fatalf("Update() = %+v, %v", got, err)
	}
	if _, hit, _ := cache.Get(ctx, p.ID); hit {
		t.Error("Update() 后缓存应被删除")
	}
	if _, err := svc.Update(ctx, p.ID, " ", 250, 2); !errors.Is(err, product.ErrInvalidName) {
		t.Errorf("空名称 error = %v, want ErrInvalidName", err)
	}
	if _, err := svc.Update(ctx, p.ID, "x", 0, 2); !errors.Is(err, product.ErrInvalidPrice) {
		t.Errorf("非正价格 error = %v, want ErrInvalidPrice", err)
	}
	if _, err := svc.Update(ctx, p.ID, "x", 300, p.Version); !errors.Is(err, product.ErrConflict) {
		t.Errorf("旧 version error = %v, want ErrConflict", err)
	}
}

func TestService_GetFresh_BypassesCache(t *testing.T) {
	cache := newFakeCache()
	svc := product.NewService(domaintest.NewRepository(), product.WithCache(cache))
	ctx := t.Context()
	p, _ := svc.Create(ctx, "商品", 100, 1)
	stale := *p
	stale.Name = "缓存里的旧值"
	_ = cache.Set(ctx, p.ID, &stale, time.Minute)

	got, err := svc.GetFresh(ctx, p.ID)
	if err != nil || got.Name != "商品" {
		t.Fatalf("GetFresh() = %+v, %v, want 读库得到的商品", got, err)
	}
	if cached, _, _ := cache.Get(ctx, p.ID); cached.Name != "缓存里的旧值" {
		t.Errorf("GetFresh() 不应回填缓存，缓存 = %+v", cached)
	}
}

func TestService_AdjustStock(t *testing.T) {
	cache := newFakeCache()
	svc := product.NewService(domaintest.NewRepository(), product.WithCache(cache))
	ctx := t.Context()
	p, _ := svc.Create(ctx, "商品", 100, 3)
	_, _ = svc.Get(ctx, p.ID)

	got, err := svc.AdjustStock(ctx, p.ID, -2)
	if err != nil || got.Stock != 1 {
		t.Fatalf("AdjustStock(-2) = %+v, %v", got, err)
	}
	if _, hit, _ := cache.Get(ctx, p.ID); hit {
		t.Error("AdjustStock() 后缓存应被删除")
	}
	for _, delta := range []int64{0, product.MaxStock + 1, -product.MaxStock - 1} {
		if _, err := svc.AdjustStock(ctx, p.ID, delta); !errors.Is(err, product.ErrInvalidStockDelta) {
			t.Errorf("delta=%d error = %v, want ErrInvalidStockDelta", delta, err)
		}
	}
}
