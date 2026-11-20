package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnershipDriftDetectsPermissionChange(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"starter-owned.txt": "internal/modules/user/\n",
		"internal/modules/user/adminapi/oapi/config.yaml": "include-tags: [users]\n",
		"internal/modules/user/httpapi/oapi/config.yaml":  "include-tags: [user]\n",
		"api/admin.yaml": `openapi: 3.2.0
paths:
  /users:
    get:
      operationId: listUsers
      tags: [users]
      x-permission: user:read
      responses:
        "200":
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/User"
components:
  schemas:
    User:
      type: object
`,
		"api/openapi.yaml": "openapi: 3.2.0\npaths: {}\n",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-qm", "base"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "api/admin.yaml"), []byte(strings.Replace(files["api/admin.yaml"], "user:read", "user:write", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	problems, err := ownershipDrift(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) == 0 {
		t.Fatal("expected permission drift")
	}
}

func TestStarterOwnedPathsIncludesModuleWebPaths(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"starter-owned.txt":                            "internal/modules/crew/\nMakefile\n",
		"Makefile":                                     "",
		"internal/modules/crew/module.go":              "package crew\n",
		"internal/modules/crew/module.yaml":            "web_paths:\n  - web/admin/src/features/crew\n",
		"web/admin/src/features/crew/page.tsx":         "",
		"web/admin/src/features/crew/meta.gen.ts":      "",
		"web/admin/src/features/project-only/page.tsx": "",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := starterOwnedPaths(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Makefile", "internal/modules/crew/module.go", "internal/modules/crew/module.yaml", "web/admin/src/features/crew/page.tsx"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("starterOwnedPaths = %v, want %v（模块的 web_paths 计入，派生文件和项目自己的页面不计入）", got, want)
	}
}
