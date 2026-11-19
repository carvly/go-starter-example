// Package adminapi 是 order 的后台 HTTP 适配器（只读）：Bind → 调 Service → 序列化，
// 不含业务规则。
package adminapi

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"app.local/internal/modules/order/adminapi/oapi"
	order "app.local/internal/modules/order/domain"
	"app.local/internal/platform/httpserver"
)

type Handler struct {
	service *order.Service
}

func NewHandler(service *order.Service) *Handler {
	return &Handler{service: service}
}

func toOrder(o *order.Order) oapi.Order {
	return oapi.Order{
		Id: o.ID, CustomerName: o.CustomerName, ProductId: o.ProductID, ProductName: o.ProductName,
		Quantity: o.Quantity, UnitPriceCents: o.UnitPriceCents, TotalCents: o.TotalCents,
		Status: oapi.OrderStatus(o.Status), CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
	}
}

func (h *Handler) ListOrders(c *echo.Context, params oapi.ListOrdersParams) error {
	req, err := httpserver.PageRequest(params.Page, params.Size)
	if err != nil {
		return err
	}
	var f order.SearchFilter
	if params.Status != nil {
		for _, st := range *params.Status {
			f.Statuses = append(f.Statuses, order.Status(st))
		}
	}
	if params.From != nil {
		f.From = *params.From
	}
	if params.To != nil {
		f.To = *params.To
	}
	r, err := h.service.Search(c.Request().Context(), f, req)
	if err != nil {
		return err
	}
	resp := oapi.OrderPage{Items: make([]oapi.Order, 0, len(r.Items)), Total: r.Total, TotalCapped: r.TotalCapped}
	for _, o := range r.Items {
		resp.Items = append(resp.Items, toOrder(o))
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetOrder(c *echo.Context, id oapi.ID) error {
	o, err := h.service.Get(c.Request().Context(), id.String())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toOrder(o))
}

var _ oapi.ServerInterface = (*Handler)(nil)
