// Package postgres 是 order 的 Repository 实现（pgx/v5，经 dbtx.Handle 取连接）。
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"app.local/internal/kit/page"
	order "app.local/internal/modules/order/domain"
	"app.local/internal/platform/dbtx"
	platformpg "app.local/internal/platform/postgres"
)

type Repository struct {
	handle *dbtx.Handle
}

func NewRepository(handle *dbtx.Handle) *Repository {
	return &Repository{handle: handle}
}

func scanOrder(row pgx.CollectableRow) (*order.Order, error) {
	var o order.Order
	err := row.Scan(&o.ID, &o.CustomerName, &o.ProductID, &o.ProductName, &o.Quantity,
		&o.UnitPriceCents, &o.TotalCents, &o.Status, &o.CreatedAt, &o.UpdatedAt)
	return &o, err
}

func (r *Repository) Create(ctx context.Context, o *order.Order) error {
	q := r.handle.Resolve(ctx)
	err := q.QueryRow(ctx, `
		INSERT INTO orders (id, customer_name, product_id, product_name, quantity,
			unit_price_cents, total_cents, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), now())
		RETURNING created_at, updated_at
	`, o.ID, o.CustomerName, o.ProductID, o.ProductName, o.Quantity,
		o.UnitPriceCents, o.TotalCents, o.Status).Scan(&o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres: create order: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id string) (*order.Order, error) {
	q := r.handle.Resolve(ctx)
	rows, err := q.Query(ctx, `
		SELECT id, customer_name, product_id, product_name, quantity,
			unit_price_cents, total_cents, status, created_at, updated_at
		FROM orders WHERE id = $1
	`, id)
	if err != nil {
		return nil, fmt.Errorf("postgres: get order: %w", err)
	}
	o, err := pgx.CollectExactlyOneRow(rows, scanOrder)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, order.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get order: %w", err)
	}
	return o, nil
}

// List 多取一行判断是否还有下一页；limit 在这里再兜底一次，避免 items[limit-1] panic。
func (r *Repository) List(ctx context.Context, cursor string, limit int) ([]*order.Order, string, error) {
	if limit <= 0 {
		limit = order.DefaultListLimit
	}

	createdAt, id, hasCursor, err := platformpg.DecodeCursor(cursor)
	if err != nil {
		return nil, "", order.ErrInvalidCursor
	}

	q := r.handle.Resolve(ctx)
	var rows pgx.Rows
	if hasCursor {
		rows, err = q.Query(ctx, `
			SELECT id, customer_name, product_id, product_name, quantity,
				unit_price_cents, total_cents, status, created_at, updated_at
			FROM orders
			WHERE (created_at, id) < ($1, $2)
			ORDER BY created_at DESC, id DESC
			LIMIT $3
		`, createdAt, id, limit+1)
	} else {
		rows, err = q.Query(ctx, `
			SELECT id, customer_name, product_id, product_name, quantity,
				unit_price_cents, total_cents, status, created_at, updated_at
			FROM orders
			ORDER BY created_at DESC, id DESC
			LIMIT $1
		`, limit+1)
	}
	if err != nil {
		return nil, "", fmt.Errorf("postgres: list orders: %w", err)
	}

	items, err := pgx.CollectRows(rows, scanOrder)
	if err != nil {
		return nil, "", fmt.Errorf("postgres: list orders: %w", err)
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		nextCursor = platformpg.EncodeCursor(last.CreatedAt, last.ID)
		items = items[:limit]
	}
	return items, nextCursor, nil
}

// UpdateStatus 用 `AND status = 'pending'` 让状态判断和更新是同一条原子 SQL。
const orderColumns = `id, customer_name, product_id, product_name, quantity,
	unit_price_cents, total_cents, status, created_at, updated_at`

// Search 按条件拼 WHERE（只放有值的条件，查询计划才能用上索引）：按状态筛选走
// idx_orders_status_created_at_id，不筛状态走 idx_orders_created_at_id。
func (r *Repository) Search(ctx context.Context, f order.SearchFilter, p page.Request) (page.Result[*order.Order], error) {
	var conds []string
	var args []any
	add := func(cond string, arg any) {
		args = append(args, arg)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if len(f.Statuses) > 0 {
		statuses := make([]string, len(f.Statuses))
		for i, st := range f.Statuses {
			statuses[i] = string(st)
		}
		add("status = ANY($%d)", statuses)
	}
	if !f.From.IsZero() {
		add("created_at >= $%d", f.From)
	}
	if !f.To.IsZero() {
		add("created_at < $%d", f.To)
	}
	from := "FROM orders"
	if len(conds) > 0 {
		from += " WHERE " + strings.Join(conds, " AND ")
	}

	q := r.handle.Resolve(ctx)
	total, capped, err := platformpg.CountCapped(ctx, q, "SELECT 1 "+from, args...)
	if err != nil {
		return page.Result[*order.Order]{}, fmt.Errorf("postgres: search orders: %w", err)
	}
	n := len(args)
	rows, err := q.Query(ctx, fmt.Sprintf("SELECT %s %s ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d",
		orderColumns, from, n+1, n+2), append(args, p.Size(), p.Offset())...)
	if err != nil {
		return page.Result[*order.Order]{}, fmt.Errorf("postgres: search orders: %w", err)
	}
	items, err := pgx.CollectRows(rows, scanOrder)
	if err != nil {
		return page.Result[*order.Order]{}, fmt.Errorf("postgres: search orders: %w", err)
	}
	return page.Result[*order.Order]{Items: items, Total: total, TotalCapped: capped}, nil
}

func (r *Repository) UpdateStatus(ctx context.Context, id string, status order.Status) error {
	q := r.handle.Resolve(ctx)
	tag, err := q.Exec(ctx, `
		UPDATE orders SET status = $1, updated_at = now()
		WHERE id = $2 AND status = $3
	`, status, id, order.StatusPending)
	if err != nil {
		return fmt.Errorf("postgres: update order status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE id = $1)`, id).Scan(&exists); err != nil {
			return fmt.Errorf("postgres: check order existence: %w", err)
		}
		if !exists {
			return order.ErrNotFound
		}
		return order.ErrInvalidStatusTransition
	}
	return nil
}

// MarkEventProcessed 用 INSERT ... ON CONFLICT DO NOTHING 去重：RowsAffected() > 0
// 说明是第一次处理。
func (r *Repository) MarkEventProcessed(ctx context.Context, orderID string) (bool, error) {
	q := r.handle.Resolve(ctx)
	tag, err := q.Exec(ctx, `
		INSERT INTO order_processed_events (order_id, created_at)
		VALUES ($1, now())
		ON CONFLICT (order_id) DO NOTHING
	`, orderID)
	if err != nil {
		return false, fmt.Errorf("postgres: mark event processed: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

var _ order.Repository = (*Repository)(nil)
