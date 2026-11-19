package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"app.local/internal/kit/page"
	"app.local/internal/kit/uow"
	"app.local/internal/modules/order/events"
)

// Service 需要商品信息时经 ProductLookup 窄接口获取，不 import product 模块。
type Service struct {
	repo      Repository
	products  ProductLookup
	publisher Publisher
	uow       uow.UnitOfWork
}

func NewService(repo Repository, products ProductLookup, publisher Publisher, u uow.UnitOfWork) *Service {
	return &Service{repo: repo, products: products, publisher: publisher, uow: u}
}

// Create 查商品单价算出总金额，订单落库和 order_created 事件入队在同一个事务里；
// 扣库存和确认订单由 fulfillment 异步完成。
func (s *Service) Create(ctx context.Context, customerName, productID string, quantity int64) (*Order, error) {
	customerName = strings.TrimSpace(customerName)
	if customerName == "" {
		return nil, ErrInvalidCustomerName
	}
	if quantity <= 0 || quantity > MaxQuantity {
		return nil, ErrInvalidQuantity
	}

	name, priceCents, err := s.products.GetPriceAndName(ctx, productID)
	if err != nil {
		if errors.Is(err, ErrProductNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("create order: lookup product: %w", err)
	}

	totalCents := priceCents * quantity
	// 单价没有上限，quantity 合法时乘积仍可能溢出 int64，除回去验证一遍。
	if quantity != 0 && totalCents/quantity != priceCents {
		return nil, ErrInvalidQuantity
	}

	o := &Order{
		ID:             uuid.NewV7().String(),
		CustomerName:   customerName,
		ProductID:      productID,
		ProductName:    name,
		Quantity:       quantity,
		UnitPriceCents: priceCents,
		TotalCents:     totalCents,
		Status:         StatusPending,
	}

	err = s.uow.Run(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, o); err != nil {
			return err
		}
		return s.publisher.PublishOrderCreated(ctx, events.OrderCreated{
			OrderID:   o.ID,
			ProductID: o.ProductID,
			Quantity:  o.Quantity,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	return o, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Order, error) {
	o, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	return o, nil
}

func (s *Service) List(ctx context.Context, cursor string, limit int) (items []*Order, nextCursor string, err error) {
	if limit <= 0 || limit > MaxListLimit {
		limit = DefaultListLimit
	}
	items, nextCursor, err = s.repo.List(ctx, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list orders: %w", err)
	}
	return items, nextCursor, nil
}

// Search 是后台的订单列表（只读）。
func (s *Service) Search(ctx context.Context, f SearchFilter, p page.Request) (page.Result[*Order], error) {
	for _, st := range f.Statuses {
		if !st.Valid() {
			return page.Result[*Order]{}, ErrInvalidStatus
		}
	}
	if !f.From.IsZero() && !f.To.IsZero() && !f.From.Before(f.To) {
		return page.Result[*Order]{}, ErrInvalidTimeRange
	}
	r, err := s.repo.Search(ctx, f, p)
	if err != nil {
		return page.Result[*Order]{}, fmt.Errorf("search orders: %w", err)
	}
	return r, nil
}

// Confirm 由 fulfillment 在扣库存成功后调用。
func (s *Service) Confirm(ctx context.Context, id string) error {
	if err := s.repo.UpdateStatus(ctx, id, StatusConfirmed); err != nil {
		return fmt.Errorf("confirm order: %w", err)
	}
	return nil
}

// MarkFailed 由 fulfillment 在扣库存永久失败后调用，让订单收口为终态。
func (s *Service) MarkFailed(ctx context.Context, id string) error {
	if err := s.repo.UpdateStatus(ctx, id, StatusFailed); err != nil {
		return fmt.Errorf("mark order failed: %w", err)
	}
	return nil
}
