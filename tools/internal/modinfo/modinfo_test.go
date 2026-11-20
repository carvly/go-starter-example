package modinfo

import (
	"errors"
	"slices"
	"testing"
)

type mapSource map[string]string

func (m mapSource) Read(name string) (string, error) {
	if v, ok := m[name]; ok {
		return v, nil
	}
	return "", errors.New("not found: " + name)
}

func (m mapSource) Files() ([]string, error) {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out, nil
}

func TestList(t *testing.T) {
	got := List([]string{"internal/modules/b/module.go", "internal/modules/a/module.go", "internal/modules/a/domain/x.go", "internal/app/modules.go"})
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("List = %v", got)
	}
}

func TestAdminAuthProviders(t *testing.T) {
	src := `package app
type Modules struct {
	Crew  *crew.Module
	Shop  *shop.Module
}
func (m *Modules) adminAuth() []echo.MiddlewareFunc {
	return []echo.MiddlewareFunc{
		m.Crew.AdminGuard(),
	}
}
func (m *Modules) adminRegistrars() []func(*echo.Group) {
	return []func(*echo.Group){m.Shop.RegisterAdminHTTP}
}
`
	got, err := AdminAuthProviders(src)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"crew"}) {
		t.Fatalf("AdminAuthProviders = %v，应只认 adminAuth() 里引用的模块，按字段类型映射到包名", got)
	}
}

func TestUserAuthProvider(t *testing.T) {
	withMW := "package member\nfunc (m *Module) Middleware() echo.MiddlewareFunc { return nil }\n"
	without := "package shop\nfunc (m *Module) RegisterHTTP(g *echo.Group) {}\n"
	src := mapSource{
		"internal/modules/member/module.go": withMW,
		"internal/modules/shop/module.go":   without,
	}
	got, err := UserAuthProvider(src, []string{"member", "shop"})
	if err != nil || got != "member" {
		t.Fatalf("UserAuthProvider = %q, %v；提供者换名后仍应从代码找到", got, err)
	}
	got, err = UserAuthProvider(src, []string{"shop"})
	if err != nil || got != "" {
		t.Fatalf("没有提供者时应返回空串，got %q, %v", got, err)
	}
	src["internal/modules/shop/module.go"] = "package shop\nfunc (m *Module) Middleware() echo.MiddlewareFunc { return nil }\n"
	if _, err := UserAuthProvider(src, []string{"member", "shop"}); err == nil {
		t.Fatal("两个提供者应报错")
	}
}

func TestReadManifestRejectsInvalidPath(t *testing.T) {
	src := mapSource{"internal/modules/x/module.yaml": "web_paths:\n  - web/admin/../etc\n"}
	if _, err := ReadManifest(src, "x"); err == nil {
		t.Fatal("非规范路径应报错")
	}
}
