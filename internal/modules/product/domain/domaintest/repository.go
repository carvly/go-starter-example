// Package domaintest 提供 product 的内存 fake，供本模块和依赖它的测试复用。
package domaintest

import (
	"context"
	"slices"
	"strings"
	"sync"

	"app.local/internal/kit/page"

	"app.local/internal/modules/product/domain"
)

// Repository 是 domain.Repository 的内存实现。List 用 ID 作 cursor，按创建倒序。
type Repository struct {
	mu    sync.Mutex
	byID  map[string]*domain.Product
	order []string
}

func NewRepository() *Repository {
	return &Repository{byID: make(map[string]*domain.Product)}
}

func (f *Repository) Create(_ context.Context, p *domain.Product) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p.Version = 1
	f.byID[p.ID] = new(*p)
	f.order = append([]string{p.ID}, f.order...)
	return nil
}

func (f *Repository) GetByID(_ context.Context, id string) (*domain.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return new(*p), nil
}

func (f *Repository) List(_ context.Context, cursor string, limit int) ([]*domain.Product, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	start := 0
	if cursor != "" {
		i := slices.Index(f.order, cursor)
		if i < 0 {
			return nil, "", domain.ErrInvalidCursor
		}
		start = i + 1
	}
	end := min(start+limit, len(f.order))
	items := make([]*domain.Product, 0, end-start)
	for _, id := range f.order[start:end] {
		items = append(items, new(*f.byID[id]))
	}
	next := ""
	if end < len(f.order) {
		next = f.order[end-1]
	}
	return items, next, nil
}

func (f *Repository) Search(_ context.Context, keyword string, p page.Request) (page.Result[*domain.Product], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var matched []*domain.Product
	for _, id := range f.order {
		if strings.Contains(strings.ToLower(f.byID[id].Name), strings.ToLower(keyword)) {
			matched = append(matched, new(*f.byID[id]))
		}
	}
	r := page.Result[*domain.Product]{Total: min(len(matched), page.MaxTotal), TotalCapped: len(matched) > page.MaxTotal}
	start := min(p.Offset(), len(matched))
	r.Items = matched[start:min(start+p.Size(), len(matched))]
	return r, nil
}

func (f *Repository) Update(_ context.Context, id, name string, priceCents, version int64) (*domain.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if p.Version != version {
		return nil, domain.ErrConflict
	}
	p.Name, p.PriceCents, p.Version = name, priceCents, p.Version+1
	return new(*p), nil
}

func (f *Repository) AdjustStock(_ context.Context, id string, delta int64) (*domain.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	switch next := p.Stock + delta; {
	case next < 0:
		return nil, domain.ErrInsufficientStock
	case next > domain.MaxStock:
		return nil, domain.ErrStockTooLarge
	default:
		p.Stock = next
	}
	return new(*p), nil
}

func (f *Repository) DecreaseStock(_ context.Context, id string, quantity int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	if p.Stock < quantity {
		return domain.ErrInsufficientStock
	}
	p.Stock -= quantity
	return nil
}

var _ domain.Repository = (*Repository)(nil)
