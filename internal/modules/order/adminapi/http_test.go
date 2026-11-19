package adminapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"app.local/internal/config"
	"app.local/internal/kit/uow/uowtest"
	"app.local/internal/modules/order/adminapi"
	order "app.local/internal/modules/order/domain"
	"app.local/internal/modules/order/domain/domaintest"
	"app.local/internal/platform/httpserver"
)

// newServer 用真实的后台 server 挂上 order 的后台操作；登录和权限检查用放行的中间件代替。
func newServer(t *testing.T) (*echo.Echo, *domaintest.Repository) {
	t.Helper()
	repo := domaintest.NewRepository()
	svc := order.NewService(repo, nil, &domaintest.Publisher{}, uowtest.Immediate{})
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
	return e, repo
}

func get(t *testing.T, e *echo.Echo, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

func TestListAndGetOrders(t *testing.T) {
	e, repo := newServer(t)
	var ids []string
	for range 3 {
		o := &order.Order{ID: uuid.NewV7().String(), CustomerName: "张三", ProductID: uuid.NewV7().String(), ProductName: "p",
			Quantity: 1, UnitPriceCents: 100, TotalCents: 100, Status: order.StatusPending}
		if err := repo.Create(t.Context(), o); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, o.ID)
	}
	if err := repo.UpdateStatus(t.Context(), ids[0], order.StatusConfirmed); err != nil {
		t.Fatal(err)
	}

	code, body := get(t, e, "/admin/v1/orders?status=confirmed")
	items, _ := body["items"].([]any)
	if code != http.StatusOK || body["total"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["id"] != ids[0] {
		t.Errorf("按状态筛选 = %d %v", code, body)
	}
	if code, body := get(t, e, "/admin/v1/orders?status=confirmed,pending"); code != http.StatusOK || body["total"] != float64(3) {
		t.Errorf("多个状态 = %d %v, want 3 条", code, body)
	}
	if code, body := get(t, e, "/admin/v1/orders?page=1&size=2"); code != http.StatusOK || body["total"] != float64(3) || len(body["items"].([]any)) != 2 {
		t.Errorf("分页 = %d %v", code, body)
	}
	if code, body := get(t, e, "/admin/v1/orders/"+ids[1]); code != http.StatusOK || body["status"] != "pending" || body["total_cents"] != float64(100) {
		t.Errorf("详情 = %d %v", code, body)
	}
	if code, body := get(t, e, "/admin/v1/orders/"+uuid.NewV7().String()); code != http.StatusNotFound || body["code"] != "order_not_found" {
		t.Errorf("不存在的订单 = %d %v, want 404", code, body)
	}
}

func TestListOrders_RejectsBadFilter(t *testing.T) {
	e, _ := newServer(t)
	now := time.Now().UTC().Format(time.RFC3339)
	for name, query := range map[string]string{
		"未知状态":       "status=shipped",
		"其中一个状态未知":   "status=pending,shipped",
		"from 等于 to": "from=" + url.QueryEscape(now) + "&to=" + url.QueryEscape(now),
		"时间格式不对":     "from=yesterday",
	} {
		if code, body := get(t, e, "/admin/v1/orders?"+query); code != http.StatusBadRequest {
			t.Errorf("%s = %d %v, want 400", name, code, body)
		}
	}
}
