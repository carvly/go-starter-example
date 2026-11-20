package domain_test

import (
	"errors"
	"testing"

	"app.local/internal/kit/uow/uowtest"
	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/domain/domaintest"
)

func newService() (*domain.Service, *domaintest.Fakes) {
	fakes := domaintest.NewFakes()
	deps := fakes.Deps()
	deps.UoW = uowtest.Immediate{}
	deps.Issuer = domaintest.Issuer{}
	deps.Config = domaintest.TestConfig()
	return domain.NewService(deps), fakes
}

func TestUser_Disable(t *testing.T) {
	svc, fakes := newService()
	v := domaintest.ValidUser()
	v.Status = domain.UserStatusActive
	if err := fakes.Users.Create(t.Context(), v); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Disable(t.Context(), v.ID)
	if err != nil || got.Status != domain.UserStatusDisabled {
		t.Fatalf("停用 = %v, %v", got, err)
	}
	if _, err := svc.Disable(t.Context(), v.ID); !errors.Is(err, domain.ErrInvalidState) {
		t.Errorf("再次停用 error = %v, want ErrInvalidState", err)
	}
	if _, err := svc.Disable(t.Context(), "0199f1a2-3b4c-7d5e-8f60-718293a4b5c6"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("不存在 error = %v, want ErrNotFound", err)
	}
}
