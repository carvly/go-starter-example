package resource

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"

	"app.local/tools/internal/modinfo"
)

// Repo 是仓库现状：生成之前要检查的冲突，以及推出 migration 序号、go module 路径。
type Repo struct {
	Root  string
	GoMod string
}

// ErrEnv 是环境问题（不在仓库根目录、工作区不干净……），退出码 2。
var ErrEnv = errors.New("环境问题")

// OpenRepo 确认 root 是本仓库的根目录。
func OpenRepo(root string) (*Repo, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !exists(filepath.Join(root, "tools/gen")) {
		return nil, fmt.Errorf("%w：必须在仓库根目录运行（找不到 go.mod 或 tools/gen）", ErrEnv)
	}
	m := regexp.MustCompile(`(?m)^module\s+(\S+)`).FindSubmatch(data)
	if m == nil {
		return nil, fmt.Errorf("%w：go.mod 里没有 module", ErrEnv)
	}
	return &Repo{Root: root, GoMod: string(m[1])}, nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (r *Repo) path(p string) string { return filepath.Join(r.Root, p) }

// NextMigration 是模块内下一个 migration 的序号，如 0008。
func (r *Repo) NextMigration(module string) string {
	files, _ := filepath.Glob(r.path("internal/modules/" + module + "/migrations/*.up.sql"))
	n := 0
	for _, f := range files {
		base := filepath.Base(f)
		if i := strings.IndexByte(base, '_'); i > 0 {
			if v, err := strconv.Atoi(base[:i]); err == nil {
				n = max(n, v)
			}
		}
	}
	return fmt.Sprintf("%04d", n+1)
}

// Check 检查描述文件和仓库现状是否冲突。
func (r *Repo) Check(p *Parsed) []Problem {
	c := &checker{p: p}
	d := &p.D
	if d.Module == "" || d.Resource == "" || (d.Admin == nil && d.Web == nil) {
		return nil
	}
	n := DeriveNames(d)
	if exists(r.path("internal/modules/" + d.Module)) {
		c.add("REPO-MODULE-EXISTS", "module", "模块 "+d.Module+" 已经存在", "只支持新模块：换一个模块名；要在已有模块里加资源，手写")
	}
	if owner := r.tableOwner(n.Table); owner != "" {
		c.add("REPO-TABLE-EXISTS", "resource", "表 "+n.Table+" 已经存在（"+owner+"）", "换一个资源名，或者写 plural 换一个表名；已有的表不经生成器（见 generator.md 的\"什么时候不用\"）")
	}
	spec, err := os.ReadFile(r.path(n.Spec))
	if err == nil {
		for _, msg := range specConflicts(spec, r.specNames(p)) {
			c.add("REPO-SPEC-CONFLICT", "resource", msg, "换一个资源名（或 plural、动作名），让推出的名字和 "+n.Spec+" 里已有的不重复")
		}
	}
	if d.Web != nil && len(d.Web.Auth) > 0 {
		provider, err := r.userAuthModule()
		switch {
		case err != nil:
			c.add("REPO-AUTH-MISSING", "web.auth", "web.auth 需要唯一一个提供前台登录检查的模块："+err.Error(), "只保留一个模块的 func (m *Module) Middleware() echo.MiddlewareFunc")
		case provider == "":
			c.add("REPO-AUTH-MISSING", "web.auth", "web.auth 需要提供前台登录检查的模块（module.go 里有 func (m *Module) Middleware() echo.MiddlewareFunc），但没有找到", "去掉 web.auth，或者先恢复提供前台登录的模块")
		}
	}
	return c.problems
}

// userAuthModule 是提供前台登录检查的模块，从代码推导（见 modinfo.UserAuthProvider）。
func (r *Repo) userAuthModule() (string, error) {
	src := fsSource{root: r.Root}
	files, err := src.Files()
	if err != nil {
		return "", err
	}
	return modinfo.UserAuthProvider(src, modinfo.List(files))
}

// fsSource 读工作区文件，实现 modinfo.Source。
type fsSource struct{ root string }

func (s fsSource) Read(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(name)))
	return string(data), err
}

func (s fsSource) Files() ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(s.root, "internal/modules/*/module.go"))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		rel, err := filepath.Rel(s.root, m)
		if err != nil {
			return nil, err
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}

