package app

import (
	"net/http"
	"testing"
)

// 创建商品只在后台（api/admin.yaml，要权限码 product:create），前台不能再有写商品的接口。
func TestWiring_WebHasNoCreateProduct(t *testing.T) {
	e := newRealAPI(t)
	if code := post(e, "/v1/products", `{"name":"p","price_cents":1,"stock":1}`); code != http.StatusMethodNotAllowed {
		t.Errorf("前台 POST /v1/products status = %d, want 405", code)
	}
}
