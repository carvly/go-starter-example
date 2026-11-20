package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"go.yaml.in/yaml/v4"

	"app.local/internal/platform/httpserver"
)

var modulePathPrefix = func() string {
	cmd := exec.Command("go", "list", "-m")
	out, err := cmd.Output()
	if err != nil {
		panic(fmt.Sprintf("go list -m: %v", err))
	}
	return strings.TrimSpace(string(out)) + "/internal/modules/"
}()

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func moduleDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), "internal", "modules"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// 编译器能保证注册了的模块一定存在，但查不出"写了模块却忘了注册"。
func TestModules_EveryModuleIsRegistered(t *testing.T) {
	root := repoRoot(t)
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "internal", "app", "modules.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	imported := map[string]int{}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if rest, ok := strings.CutPrefix(path, modulePathPrefix); ok {
			imported[rest]++
		}
	}
	vars := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
			for _, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					vars[id.Name] = true
				}
			}
		}
		return true
	})

	for _, m := range moduleDirs(t) {
		if _, err := os.Stat(filepath.Join(root, "internal", "modules", m, "module.go")); err != nil {
			t.Errorf("internal/modules/%s 缺少 module.go：每个模块必须用 module.go 作为对组合根暴露的唯一入口", m)
		}
		if imported[m] != 1 {
			t.Errorf("internal/modules/%s 在 internal/app/modules.go 中被 import %d 次，want 1：新模块要在 modules.go 里 import 并在 buildModules 中构造", m, imported[m])
		}
		if !vars[m+"M"] {
			t.Errorf("buildModules 里没有变量 %sM：约定每个模块构造成一个名为 <模块>M 的变量，tools/starter 的删除执行器依赖这个约定", m)
		}
	}
	for path := range imported {
		if strings.Contains(path, "/") {
			t.Errorf("modules.go 导入了 %s%s：组合根只能导入模块根包（module.go 所在的包）", modulePathPrefix, path)
		}
	}
}

// tools/starter 的删除执行器（modules_go.go）按行删除模块，依赖 modules.go 的书写约定：
// 每个 <m>M 的构造语句只占一行（构造参数里引用了谁就依赖谁，推导依赖只读这一行）；
// 列出多个模块的复合字面量（Modules 返回值、注册切片）每个元素独占一行。
func TestModules_OneLinePerModule(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(repoRoot(t), "internal", "app", "modules.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range moduleLineProblems(fset, f) {
		t.Error(problem)
	}
}

func moduleLineProblems(fset *token.FileSet, f *ast.File) []string {
	var problems []string
	line := func(p token.Pos) int { return fset.Position(p).Line }
	isModuleRef := func(e ast.Expr) bool {
		found := false
		ast.Inspect(e, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && strings.HasSuffix(id.Name, "M") && len(id.Name) > 1 {
				found = true
			}
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.SelectorExpr); ok {
					if id, ok := x.X.(*ast.Ident); ok && id.Name == "m" {
						found = true
					}
				}
			}
			return !found
		})
		return found
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range v.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && strings.HasSuffix(id.Name, "M") {
					if line(v.Pos()) != line(v.End()) {
						problems = append(problems, fmt.Sprintf("modules.go:%d: %s 的构造语句跨了多行，必须写在一行里（删除演练按行推导依赖）", line(v.Pos()), id.Name))
					}
					return false
				}
			}
		case *ast.CallExpr:
			if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AddDataCleaner" {
				if len(v.Args) != 1 || line(v.Pos()) != line(v.End()) || !isModuleRef(v) {
					problems = append(problems, fmt.Sprintf("modules.go:%d: 注销清理登记必须写成一行 userM.AddDataCleaner(memberM.UserDataCleaner())", line(v.Pos())))
				}
			}
		case *ast.CompositeLit:
			refs := 0
			for _, el := range v.Elts {
				if isModuleRef(el) {
					refs++
				}
			}
			if refs < 2 {
				return true
			}
			prev := line(v.Lbrace)
			for _, el := range v.Elts {
				if line(el.Pos()) == prev || line(el.Pos()) != line(el.End()) {
					problems = append(problems, fmt.Sprintf("modules.go:%d: 列出多个模块的复合字面量必须每个元素独占一行（删除演练按行删除）", line(el.Pos())))
				}
				prev = line(el.End())
			}
			if line(v.Rbrace) == prev {
				problems = append(problems, fmt.Sprintf("modules.go:%d: 复合字面量的右括号要单独一行", line(v.Rbrace)))
			}
		}
		return true
	})
	return problems
}

