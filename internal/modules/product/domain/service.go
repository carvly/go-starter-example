package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
	"uuid"

	"app.local/internal/kit/page"
	"app.local/internal/kit/uow"
)

// cacheTTL 只是兜底：真正的失效靠"写 DB 后删缓存"。
const cacheTTL = 5 * time.Minute

type Service struct {
	repo  Repository
	cache Cache // 可以是 nil
}

// Option 让 Cache 成为可选协作者，测试等调用点不必硬塞一个 NoopCache。
type Option func(*Service)

func WithCache(cache Cache) Option {
	return func(s *Service) { s.cache = cache }
}

func NewService(repo Repository, opts ...Option) *Service {
	s := &Service{repo: repo}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create 创建一个新商品。名称、价格、库存的校验放在 Service 层，Repository 只负责
// 持久化，不重复校验。
func (s *Service) Create(ctx context.Context, name string, priceCents, stock int64) (*Product, error) {
	name, err := validate(name, priceCents)
	if err != nil {
		return nil, err
	}
	if stock < 0 || stock > MaxStock {
		return nil, ErrInvalidStock
	}

	p := &Product{
		ID:         uuid.NewV7().String(),
		Name:       name,
		PriceCents: priceCents,
		Stock:      stock,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}
	return p, nil
}

func validate(name string, priceCents int64) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrInvalidName
	}
	if priceCents <= 0 {
		return "", ErrInvalidPrice
	}
	return name, nil
}

// Update 改名称和单价。改价只影响之后的订单：订单保存了下单时的名称和单价。version 是打开
// 编辑表单时读到的版本，两人同时编辑时后提交的得到 ErrConflict，而不是悄悄覆盖先提交的。
func (s *Service) Update(ctx context.Context, id, name string, priceCents, version int64) (*Product, error) {
	name, err := validate(name, priceCents)
	if err != nil {
		return nil, err
	}
	p, err := s.repo.Update(ctx, id, name, priceCents, version)
	if err != nil {
		return nil, fmt.Errorf("update product: %w", err)
	}
	s.invalidate(ctx, id)
	return p, nil
}

// AdjustStock 把库存加上 delta（进货为正，盘亏为负），与下单扣减并发也不会丢更新。
func (s *Service) AdjustStock(ctx context.Context, id string, delta int64) (*Product, error) {
	if delta == 0 || delta > MaxStock || delta < -MaxStock {
		return nil, ErrInvalidStockDelta
	}
	p, err := s.repo.AdjustStock(ctx, id, delta)
	if err != nil {
		return nil, fmt.Errorf("adjust stock: %w", err)
	}
	s.invalidate(ctx, id)
	return p, nil
}

// Search 是后台的商品列表。直接读库、不走缓存：后台改价、调库存后要立刻看到结果。
func (s *Service) Search(ctx context.Context, keyword string, p page.Request) (page.Result[*Product], error) {
	r, err := s.repo.Search(ctx, strings.TrimSpace(keyword), p)
	if err != nil {
		return page.Result[*Product]{}, fmt.Errorf("search products: %w", err)
	}
	return r, nil
}

// GetFresh 直接读库、不读也不回填缓存：后台编辑冲突后要拿到最新的 Version，缓存里可能还是
// 修改前的值（并发 Get 在删缓存之后回填了旧值，要等 TTL 才过期）。
func (s *Service) GetFresh(ctx context.Context, id string) (*Product, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get product: %w", err)
	}
	return p, nil
}

// invalidate 在事务提交后删缓存（不在事务里时立即删）：提交前删的话，并发 Get 会把
// 修改前的值回填进缓存。
func (s *Service) invalidate(ctx context.Context, id string) {
	if s.cache != nil {
		uow.AfterCommit(ctx, func(ctx context.Context) { _ = s.cache.Delete(ctx, id) })
	}
}

// Get 走 cache-aside。缓存实现自己 fail open，故障在这里表现为一次普通未命中。
// 在事务里不读缓存（要读到本事务自己的写入），回填也等提交后再做（不缓存未提交的值）。
func (s *Service) Get(ctx context.Context, id string) (*Product, error) {
	if s.cache != nil && !uow.InTransaction(ctx) {
		if cached, hit, err := s.cache.Get(ctx, id); err == nil && hit {
			return cached, nil
		}
	}

	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get product: %w", err)
	}

	if s.cache != nil {
		uow.AfterCommit(ctx, func(ctx context.Context) { _ = s.cache.Set(ctx, id, p, cacheTTL) })
	}
	return p, nil
}

// List 分页列出商品。limit 不在 [1, MaxListLimit] 范围内时回落到
// DefaultListLimit，不能让调用方传一个超大 limit 绕过分页的本意。
func (s *Service) List(ctx context.Context, cursor string, limit int) (items []*Product, nextCursor string, err error) {
	if limit <= 0 || limit > MaxListLimit {
		limit = DefaultListLimit
	}
	items, nextCursor, err = s.repo.List(ctx, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list products: %w", err)
	}
	return items, nextCursor, nil
}

// DecreaseStock 由 fulfillment 在处理 order_created 事件时经组合根适配器调用。
func (s *Service) DecreaseStock(ctx context.Context, id string, quantity int64) error {
	if quantity <= 0 {
		return ErrInvalidStock
	}
	if err := s.repo.DecreaseStock(ctx, id, quantity); err != nil {
		return fmt.Errorf("decrease stock: %w", err)
	}
	// fulfillment 在事务里调用这里，删缓存要等提交之后。
	s.invalidate(ctx, id)
	return nil
}
