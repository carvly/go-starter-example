package domaintest

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"app.local/internal/modules/user/domain"
)

// IdentityRepository 是 domain.IdentityRepository 的内存实现。
type IdentityRepository struct {
	mu     sync.Mutex
	byID   map[string]*domain.Identity
	nextID int
}

func NewIdentityRepository() *IdentityRepository {
	return &IdentityRepository{byID: make(map[string]*domain.Identity)}
}

func (f *IdentityRepository) next() string {
	f.nextID++
	return identityID(f.nextID)
}

func identityID(n int) string {
	// 固定形状，便于测试断言。
	return "01900000-0000-7000-0000-" + pad4(n)
}

func pad4(n int) string {
	s := []byte("000000000000")
	for i := len(s) - 1; i >= 0 && n > 0; i-- {
		s[i] = byte('0' + n%10)
		n /= 10
	}
	return string(s)
}

func (f *IdentityRepository) GetByProviderSubject(_ context.Context, provider, subject string) (*domain.Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range f.byID {
		if v.Provider == provider && v.Subject == subject {
			return new(*v), nil
		}
	}
	return nil, domain.ErrIdentityNotFound
}

func (f *IdentityRepository) GetByUserAndProvider(_ context.Context, userID, provider string) (*domain.Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range f.byID {
		if v.UserID == userID && v.Provider == provider {
			return new(*v), nil
		}
	}
	return nil, domain.ErrIdentityNotFound
}

func (f *IdentityRepository) ListByUser(_ context.Context, userID string) ([]*domain.Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Identity
	for _, v := range f.byID {
		if v.UserID == userID {
			out = append(out, new(*v))
		}
	}
	slices.SortFunc(out, func(a, b *domain.Identity) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}

// PageByUser 用最后一条的 ID 作 cursor，按 created_at、id 倒序。
func (f *IdentityRepository) PageByUser(_ context.Context, userID, cursor string, limit int) ([]*domain.Identity, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []*domain.Identity
	for _, v := range f.byID {
		if v.UserID == userID {
			all = append(all, new(*v))
		}
	}
	return pageDesc(all, cursor, limit, func(v *domain.Identity) (time.Time, string) { return v.CreatedAt, v.ID })
}

// pageDesc 是两个 fake 共用的倒序分页：cursor 是上一页最后一条的 ID。
func pageDesc[T any](all []T, cursor string, limit int, key func(T) (time.Time, string)) ([]T, string, error) {
	slices.SortFunc(all, func(a, b T) int {
		ta, ia := key(a)
		tb, ib := key(b)
		if c := tb.Compare(ta); c != 0 {
			return c
		}
		return strings.Compare(ib, ia)
	})
	start := 0
	if cursor != "" {
		i := slices.IndexFunc(all, func(v T) bool { _, id := key(v); return id == cursor })
		if i < 0 {
			return nil, "", domain.ErrInvalidCursor
		}
		start = i + 1
	}
	end := min(start+limit, len(all))
	next := ""
	if end < len(all) {
		_, next = key(all[end-1])
	}
	return all[start:end], next, nil
}

func (f *IdentityRepository) Create(_ context.Context, v *domain.Identity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v.ID == "" {
		v.ID = f.next()
	}
	if err := f.unique(v); err != nil {
		return err
	}
	now := time.Now()
	v.CreatedAt, v.UpdatedAt = now, now
	f.byID[v.ID] = new(*v)
	return nil
}

func (f *IdentityRepository) unique(v *domain.Identity) error {
	for _, cur := range f.byID {
		if cur.Provider == v.Provider && cur.Subject == v.Subject {
			return domain.ErrAlreadyBound
		}
		if cur.UserID == v.UserID && cur.Provider == v.Provider {
			return domain.ErrAlreadyBound
		}
	}
	return nil
}

// Replace 模拟 DELETE+INSERT（新 ID）。
func (f *IdentityRepository) Replace(_ context.Context, userID, provider string, v *domain.Identity) (*domain.Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var old *domain.Identity
	for _, cur := range f.byID {
		if cur.UserID == userID && cur.Provider == provider {
			old = cur
		}
	}
	if old == nil {
		return nil, domain.ErrIdentityNotFound
	}
	delete(f.byID, old.ID)
	if v.ID == "" {
		v.ID = f.next()
	}
	if err := f.unique(v); err != nil {
		return nil, err
	}
	now := time.Now()
	v.CreatedAt, v.UpdatedAt = now, now
	f.byID[v.ID] = new(*v)
	return new(*v), nil
}

func (f *IdentityRepository) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byID[id]; !ok {
		return domain.ErrIdentityNotFound
	}
	delete(f.byID, id)
	return nil
}

func (f *IdentityRepository) TouchLogin(_ context.Context, id string, profile map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.byID[id]
	if !ok {
		return domain.ErrIdentityNotFound
	}
	next := *v
	now := time.Now()
	next.LastLoginAt = &now
	next.Profile = profile
	next.UpdatedAt = now
	f.byID[id] = &next
	return nil
}

var _ domain.IdentityRepository = (*IdentityRepository)(nil)

// WeChatOpenIDRepository 是 domain.WeChatOpenIDRepository 的内存实现。
type WeChatOpenIDRepository struct {
	mu        sync.Mutex
	byUserApp map[string]*openidRow
	byAppOpen map[string]string // appid|openid -> userID，模拟 (appid, openid) 唯一
}

type openidRow struct {
	userID string
	appid  string
	openid string
}

func NewWeChatOpenIDRepository() *WeChatOpenIDRepository {
	return &WeChatOpenIDRepository{byUserApp: make(map[string]*openidRow), byAppOpen: make(map[string]string)}
}

func (f *WeChatOpenIDRepository) Get(_ context.Context, userID, appid string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.byUserApp[userID+"|"+appid]
	if !ok {
		return "", domain.ErrNotFound
	}
	return row.openid, nil
}

func (f *WeChatOpenIDRepository) Upsert(_ context.Context, userID, appid, openid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if owner, ok := f.byAppOpen[appid+"|"+openid]; ok && owner != userID {
		return domain.ErrAlreadyBound
	}
	key := userID + "|" + appid
	if old, ok := f.byUserApp[key]; ok {
		delete(f.byAppOpen, old.appid+"|"+old.openid)
	}
	f.byUserApp[key] = &openidRow{userID: userID, appid: appid, openid: openid}
	f.byAppOpen[appid+"|"+openid] = userID
	return nil
}

func (f *WeChatOpenIDRepository) DeleteByUser(_ context.Context, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, row := range f.byUserApp {
		if row.userID == userID {
			delete(f.byAppOpen, row.appid+"|"+row.openid)
			delete(f.byUserApp, key)
		}
	}
	return nil
}

// OpenIDs 返回该账号的全部 openid，测试断言用。
func (f *WeChatOpenIDRepository) OpenIDs(userID string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for _, row := range f.byUserApp {
		if row.userID == userID {
			out[row.appid] = row.openid
		}
	}
	return out
}

var _ domain.WeChatOpenIDRepository = (*WeChatOpenIDRepository)(nil)
