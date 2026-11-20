// Package domaintest 提供 user 的内存 fake、夹具和 Repository 契约，供本模块和组合根的测试复用。
package domaintest

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"app.local/internal/kit/page"
	"app.local/internal/modules/user/domain"
)

// Repository 是 domain.Repository 的内存实现。
type Repository struct {
	mu   sync.Mutex
	byID map[string]*domain.User
}

func NewRepository() *Repository {
	return &Repository{byID: make(map[string]*domain.User)}
}

func (f *Repository) Create(_ context.Context, v *domain.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.unique(v); err != nil {
		return err
	}
	now := time.Now()
	v.CreatedAt, v.UpdatedAt = now, now
	f.byID[v.ID] = new(*v)
	return nil
}

// unique 模拟唯一约束。
func (f *Repository) unique(v *domain.User) error {
	return nil
}

func (f *Repository) GetByID(_ context.Context, id string) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return new(*v), nil
}

func (f *Repository) ListByIDs(_ context.Context, ids []string) ([]*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*domain.User, 0, len(ids))
	for _, id := range ids {
		if v, ok := f.byID[id]; ok {
			out = append(out, new(*v))
		}
	}
	return out, nil
}

func (f *Repository) TouchLogin(_ context.Context, id string) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	next := *cur
	next.LastLoginAt = time.Now()
	next.UpdatedAt = next.LastLoginAt
	f.byID[id] = &next
	return new(next), nil
}

func (f *Repository) List(_ context.Context, flt domain.UserFilter, s page.Sort, p page.Request) (page.Result[*domain.User], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []*domain.User
	for _, v := range f.byID {
		if match(v, flt) {
			items = append(items, new(*v))
		}
	}
	slices.SortFunc(items, func(a, b *domain.User) int {
		c := cmp.Or(compare(a, b, s.Field), cmp.Compare(a.ID, b.ID))
		if s.Desc {
			return -c
		}
		return c
	})
	total, capped := len(items), false
	if total > page.MaxTotal {
		total, capped = page.MaxTotal, true
	}
	start := min(p.Offset(), len(items))
	end := min(start+p.Size(), len(items))
	return page.Result[*domain.User]{Items: items[start:end], Total: total, TotalCapped: capped}, nil
}

func match(v *domain.User, f domain.UserFilter) bool {
	if f.Keyword != "" && !containsAny(f.Keyword, v.DisplayName) {
		return false
	}
	if len(f.Status) > 0 && !slices.Contains(f.Status, v.Status) {
		return false
	}
	if !inRange(&v.CreatedAt, f.CreatedAtFrom, f.CreatedAtTo) {
		return false
	}
	if !inRange(&v.LastLoginAt, f.LastLoginAtFrom, f.LastLoginAtTo) {
		return false
	}
	return true
}

// compare 按排序字段比较。可空的字段按 Postgres 的默认顺序：正序时 NULL 在最后。
func compare(a, b *domain.User, field string) int {
	switch field {
	case "created_at":
		return a.CreatedAt.Compare(b.CreatedAt)
	case "last_login_at":
		return a.LastLoginAt.Compare(b.LastLoginAt)
	}
	return 0
}

// containsAny 模拟 ILIKE '%keyword%'：任一字段包含关键字（不区分大小写）。
func containsAny(keyword string, values ...string) bool {
	k := strings.ToLower(keyword)
	for _, v := range values {
		if strings.Contains(strings.ToLower(v), k) {
			return true
		}
	}
	return false
}

// inRange 报告 t 是否在半开区间 [from, to) 内；t 为 nil 时只有不筛选才算在内。
func inRange(t, from, to *time.Time) bool {
	if from == nil && to == nil {
		return true
	}
	return t != nil && (from == nil || !t.Before(*from)) && (to == nil || t.Before(*to))
}

func (f *Repository) Enable(_ context.Context, id string) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if !slices.Contains([]domain.UserStatus{domain.UserStatusDisabled}, cur.Status) {
		return nil, domain.ErrInvalidState
	}
	next := *cur
	next.Status = domain.UserStatusActive
	if err := f.unique(&next); err != nil {
		return nil, err
	}
	next.UpdatedAt = time.Now()
	f.byID[id] = &next
	return new(next), nil
}

// Disable 只改状态：内存 fake 没有会话，吊销会话由 Postgres 的集成测试覆盖。
func (f *Repository) Disable(_ context.Context, id string) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if cur.Status != domain.UserStatusActive {
		return nil, domain.ErrInvalidState
	}
	next := *cur
	next.Status = domain.UserStatusDisabled
	next.UpdatedAt = time.Now()
	f.byID[id] = &next
	return new(next), nil
}

// UpdateProfile 模拟 COALESCE：nil 保持原值。
func (f *Repository) UpdateProfile(_ context.Context, id string, displayName, avatarURL *string) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	next := *cur
	if displayName != nil {
		next.DisplayName = *displayName
	}
	if avatarURL != nil {
		next.AvatarUrl = *avatarURL
	}
	next.UpdatedAt = time.Now()
	f.byID[id] = &next
	return new(next), nil
}

// MarkDeleted 模拟注销：状态置 deleted 并清空昵称、头像。
func (f *Repository) MarkDeleted(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	if cur.Status == domain.UserStatusDeleted {
		return domain.ErrNotFound
	}
	next := *cur
	next.Status = domain.UserStatusDeleted
	next.DisplayName = ""
	next.AvatarUrl = ""
	next.UpdatedAt = time.Now()
	f.byID[id] = &next
	return nil
}

var _ domain.Repository = (*Repository)(nil)

// UsersList 返回全部用户，测试断言用。
func (f *Repository) UsersList() []*domain.User {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*domain.User, 0, len(f.byID))
	for _, v := range f.byID {
		out = append(out, new(*v))
	}
	return out
}
