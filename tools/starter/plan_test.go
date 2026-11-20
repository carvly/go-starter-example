package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func testRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"internal/app/modules.go": `package app
var moduleRequires = map[string][]string{"beta": {"gamma"}}
type Modules struct {
	Alpha *alpha.Module
	Beta  *beta.Module
	Gamma *gamma.Module
	Staff *staff.Module
}
func build() {
	alphaM := alpha.New()
	betaM := beta.New(alphaM)
	gammaM := gamma.New(betaM)
	staffM := staff.New()
}
func (m *Modules) adminAuth() {
	m.Staff.AdminGuard()
}
`,
		"internal/app/adapters_beta_alpha.go":   "package app\n",
		"internal/app/beta_integration_test.go": "package app\nimport _ \"app.local/internal/modules/beta\"\n",
		"internal/app/module_configs.go":        "package app\n",
		"internal/app/migrate.go":               "package app\n",
		"api/openapi.yaml":                      "openapi: 3.2.0\npaths: {}\n",
		"api/admin.yaml":                        "openapi: 3.2.0\npaths: {}\n",
	}
	for _, module := range []string{"alpha", "beta", "gamma", "staff"} {
		files["internal/modules/"+module+"/module.go"] = "package " + module + "\n"
		files["internal/modules/"+module+"/module.yaml"] = "web_paths: []\n"
	}
	files["internal/modules/beta/module.yaml"] = "web_paths:\n  - web/admin/src/routes/_authed/betas\n"
	files["internal/modules/beta/adminapi/oapi/config.yaml"] = "include-tags: [betas]\n"
	files["internal/modules/staff/module.yaml"] = "web_paths:\n  - web/admin/src/features/auth\n  - web/admin/src/features/home\n  - web/admin/src/routes/_authed/accounts\n"
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestComputeOrderClosure(t *testing.T) {
	plan, err := compute(repository{root: testRepository(t)}, []string{"beta"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(plan.Modules, []string{"beta", "gamma"}) {
		t.Fatalf("modules = %v", plan.Modules)
	}
	for _, path := range []string{"internal/modules/beta/", "internal/modules/gamma/", "internal/app/adapters_beta_alpha.go", "web/admin/src/routes/_authed/betas"} {
		if !slices.Contains(plan.Paths, path) {
			t.Errorf("paths 缺少 %s", path)
		}
	}
	if slices.Contains(plan.Paths, "internal/modules/alpha/") {
		t.Error("删除 beta 不应删除 alpha")
	}
	if plan.AdminSurface {
		t.Error("删除 beta 不应移除整个后台接入面")
	}
}

func TestComputeStaffRemovesAdminSurface(t *testing.T) {
	plan, err := compute(repository{root: testRepository(t)}, []string{"staff"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.AdminSurface {
		t.Fatal("删除 staff 应移除后台接入面")
	}
	for _, path := range []string{"web/admin/src/features/auth", "web/admin/src/features/home", "web/admin/src/routes/_authed/accounts"} {
		if !slices.Contains(plan.Paths, path) {
			t.Errorf("paths 缺少 staff 声明的 %s", path)
		}
	}
}

func TestClosureRejectsUnknownModule(t *testing.T) {
	if _, err := closure([]string{"user"}, nil, []string{"missing"}); err == nil {
		t.Fatal("unknown module should fail")
	}
}
