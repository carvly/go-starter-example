// Package adminapi 是 product 的后台 HTTP 适配器：Bind → Validate → 调 Service →
// 序列化，不含业务规则。
package adminapi

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"app.local/internal/modules/product/adminapi/oapi"
	product "app.local/internal/modules/product/domain"
	"app.local/internal/platform/httpserver"
)

type Handler struct {
	service *product.Service
}

func NewHandler(service *product.Service) *Handler {
	return &Handler{service: service}
}

func toProduct(p *product.Product) oapi.Product {
	return oapi.Product{
		Id: p.ID, Name: p.Name, PriceCents: p.PriceCents, Stock: p.Stock, Version: p.Version,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

func (h *Handler) ListProducts(c *echo.Context, params oapi.ListProductsParams) error {
	req, err := httpserver.PageRequest(params.Page, params.Size)
	if err != nil {
		return err
	}
	var keyword string
	if params.Q != nil {
		keyword = *params.Q
	}
	r, err := h.service.Search(c.Request().Context(), keyword, req)
	if err != nil {
		return err
	}
	resp := oapi.ProductPage{Items: make([]oapi.Product, 0, len(r.Items)), Total: r.Total, TotalCapped: r.TotalCapped}
	for _, p := range r.Items {
		resp.Items = append(resp.Items, toProduct(p))
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetProduct(c *echo.Context, id oapi.ID) error {
	p, err := h.service.GetFresh(c.Request().Context(), id.String())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toProduct(p))
}

func (h *Handler) CreateProduct(c *echo.Context) error {
	var req oapi.CreateProductJSONRequestBody
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := c.Validate(&req); err != nil {
		return err
	}
	p, err := h.service.Create(c.Request().Context(), req.Name, req.PriceCents, req.Stock)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, toProduct(p))
}

func (h *Handler) UpdateProduct(c *echo.Context, id oapi.ID) error {
	var req oapi.UpdateProductJSONRequestBody
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := c.Validate(&req); err != nil {
		return err
	}
	p, err := h.service.Update(c.Request().Context(), id.String(), req.Name, req.PriceCents, req.Version)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toProduct(p))
}

func (h *Handler) AdjustProductStock(c *echo.Context, id oapi.ID) error {
	var req oapi.AdjustProductStockJSONRequestBody
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := c.Validate(&req); err != nil {
		return err
	}
	p, err := h.service.AdjustStock(c.Request().Context(), id.String(), req.Delta)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toProduct(p))
}

var _ oapi.ServerInterface = (*Handler)(nil)
