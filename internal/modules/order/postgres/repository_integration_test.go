//go:build integration

package postgres_test

import (
	"testing"
	"uuid"

	"app.local/internal/modules/order"
	"app.local/internal/modules/order/domain"
	"app.local/internal/modules/order/domain/domaintest"
	"app.local/internal/modules/order/postgres"
	"app.local/internal/platform/dbtx"
	"app.local/internal/platform/postgres/pgtest"
)

func TestRepository_Contract(t *testing.T) {
	domaintest.RunRepositoryContract(t, func(t *testing.T) (domain.Repository, string) {
		// orders.product_id 没有外键，商品不必真实存在。
		return postgres.NewRepository(dbtx.NewHandle(pgtest.Pool(t, order.Migrations))), uuid.NewV7().String()
	})
}
