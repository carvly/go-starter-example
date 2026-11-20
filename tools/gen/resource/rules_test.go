package resource

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const base = `module: shop
resource: item
title: 物品
display: name
fields:
  name: { type: string, title: 名称, required: true, max: 50 }
  status: { type: enum, title: 状态, values: { open: 开, closed: 关 }, default: open, readonly: true }
indexes:
  - columns: [name]
    for: 按名称排序
admin:
  operations: [create, update]
  list: { columns: [name], sort: [name] }
  actions:
    close: { title: 关闭, set: { status: closed }, when: { status: open } }
`

const webBase = `module: shop
resource: item
title: 物品
fields:
  name: { type: string, title: 名称, required: true, max: 50 }
web:
  operations: [list, create]
`

// 每条规则一个故意写错的描述文件：断言报出这条规则。
var ruleCases = map[string]string{
	"YAML-SYNTAX":       "module: [",
	"YAML-UNKNOWN-KEY":  base + "titel: 物品\n",
	"YAML-TYPE":         strings.Replace(base, "max: 50", "max: abc", 1),
	"TOP-REQUIRED":      strings.Replace(base, "title: 物品\n", "", 1),
	"TOP-MODULE-NAME":   strings.Replace(base, "module: shop", "module: Shop_1", 1),
	"TOP-RESOURCE-NAME": strings.Replace(base, "resource: item", "resource: Item", 1),
	"TOP-PLURAL-NAME":   base + "plural: item\n",
	"TOP-DISPLAY":       strings.Replace(base, "display: name", "display: status", 1),
	"TOP-SURFACE":       base + "web: { operations: [list] }\n",
	"TEXT-TITLE-CHARS":  strings.Replace(base, "title: 名称,", "title: \"名称{x}\",", 1),

	"FIELD-NAME":                 strings.Replace(base, "  name: { type: string", "  order: { type: int, title: 顺序 }\n  name: { type: string", 1),
	"FIELD-TYPE":                 base + "  # 占位\n" + "fields_x: 1\n",
	"FIELD-TITLE-REQUIRED":       strings.Replace(base, "title: 名称, ", "", 1),
	"FIELD-MODIFIER":             strings.Replace(base, "fields:\n", "fields:\n  flag: { type: bool, title: 开关, required: true }\n", 1),
	"FIELD-MAX-REQUIRED":         strings.Replace(base, ", max: 50 }", " }", 1),
	"FIELD-MIN-MAX":              strings.Replace(base, "max: 50", "min: 10, max: 5", 1),
	"FIELD-MONEY-NAME":           strings.Replace(base, "fields:\n", "fields:\n  price: { type: money, title: 价格 }\n", 1),
	"FIELD-ENUM-VALUES":          strings.Replace(base, "fields:\n", "fields:\n  kind: { type: enum, title: 种类 }\n", 1),
	"FIELD-DEFAULT":              strings.Replace(base, "default: open", "default: maybe", 1),
	"FIELD-IMMUTABLE-READONLY":   strings.Replace(base, "readonly: true", "readonly: true, immutable: true", 1),
	"FIELD-READONLY-UNREACHABLE": strings.Replace(base, "    close: { title: 关闭, set: { status: closed }, when: { status: open } }\n", "", 1),

	"ADMIN-OPERATION":    strings.Replace(base, "[create, update]", "[create, archive]", 1),
	"ADMIN-LIST-FIELD":   strings.Replace(base, "list: { columns: [name]", "list: { columns: [name], search: [status]", 1),
	"ADMIN-DEFAULT-SORT": strings.Replace(base, "sort: [name]", "sort: [name], default_sort: status", 1),
	"ADMIN-NAV":          strings.Replace(base, "  list: {", "  nav: { icon: users, group: 业务, order: 3 }\n  list: {", 1),

	"ACTION-NAME":         strings.Replace(base, "    close:", "    Close:", 1),
	"ACTION-RESERVED":     strings.Replace(base, "    close: {", "    delete: {", 1),
	"ACTION-TITLE":        strings.Replace(base, "title: 关闭, ", "", 1),
	"ACTION-KIND":         strings.Replace(base, "set: { status: closed }, when: { status: open }", "confirm: x", 1),
	"ACTION-INPUT":        base + "    rename: { title: 改名, input: { name: { like: name, unique: true } }, set: { name: $name } }\n",
	"ACTION-SET":          strings.Replace(base, "set: { status: closed }", "set: { color: red }", 1),
	"ACTION-WHEN":         strings.Replace(base, "when: { status: open }", "when: { name: x }", 1),
	"ACTION-ERROR":        strings.Replace(base, "when: { status: open } }", "when: { status: open }, errors: { x: { kind: conflict, message: 不行 } } }", 1),
	"ACTION-SET-EDITABLE": base + "    reset: { title: 重置名称, set: { name: 未命名 } }\n",

	"RULE-ON":     base + "rules:\n  - on: [delete]\n    custom: 有引用时不能删\n",
	"RULE-CUSTOM": base + "rules:\n  - on: [create]\n",
	"RULE-ERROR":  base + "rules:\n  - on: [create]\n    custom: 规则\n    errors: { bad: { kind: oops, message: 不行 } }\n",

	"WEB-OPERATION": strings.Replace(webBase, "[list, create]", "[list, update]", 1),
	"WEB-AUTH":      webBase + "  auth: [get]\n",

	"INDEX-COLUMN":    strings.Replace(base, "columns: [name]\n    for", "columns: [nope]\n    for", 1),
	"INDEX-FOR":       strings.Replace(base, "    for: 按名称排序\n", "", 1),
	"INDEX-REDUNDANT": strings.Replace(base, "admin:\n", "  - columns: [id]\n    for: 按 id 查\nadmin:\n", 1),
	"INDEX-MISSING":   strings.Replace(base, "sort: [name]", "sort: [name], filter: [status]", 1),

	"REPO-MODULE-EXISTS": strings.Replace(base, "module: shop", "module: existing", 1),
	"REPO-TABLE-EXISTS":  strings.Replace(base, "resource: item", "resource: thing", 1),
	"REPO-SPEC-CONFLICT": strings.Replace(base, "resource: item", "resource: account", 1),
}

