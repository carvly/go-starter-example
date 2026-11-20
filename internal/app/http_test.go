package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"app.local/internal/kit/uow/uowtest"
	"app.local/internal/modules/user/domain"
	"app.local/internal/modules/user/domain/domaintest"
	"app.local/internal/modules/user/httpapi"
	"app.local/internal/platform/httpserver"
	"app.local/internal/platform/oauth"
)

func newPipelineTestAPI(t *testing.T) *echo.Echo {
	t.Helper()
	cfg := baseTestConfig()
	fakes := domaintest.NewFakes()
	deps := fakes.Deps()
	deps.UoW = uowtest.Immediate{}
	deps.Issuer = domaintest.Issuer{}
	deps.Config = domaintest.TestConfig()
	svc := domain.NewService(deps)
	pass := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	identified := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			httpserver.SetUserSession(c, httpserver.UserSession{UserID: "0199f1a2-3b4c-7d5e-8f60-718293a4b5c6"})
			return next(c)
		}
	}
	e, err := httpserver.New(httpserver.Deps{
		Logger: discardLogger(),
		HTTP:   cfg.HTTP,
		CORS:   cfg.CORS,
		Routes: []func(*echo.Group){func(g *echo.Group) {
			httpapi.Register(g, httpapi.NewHandler(httpapi.Deps{
				Service: svc, OAuth: oauth.Registry{}, PublicBaseURL: "https://api.example.com",
			}), httpapi.Middlewares{Auth: identified, RequireRecentAuth: pass, RateLimit: pass})
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func doPipelineJSON(t *testing.T, e *echo.Echo, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set(echo.HeaderContentType, "application/json")
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

func TestAPI_BusinessErrorHasNoCallChain(t *testing.T) {
	e := newPipelineTestAPI(t)
	status, resp := doPipelineJSON(t, e, http.MethodPatch, "/v1/me", map[string]any{})
	if status != http.StatusBadRequest || resp["code"] != "invalid_request" || resp["message"] != "没有要修改的字段" {
		t.Errorf("status = %d, body = %v", status, resp)
	}
}

// ContentLength = -1 模拟 chunked 请求，强制 BodyLimit 走边读边计数的路径。
func TestAPI_BodyTooLargeReturns413(t *testing.T) {
	e := newPipelineTestAPI(t)
	payload := []byte(`{"code":"` + string(bytes.Repeat([]byte("a"), 3*1024*1024)) + `","client":"miniprogram"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/native/wechat-miniprogram/login", bytes.NewReader(payload))
	req.Header.Set(echo.HeaderContentType, "application/json")
	req.ContentLength = -1
	req.Body = io.NopCloser(bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAPI_RequestValidation_RejectsInvalidInput(t *testing.T) {
	e := newPipelineTestAPI(t)
	for _, body := range []map[string]any{
		{"client": "miniprogram"},
		{"code": "x", "client": "invalid"},
	} {
		if status, resp := doPipelineJSON(t, e, http.MethodPost, "/v1/auth/native/wechat-miniprogram/login", body); status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400, body = %v", status, resp)
		}
	}
}

func TestAPI_PathIDMustBeUUID(t *testing.T) {
	e := newPipelineTestAPI(t)
	if status, resp := doPipelineJSON(t, e, http.MethodDelete, "/v1/me/sessions/not-a-uuid", nil); status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body = %v", status, resp)
	}
}
