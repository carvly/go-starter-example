package app

import (
	"context"
	"errors"
	"testing"

	"app.local/internal/modules/fulfillment"
	productdomain "app.local/internal/modules/product/domain"
	"app.local/internal/modules/product/domain/domaintest"
)

type failingProductRepo struct {
	*domaintest.Repository
	err error
}

func (r failingProductRepo) DecreaseStock(context.Context, string, int64) error { return r.err }

func TestStockReserver_ClassifiesPermanentFailures(t *testing.T) {
	ctx := t.Context()
	repo := domaintest.NewRepository()
	svc := productdomain.NewService(repo)
	p, err := svc.Create(ctx, "p", 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	a := stockReserver{svc}

	if err := a.Decrease(ctx, p.ID, 5); !errors.Is(err, fulfillment.ErrStockUnavailable) {
		t.Errorf("库存不足: error = %v, want ErrStockUnavailable", err)
	}
	if err := a.Decrease(ctx, "missing", 1); !errors.Is(err, fulfillment.ErrStockUnavailable) {
		t.Errorf("商品不存在: error = %v, want ErrStockUnavailable", err)
	}

	transient := errors.New("connection reset")
	a = stockReserver{productdomain.NewService(failingProductRepo{repo, transient})}
	if err := a.Decrease(ctx, p.ID, 1); !errors.Is(err, transient) || errors.Is(err, fulfillment.ErrStockUnavailable) {
		t.Errorf("临时故障: error = %v, want 原样返回且不是 ErrStockUnavailable", err)
	}
}
