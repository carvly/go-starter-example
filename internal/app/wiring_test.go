package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"app.local/internal/platform/httpserver"
)

// wiring_<模块>_test.go 用真实的 buildModules 验证操作级中间件真的挂上了。http_test.go 为了用 fake
// Repository 手工装配了一遍模块，覆盖不到 modules.go 里的接线错误。下面的请求都在
// 到达 Service 之前就被中间件或参数校验挡下，所以零值 Infra（没有数据库）就够。
func newRealAPI(t *testing.T) *echo.Echo {
	t.Helper()
	in := newZeroInfra()
	e, err := httpserver.New(apiServerDeps(in, buildModules(in, testModuleConfigs(t))))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func post(e *echo.Echo, path, body string) int {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code
}
