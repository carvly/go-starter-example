package domaintest_test

import (
	"testing"

	"app.local/internal/modules/product/domain"
	"app.local/internal/modules/product/domain/domaintest"
)

func TestFakeRepository_Contract(t *testing.T) {
	domaintest.RunRepositoryContract(t, func(*testing.T) domain.Repository { return domaintest.NewRepository() })
}
