// Package scripttest 用临时仓库测试 scripts/ 下的检查脚本：构造违规输入，断言脚本会报错，防止规则退化成
// 永远不报错。
package scripttest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runCheckDocs 在只含给定文件的临时 git 仓库里运行 check-docs.sh。
func runCheckDocs(t *testing.T, files map[string]string) (string, error) {
	t.Helper()
	root := t.TempDir()
	script, err := os.ReadFile("../../../scripts/check-docs.sh")
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]string{
		"scripts/check-docs.sh": string(script),
		"go.mod":                "module example.com/m\n",
		"Makefile":              "ci:\n",
		"a.go":                  "package a\n\nimport (\n\tfoo \"example.com/m/pkg\"\n)\n\nvar _ = foo.X\n",
		"README.md":             "# x\n",
	}
	for k, v := range files {
		all[k] = v
	}
	for name, content := range all {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	cmd := exec.Command("bash", "scripts/check-docs.sh")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCheckDocs_ReportsEachMistake(t *testing.T) {
	tests := []struct{ name, doc, want string }{
		{"链接本地提案", "见 [提案](docs/proposals/0001-x.md)。\n", "链接到只在本地保留的文档"},
		{"反引号引用本地提案", "见 `docs/proposals/0001-x.md`。\n", "引用了只在本地保留的文档"},
		{"链接目标不存在", "见 [别处](docs/agents/missing.md)。\n", "链接的目标不存在"},
		{"反引号路径不存在", "见 `docs/agents/missing.md`。\n", "引用的路径不存在"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runCheckDocs(t, map[string]string{"AGENTS.md": tt.doc})
			if err == nil || !strings.Contains(out, tt.want) {
				t.Fatalf("err = %v, want 报出 %q；输出：\n%s", err, tt.want, out)
			}
		})
	}
}

func TestCheckDocs_AcceptsValidDoc(t *testing.T) {
	doc := "规划写在本地的 `docs/proposals/` 目录。见 [README](README.md#x)、[外部](https://example.com)。\n" +
		"行内代码里的 `f[T](a, b)` 和代码块不是链接：\n\n```go\nx := m[k](v)\n```\n"
	if out, err := runCheckDocs(t, map[string]string{"AGENTS.md": doc}); err != nil {
		t.Fatalf("合法文档被拒绝：%v\n%s", err, out)
	}
}
