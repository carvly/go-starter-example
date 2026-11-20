package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"app.local/internal/kit/uow/uowtest"
	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/domain/domaintest"
)

func newAccountService() (*domain.Service, *domaintest.Fakes) {
	fakes := domaintest.NewFakes()
	deps := fakes.Deps()
	deps.UoW = uowtest.Immediate{}
	deps.Issuer = domaintest.Issuer{}
	deps.Config = domaintest.TestConfig()
	return domain.NewService(deps), fakes
}

func wechatIdentity(subject string) domain.ExternalIdentity {
	return domain.ExternalIdentity{
		Provider:     domain.ProviderWeChat,
		Subject:      subject,
		Name:         "微信昵称",
		AvatarURL:    "https://example.com/a.png",
		WeChatAppID:  "mini-appid",
		WeChatOpenID: "openid-" + subject,
	}
}

func TestResolveLogin_NewIdentityCreatesUser(t *testing.T) {
	svc, fakes := newAccountService()
	tokens, err := svc.Login(t.Context(), "miniprogram", "dev", "127.0.0.1", "test-agent", wechatIdentity("u1"))
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("tokens = %+v", tokens)
	}
	user, err := fakes.Users.GetByID(t.Context(), tokens.Session.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "微信昵称" || user.Status != domain.UserStatusActive {
		t.Errorf("user = %+v", user)
	}
	if user.SignupClient != "miniprogram" || user.LastLoginAt.IsZero() {
		t.Errorf("注册客户端或最近登录时间未写入：%+v", user)
	}
	idns, err := fakes.Identities.ListByUser(t.Context(), user.ID)
	if err != nil || len(idns) != 1 || idns[0].Subject != "u1" {
		t.Fatalf("identities = %v, %v", idns, err)
	}
	if got := fakes.OpenIDs.OpenIDs(user.ID); got["mini-appid"] != "openid-u1" {
		t.Errorf("openids = %v", got)
	}
	if tokens.Session.LoginIdentityID == nil || *tokens.Session.LoginIdentityID != idns[0].ID {
		t.Errorf("login_identity_id = %v, want %v", tokens.Session.LoginIdentityID, idns[0].ID)
	}
	if len(tokens.Session.AMR) != 1 || tokens.Session.AMR[0] != "wechat" {
		t.Errorf("amr = %v", tokens.Session.AMR)
	}
}

func TestResolveLogin_FallbackNickname(t *testing.T) {
	svc, fakes := newAccountService()
	ext := wechatIdentity("u-empty")
	ext.Name = "   " // 空白昵称回退兜底值
	if _, err := svc.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", ext); err != nil {
		t.Fatal(err)
	}
	users := fakes.Users.UsersList()
	if len(users) != 1 {
		t.Fatalf("用户数 = %d, want 1", len(users))
	}
	user, err := fakes.Users.GetByID(t.Context(), users[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "微信用户" {
		t.Errorf("display_name = %q, want 兜底昵称", user.DisplayName)
	}
}

func TestResolveLogin_LongNicknameTruncated(t *testing.T) {
	svc, fakes := newAccountService()
	ext := wechatIdentity("u-long")
	ext.Name = strings.Repeat("长", 100)
	if _, err := svc.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", ext); err != nil {
		t.Fatal(err)
	}
	for _, user := range fakes.Users.UsersList() {
		if n := len([]rune(user.DisplayName)); n != 64 {
			t.Errorf("昵称长度 = %d, want 64", n)
		}
	}
}

func TestResolveLogin_ExistingIdentitySameAccount(t *testing.T) {
	svc, fakes := newAccountService()
	first, err := svc.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", wechatIdentity("u2"))
	if err != nil {
		t.Fatal(err)
	}
	// 同一 unionid 从另一个入口进来：不建号，更新 openid。
	ext := wechatIdentity("u2")
	ext.WeChatAppID, ext.WeChatOpenID = "app-appid", "openid-app-u2"
	second, err := svc.Login(t.Context(), "app", "", "127.0.0.1", "", ext)
	if err != nil {
		t.Fatal(err)
	}
	if second.Session.UserID != first.Session.UserID {
		t.Errorf("两次登录的账号不同：%v vs %v", first.Session.UserID, second.Session.UserID)
	}
	if got := fakes.OpenIDs.OpenIDs(first.Session.UserID); got["app-appid"] != "openid-app-u2" || got["mini-appid"] != "openid-u2" {
		t.Errorf("openids = %v, want 两个入口都在", got)
	}
	if n := len(fakes.Users.UsersList()); n != 1 {
		t.Errorf("用户数 = %d, want 1", n)
	}
}

