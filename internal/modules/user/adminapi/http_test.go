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
	"app.local/internal/kit/uow/uowtest"
	"app.local/internal/modules/user/adminapi"
	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/domain/domaintest"
	"app.local/internal/platform/httpserver"
)

// newServer 用真实的后台 server（中间件链、校验器、错误处理）挂上本模块的操作。
// 登录和权限检查不在这里测，用一个放行的中间件代替。
func newServer(t *testing.T) (*echo.Echo, *domain.Service, *domaintest.Fakes) {
	t.Helper()
	fakes := domaintest.NewFakes()
	deps := fakes.Deps()
	deps.UoW = uowtest.Immediate{}
	deps.Issuer = domaintest.Issuer{}
	deps.Config = domaintest.TestConfig()
	svc := domain.NewService(deps)
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
	return e, svc, fakes
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

const prefix = "/admin/v1/users"

func TestListUsers(t *testing.T) {
	e, _, fakes := newServer(t)
	for range 3 {
		if err := fakes.Users.Create(t.Context(), domaintest.ValidUser()); err != nil {
			t.Fatal(err)
		}
	}
	code, body := do(t, e, http.MethodGet, prefix+"?page=1&size=2", "")
	if items, _ := body["items"].([]any); code != http.StatusOK || body["total"] != float64(3) || len(items) != 2 {
		t.Errorf("列表 = %d %v", code, body)
	}
	if code, body := do(t, e, http.MethodGet, prefix+"?sort=nope", ""); code != http.StatusBadRequest {
		t.Errorf("不允许的排序 = %d %v, want 400", code, body)
	}
}

func TestGetUser_NotFound(t *testing.T) {
	e, _, _ := newServer(t)
	if code, body := do(t, e, http.MethodGet, prefix+"/0199f1a2-3b4c-7d5e-8f60-718293a4b5c6", ""); code != http.StatusNotFound || body["code"] != "user_not_found" {
		t.Errorf("不存在 = %d %v, want 404", code, body)
	}
}

func TestEnableUser(t *testing.T) {
	e, _, fakes := newServer(t)
	v := domaintest.ValidUser()
	v.Status = domain.UserStatusDisabled
	if err := fakes.Users.Create(t.Context(), v); err != nil {
		t.Fatal(err)
	}
	path := prefix + "/" + v.ID + "/enable"
	if code, body := do(t, e, http.MethodPost, path, ``); code != http.StatusOK {
		t.Fatalf("启用 = %d %v", code, body)
	}
	if code, body := do(t, e, http.MethodPost, path, ``); code != http.StatusOK && code != http.StatusConflict {
		t.Errorf("再次启用 = %d %v", code, body)
	}
}

func TestDisableUser(t *testing.T) {
	e, _, fakes := newServer(t)
	v := domaintest.ValidUser()
	v.Status = domain.UserStatusActive
	if err := fakes.Users.Create(t.Context(), v); err != nil {
		t.Fatal(err)
	}
	path := prefix + "/" + v.ID + "/disable"
	if code, body := do(t, e, http.MethodPost, path, ``); code != http.StatusOK || body["status"] != "disabled" {
		t.Fatalf("停用 = %d %v", code, body)
	}
	if code, body := do(t, e, http.MethodPost, path, ``); code != http.StatusConflict || body["code"] != "user_invalid_state" {
		t.Errorf("再次停用 = %d %v, want 409 user_invalid_state", code, body)
	}
}
