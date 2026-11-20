package resource

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSpecSections_InsertAfterLastContentLine(t *testing.T) {
	spec := `openapi: 3.2.0
tags:
  - name: a

# 注释属于下一段
paths:
  /a:
    get: {}

components:
  parameters: {}
  schemas:
    A:
      type: object
`
	at, err := specSections([]byte(spec))
	if err != nil {
		t.Fatal(err)
	}
	if at["tags"] != 3 || at["paths"] != 8 || at["schemas"] != 14 {
		t.Fatalf("at = %v", at)
	}
	out := insertLines(spec, []Insert{
		{After: at["tags"], Text: "  - name: b\n"},
		{After: at["paths"], Text: "\n  /b:\n    get: {}\n"},
		{After: at["schemas"], Text: "\n    B:\n      type: object\n"},
	})
	for _, want := range []string{"  - name: a\n  - name: b\n\n# 注释", "    get: {}\n\n  /b:\n    get: {}\n\ncomponents", "      type: object\n\n    B:\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("插入结果里没有 %q：\n%s", want, out)
		}
	}
}

// testdata/modules.go.txt 是 internal/app/modules.go 的一份副本：真实文件会在删除演练里被删掉几行。
// 真实文件上的插入由夹具 CI 覆盖。
func TestInsertModules_FollowsConvention(t *testing.T) {
	src, err := os.ReadFile("testdata/modules.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	repo := &Repo{GoMod: "app.local"}
	p, _ := Parse("x.yaml", ExampleAdmin)
	m := BuildModel(&p.D, repo.GoMod, "0000")
	var inserts []Insert
	for _, in := range repo.modulesInserts(m) {
		if in.File == "internal/app/modules.go" {
			inserts = append(inserts, in)
		}
	}
	out, err := insertModules(string(src), inserts)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"app.local/internal/modules/catalog"`,
		"Catalog *catalog.Module",
		"catalogM := catalog.New(catalog.Deps{DB: in.DBTx})",
		"Catalog: catalogM,",
		"m.Catalog.RegisterAdminHTTP,\n\t}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("modules.go 里没有 %q", want)
		}
	}
	if _, err := insertModules("package app\n", repo.modulesInserts(m)); err == nil || !strings.Contains(err.Error(), "硬规则 3") {
		t.Errorf("不符合约定时 error = %v", err)
	}
}

func TestInsertMigrationSource(t *testing.T) {
	repo := &Repo{GoMod: "app.local"}
	p, _ := Parse("x.yaml", ExampleAdmin)
	m := BuildModel(&p.D, repo.GoMod, "0000")
	var inserts []Insert
	for _, in := range repo.modulesInserts(m) {
		if in.File == "internal/app/migrate.go" {
			inserts = append(inserts, in)
		}
	}
	src := `package app

import (
	"app.local/internal/modules/user"
	"app.local/internal/platform/migrate"
)

func migrationSources() []migrate.Source {
	return []migrate.Source{
		user.Migrations,
	}
}
`
	out, err := insertModules(src, inserts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"app.local/internal/modules/catalog"`) || !strings.Contains(out, "catalog.Migrations,") {
		t.Fatalf("migrationSources 插入结果：\n%s", out)
	}
}