func init() {
	// FIELD-TYPE 单独写，避免和上面的字符串替换互相影响。
	ruleCases["FIELD-TYPE"] = strings.Replace(base, "type: string, title: 名称", "type: float, title: 名称", 1)
}

func checkAll(t *testing.T, root, src string) []Problem {
	t.Helper()
	p, probs := Parse("case.yaml", []byte(src))
	if len(probs) > 0 {
		return probs
	}
	probs = Validate(p)
	if len(probs) == 0 || !slices.ContainsFunc(probs, func(p Problem) bool { return !p.Hint }) {
		repo, err := OpenRepo(root)
		if err != nil {
			t.Fatal(err)
		}
		probs = append(probs, repo.Check(p)...)
	}
	return probs
}

// fakeRepo 是一个最小的仓库：仓库现状的规则不依赖真实仓库里有哪些模块（删除演练会删掉它们）。
func fakeRepo(t *testing.T, withAuth bool) string {
	t.Helper()
	root := t.TempDir()
	modules := "package app\n"
	files := map[string]string{}
	if withAuth {
		files["internal/modules/user/module.go"] = "package user\n\nfunc (m *Module) Middleware() echo.MiddlewareFunc { return nil }\n"
	}
	spec := "openapi: 3.2.0\ntags: []\npaths:\n  /accounts:\n    get: {}\ncomponents:\n  schemas:\n    Account: {}\n"
	for f, content := range map[string]string{
		"go.mod":                                       "module example.com/x\n",
		"tools/gen/.keep":                              "",
		"internal/app/modules.go":                      modules,
		"internal/modules/existing/module.go":          "package existing\n",
		"internal/modules/existing/postgres/tables.go": "package postgres\n\nvar Tables = []string{\"things\"}\n",
		"api/admin.yaml":                               spec,
		"api/openapi.yaml":                             spec,
	} {
		files[f] = content
	}
	for f, content := range files {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o755)
		_ = os.WriteFile(filepath.Join(root, f), []byte(content), 0o644)
	}
	return root
}

