// Package domaintest 提供 user 全部端口内存 fake、夹具和 Repository 契约，供本模块
// 和组合根的测试复用。注意：这里不能组装 domain.Service（uowtest 只允许在
// _test.go 里导入），需要 Service 的测试自己用 domaintest.NewFakes() 组装。
package domaintest

import (
	"context"
	"fmt"
	"time"

	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/events"
)

// Fakes 是 user 全部端口内存实现的集合，测试断言时直接读。
type Fakes struct {
	Users      *Repository
	Identities *IdentityRepository
	OpenIDs    *WeChatOpenIDRepository
	Sessions   *SessionRepository
	States     *OAuthStateRepository
	Publisher  *Publisher
}

func NewFakes() *Fakes {
	return &Fakes{
		Users:      NewRepository(),
		Identities: NewIdentityRepository(),
		OpenIDs:    NewWeChatOpenIDRepository(),
		Sessions:   NewSessionRepository(),
		States:     NewOAuthStateRepository(),
		Publisher:  &Publisher{},
	}
}

// Deps 把 fakes 转成 domain.Deps（UoW/Issuer/Config 由调用方补）。
func (f *Fakes) Deps() domain.Deps {
	return domain.Deps{
		Users:      f.Users,
		Identities: f.Identities,
		OpenIDs:    f.OpenIDs,
		Sessions:   f.Sessions,
		States:     f.States,
		Publisher:  f.Publisher,
	}
}

// TestConfig 是跑在内存 fake 上的 Service 配置。
func TestConfig() domain.Config {
	return domain.Config{
		SessionMaxAge:      30 * 24 * time.Hour,
		SessionIdleTimeout: 7 * 24 * time.Hour,
		AccessTokenTTL:     15 * time.Minute,
		RefreshGracePeriod: 10 * time.Second,
		ReauthWindow:       10 * time.Minute,
	}
}

// Issuer 是 AccessTokenIssuer 的内存实现：不签真 JWT，值里带 userID/sid 便于断言。
type Issuer struct{}

func (Issuer) Issue(userID, sid string, _ time.Time) (string, error) {
	return fmt.Sprintf("token.%s.%s", userID, sid), nil
}

// Publisher 记录发布的事件，测试断言用。
type Publisher struct {
	UserDeleted []events.UserDeleted
}

func (p *Publisher) PublishUserDeleted(_ context.Context, evt events.UserDeleted) error {
	p.UserDeleted = append(p.UserDeleted, evt)
	return nil
}
