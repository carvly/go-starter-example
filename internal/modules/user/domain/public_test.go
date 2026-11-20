package domain_test

import (
	"errors"
	"testing"

	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/domain/domaintest"
)

func TestGetPublicProfiles(t *testing.T) {
	svc, fakes := newService()
	first, second := domaintest.ValidUser(), domaintest.ValidUser()
	for _, v := range []*domain.User{first, second} {
		if err := fakes.Users.Create(t.Context(), v); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.GetPublicProfiles(t.Context(), []string{first.ID, "0199f1a2-3b4c-7d5e-8f60-718293a4b5c6", first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[first.ID].DisplayName != first.DisplayName || got[first.ID].Status != first.Status {
		t.Fatalf("profiles = %+v", got)
	}
}

func TestWeChatOpenID(t *testing.T) {
	svc, _ := newAccountService()
	tokens, err := svc.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", wechatIdentity("openid-query"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.WeChatOpenID(t.Context(), tokens.Session.UserID, "mini-appid")
	if err != nil || got != "openid-openid-query" {
		t.Fatalf("WeChatOpenID = %q, %v", got, err)
	}
	if _, err := svc.WeChatOpenID(t.Context(), tokens.Session.UserID, "unknown"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("不存在 error = %v, want ErrNotFound", err)
	}
}