func TestRules_EachTriggeredWithPositionAndFix(t *testing.T) {
	root := fakeRepo(t, true)
	for rule := range Rules {
		if rule == "REPO-AUTH-MISSING" {
			continue // 见 TestRepoAuthMissing
		}
		src, ok := ruleCases[rule]
		if !ok {
			t.Errorf("规则 %s 没有用例：在 ruleCases 里加一个故意写错的描述文件", rule)
			continue
		}
		probs := checkAll(t, root, src)
		i := slices.IndexFunc(probs, func(p Problem) bool { return p.Rule == rule })
		if i < 0 {
			t.Errorf("%s 的用例没有报出 %s，报出的是：%v", rule, rule, probs)
			continue
		}
		p := probs[i]
		if p.Line == 0 || p.Fix == "" || !strings.Contains(p.String(), "改法：") || p.Doc != docRef(rule) {
			t.Errorf("%s 的报错缺位置、改法或文档链接：%s", rule, p)
		}
	}
	for rule := range ruleCases {
		if _, ok := Rules[rule]; !ok {
			t.Errorf("用例 %s 不是已登记的规则", rule)
		}
	}
}

func TestRules_EveryProblemIsRegistered(t *testing.T) {
	for _, src := range ruleCases {
		for _, p := range checkAll(t, fakeRepo(t, true), src) {
			if _, ok := Rules[p.Rule]; !ok {
				t.Errorf("报出了未登记的规则 %s：%s", p.Rule, p)
			}
		}
	}
}

// 每个规则编号在 generator-rules.md 里有同名的小节（锚点是编号的小写），规则和文档不会脱节。
func TestRules_DocumentedInGeneratorMD(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/agents/generator-rules.md")
	if err != nil {
		t.Fatal(err)
	}
	for rule := range Rules {
		if !regexp.MustCompile(`(?m)^#{3,4} ` + regexp.QuoteMeta(rule) + `$`).Match(doc) {
			t.Errorf("docs/agents/generator-rules.md 缺少规则 %s 的小节（### %s）", rule, rule)
		}
	}
}

