// Package httpapi 是 order 的 HTTP 适配器：Bind → Validate → 调 Service → 序列化。
package httpapi

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	order "app.local/internal/modules/order/domain"
	"app.local/internal/modules/order/httpapi/oapi"
)

type Handler struct {
	service *order.Service
}

func NewHandler(service *order.Service) *Handler {
	return &Handler{service: service}
}

func toOrderResponse(o *order.Order) oapi.Order {
	return oapi.Order{
		Id:             o.ID,
		CustomerName:   o.CustomerName,
		ProductId:      o.ProductID,
		ProductName:    o.ProductName,
		Quantity:       o.Quantity,
		UnitPriceCents: o.UnitPriceCents,
		TotalCents:     o.TotalCents,
		Status:         oapi.OrderStatus(o.Status),
		CreatedAt:      o.CreatedAt,
		UpdatedAt:      o.UpdatedAt,
	}
}

// CreateOrder 返回时订单是 pending：扣库存和确认是异步的，客户端稍后再查最终状态。
func (h *Handler) CreateOrder(c *echo.Context) error {
	var req oapi.CreateOrderJSONRequestBody
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := c.Validate(&req); err != nil {
		return err
	}
	// validate 已保证是带连字符的 UUID，但大小写不限；转小写后与从 uuid 列读回的形式一致。
	productID := strings.ToLower(req.ProductId)
	o, err := h.service.Create(c.Request().Context(), req.CustomerName, productID, req.Quantity)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, toOrderResponse(o))
}

func (h *Handler) GetOrder(c *echo.Context, id oapi.ID) error {
	o, err := h.service.Get(c.Request().Context(), id.String())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toOrderResponse(o))
}

func (h *Handler) ListOrders(c *echo.Context, params oapi.ListOrdersParams) error {
	var cursor string
	if params.Cursor != nil {
		cursor = *params.Cursor
	}
	var limit int
	if params.Limit != nil {
		limit = *params.Limit
	}

	items, next, err := h.service.List(c.Request().Context(), cursor, limit)
	if err != nil {
		return err
	}
	resp := oapi.OrderList{Items: make([]oapi.Order, 0, len(items))}
	for _, o := range items {
		resp.Items = append(resp.Items, toOrderResponse(o))
	}
	if next != "" {
		resp.NextCursor = &next
	}
	return c.JSON(http.StatusOK, resp)
}

var _ oapi.ServerInterface = (*Handler)(nil)