func TestResolveLogin_DisabledAccountRejected(t *testing.T) {
	svc, _ := newAccountService()
	tokens, err := svc.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", wechatIdentity("u3"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Disable(t.Context(), tokens.Session.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", wechatIdentity("u3")); !errors.Is(err, domain.ErrAccountUnavailable) {
		t.Errorf("停用后登录 error = %v, want ErrAccountUnavailable", err)
	}
	// 换个入口也不行了。
	ext := wechatIdentity("u3")
	ext.WeChatAppID = "web-appid"
	if _, err := svc.Login(t.Context(), "web", "", "127.0.0.1", "", ext); !errors.Is(err, domain.ErrAccountUnavailable) {
		t.Errorf("停用后换入口登录 error = %v, want ErrAccountUnavailable", err)
	}
}

func TestResolveBind_Branches(t *testing.T) {
	// 当前会话属于 A。
	svcA, fakesA := newAccountService()
	tokensA, err := svcA.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", wechatIdentity("bind-a"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("换成另一个 unionid：替换、吊销旧会话、当前会话改指向、旧 openid 清空", func(t *testing.T) {
		// A 的另一个会话（用旧微信登录）。
		other, err := svcA.Login(t.Context(), "app", "", "127.0.0.1", "", wechatIdentity("bind-a"))
		if err != nil {
			t.Fatal(err)
		}
		bound, err := svcA.BindIdentity(t.Context(), tokensA.Session.ID, wechatIdentity("bind-a2"))
		if err != nil {
			t.Fatal(err)
		}
		idns, _ := fakesA.Identities.ListByUser(t.Context(), tokensA.Session.UserID)
		if len(idns) != 1 || idns[0].ID != bound.ID || idns[0].Subject != "bind-a2" {
			t.Fatalf("identities = %+v", idns)
		}
		if _, err := fakesA.Sessions.GetValid(t.Context(), other.Session.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("旧会话未被吊销：%v", err)
		}
		if _, err := fakesA.Sessions.GetValid(t.Context(), tokensA.Session.ID); err != nil {
			t.Errorf("当前会话不应被吊销：%v", err)
		}
		cur, err := fakesA.Sessions.Get(t.Context(), tokensA.Session.ID)
		if err != nil || cur.LoginIdentityID == nil || *cur.LoginIdentityID != bound.ID {
			t.Errorf("当前会话的 login_identity_id = %v, want 指向 %v", cur.LoginIdentityID, bound.ID)
		}
		if got := fakesA.OpenIDs.OpenIDs(tokensA.Session.UserID); len(got) != 1 || got["mini-appid"] != "openid-bind-a2" {
			t.Errorf("openids = %v, want 只剩新 unionid 的本入口", got)
		}
	})

	t.Run("绑定已被其它账号占用的 unionid：拒绝", func(t *testing.T) {
		// 同一个 service：账号 B 先登录占住 bind-b，A 再绑它。
		if _, err := svcA.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", wechatIdentity("bind-b")); err != nil {
			t.Fatal(err)
		}
		if _, err := svcA.BindIdentity(t.Context(), tokensA.Session.ID, wechatIdentity("bind-b")); !errors.Is(err, domain.ErrAlreadyBound) {
			t.Errorf("绑定别人的 unionid error = %v, want ErrAlreadyBound", err)
		}
	})

	t.Run("重复绑定同一个 unionid：幂等，不换行", func(t *testing.T) {
		svcC, fakesC := newAccountService()
		tokens, err := svcC.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", wechatIdentity("bind-c"))
		if err != nil {
			t.Fatal(err)
		}
		first, _ := fakesC.Identities.ListByUser(t.Context(), tokens.Session.UserID)
		if _, err := svcC.BindIdentity(t.Context(), tokens.Session.ID, wechatIdentity("bind-c")); err != nil {
			t.Fatal(err)
		}
		second, _ := fakesC.Identities.ListByUser(t.Context(), tokens.Session.UserID)
		if len(second) != 1 || second[0].ID != first[0].ID {
			t.Errorf("重复绑定换了行：%v -> %v", first, second)
		}
	})
}

