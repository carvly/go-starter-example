// Package modinfo 从代码和 module.yaml 推导模块信息，供 tools/starter 与 tools/gen 共用。
// 能从代码推导的事实（谁提供后台鉴权、谁提供前台登录检查）直接解析 Go 源码，不另写声明。
package modinfo

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	pathpkg "path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Source 读取仓库文件（工作区或某个 git 版本）。
type Source interface {
	Read(name string) (string, error)
	Files() ([]string, error)
}

// ModulesGo 是组合根里模块清单所在的文件。
const ModulesGo = "internal/app/modules.go"

// List 是 internal/modules/<m>/module.go 存在的模块名，已排序。
func List(files []string) []string {
	var out []string
	for _, name := range files {
		parts := strings.Split(name, "/")
		if len(parts) == 4 && parts[0] == "internal" && parts[1] == "modules" && parts[3] == "module.go" {
			out = append(out, parts[2])
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Manifest 是 internal/modules/<m>/module.yaml：模块拥有的、无法从 Go 代码推导的文件。
type Manifest struct {
	WebPaths []string `yaml:"web_paths"`
}

// ReadManifest 读取并校验模块的 module.yaml。
func ReadManifest(src Source, module string) (Manifest, error) {
	data, err := src.Read("internal/modules/" + module + "/module.yaml")
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := yaml.Unmarshal([]byte(data), &m); err != nil {
		return Manifest{}, fmt.Errorf("%s/module.yaml: %w", module, err)
	}
	for _, p := range m.WebPaths {
		if pathpkg.Clean(p) != p || !strings.HasPrefix(p, "web/admin/") {
			return Manifest{}, fmt.Errorf("%s/module.yaml: invalid web path %q", module, p)
		}
	}
	return m, nil
}

// AdminAuthProviders 是 modules.go 的 adminAuth() 里引用到的模块：删掉它们，后台接入面整体去掉。
func AdminAuthProviders(modulesGo string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), ModulesGo, modulesGo, 0)
	if err != nil {
		return nil, err
	}
	fields := moduleFields(f)
	var out []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "adminAuth" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			// m.<Field>.X()：取 <Field>
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			inner, ok := sel.X.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if module, ok := fields[inner.Sel.Name]; ok && !slices.Contains(out, module) {
				out = append(out, module)
			}
			return true
		})
	}
	slices.Sort(out)
	return out, nil
}

// moduleFields 是 Modules 结构体的字段名到模块包名的映射（Staff *staff.Module → staff）。
func moduleFields(f *ast.File) map[string]string {
	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "Modules" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, field := range st.Fields.List {
			star, ok := field.Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			sel, ok := star.X.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Module" {
				continue
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				continue
			}
			for _, name := range field.Names {
				out[name.Name] = pkg.Name
			}
		}
		return false
	})
	return out
}

// UserAuthProvider 是提供前台登录检查的模块：module.go 里有 func (m *Module) Middleware() echo.MiddlewareFunc。
// 没有时返回空串；多于一个时报错（生成的代码不知道该用哪个）。
func UserAuthProvider(src Source, modules []string) (string, error) {
	var found []string
	for _, module := range modules {
		data, err := src.Read("internal/modules/" + module + "/module.go")
		if err != nil {
			return "", err
		}
		f, err := parser.ParseFile(token.NewFileSet(), module+"/module.go", data, 0)
		if err != nil {
			return "", err
		}
		if hasUserAuthMiddleware(f) {
			found = append(found, module)
		}
	}
	switch len(found) {
	case 0:
		return "", nil
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("多个模块提供前台登录检查 Middleware()：%s", strings.Join(found, ", "))
	}
}

func hasUserAuthMiddleware(f *ast.File) bool {
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "Middleware" || len(fn.Recv.List) != 1 {
			continue
		}
		star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		if id, ok := star.X.(*ast.Ident); !ok || id.Name != "Module" {
			continue
		}
		if fn.Type.Params.NumFields() != 0 || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
			continue
		}
		if sel, ok := fn.Type.Results.List[0].Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "MiddlewareFunc" {
			return true
		}
	}
	return false
}