func (r *Repo) tableOwner(table string) string {
	files, _ := filepath.Glob(r.path("internal/modules/*/postgres/tables.go"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		if bytes.Contains(data, []byte(strconv.Quote(table))) {
			rel, _ := filepath.Rel(r.Root, f)
			return "登记在 " + rel
		}
	}
	re := regexp.MustCompile(`(?i)create\s+table\s+(if\s+not\s+exists\s+)?"?` + regexp.QuoteMeta(table) + `"?[\s(]`)
	migs, _ := filepath.Glob(r.path("internal/modules/*/migrations/*.up.sql"))
	for _, f := range migs {
		data, _ := os.ReadFile(f)
		if re.Match(data) {
			rel, _ := filepath.Rel(r.Root, f)
			return "在 " + rel + " 里创建"
		}
	}
	return ""
}

type specNames struct {
	tag, permission string
	paths, ops      []string
	schemas         []string
}

func (r *Repo) specNames(p *Parsed) specNames {
	m := BuildModel(&p.D, r.GoMod, "0000")
	n := m.N
	s := specNames{tag: n.Tag, permission: n.Permission + ":", paths: []string{n.Path, n.Path + "/{id}"},
		ops: []string{n.OpList, n.OpGet, n.OpCreate}, schemas: []string{n.Type, n.Type + "Page", n.Type + "List", "Create" + n.Type + "Request", "Update" + n.Type + "Request"}}
	if m.HasUpdate {
		s.ops = append(s.ops, n.OpUpdate)
	}
	if m.HasDelete {
		s.ops = append(s.ops, n.OpDelete)
	}
	for _, f := range m.Enums {
		s.schemas = append(s.schemas, f.EnumType)
	}
	for _, a := range m.Actions {
		s.paths = append(s.paths, a.Path)
		s.ops = append(s.ops, a.Op)
		s.schemas = append(s.schemas, pascal(a.Name)+n.Type+"Request")
	}
	return s
}

func specConflicts(spec []byte, want specNames) []string {
	var doc struct {
		Tags []struct {
			Name string `yaml:"name"`
		} `yaml:"tags"`
		Paths      map[string]map[string]yaml.Node `yaml:"paths"`
		Components struct {
			Schemas map[string]yaml.Node `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(spec, &doc); err != nil {
		return []string{"spec 解析失败：" + err.Error()}
	}
	var out []string
	for _, t := range doc.Tags {
		if t.Name == want.tag {
			out = append(out, "tag "+want.tag+" 已经存在")
		}
	}
	for p, ops := range doc.Paths {
		if slices.Contains(want.paths, p) {
			out = append(out, "路径 "+p+" 已经存在")
		}
		for _, node := range ops {
			var op struct {
				ID   string `yaml:"operationId"`
				Perm string `yaml:"x-permission"`
			}
			_ = node.Decode(&op)
			if slices.Contains(want.ops, op.ID) {
				out = append(out, "operationId "+op.ID+" 已经存在")
			}
			if want.permission != "" && strings.HasPrefix(op.Perm, want.permission) {
				out = append(out, "权限码前缀 "+want.permission+" 已经被 "+op.Perm+" 使用")
			}
		}
	}
	for _, s := range want.schemas {
		if _, ok := doc.Components.Schemas[s]; ok {
			out = append(out, "schema "+s+" 已经存在")
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// CheckWorkspace 检查要新建的路径下没有任何文件（已跟踪或未跟踪）：回退时会整个删掉这些路径，不能误删别的。
// 其它文件有没有未提交的改动不影响——被插入的共享文件在写入前留了原样，回退按原样还原。
func (r *Repo) CheckWorkspace(paths []string) error {
	args := append([]string{"status", "--porcelain", "--untracked-files=all", "--"}, paths...)
	out, err := r.git(args...)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrEnv, err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("%w：要写的路径下已经有文件，先移走或删掉：\n%s", ErrEnv, out)
	}
	for _, p := range paths {
		if exists(r.path(p)) {
			return fmt.Errorf("%w：%s 已经存在，先移走或删掉", ErrEnv, p)
		}
	}
	return nil
}

func (r *Repo) git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s：%v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}
