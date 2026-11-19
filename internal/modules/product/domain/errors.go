package domain

import "app.local/internal/kit/errs"

var (
	ErrNotFound     = errs.New(errs.KindNotFound, "product_not_found", "product not found")
	ErrInvalidName  = errs.New(errs.KindInvalid, "invalid_name", "product name must not be empty")
	ErrInvalidPrice = errs.New(errs.KindInvalid, "invalid_price", "price must be positive")
	ErrInvalidStock = errs.New(errs.KindInvalid, "invalid_stock", "stock must be between 0 and 1000000000")
	// ErrInvalidStockDelta 是调整库存时的增减量为 0 或超出范围。
	ErrInvalidStockDelta = errs.New(errs.KindInvalid, "invalid_stock_delta", "stock delta must be non-zero and within ±1000000000")
	// ErrStockTooLarge 表示调整后库存会超过 MaxStock。
	ErrStockTooLarge = errs.New(errs.KindConflict, "stock_too_large", "stock would exceed 1000000000")
	// ErrInsufficientStock 来自一次原子条件 UPDATE（RowsAffected == 0），
	// 不是先 SELECT 再判断，所以并发扣减不会超卖。
	ErrInsufficientStock = errs.New(errs.KindConflict, "insufficient_stock", "insufficient stock")
	// ErrConflict 是编辑时带的 version 已经不是最新：打开表单之后别人改过这个商品。
	ErrConflict = errs.New(errs.KindConflict, "product_conflict", "product was modified by someone else")
	// ErrInvalidCursor 是客户端输入错误（比如手改了 cursor），必须是 4xx。
	ErrInvalidCursor = errs.New(errs.KindInvalid, "invalid_cursor", "invalid pagination cursor")
)
