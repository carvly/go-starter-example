package domain_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"app.local/internal/kit/page"
	"app.local/internal/kit/uow/uowtest"
	order "app.local/internal/modules/order/domain"
	"app.local/internal/modules/order/domain/domaintest"
)

type fakeProductLookup struct {
	name       string
	priceCents int64
	err        error
}

func (f fakeProductLookup) GetPriceAndName(context.Context, string) (string, int64, error) {
	if f.err != nil {
		return "", 0, f.err
	}
	return f.name, f.priceCents, nil
}

func TestService_Create(t *testing.T) {
	ctx := t.Context()

	t.Run("成功下单：金额按单价x数量计算，事件同事务发布", func(t *testing.T) {
		repo := domaintest.NewRepository()
		pub := &domaintest.Publisher{}
		svc := order.NewService(repo, fakeProductLookup{name: "机械键盘", priceCents: 19900}, pub, uowtest.Immediate{})

		o, err := svc.Create(ctx, "张三", "prod-1", 2)
		if err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		if o.TotalCents != 39800 {
			t.Errorf("TotalCents = %d, want 39800", o.TotalCents)
		}
		if o.Status != order.StatusPending {
			t.Errorf("Status = %v, want pending", o.Status)
		}
		if len(pub.Published) != 1 || pub.Published[0].OrderID != o.ID {
			t.Errorf("事件未按预期发布: %+v", pub.Published)
		}
	})

	t.Run("客户名为空拒绝", func(t *testing.T) {
		svc := order.NewService(domaintest.NewRepository(), fakeProductLookup{}, &domaintest.Publisher{}, uowtest.Immediate{})
		_, err := svc.Create(ctx, "  ", "prod-1", 1)
		if !errors.Is(err, order.ErrInvalidCustomerName) {
			t.Errorf("Create() error = %v, want ErrInvalidCustomerName", err)
		}
	})

	t.Run("数量非正拒绝", func(t *testing.T) {
		svc := order.NewService(domaintest.NewRepository(), fakeProductLookup{}, &domaintest.Publisher{}, uowtest.Immediate{})
		_, err := svc.Create(ctx, "张三", "prod-1", 0)
		if !errors.Is(err, order.ErrInvalidQuantity) {
			t.Errorf("Create() error = %v, want ErrInvalidQuantity", err)
		}
	})

	t.Run("数量超过上限拒绝", func(t *testing.T) {
		svc := order.NewService(domaintest.NewRepository(), fakeProductLookup{name: "p", priceCents: 100}, &domaintest.Publisher{}, uowtest.Immediate{})
		_, err := svc.Create(ctx, "张三", "prod-1", order.MaxQuantity+1)
		if !errors.Is(err, order.ErrInvalidQuantity) {
			t.Errorf("Create() error = %v, want ErrInvalidQuantity", err)
		}
	})

	t.Run("单价x数量溢出int64时拒绝而不是让total_cents变成负数", func(t *testing.T) {
		// quantity 合法，但极大的单价让乘积溢出。
		svc := order.NewService(domaintest.NewRepository(), fakeProductLookup{name: "p", priceCents: math.MaxInt64 / 2}, &domaintest.Publisher{}, uowtest.Immediate{})
		_, err := svc.Create(ctx, "张三", "prod-1", order.MaxQuantity)
		if !errors.Is(err, order.ErrInvalidQuantity) {
			t.Errorf("Create() error = %v, want ErrInvalidQuantity（应该检测到溢出）", err)
		}
	})

	t.Run("商品不存在时错误透传", func(t *testing.T) {
		svc := order.NewService(domaintest.NewRepository(), fakeProductLookup{err: order.ErrProductNotFound}, &domaintest.Publisher{}, uowtest.Immediate{})
		_, err := svc.Create(ctx, "张三", "prod-x", 1)
		if !errors.Is(err, order.ErrProductNotFound) {
			t.Errorf("Create() error = %v, want ErrProductNotFound", err)
		}
	})
}

func TestService_ConfirmAndMarkFailed(t *testing.T) {
	ctx := t.Context()
	repo := domaintest.NewRepository()
	svc := order.NewService(repo, fakeProductLookup{name: "p", priceCents: 100}, &domaintest.Publisher{}, uowtest.Immediate{})

	o, err := svc.Create(ctx, "张三", "prod-1", 1)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := svc.Confirm(ctx, o.ID); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	got, _ := svc.Get(ctx, o.ID)
	if got.Status != order.StatusConfirmed {
		t.Errorf("Status = %v, want confirmed", got.Status)
	}

	// 已经 confirmed 的订单不能再被标记 failed——状态机不允许的转换。
	if err := svc.MarkFailed(ctx, o.ID); !errors.Is(err, order.ErrInvalidStatusTransition) {
		t.Errorf("MarkFailed() error = %v, want ErrInvalidStatusTransition", err)
	}
}

func TestService_Search_ValidatesFilter(t *testing.T) {
	svc := order.NewService(domaintest.NewRepository(), fakeProductLookup{}, &domaintest.Publisher{}, uowtest.Immediate{})
	p, _ := page.New(1, 20)
	now := time.Now()
	if _, err := svc.Search(t.Context(), order.SearchFilter{Statuses: []order.Status{order.StatusPending, "shipped"}}, p); !errors.Is(err, order.ErrInvalidStatus) {
		t.Errorf("未知状态 error = %v, want ErrInvalidStatus", err)
	}
	if _, err := svc.Search(t.Context(), order.SearchFilter{From: now, To: now}, p); !errors.Is(err, order.ErrInvalidTimeRange) {
		t.Errorf("from == to error = %v, want ErrInvalidTimeRange", err)
	}
	if _, err := svc.Search(t.Context(), order.SearchFilter{Statuses: []order.Status{order.StatusPending}, From: now.Add(-time.Hour), To: now}, p); err != nil {
		t.Errorf("合法条件 error = %v", err)
	}
}
