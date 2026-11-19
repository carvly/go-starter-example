// Package domaintest 提供 order 的内存 fake，供本模块和 fulfillment 的测试复用。
package domaintest

import (
	"context"
	"slices"
	"sync"
	"time"

	"app.local/internal/kit/page"

	"app.local/internal/modules/order/domain"
	"app.local/internal/modules/order/events"
)

// Repository 是 domain.Repository 的内存实现，状态机规则与 Postgres 实现一致。
type Repository struct {
	mu        sync.Mutex
	byID      map[string]*domain.Order
	seq       []string
	processed map[string]bool
}

func NewRepository() *Repository {
	return &Repository{byID: make(map[string]*domain.Order), processed: make(map[string]bool)}
}

func (f *Repository) Create(_ context.Context, o *domain.Order) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	o.CreatedAt, o.UpdatedAt = now, now
	f.byID[o.ID] = new(*o)
	f.seq = append([]string{o.ID}, f.seq...)
	return nil
}

func (f *Repository) GetByID(_ context.Context, id string) (*domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return new(*o), nil
}

func (f *Repository) List(_ context.Context, cursor string, limit int) ([]*domain.Order, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	start := 0
	if cursor != "" {
		i := slices.Index(f.seq, cursor)
		if i < 0 {
			return nil, "", domain.ErrInvalidCursor
		}
		start = i + 1
	}
	end := min(start+limit, len(f.seq))
	items := make([]*domain.Order, 0, end-start)
	for _, id := range f.seq[start:end] {
		items = append(items, new(*f.byID[id]))
	}
	next := ""
	if end < len(f.seq) {
		next = f.seq[end-1]
	}
	return items, next, nil
}

func (f *Repository) Search(_ context.Context, filter domain.SearchFilter, p page.Request) (page.Result[*domain.Order], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var matched []*domain.Order
	for _, id := range f.seq {
		o := f.byID[id]
		if (len(filter.Statuses) == 0 || slices.Contains(filter.Statuses, o.Status)) &&
			(filter.From.IsZero() || !o.CreatedAt.Before(filter.From)) &&
			(filter.To.IsZero() || o.CreatedAt.Before(filter.To)) {
			matched = append(matched, new(*o))
		}
	}
	r := page.Result[*domain.Order]{Total: min(len(matched), page.MaxTotal), TotalCapped: len(matched) > page.MaxTotal}
	start := min(p.Offset(), len(matched))
	r.Items = matched[start:min(start+p.Size(), len(matched))]
	return r, nil
}

func (f *Repository) UpdateStatus(_ context.Context, id string, status domain.Status) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	if o.Status != domain.StatusPending {
		return domain.ErrInvalidStatusTransition
	}
	o.Status = status
	return nil
}

func (f *Repository) MarkEventProcessed(_ context.Context, orderID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.processed[orderID] {
		return false, nil
	}
	f.processed[orderID] = true
	return true, nil
}

// Publisher 记录发布过的事件；Err 非 nil 时发布失败。
type Publisher struct {
	mu        sync.Mutex
	Published []events.OrderCreated
	Err       error
}

func (f *Publisher) PublishOrderCreated(_ context.Context, evt events.OrderCreated) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Published = append(f.Published, evt)
	return nil
}

var (
	_ domain.Repository = (*Repository)(nil)
	_ domain.Publisher  = (*Publisher)(nil)
)