func TestRepoAuthMissing(t *testing.T) {
	probs := checkAll(t, fakeRepo(t, false), webBase+"  auth: [create]\n")
	if !slices.ContainsFunc(probs, func(p Problem) bool { return p.Rule == "REPO-AUTH-MISSING" }) {
		t.Errorf("没有报出 REPO-AUTH-MISSING：%v", probs)
	}
	// 提供者从代码推导：换了名字的模块只要有 Middleware() 就能用。
	root := fakeRepo(t, false)
	if err := os.MkdirAll(filepath.Join(root, "internal/modules/member"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/modules/member/module.go"), []byte("package member\n\nfunc (m *Module) Middleware() echo.MiddlewareFunc { return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if probs = checkAll(t, root, webBase+"  auth: [create]\n"); slices.ContainsFunc(probs, func(p Problem) bool { return p.Rule == "REPO-AUTH-MISSING" }) {
		t.Errorf("换名的提供者没有被识别：%v", probs)
	}
	// 两个提供者：生成的代码不知道用哪个。
	if err := os.WriteFile(filepath.Join(root, "internal/modules/existing/module.go"), []byte("package existing\n\nfunc (m *Module) Middleware() echo.MiddlewareFunc { return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	probs = checkAll(t, root, webBase+"  auth: [create]\n")
	if !slices.ContainsFunc(probs, func(p Problem) bool {
		return p.Rule == "REPO-AUTH-MISSING" && strings.Contains(p.Message, "唯一")
	}) {
		t.Errorf("两个提供者时没有报出 REPO-AUTH-MISSING：%v", probs)
	}
}

func TestExamplesAreValid(t *testing.T) {
	for name, src := range map[string][]byte{"admin": ExampleAdmin, "web": ExampleWeb} {
		p, probs := Parse(name, src)
		probs = append(probs, Validate(p)...)
		for _, pr := range probs {
			if !pr.Hint {
				t.Errorf("示例 %s：%s", name, pr)
			}
		}
	}
}

func TestParse_ReportsAllProblemsAtOnce(t *testing.T) {
	src := strings.Replace(strings.Replace(base, "title: 名称, ", "", 1), "[create, update]", "[create, archive]", 1)
	p, _ := Parse("x.yaml", []byte(src))
	probs := Validate(p)
	rules := map[string]bool{}
	for _, pr := range probs {
		rules[pr.Rule] = true
	}
	if !rules["FIELD-TITLE-REQUIRED"] || !rules["ADMIN-OPERATION"] {
		t.Errorf("应该一次报出全部错误，实际：%v", probs)
	}
}

func TestParse_UnknownKeySuggestsClosest(t *testing.T) {
	_, probs := Parse("x.yaml", []byte(base+"titel: x\n"))
	if len(probs) != 1 || !strings.Contains(probs[0].Fix, "是不是 title") || probs[0].Line != 16 {
		t.Errorf("probs = %v", probs)
	}
}

func TestDeriveNames(t *testing.T) {
	for res, want := range map[string]string{"category": "categories", "box": "boxes", "key": "keys", "line_item": "line_items", "branch": "branches"} {
		if got := pluralize(res); got != want {
			t.Errorf("pluralize(%q) = %q, want %q", res, got, want)
		}
	}
	n := DeriveNames(&Descriptor{Module: "catalog", Resource: "line_item", Admin: &Admin{Operations: []string{"create", "update"}}})
	if n.Table != "line_items" || n.Path != "/line-items" || n.Tag != "line-items" || n.Type != "LineItem" ||
		n.OpList != "listLineItems" || n.OpUpdate != "updateLineItem" || n.OpDelete != "" || n.ActionOp("adjust_stock") != "adjustStockLineItem" {
		t.Errorf("names = %+v", n)
	}
}

func TestParse_FoldedCJKHasNoSpace(t *testing.T) {
	src := strings.Replace(base, "    close: { title: 关闭, set: { status: closed }, when: { status: open } }\n",
		"    close:\n      title: 关闭\n      when: { status: open }\n      custom: >-\n        状态改为 closed，\n        并且通知对方\n        and notify the owner\n", 1)
	p, problems := Parse("x.yaml", []byte(src))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	act, _ := p.D.Admin.Actions.Get("close")
	if want := "状态改为 closed，并且通知对方 and notify the owner"; act.Custom != want {
		t.Errorf("custom = %q, want %q", act.Custom, want)
	}
}

func TestRules_DisplayMustBeListColumn(t *testing.T) {
	src := strings.Replace(base, "fields:\n", "fields:\n  rank: { type: int, title: 排名 }\n", 1)
	src = strings.Replace(src, "list: { columns: [name]", "list: { columns: [rank]", 1)
	p, _ := Parse("x.yaml", []byte(src))
	if probs := Validate(p); !slices.ContainsFunc(probs, func(p Problem) bool { return p.Rule == "ADMIN-LIST-FIELD" && strings.Contains(p.Message, "display") }) {
		t.Errorf("display 不在 columns 里时没有报错：%v", probs)
	}
}

func TestRules_ActionInputReservedName(t *testing.T) {
	src := strings.Replace(base, "    close: { title: 关闭, set: { status: closed }, when: { status: open } }\n",
		"    close: { title: 关闭, set: { status: closed }, when: { status: open } }\n    move: { title: 移动, input: { id: { type: string, title: 目标, max: 10 } }, custom: 移动 }\n", 1)
	p, _ := Parse("x.yaml", []byte(src))
	if probs := Validate(p); !slices.ContainsFunc(probs, func(p Problem) bool { return p.Rule == "ACTION-RESERVED" }) {
		t.Errorf("参数名 id 没有报错：%v", probs)
	}
}

func TestModel_LikeEnumInputReusesFieldType(t *testing.T) {
	src := strings.Replace(base, "    close: { title: 关闭, set: { status: closed }, when: { status: open } }\n",
		"    close: { title: 关闭, set: { status: closed }, when: { status: open } }\n    move: { title: 改状态, input: { target: { like: status } }, set: { status: $target } }\n", 1)
	p, probs := Parse("x.yaml", []byte(src))
	if probs = append(probs, Validate(p)...); slices.ContainsFunc(probs, func(p Problem) bool { return !p.Hint }) {
		t.Fatal(probs)
	}
	m := BuildModel(&p.D, "example.com/m", "0000")
	for _, a := range m.Actions {
		if a.Name == "move" && (a.Input[0].EnumType != "ItemStatus" || a.Input[0].GoType != "*ItemStatus") {
			t.Errorf("参数的枚举类型 = %s / %s，want ItemStatus / *ItemStatus", a.Input[0].EnumType, a.Input[0].GoType)
		}
	}
	bad := strings.Replace(src, "input: { target: { like: status } }", "input: { target: { type: enum, title: 目标, values: { a: 甲 } } }", 1)
	p, _ = Parse("x.yaml", []byte(bad))
	if probs := Validate(p); !slices.ContainsFunc(probs, func(p Problem) bool { return p.Rule == "ACTION-INPUT" }) {
		t.Errorf("单独定义枚举的参数没有报错：%v", probs)
	}
}
