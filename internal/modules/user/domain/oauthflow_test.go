package domain_test

import (
	"testing"

	"app.local/internal/modules/user/domain"
)

func TestValidReturnTo(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want bool
	}{
		{"/orders", true},
		{"/orders?id=1#x", true},
		{"/", true},
		{"", false},
		{"orders", false},
		{"//evil.com", false},
		{`/\evil.com`, false},
		{"/\t/evil.com", false}, // 浏览器去掉制表符后是 //evil.com
		{"/\n/evil.com", false},
		{"/a\\b", false},
		{"https://evil.com", false},
		{"/x://y", true}, // 站内路径里的 :// 不是 scheme
	} {
		if got := domain.ValidReturnTo(tt.in); got != tt.want {
			t.Errorf("ValidReturnTo(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
