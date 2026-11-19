package adminapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"app.local/internal/config"
	"app.local/internal/modules/product/adminapi"
	product "app.local/internal/modules/product/domain"
	"app.local/internal/modules/product/domain/domaintest"
	"app.local/internal/platform/httpserver"
)

// newServer 用真实的后台 server（中间件链、校验器、错误处理）挂上 product 的后台操作。
// 登录和权限检查不在这里测（见 internal/app 的集成测试），用一个放行的中间件代替。
func newServer(t *testing.T) (*echo.Echo, *product.Service) {
	t.Helper()
	svc := product.NewService(domaintest.NewRepository())
	pass := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	e, err := httpserver.NewAdmin(httpserver.AdminDeps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		HTTP:   config.HTTPConfig{BodyLimitBytes: 1 << 20, RequestTimeout: 5 * time.Second, GlobalRateLimitPerSecond: 1000},
		Origin: "https://admin.example.com",
		Routes: []func(*echo.Group){func(g *echo.Group) { adminapi.Register(g, adminapi.NewHandler(svc)) }},
		Auth:   []echo.MiddlewareFunc{pass},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e, svc
}

func do(t *testing.T, e *echo.Echo, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

func TestCreateProduct(t *testing.T) {
	e, _ := newServer(t)
	code, body := do(t, e, http.MethodPost, "/admin/v1/products", `{"name":"机械键盘","price_cents":19900,"stock":10}`)
	if code != http.StatusCreated || body["name"] != "机械键盘" || body["stock"] != float64(10) {
		t.Fatalf("创建 = %d %v", code, body)
	}
	for name, req := range map[string]string{
		"缺 name":          `{"price_cents":100,"stock":1}`,
		"name 超过 200":     `{"name":"` + strings.Repeat("a", 201) + `","price_cents":100,"stock":1}`,
		"price_cents 为 0": `{"name":"p","price_cents":0,"stock":1}`,
		"stock 为负":        `{"name":"p","price_cents":100,"stock":-1}`,
		"stock 超过上限":      `{"name":"p","price_cents":100,"stock":1000000001}`,
	} {
		if code, body := do(t, e, http.MethodPost, "/admin/v1/products", req); code != http.StatusBadRequest {
			t.Errorf("%s = %d %v, want 400", name, code, body)
		}
	}
}

func TestUpdateAndAdjustStock(t *testing.T) {
	e, svc := newServer(t)
	p, err := svc.Create(t.Context(), "旧名字", 100, 5)
	if err != nil {
		t.Fatal(err)
	}
	if code, body := do(t, e, http.MethodPut, "/admin/v1/products/"+p.ID, `{"name":"新名字","price_cents":250,"version":1}`); code != http.StatusOK ||
		body["name"] != "新名字" || body["price_cents"] != float64(250) || body["stock"] != float64(5) || body["version"] != float64(2) {
		t.Errorf("修改 = %d %v", code, body)
	}
	if code, body := do(t, e, http.MethodPut, "/admin/v1/products/"+p.ID, `{"name":"又改","price_cents":300,"version":1}`); code != http.StatusConflict || body["code"] != "product_conflict" {
		t.Errorf("用旧 version 修改 = %d %v, want 409 product_conflict", code, body)
	}
	if code, body := do(t, e, http.MethodGet, "/admin/v1/products/"+p.ID, ""); code != http.StatusOK || body["name"] != "新名字" || body["version"] != float64(2) {
		t.Errorf("详情 = %d %v, want 最新的名称和 version 2", code, body)
	}
	if code, body := do(t, e, http.MethodPut, "/admin/v1/products/"+p.ID, `{"name":"又改","price_cents":300}`); code != http.StatusBadRequest {
		t.Errorf("缺 version = %d %v, want 400", code, body)
	}
	const missing = "0199f1a2-3b4c-7d5e-8f60-718293a4b5c6"
	if code, body := do(t, e, http.MethodPut, "/admin/v1/products/"+missing, `{"name":"x","price_cents":1,"version":1}`); code != http.StatusNotFound || body["code"] != "product_not_found" {
		t.Errorf("修改不存在的商品 = %d %v, want 404", code, body)
	}

	adjust := func(delta string) (int, map[string]any) {
		return do(t, e, http.MethodPost, "/admin/v1/products/"+p.ID+"/stock-adjustments", `{"delta":`+delta+`}`)
	}
	if code, body := adjust("-3"); code != http.StatusOK || body["stock"] != float64(2) {
		t.Errorf("-3 = %d %v, want 200 / stock 2", code, body)
	}
	if code, body := adjust("-3"); code != http.StatusConflict || body["code"] != "insufficient_stock" {
		t.Errorf("减到负数 = %d %v, want 409 insufficient_stock", code, body)
	}
	if code, body := adjust("0"); code != http.StatusBadRequest {
		t.Errorf("delta 为 0 = %d %v, want 400", code, body)
	}
	if code, body := adjust("1000000001"); code != http.StatusBadRequest {
		t.Errorf("delta 超出范围 = %d %v, want 400", code, body)
	}
}

func TestListProducts(t *testing.T) {
	e, svc := newServer(t)
	for _, n := range []string{"键盘 A", "鼠标", "键盘 B"} {
		if _, err := svc.Create(t.Context(), n, 100, 1); err != nil {
			t.Fatal(err)
		}
	}
	code, body := do(t, e, http.MethodGet, "/admin/v1/products?q=键盘&page=1&size=1", "")
	items, _ := body["items"].([]any)
	if code != http.StatusOK || body["total"] != float64(2) || body["total_capped"] != false || len(items) != 1 ||
		items[0].(map[string]any)["name"] != "键盘 B" {
		t.Errorf("搜索第一页 = %d %v", code, body)
	}
	if code, body := do(t, e, http.MethodGet, "/admin/v1/products?page=101&size=100", ""); code != http.StatusBadRequest || body["code"] != "page_out_of_range" {
		t.Errorf("超出前 10000 条 = %d %v, want 400 page_out_of_range", code, body)
	}
}
