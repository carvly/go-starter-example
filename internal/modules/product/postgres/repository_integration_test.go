//go:build integration

package postgres_test

import (
	"testing"

	"app.local/internal/modules/product"
	"app.local/internal/modules/product/domain"
	"app.local/internal/modules/product/domain/domaintest"
	"app.local/internal/modules/product/postgres"
	"app.local/internal/platform/postgres/pgtest"
)

func TestRepository_Contract(t *testing.T) {
	domaintest.RunRepositoryContract(t, func(t *testing.T) domain.Repository {
		return postgres.NewRepository(pgtest.Handle(t, product.Migrations))
	})
}
