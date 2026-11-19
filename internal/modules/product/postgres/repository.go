// Package postgres 是 product 的 Repository 实现（pgx/v5，经 dbtx.Handle 取连接）。
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"app.local/internal/kit/page"
	product "app.local/internal/modules/product/domain"
	"app.local/internal/platform/dbtx"
	platformpg "app.local/internal/platform/postgres"
)

type Repository struct {
	handle *dbtx.Handle
}

func NewRepository(handle *dbtx.Handle) *Repository {
	return &Repository{handle: handle}
}

func scanProduct(row pgx.CollectableRow) (*product.Product, error) {
	var p product.Product
	err := row.Scan(&p.ID, &p.Name, &p.PriceCents, &p.Stock, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	return &p, err
}

func (r *Repository) Create(ctx context.Context, p *product.Product) error {
	q := r.handle.Resolve(ctx)
	err := q.QueryRow(ctx, `
		INSERT INTO products (id, name, price_cents, stock, created_at, updated_at)
		VALUES ($1, $2, $3, $4, now(), now())
		RETURNING version, created_at, updated_at
	`, p.ID, p.Name, p.PriceCents, p.Stock).Scan(&p.Version, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres: create product: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id string) (*product.Product, error) {
	q := r.handle.Resolve(ctx)
	rows, err := q.Query(ctx, `
		SELECT id, name, price_cents, stock, version, created_at, updated_at
		FROM products WHERE id = $1
	`, id)
	if err != nil {
		return nil, fmt.Errorf("postgres: get product: %w", err)
	}
	p, err := pgx.CollectExactlyOneRow(rows, scanProduct)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, product.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get product: %w", err)
	}
	return p, nil
}

// List 多取一行（limit+1）判断是否还有下一页，省掉 COUNT 查询。limit 在这里
// 再兜底一次：绕过 Service 传入 limit<=0 时，items[limit-1] 会 panic。
func (r *Repository) List(ctx context.Context, cursor string, limit int) ([]*product.Product, string, error) {
	if limit <= 0 {
		limit = product.DefaultListLimit
	}

	createdAt, id, hasCursor, err := platformpg.DecodeCursor(cursor)
	if err != nil {
		return nil, "", product.ErrInvalidCursor
	}

	q := r.handle.Resolve(ctx)
	var rows pgx.Rows
	if hasCursor {
		rows, err = q.Query(ctx, `
			SELECT id, name, price_cents, stock, version, created_at, updated_at
			FROM products
			WHERE (created_at, id) < ($1, $2)
			ORDER BY created_at DESC, id DESC
			LIMIT $3
		`, createdAt, id, limit+1)
	} else {
		rows, err = q.Query(ctx, `
			SELECT id, name, price_cents, stock, version, created_at, updated_at
			FROM products
			ORDER BY created_at DESC, id DESC
			LIMIT $1
		`, limit+1)
	}
	if err != nil {
		return nil, "", fmt.Errorf("postgres: list products: %w", err)
	}

	items, err := pgx.CollectRows(rows, scanProduct)
	if err != nil {
		return nil, "", fmt.Errorf("postgres: list products: %w", err)
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		nextCursor = platformpg.EncodeCursor(last.CreatedAt, last.ID)
		items = items[:limit]
	}
	return items, nextCursor, nil
}

const productColumns = `id, name, price_cents, stock, version, created_at, updated_at`

func (r *Repository) Search(ctx context.Context, keyword string, p page.Request) (page.Result[*product.Product], error) {
	q := r.handle.Resolve(ctx)
	// keyword 为空时 ILIKE '%%' 匹配全部行。名称搜索没有索引：后台商品量级下顺序扫描足够，
	// 量大了再加 pg_trgm 的 GIN 索引。
	const from = `FROM products WHERE name ILIKE $1`
	pattern := platformpg.ContainsPattern(keyword)
	total, capped, err := platformpg.CountCapped(ctx, q, `SELECT 1 `+from, pattern)
	if err != nil {
		return page.Result[*product.Product]{}, fmt.Errorf("postgres: search products: %w", err)
	}
	rows, err := q.Query(ctx, `SELECT `+productColumns+` `+from+`
		ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, pattern, p.Size(), p.Offset())
	if err != nil {
		return page.Result[*product.Product]{}, fmt.Errorf("postgres: search products: %w", err)
	}
	items, err := pgx.CollectRows(rows, scanProduct)
	if err != nil {
		return page.Result[*product.Product]{}, fmt.Errorf("postgres: search products: %w", err)
	}
	return page.Result[*product.Product]{Items: items, Total: total, TotalCapped: capped}, nil
}

// Update 把 version 的比较写进同一条 UPDATE：先读再比较的话，两次请求可能都通过比较。
func (r *Repository) Update(ctx context.Context, id, name string, priceCents, version int64) (*product.Product, error) {
	q := r.handle.Resolve(ctx)
	rows, err := q.Query(ctx, `
		UPDATE products SET name = $2, price_cents = $3, version = version + 1, updated_at = now()
		WHERE id = $1 AND version = $4 RETURNING `+productColumns, id, name, priceCents, version)
	if err != nil {
		return nil, fmt.Errorf("postgres: update product: %w", err)
	}
	p, err := pgx.CollectExactlyOneRow(rows, scanProduct)
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("postgres: update product: %w", err)
	}
	// 没有更新：只在失败路径多查一次，区分不存在和已被别人修改。
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)`, id).Scan(&exists); err != nil {
		return nil, fmt.Errorf("postgres: update product: %w", err)
	}
	if !exists {
		return nil, product.ErrNotFound
	}
	return nil, product.ErrConflict
}

// AdjustStock 是单条原子条件 UPDATE：与 DecreaseStock 并发时各自在行锁上排队，
// 都基于最新库存计算，不会互相覆盖。
func (r *Repository) AdjustStock(ctx context.Context, id string, delta int64) (*product.Product, error) {
	q := r.handle.Resolve(ctx)
	rows, err := q.Query(ctx, `
		UPDATE products SET stock = stock + $2, updated_at = now()
		WHERE id = $1 AND stock + $2 BETWEEN 0 AND $3
		RETURNING `+productColumns, id, delta, int64(product.MaxStock))
	if err != nil {
		return nil, fmt.Errorf("postgres: adjust stock: %w", err)
	}
	p, err := pgx.CollectExactlyOneRow(rows, scanProduct)
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("postgres: adjust stock: %w", err)
	}
	// 没有更新：只在失败路径多查一次是否存在。库存总在 [0, MaxStock] 内，所以减少只可能撞下限、
	// 增加只可能撞上限；按 delta 的符号归类，不按重读到的库存：两条语句之间库存可能已被并发修改。
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)`, id).Scan(&exists); err != nil {
		return nil, fmt.Errorf("postgres: adjust stock: %w", err)
	}
	switch {
	case !exists:
		return nil, product.ErrNotFound
	case delta < 0:
		return nil, product.ErrInsufficientStock
	default:
		return nil, product.ErrStockTooLarge
	}
}

// DecreaseStock 是单条原子条件 UPDATE，并发扣同一商品不会超卖。
func (r *Repository) DecreaseStock(ctx context.Context, id string, quantity int64) error {
	q := r.handle.Resolve(ctx)
	tag, err := q.Exec(ctx, `
		UPDATE products SET stock = stock - $1, updated_at = now()
		WHERE id = $2 AND stock >= $1
	`, quantity, id)
	if err != nil {
		return fmt.Errorf("postgres: decrease stock: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// 区分"不存在"和"库存不够"，多查的一次只发生在失败路径。
		var exists bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)`, id).Scan(&exists); err != nil {
			return fmt.Errorf("postgres: check product existence: %w", err)
		}
		if !exists {
			return product.ErrNotFound
		}
		return product.ErrInsufficientStock
	}
	return nil
}

var _ product.Repository = (*Repository)(nil)
