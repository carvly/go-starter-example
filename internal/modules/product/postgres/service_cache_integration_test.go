//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"app.local/internal/modules/product"
	"app.local/internal/modules/product/domain"
	"app.local/internal/modules/product/postgres"
	"app.local/internal/platform/postgres/pgtest"
)

type mapCache struct {
	mu   sync.Mutex
	data map[string]domain.Product
}

func (c *mapCache) Get(_ context.Context, id string) (*domain.Product, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.data[id]
	return &p, ok, nil
}

func (c *mapCache) Set(_ context.Context, id string, v *domain.Product, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[id] = *v
	return nil
}

func (c *mapCache) Delete(_ context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, id)
	return nil
}

// 事务里扣库存时，提交前的并发读只能看到旧库存并会回填缓存；删缓存必须发生在
// 提交之后，否则旧库存会一直留在缓存里直到 TTL 过期。
func TestService_DecreaseStockInTransaction_NoStaleCacheAfterCommit(t *testing.T) {
	ctx := t.Context()
	h := pgtest.Handle(t, product.Migrations)
	svc := domain.NewService(postgres.NewRepository(h), domain.WithCache(&mapCache{data: map[string]domain.Product{}}))

	p, err := svc.Create(ctx, "机械键盘", 19900, 10)
	if err != nil {
		t.Fatal(err)
	}

	err = h.Run(ctx, func(txCtx context.Context) error {
		if err := svc.DecreaseStock(txCtx, p.ID, 3); err != nil {
			return err
		}
		// 事务外的读者：读到已提交的旧库存，回填缓存。
		_, err := svc.Get(ctx, p.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stock != 7 {
		t.Errorf("提交后 Get().Stock = %d, want 7（缓存里留下了提交前的旧库存）", got.Stock)
	}
}

// 事务里改了商品再 Get，读到的是未提交的值；回填缓存必须等提交，否则事务回滚后
// 缓存里会留下一个从未提交过的值。
func TestService_GetInTransaction_DoesNotCacheUncommittedValue(t *testing.T) {
	ctx := t.Context()
	h := pgtest.Handle(t, product.Migrations)
	svc := domain.NewService(postgres.NewRepository(h), domain.WithCache(&mapCache{data: map[string]domain.Product{}}))

	p, err := svc.Create(ctx, "机械键盘", 19900, 10)
	if err != nil {
		t.Fatal(err)
	}

	rollback := errors.New("rollback")
	err = h.Run(ctx, func(txCtx context.Context) error {
		if err := svc.DecreaseStock(txCtx, p.ID, 3); err != nil {
			return err
		}
		if _, err := svc.Get(txCtx, p.ID); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("Run() error = %v, want rollback", err)
	}

	got, err := svc.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stock != 10 {
		t.Errorf("回滚后 Get().Stock = %d, want 10（缓存里留下了回滚事务里未提交的库存）", got.Stock)
	}
}

// 删缓存推迟到提交后，事务里的 Get 不能命中缓存，否则读不到本事务自己的写入。
func TestService_GetInTransaction_ReadsOwnWrites(t *testing.T) {
	ctx := t.Context()
	h := pgtest.Handle(t, product.Migrations)
	svc := domain.NewService(postgres.NewRepository(h), domain.WithCache(&mapCache{data: map[string]domain.Product{}}))

	p, err := svc.Create(ctx, "机械键盘", 19900, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, p.ID); err != nil { // 预热缓存
		t.Fatal(err)
	}

	err = h.Run(ctx, func(txCtx context.Context) error {
		if err := svc.DecreaseStock(txCtx, p.ID, 3); err != nil {
			return err
		}
		got, err := svc.Get(txCtx, p.ID)
		if err != nil {
			return err
		}
		if got.Stock != 7 {
			t.Errorf("事务内 Get().Stock = %d, want 7（命中了缓存里事务开始前的值）", got.Stock)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
