package app

import (
	"errors"
	"testing"

	orderdomain "app.local/internal/modules/order/domain"
	productdomain "app.local/internal/modules/product/domain"
	"app.local/internal/modules/product/domain/domaintest"
)

func TestProductLookup_TranslatesNotFound(t *testing.T) {
	a := productLookup{productdomain.NewService(domaintest.NewRepository())}
	if _, _, err := a.GetPriceAndName(t.Context(), "missing"); !errors.Is(err, orderdomain.ErrProductNotFound) {
		t.Errorf("error = %v, want order.ErrProductNotFound", err)
	}
}

// 订单创建事件必须有履约消费者；删除履约时订单也要一起删除。
func TestOrderRequiresFulfillment(t *testing.T) {
	deps := moduleRequires["order"]
	if len(deps) != 1 || deps[0] != "fulfillment" {
		t.Fatalf("moduleRequires[order] = %v, want [fulfillment]", deps)
	}
}
