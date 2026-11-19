//go:build integration

package app

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"app.local/internal/modules/fulfillment"
	orderdomain "app.local/internal/modules/order/domain"
	"app.local/internal/modules/order/events"
	orderpg "app.local/internal/modules/order/postgres"
	productdomain "app.local/internal/modules/product/domain"
	productpg "app.local/internal/modules/product/postgres"
	"app.local/internal/platform/dbtx"
	"app.local/internal/platform/postgres/pgtest"
)

// flakyProducts 第一次扣库存返回临时故障，之后正常。
type flakyProducts struct {
	*productpg.Repository
	failures int
}

func (r *flakyProducts) DecreaseStock(ctx context.Context, id string, qty int64) error {
	if r.failures > 0 {
		r.failures--
		return errors.New("connection reset")
	}
	return r.Repository.DecreaseStock(ctx, id, qty)
}

// 临时故障时，去重记录必须随事务一起回滚，否则重试会被当成"已处理"跳过，订单永远
// 停在 pending。内存 fake 的 UnitOfWork 不回滚，这条性质只能在真实数据库上验证。
func TestFulfillment_TransientFailureRollsBackLedger(t *testing.T) {
	ctx := t.Context()
	pool := pgtest.Pool(t, migrationSources()...)
	h := dbtx.NewHandle(pool)
	productID, orderID := uuid.NewV7().String(), uuid.NewV7().String()
	if _, err := pool.Exec(ctx, `INSERT INTO products (id, name, price_cents, stock) VALUES ($1, 'p', 100, 10)`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO orders (id, customer_name, product_id, product_name, quantity, unit_price_cents, total_cents, status)
		VALUES ($1, 'c', $2, 'p', 3, 100, 300, 'pending')
	`, orderID, productID); err != nil {
		t.Fatal(err)
	}

	orderRepo := orderpg.NewRepository(h)
	products := &flakyProducts{Repository: productpg.NewRepository(h), failures: 1}
	process := fulfillment.NewProcess(h, orderRepo,
		stockReserver{productdomain.NewService(products)},
		orderdomain.NewService(orderRepo, nil, nil, h))
	evt := events.OrderCreated{OrderID: orderID, ProductID: productID, Quantity: 3}

	state := func() (stock int64, status string, processed int) {
		t.Helper()
		if err := pool.QueryRow(ctx, `
			SELECT (SELECT stock FROM products WHERE id = $1),
			       (SELECT status FROM orders WHERE id = $2),
			       (SELECT count(*) FROM order_processed_events WHERE order_id = $2)
		`, productID, orderID).Scan(&stock, &status, &processed); err != nil {
			t.Fatal(err)
		}
		return
	}

	if err := process.OnOrderCreated(ctx, evt); err == nil {
		t.Fatal("第一次处理应该返回临时故障，交给队列重试")
	}
	if stock, status, processed := state(); stock != 10 || status != "pending" || processed != 0 {
		t.Fatalf("临时故障后 stock=%d status=%s processed=%d, want 10/pending/0（去重记录必须回滚）", stock, status, processed)
	}

	if err := process.OnOrderCreated(ctx, evt); err != nil {
		t.Fatalf("重试 error = %v", err)
	}
	if err := process.OnOrderCreated(ctx, evt); err != nil {
		t.Fatalf("重放 error = %v", err)
	}
	if stock, status, processed := state(); stock != 7 || status != "confirmed" || processed != 1 {
		t.Errorf("重试并重放后 stock=%d status=%s processed=%d, want 7/confirmed/1", stock, status, processed)
	}
}