func TestModules_CleanerRegistrationMustBeOneLine(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "modules.go", `package app
func register() {
	userM.AddDataCleaner(
		memberM.UserDataCleaner())
}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := moduleLineProblems(fset, f); len(got) != 1 || !strings.Contains(got[0], "注销清理登记") {
		t.Fatalf("problems = %v, want 跨行登记报错", got)
	}
}

func newZeroInfra() *Infra {
	return &Infra{Config: baseTestConfig(), Logger: discardLogger()}
}

// specParamToEcho 把 spec 的 /orders/{id} 写法换成 Echo 的 /orders/:id。
var specParamToEcho = strings.NewReplacer("{", ":", "}", "")

type spec struct {
	Paths map[string]map[string]yaml.Node `yaml:"paths"`
}

// 双向比对：spec 有而路由没有 = 漏注册模块或 operation 漏写 tag；路由有而 spec
// 没有 = 绕过 spec 手写了路由。
func TestRoutes_MatchOpenAPISpec(t *testing.T) {
	e, err := httpserver.New(apiServerDeps(newZeroInfra(), buildModules(newZeroInfra(), testModuleConfigs(t))))
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, r := range e.Router().Routes() {
		if strings.HasPrefix(r.Path, httpserver.APIPrefix+"/") {
			got = append(got, r.Method+" "+strings.TrimPrefix(r.Path, httpserver.APIPrefix))
		}
	}

	var want []string
	for path, ops := range readSpec(t, "openapi.yaml").Paths {
		for method := range ops {
			if isHTTPMethod(method) {
				want = append(want, strings.ToUpper(method)+" "+specParamToEcho.Replace(path))
			}
		}
	}

	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("api/openapi.yaml 定义了 %s，但没有任何模块注册它：检查 operation 的 tag 是否对应某个模块的 oapi/config.yaml，以及模块是否在 modules.go 的 httpRegistrars 里", w)
		}
	}
	for _, g := range got {
		if !slices.Contains(want, g) {
			t.Errorf("注册了路由 %s，但 api/openapi.yaml 里没有：业务路由必须先写进 spec，再由 oapi-codegen 生成注册代码", g)
		}
	}
}

// 后台路由与 api/admin.yaml 双向比对，规则同 TestRoutes_MatchOpenAPISpec。Echo 给带中间件的
// 分组注册的兜底路由（RouteNotFound）不是业务路由，不参与比对。
func TestAdminRoutes_MatchAdminSpec(t *testing.T) {
	in := newZeroInfra()
	in.Config.HTTP.AdminOrigin = "https://admin.example.com"
	e, err := httpserver.NewAdmin(adminServerDeps(in, buildModules(in, testModuleConfigs(t))))
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, r := range e.Router().Routes() {
		if r.Method != echo.RouteNotFound && strings.HasPrefix(r.Path, httpserver.AdminAPIPrefix+"/") {
			got = append(got, r.Method+" "+strings.TrimPrefix(r.Path, httpserver.AdminAPIPrefix))
		}
	}

	var want []string
	for path, ops := range readSpec(t, "admin.yaml").Paths {
		for method := range ops {
			if isHTTPMethod(method) {
				want = append(want, strings.ToUpper(method)+" "+specParamToEcho.Replace(path))
			}
		}
	}

	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("api/admin.yaml 定义了 %s，但没有任何模块注册它：检查 operation 的 tag 是否对应某个模块的 adminapi/oapi/config.yaml，以及模块是否在 modules.go 的 adminRegistrars 里", w)
		}
	}
	for _, g := range got {
		if !slices.Contains(want, g) {
			t.Errorf("后台注册了路由 %s，但 api/admin.yaml 里没有：后台路由必须先写进 admin.yaml，再由 oapi-codegen 生成注册代码", g)
		}
	}
}

// 后台的权限检查按 x-permission 放行，漏写或写错的操作会被一律拒绝或被错误放行。
func TestAdminSpec_EveryOperationHasValidPermission(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "api", "admin.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range permissionProblems(t, data) {
		t.Error(p)
	}
}

// x-permission 只属于后台：前台的授权按资源归属写在 Service 里。
func TestOpenAPISpec_HasNoPermission(t *testing.T) {
	for path, ops := range readSpec(t, "openapi.yaml").Paths {
		for method, node := range ops {
			var op struct {
				Permission *string `yaml:"x-permission"`
			}
			if isHTTPMethod(method) && node.Decode(&op) == nil && op.Permission != nil {
				t.Errorf("api/openapi.yaml 的 %s %s 写了 x-permission：它只用于 api/admin.yaml", strings.ToUpper(method), path)
			}
		}
	}
}

// permissionCode 是 <资源>:<动作>，如 product:update。
var permissionCode = regexp.MustCompile(`^[a-z][a-z0-9_]*:[a-z][a-z0-9_]*$`)

// publicOperations 是唯一允许 x-permission: public（不需要登录）的操作。
var publicOperations = []string{"staffLogin"}

func permissionProblems(t *testing.T, data []byte) []string {
	t.Helper()
	var s spec
	if err := yaml.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	var problems []string
	for path, ops := range s.Paths {
		for method, node := range ops {
			if !isHTTPMethod(method) {
				continue
			}
			var op struct {
				OperationID string  `yaml:"operationId"`
				Permission  *string `yaml:"x-permission"`
			}
			if err := node.Decode(&op); err != nil {
				t.Fatal(err)
			}
			where := strings.ToUpper(method) + " " + path
			switch {
			case op.Permission == nil:
				problems = append(problems, fmt.Sprintf("api/admin.yaml 的 %s 没有声明 x-permission：每个后台操作都要写权限码（<资源>:<动作>）或 public/authenticated/superadmin", where))
			case *op.Permission == "public":
				if !slices.Contains(publicOperations, op.OperationID) {
					problems = append(problems, fmt.Sprintf("api/admin.yaml 的 %s（%s）声明了 x-permission: public：只有员工登录（%v）可以不登录访问", where, op.OperationID, publicOperations))
				}
			case *op.Permission == "authenticated", *op.Permission == "superadmin":
			case !permissionCode.MatchString(*op.Permission):
				problems = append(problems, fmt.Sprintf("api/admin.yaml 的 %s 的 x-permission %q 不合法：格式是 <资源>:<动作>（小写字母、数字、下划线），或 public/authenticated/superadmin", where, *op.Permission))
			}
		}
	}
	return problems
}

func TestPermissionProblems_ReportsEachMistake(t *testing.T) {
	data := []byte(`
paths:
  /things:
    parameters: []
    get:
      operationId: listThings
      x-permission: thing:read
    post:
      operationId: createThing
  /things/{id}:
    put:
      operationId: updateThing
      x-permission: Thing.Update
    delete:
      operationId: deleteThing
      x-permission: public
  /auth/login:
    post:
      operationId: staffLogin
      x-permission: public
  /me:
    get:
      operationId: getMe
      x-permission: authenticated
`)
	got := strings.Join(permissionProblems(t, data), "\n")
	for _, want := range []string{"POST /things 没有声明", `"Thing.Update" 不合法`, "DELETE /things/{id}（deleteThing）声明了 x-permission: public"} {
		if !strings.Contains(got, want) {
			t.Errorf("problems 缺少 %q，got:\n%s", want, got)
		}
	}
	for _, ok := range []string{"GET /things ", "/auth/login", "GET /me"} {
		if strings.Contains(got, ok) {
			t.Errorf("合法的操作 %s 被报告了：\n%s", ok, got)
		}
	}
}

func readSpec(t *testing.T, name string) spec {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "api", name))
	if err != nil {
		t.Fatal(err)
	}
	var s spec
	if err := yaml.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func isHTTPMethod(m string) bool {
	switch m {
	case "get", "post", "put", "patch", "delete":
		return true
	}
	return false
}

// 事件入队后如果没有 worker 消费，任务会一直堆在队列里。
func TestWorkers_EveryEventKindHasWorker(t *testing.T) {
	kinds := newWorkers(buildModules(newZeroInfra(), testModuleConfigs(t))).Kinds()

	for _, kind := range eventKinds(t) {
		if !slices.Contains(kinds, kind) {
			t.Errorf("事件 %q 没有对应的 worker：在消费它的模块的 RegisterJobs 里 queue.AddWorker，并把该模块加进 modules.go 的 jobRegistrars", kind)
		}
	}
}

// eventKinds 从 internal/modules/*/events 的源码里找出所有 Kind() 的返回值，
// 支持 return "x" 和 return 同包字符串常量两种写法。
func eventKinds(t *testing.T) []string {
	t.Helper()
	var kinds []string
	for _, m := range moduleDirs(t) {
		dir := filepath.Join(repoRoot(t), "internal", "modules", m, "events")
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		consts := map[string]string{}
		var returns []ast.Expr
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						vs, ok := spec.(*ast.ValueSpec)
						if !ok || d.Tok != token.CONST {
							continue
						}
						for i, name := range vs.Names {
							if i < len(vs.Values) {
								if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
									consts[name.Name], _ = strconv.Unquote(lit.Value)
								}
							}
						}
					}
				case *ast.FuncDecl:
					if d.Recv != nil && d.Name.Name == "Kind" && d.Body != nil && len(d.Body.List) == 1 {
						if ret, ok := d.Body.List[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
							returns = append(returns, ret.Results[0])
						}
					}
				}
			}
		}
		for _, r := range returns {
			switch v := r.(type) {
			case *ast.BasicLit:
				s, _ := strconv.Unquote(v.Value)
				kinds = append(kinds, s)
			case *ast.Ident:
				if s, ok := consts[v.Name]; ok {
					kinds = append(kinds, s)
				} else {
					t.Errorf("internal/modules/%s/events 的 Kind() 返回了无法解析的 %s：Kind() 只能返回字符串字面量或同包字符串常量", m, v.Name)
				}
			default:
				t.Errorf("internal/modules/%s/events 的 Kind() 只能返回字符串字面量或同包字符串常量", m)
			}
		}
	}
	return kinds
}
