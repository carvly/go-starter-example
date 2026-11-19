package domain

import "app.local/internal/kit/errs"

var (
	ErrNotFound            = errs.New(errs.KindNotFound, "order_not_found", "order not found")
	ErrInvalidCustomerName = errs.New(errs.KindInvalid, "invalid_customer_name", "customer name must not be empty")
	ErrInvalidQuantity     = errs.New(errs.KindInvalid, "invalid_quantity", "quantity is out of range")
	// ErrProductNotFound 由 internal/app 的适配器把 product 的 ErrNotFound 翻译而来，
	// order 不认识 product 的任何类型。
	ErrProductNotFound = errs.New(errs.KindNotFound, "product_not_found", "referenced product not found")
	// ErrInvalidStatusTransition 表示状态机不允许的转换，比如已 confirmed 的订单再标记 failed。
	ErrInvalidStatusTransition = errs.New(errs.KindConflict, "invalid_status_transition", "invalid order status transition")
	ErrInvalidCursor           = errs.New(errs.KindInvalid, "invalid_cursor", "invalid pagination cursor")
	ErrInvalidStatus           = errs.New(errs.KindInvalid, "invalid_status", "unknown order status")
	ErrInvalidTimeRange        = errs.New(errs.KindInvalid, "invalid_time_range", "from must be earlier than to")
)