func TestDrift_SeparatesTodoFromOtherChanges(t *testing.T) {
	root := t.TempDir()
	snap := SnapshotDir(root, "item")
	gen := `package domain

func (s *Service) Close() error { return nil }

func (s *Service) Adjust() error { return ErrNotImplemented }

func helper() int { return 1 }
`
	cur := strings.Replace(strings.Replace(gen, "return ErrNotImplemented", "return nil", 1), "return 1", "return 2", 1) + "\nfunc extra() {}\n"
	desc := strings.Replace(base, "    close:", "    adjust: { title: 调整, custom: 调整 }\n    close:", 1)
	for p, c := range map[string]string{
		"go.mod":                             "module example.com/x\n",
		"tools/gen/.keep":                    "",
		"a.go":                               cur,
		"shared.yaml":                        "x\n  - name: items\n",
		".gen/item/snapshot/files/a.go":      gen,
		".gen/item/snapshot/descriptor.yaml": desc,
	} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		_ = os.WriteFile(filepath.Join(root, p), []byte(c), 0o644)
	}
	plan, _ := json.Marshal(Plan{Create: []string{"a.go"}, Insert: []Insert{{File: "shared.yaml", Section: "tags", Text: "  - name: items\n"}, {File: "shared.yaml", Section: "paths", Text: "  /items:\n"}}})
	_ = os.WriteFile(filepath.Join(snap, "plan.json"), plan, 0o644)
	repo, err := OpenRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	items, err := repo.Drift("item")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]DriftItem{}
	for _, it := range items {
		got[it.Kind+" "+it.Symbol] = it
	}
	if it, ok := got["changed Service.Adjust"]; !ok || !it.Expected {
		t.Errorf("补足 custom 动作应当算待办内：%v", items)
	}
	if it, ok := got["changed helper"]; !ok || it.Expected {
		t.Errorf("改了生成的 helper 应当算待办外：%v", items)
	}
	if it, ok := got["added extra"]; !ok || !it.Expected {
		t.Errorf("新增的声明应当算待办内：%v", items)
	}
	if _, ok := got["insert_changed paths"]; !ok {
		t.Errorf("被改掉的插入内容应当报出：%v", items)
	}
	if _, ok := got["insert_changed tags"]; ok {
		t.Errorf("没改的插入内容不应报出：%v", items)
	}
	if slices.ContainsFunc(items, func(it DriftItem) bool { return it.Symbol == "Service.Close" }) {
		t.Errorf("没改的声明不应报出：%v", items)
	}
}

// 没有新建和编辑时，Service 里不生成字段校验（没有调用方，golangci 的 unused 会报错）。
func TestBuildModel_NoWriteOperationsHasNoChecks(t *testing.T) {
	src := strings.Replace(base, "operations: [create, update]", "operations: []", 1)
	src = strings.Replace(src, "  list: { columns: [name], sort: [name] }", "  list: { columns: [name], sort: [name] }", 1)
	p, problems := Parse("x.yaml", []byte(src))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	m := BuildModel(&p.D, "example.com/m", "0000")
	if len(m.Checked) != 0 {
		t.Errorf("Checked = %v, want 空", m.Checked)
	}
}

func TestFixtureNames(t *testing.T) {
	in := "# 注释 module: x\nmodule: member\nresource: user\nplural: people\ntitle: 用户\nfields:\n  module: { type: string }\n"
	want := "# 注释 module: x\nmodule: fxmember\nresource: fx_user\nplural: fx_people\ntitle: 用户\nfields:\n  module: { type: string }\n"
	if got := string(FixtureNames([]byte(in))); got != want {
		t.Errorf("FixtureNames =\n%s\nwant\n%s", got, want)
	}
	if got := string(FixtureNames([]byte(want))); got != want {
		t.Errorf("已经加过前缀的不再加：\n%s", got)
	}
}

// 改名这类动作写唯一字段：冲突的错误码显示在参数下。
func TestFieldErrors_ActionSetsUniqueField(t *testing.T) {
	src := strings.Replace(base, "  name: { type: string, title: 名称, required: true, max: 50 }", "  name: { type: string, title: 名称, required: true, max: 50, unique: true }", 1)
	src = strings.Replace(src, "    close: { title: 关闭, set: { status: closed }, when: { status: open } }\n",
		"    close: { title: 关闭, set: { status: closed }, when: { status: open } }\n    rename: { title: 改名, input: { new_name: { like: name } }, set: { name: $new_name } }\n", 1)
	p, probs := Parse("x.yaml", []byte(src))
	if len(probs) > 0 {
		t.Fatal(probs)
	}
	m := BuildModel(&p.D, "example.com/m", "0000")
	if got := m.FieldErrors("rename"); got != `item_name_taken: "new_name"` {
		t.Errorf("FieldErrors(rename) = %s", got)
	}
}
