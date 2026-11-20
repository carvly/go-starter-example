package domaintest_test

import (
	"testing"

	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/domain/domaintest"
)

func TestFakeRepository_Contract(t *testing.T) {
	domaintest.RunRepositoryContract(t, func(*testing.T) domain.Repository { return domaintest.NewRepository() })
}

func TestFakeIdentityRepository_Contract(t *testing.T) {
	domaintest.RunIdentityRepositoryContract(t, func(*testing.T) domain.IdentityRepository { return domaintest.NewIdentityRepository() })
}

func TestFakeSessionRepository_Contract(t *testing.T) {
	// 内存 fake 不检查 users 表，createUser 是空操作。
	domaintest.RunSessionRepositoryContract(t,
		func(*testing.T) (domain.SessionRepository, func(*testing.T, string)) {
			return domaintest.NewSessionRepository(), func(*testing.T, string) {}
		})
}

func TestFakeOAuthStateRepository_Contract(t *testing.T) {
	domaintest.RunOAuthStateRepositoryContract(t, func(*testing.T) domain.OAuthStateRepository { return domaintest.NewOAuthStateRepository() })
}