func TestReauthenticate(t *testing.T) {
	svc, fakes := newAccountService()
	tokens, err := svc.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", wechatIdentity("re-1"))
	if err != nil {
		t.Fatal(err)
	}

	// 属于当前账号：更新 auth_time、amr 追加。
	if err := svc.Reauthenticate(t.Context(), tokens.Session.ID, wechatIdentity("re-1")); err != nil {
		t.Fatal(err)
	}
	sess, err := fakes.Sessions.Get(t.Context(), tokens.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sess.AuthTime.After(time.Now().Add(-time.Minute)) || len(sess.AMR) != 2 {
		t.Errorf("auth_time/amr = %v %v", sess.AuthTime, sess.AMR)
	}

	// 别人的 unionid：拒绝。
	if err := svc.Reauthenticate(t.Context(), tokens.Session.ID, wechatIdentity("someone-else")); !errors.Is(err, domain.ErrReauthWrongAccount) {
		t.Errorf("用别人的微信重新验证 error = %v, want ErrReauthWrongAccount", err)
	}
}

func TestUnbindIdentity(t *testing.T) {
	svc, fakes := newAccountService()
	tokens, err := svc.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", wechatIdentity("un-1"))
	if err != nil {
		t.Fatal(err)
	}
	idns, _ := fakes.Identities.ListByUser(t.Context(), tokens.Session.UserID)

	// 唯一的登录方式不能解绑。
	if err := svc.UnbindIdentity(t.Context(), tokens.Session.UserID, idns[0].ID, tokens.Session.ID); !errors.Is(err, domain.ErrLastIdentity) {
		t.Errorf("解绑唯一登录方式 error = %v, want ErrLastIdentity", err)
	}
	if err := svc.UnbindIdentity(t.Context(), tokens.Session.UserID, "no-such", tokens.Session.ID); !errors.Is(err, domain.ErrIdentityNotFound) {
		t.Errorf("解绑不存在 error = %v, want ErrIdentityNotFound", err)
	}
}

func TestDeleteMe(t *testing.T) {
	svc, fakes := newAccountService()
	tokens, err := svc.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", wechatIdentity("del-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteMe(t.Context(), tokens.Session.UserID); err != nil {
		t.Fatal(err)
	}
	user, err := fakes.Users.GetByID(t.Context(), tokens.Session.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if user.Status != domain.UserStatusDeleted || user.DisplayName != "" || user.AvatarUrl != "" {
		t.Errorf("user = %+v", user)
	}
	if _, err := fakes.Sessions.GetValid(t.Context(), tokens.Session.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("会话未吊销：%v", err)
	}
	if idns, _ := fakes.Identities.ListByUser(t.Context(), user.ID); len(idns) != 0 {
		t.Errorf("identities = %v, want 空", idns)
	}
	if got := fakes.OpenIDs.OpenIDs(user.ID); len(got) != 0 {
		t.Errorf("openids = %v, want 空", got)
	}
	if n := len(fakes.Publisher.UserDeleted); n != 1 || fakes.Publisher.UserDeleted[0].UserID != user.ID {
		t.Errorf("user_deleted = %v", fakes.Publisher.UserDeleted)
	}

	// 注销后用同一个微信登录是新账号。
	tokens2, err := svc.Login(t.Context(), "miniprogram", "", "127.0.0.1", "", wechatIdentity("del-1"))
	if err != nil {
		t.Fatal(err)
	}
	if tokens2.Session.UserID == user.ID {
		t.Error("注销后同一微信登录应得到新账号")
	}
}

func TestRefresh_Logout(t *testing.T) {
	svc, fakes := newAccountService()
	tokens, err := svc.Login(t.Context(), "web", "", "127.0.0.1", "", wechatIdentity("rf-1"))
	if err != nil {
		t.Fatal(err)
	}

	// 正常轮换：拿到新一代 token。
	next, err := svc.Refresh(t.Context(), tokens.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if next.RefreshToken == tokens.RefreshToken || next.AccessToken == "" {
		t.Errorf("next = %+v", next)
	}
	// 宽限期内上一代还能用（10 秒）。
	if _, err := svc.Refresh(t.Context(), tokens.RefreshToken); err != nil {
		t.Errorf("宽限期内上一代 error = %v", err)
	}
	// 上上代已不在宽限期：视为泄露，会话被吊销。
	if _, err := svc.Refresh(t.Context(), tokens.RefreshToken); !errors.Is(err, domain.ErrInvalidToken) {
		t.Errorf("上上代 error = %v, want ErrInvalidToken", err)
	}
	sess, err := fakes.Sessions.Get(t.Context(), tokens.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.RevokedAt == nil {
		t.Error("泄露复用后会话应被吊销")
	}

	// 登出示意：摘要匹配当前值才吊销。
	tokens2, err := svc.Login(t.Context(), "web", "", "127.0.0.1", "", wechatIdentity("rf-2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(t.Context(), tokens2.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := fakes.Sessions.GetValid(t.Context(), tokens2.Session.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("登出后会话应失效：%v", err)
	}
	if err := svc.Logout(t.Context(), "garbage"); err != nil {
		t.Errorf("乱码登出应幂等：%v", err)
	}
}
