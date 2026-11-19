package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"app.local/internal/kit/uow/uowtest"
	orderdomain "app.local/internal/modules/order/domain"
	ordertest "app.local/internal/modules/order/domain/domaintest"
	orderhttp "app.local/internal/modules/order/httpapi"
	productdomain "app.local/internal/modules/product/domain"
	producttest "app.local/internal/modules/product/domain/domaintest"
	producthttp "app.local/internal/modules/product/httpapi"
	"app.local/internal/platform/httpserver"
)

// newTestAPI 按 buildModules 的同样方式把前台模块的 HTTP 层挂起来，只把 Repository 换成内存
// fake；中间件链、跨模块适配器都是真实实现。前台没有创建商品的接口（在后台），测试经返回的
// product Service 准备商品。
func newTestAPI(t *testing.T) (*echo.Echo, *productdomain.Service) {
	t.Helper()
	cfg := baseTestConfig()

	productSvc := productdomain.NewService(producttest.NewRepository())
	orderSvc := orderdomain.NewService(ordertest.NewRepository(), productLookup{productSvc}, &ordertest.Publisher{}, uowtest.Immediate{})

	e, err := httpserver.New(httpserver.Deps{
		Logger: discardLogger(),
		HTTP:   cfg.HTTP,
		CORS:   cfg.CORS,
		Routes: []func(*echo.Group){
			func(g *echo.Group) {
				producthttp.Register(g, producthttp.NewHandler(productSvc))
			},
			func(g *echo.Group) { orderhttp.Register(g, orderhttp.NewHandler(orderSvc)) },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e, productSvc
}

func doJSON(t *testing.T, e *echo.Echo, method, path string, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set(echo.HeaderContentType, "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var resp map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
		}
	}
	return rec.Code, resp
}

// 商品（由后台创建）→ 前台查商品 → 下单 → 查订单，经过 product/order 两个模块和组合根适配器。
func TestExampleAPI_FullFlow(t *testing.T) {
	e, products := newTestAPI(t)
	p, err := products.Create(t.Context(), "机械键盘", 19900, 10)
	if err != nil {
		t.Fatal(err)
	}
	productID := p.ID
	if status, resp := doJSON(t, e, http.MethodGet, "/v1/products/"+productID, nil, nil); status != http.StatusOK || resp["name"] != "机械键盘" {
		t.Fatalf("查商品 status = %d, body = %v", status, resp)
	}

	status, orderResp := doJSON(t, e, http.MethodPost, "/v1/orders", map[string]any{
		"customer_name": "张三", "product_id": productID, "quantity": 2,
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("下单 status = %d, body = %v", status, orderResp)
	}
	if total, _ := orderResp["total_cents"].(float64); total != 39800 {
		t.Errorf("total_cents = %v, want 39800", orderResp["total_cents"])
	}

	orderID, _ := orderResp["id"].(string)
	status, getResp := doJSON(t, e, http.MethodGet, "/v1/orders/"+orderID, nil, nil)
	if status != http.StatusOK || getResp["status"] != "pending" {
		t.Errorf("查订单 status = %d, 订单状态 = %v, want 200 / pending", status, getResp["status"])
	}
}

// 业务错误经过组合根适配器翻译、多层 %w 包装后，响应里仍只有 Code/Message。
func TestExampleAPI_BusinessErrorHasNoCallChain(t *testing.T) {
	e, _ := newTestAPI(t)
	const missing = "0199f1a2-3b4c-7d5e-8f60-718293a4b5c6"

	status, resp := doJSON(t, e, http.MethodPost, "/v1/orders", map[string]any{
		"customer_name": "张三", "product_id": missing, "quantity": 1,
	}, nil)
	if status != http.StatusNotFound || resp["code"] != "product_not_found" || resp["message"] != "referenced product not found" {
		t.Errorf("status = %d, body = %v", status, resp)
	}

	status, resp = doJSON(t, e, http.MethodGet, "/v1/products/"+missing, nil, nil)
	if status != http.StatusNotFound || resp["code"] != "product_not_found" || resp["message"] != "product not found" {
		t.Errorf("status = %d, body = %v", status, resp)
	}
}

// ContentLength = -1 模拟 chunked 请求，强制 BodyLimit 走边读边计数的路径；
// 带准确 Content-Length 时会被提前拒绝，掩盖问题。
func TestExampleAPI_BodyTooLargeReturns413(t *testing.T) {
	e, _ := newTestAPI(t)

	payload := []byte(`{"customer_name":"` + string(bytes.Repeat([]byte("a"), 3*1024*1024)) + `","product_id":"p","quantity":1}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/orders", bytes.NewReader(payload))
	req.Header.Set(echo.HeaderContentType, "application/json")
	req.ContentLength = -1
	req.Body = io.NopCloser(bytes.NewReader(payload))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413, body = %s", rec.Code, rec.Body.String())
	}
}

// 校验标签来自 api/openapi.yaml 的 x-oapi-codegen-extra-tags，按模块分别生成后必须仍然生效。
func TestExampleAPI_RequestValidation_RejectsInvalidInput(t *testing.T) {
	e, _ := newTestAPI(t)

	cases := []struct {
		name    string
		path    string
		body    map[string]any
		headers map[string]string
	}{
		{"下单 quantity 是字符串", "/v1/orders", map[string]any{"customer_name": "张三", "product_id": "p", "quantity": "x"}, nil},
		{"下单 product_id 不是 UUID", "/v1/orders", map[string]any{"customer_name": "张三", "product_id": "p", "quantity": 1}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if status, body := doJSON(t, e, http.MethodPost, tc.path, tc.body, tc.headers); status != http.StatusBadRequest {
				t.Errorf("status = %d, want 400, body = %v", status, body)
			}
		})
	}
}

// 路径里的 id 不是合法 UUID 时在生成的包装函数里就返回 400，不会带到数据库变成 22P02 / 500。
func TestExampleAPI_PathIDMustBeUUID(t *testing.T) {
	e, _ := newTestAPI(t)
	for _, path := range []string{"/v1/products/not-a-uuid", "/v1/orders/not-a-uuid"} {
		if status, body := doJSON(t, e, http.MethodGet, path, nil, nil); status != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want 400, body = %v", path, status, body)
		}
	}
}

// 请求体里的 UUID 大小写不敏感，存储和响应统一成小写标准形式，与从 uuid 列读回的一致。
func TestExampleAPI_BodyUUIDIsNormalized(t *testing.T) {
	e, products := newTestAPI(t)
	p, err := products.Create(t.Context(), "p", 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	productID := p.ID

	status, orderResp := doJSON(t, e, http.MethodPost, "/v1/orders", map[string]any{
		"customer_name": "张三", "product_id": strings.ToUpper(productID), "quantity": 1,
	}, nil)
	if status != http.StatusCreated || orderResp["product_id"] != productID {
		t.Errorf("status = %d, product_id = %v, want 201 / %s", status, orderResp["product_id"], productID)
	}
}
