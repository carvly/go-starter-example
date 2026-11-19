// Package httpapi 是 product 的前台 HTTP 适配器（只读）：调 Service → 序列化，不含
// 业务规则。创建、改价、调库存在后台接入面（adminapi）。
package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v5"

	product "app.local/internal/modules/product/domain"
	"app.local/internal/modules/product/httpapi/oapi"
)

type Handler struct {
	service *product.Service
}

func NewHandler(service *product.Service) *Handler {
	return &Handler{service: service}
}

func toProductResponse(p *product.Product) oapi.Product {
	return oapi.Product{
		Id:         p.ID,
		Name:       p.Name,
		PriceCents: p.PriceCents,
		Stock:      p.Stock,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
	}
}

func (h *Handler) GetProduct(c *echo.Context, id oapi.ID) error {
	p, err := h.service.Get(c.Request().Context(), id.String())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toProductResponse(p))
}

func (h *Handler) ListProducts(c *echo.Context, params oapi.ListProductsParams) error {
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
	resp := oapi.ProductList{Items: make([]oapi.Product, 0, len(items))}
	for _, p := range items {
		resp.Items = append(resp.Items, toProductResponse(p))
	}
	if next != "" {
		resp.NextCursor = &next
	}
	return c.JSON(http.StatusOK, resp)
}

var _ oapi.ServerInterface = (*Handler)(nil)
