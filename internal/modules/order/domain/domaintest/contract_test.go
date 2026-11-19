package domaintest_test

import (
	"testing"

	"app.local/internal/modules/order/domain"
	"app.local/internal/modules/order/domain/domaintest"
)

func TestFakeRepository_Contract(t *testing.T) {
	domaintest.RunRepositoryContract(t, func(*testing.T) (domain.Repository, string) {
		return domaintest.NewRepository(), "any-product"
	})
}
