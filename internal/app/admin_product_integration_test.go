//go:build integration

package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"app.local/internal/platform/cli"
	"app.local/internal/platform/dbtx"
	"app.local/internal/platform/httpserver"
	"app.local/internal/platform/postgres/pgtest"
)

// 后台管理商品的验收流程，跑在真实 Postgres 和真实的组合根上：超级管理员建商品、改价、调库存；
// 只有 product:read、order:read 的员工能看商品和订单列表，不能新建、改价、调库存；
// 调库存记进操作日志，目标是商品 ID。
func TestProductOrderAdmin_AcceptanceFlow(t *testing.T) {
	in := newZeroInfra()
	pool := pgtest.Pool(t, migrationSources()...)
	in.Pool, in.DBTx = pool, dbtx.NewHandle(pool)
	in.Config.HTTP.AdminOrigin = "https://admin.example.com"
	configs := testModuleConfigs(t)
	configs.Staff.SessionIdleTimeout = 30 * time.Minute
	configs.Staff.SessionMaxAge = 12 * time.Hour
	configs.Staff.TempPasswordTTL = 24 * time.Hour
	mods := buildModules(in, configs)
	e, err := httpserver.NewAdmin(adminServerDeps(in, mods))
	if err != nil {
		t.Fatal(err)
	}

	call := func(method, path, cookie, body string) (int, map[string]any, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "__Host-admin_session", Value: cookie})
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		session := ""
		for _, c := range rec.Result().Cookies() {
			if c.Name == "__Host-admin_session" {
				session = c.Value
			}
		}
		return rec.Code, m, session
	}
	// activate 登录并把临时密码改掉，返回会话 cookie。
	activate := func(user, temp string) string {
		t.Helper()
		code, body, session := call(http.MethodPost, "/admin/v1/auth/login", "", `{"username":"`+user+`","password":"`+temp+`"}`)
		if code != http.StatusOK {
			t.Fatalf("login %s = %d %v", user, code, body)
		}
		if code, body, _ := call(http.MethodPut, "/admin/v1/me/password", session, `{"current_password":"`+temp+`","new_password":"a-long-new-password"}`); code != http.StatusNoContent {
			t.Fatalf("change password %s = %d %v", user, code, body)
		}
		return session
	}
	must := func(want, code int, body map[string]any, what string) map[string]any {
		t.Helper()
		if code != want {
			t.Fatalf("%s = %d %v, want %d", what, code, body, want)
		}
		return body
	}

	var out, errOut bytes.Buffer
	if code := cli.Run(t.Context(), "api", commands(mods), []string{"staff", "create", "-username", "root"}, &out, &errOut); code != cli.ExitOK {
		t.Fatalf("staff create = %d: %s", code, errOut.String())
	}
	s := out.String()
	root := activate("root", strings.TrimSpace(s[strings.LastIndex(s, "：")+len("："):]))

	code, body, _ := call(http.MethodPost, "/admin/v1/products", root, `{"name":"机械键盘","price_cents":19900,"stock":10}`)
	productID := must(http.StatusCreated, code, body, "创建商品")["id"].(string)
	code, body, _ = call(http.MethodPut, "/admin/v1/products/"+productID, root, `{"name":"机械键盘 Pro","price_cents":29900,"version":1}`)
	must(http.StatusOK, code, body, "改价")
	code, body, _ = call(http.MethodPost, "/admin/v1/products/"+productID+"/stock-adjustments", root, `{"delta":-4}`)
	if must(http.StatusOK, code, body, "调库存")["stock"] != float64(6) {
		t.Errorf("调库存后 stock = %v, want 6", body["stock"])
	}

	code, body, _ = call(http.MethodPost, "/admin/v1/roles", root, `{"name":"运营","description":"只看商品和订单"}`)
	roleID := must(http.StatusCreated, code, body, "创建角色")["id"].(string)
	code, body, _ = call(http.MethodPut, "/admin/v1/roles/"+roleID+"/permissions", root, `{"permissions":["product:read","order:read"]}`)
	must(http.StatusOK, code, body, "设置角色权限")
	code, body, _ = call(http.MethodPost, "/admin/v1/accounts", root, `{"username":"ops"}`)
	created := must(http.StatusCreated, code, body, "创建员工")
	opsID := created["account"].(map[string]any)["id"].(string)
	code, body, _ = call(http.MethodPut, "/admin/v1/accounts/"+opsID+"/roles", root, `{"role_ids":["`+roleID+`"]}`)
	must(http.StatusNoContent, code, body, "分配角色")
	ops := activate("ops", created["temporary_password"].(string))

	code, body, _ = call(http.MethodGet, "/admin/v1/products?q=Pro", ops, "")
	if must(http.StatusOK, code, body, "运营看商品列表")["total"] != float64(1) {
		t.Errorf("商品列表 = %v", body)
	}
	code, body, _ = call(http.MethodGet, "/admin/v1/orders?status=pending", ops, "")
	must(http.StatusOK, code, body, "运营看订单列表")
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/admin/v1/products", `{"name":"p","price_cents":1,"stock":1}`},
		{http.MethodPut, "/admin/v1/products/" + productID, `{"name":"p","price_cents":1,"version":2}`},
		{http.MethodPost, "/admin/v1/products/" + productID + "/stock-adjustments", `{"delta":1}`},
	} {
		if code, body, _ := call(c.method, c.path, ops, c.body); code != http.StatusForbidden || body["code"] != "permission_denied" {
			t.Errorf("运营 %s %s = %d %v, want 403", c.method, c.path, code, body)
		}
	}

	code, body, _ = call(http.MethodGet, "/admin/v1/audit-logs?limit=100", root, "")
	found := false
	for _, it := range must(http.StatusOK, code, body, "操作日志")["items"].([]any) {
		m := it.(map[string]any)
		if m["action"] == "POST /admin/v1/products/:id/stock-adjustments" && m["target"] == productID && m["result"] == "200" {
			found = true
		}
	}
	if !found {
		t.Errorf("操作日志里没有调库存记录：%v", body["items"])
	}
}
