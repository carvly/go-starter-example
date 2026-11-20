package oauth_test

import (
	"testing"

	"app.local/internal/platform/oauth"
)

// RFC 7636 附录 B 的测试向量。
func TestS256Challenge_RFC7636Vector(t *testing.T) {
	got := oauth.S256Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"; got != want {
		t.Fatalf("S256Challenge = %q, want %q", got, want)
	}
}
