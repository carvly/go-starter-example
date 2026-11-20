package resource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"app.local/tools/internal/specmap"
)

// Plan 是生成之前的完整规划：推出的名字、要新建的文件、要插入的位置。写入之前先算好，任何
// 一步不成立就整体放弃，一个文件也不写。
type Plan struct {
	Names  Names     `json:"names"`
	Create []string  `json:"create"`
	Insert []Insert  `json:"insert"`
	Hints  []Problem `json:"hints,omitempty"`

	files []File
	model *Model
}

// Insert 是往共享文件里插入的一段内容：插在第 After 行之后（spec），或者按 modules.go 的约定
// 插在各自的位置（After 为 0）。
type Insert struct {
	File    string `json:"file"`
	Section string `json:"section"`
	After   int    `json:"after_line,omitempty"`
	Text    string `json:"text"`
}

// Todo 是生成之后 AI 要完成的一项。
type Todo struct {
	Kind        string `json:"kind"`
	File        string `json:"file"`
	Symbol      string `json:"symbol"`
	Instruction string `json:"instruction"`
	Test        string `json:"test,omitempty"`
	Reference   string `json:"reference,omitempty"`
}

// Result 是 apply 的结果。
type Result struct {
	*Plan
	Todo   []Todo `json:"todo"`
	Revert string `json:"revert"`
}

// MakePlan 渲染全部文件并算出插入位置（不写文件）。
func (r *Repo) MakePlan(p *Parsed, hints []Problem) (*Plan, error) {
	m := BuildModel(&p.D, r.GoMod, r.NextMigration(p.D.Module))
	files, err := Render(m)
	if err != nil {
		return nil, err
	}
	pl := &Plan{Names: m.N, Hints: hints, files: files, model: m}
	for _, f := range files {
		if exists(r.path(f.Path)) {
			return nil, fmt.Errorf("%w：%s 已经存在，一个文件也没有写", ErrEnv, f.Path)
		}
		pl.Create = append(pl.Create, f.Path)
	}
	if m.Admin {
		// 同名的手写路由文件（如 routes/_authed/catalog.tsx）会让生成的 <tag>/index.tsx 变成它的子路由，
		// 页面渲染不出来，要到运行时才发现。
		if f := AdminDir + "src/routes/_authed/" + m.Feature() + ".tsx"; exists(r.path(f)) {
			return nil, fmt.Errorf("%w：已经有路由文件 %s，和生成的 %s/ 冲突：换一个资源名（或 plural），一个文件也没有写", ErrEnv, f, m.Feature())
		}
	}
	tag, paths, schemas, err := SpecParts(m)
	if err != nil {
		return nil, err
	}
	spec, err := os.ReadFile(r.path(m.N.Spec))
	if err != nil {
		return nil, err
	}
	at, err := specSections(spec)
	if err != nil {
		return nil, fmt.Errorf("%s：%w", m.N.Spec, err)
	}
	pl.Insert = append(pl.Insert,
		Insert{File: m.N.Spec, Section: "tags", After: at["tags"], Text: tag},
		Insert{File: m.N.Spec, Section: "paths", After: at["paths"], Text: paths},
		Insert{File: m.N.Spec, Section: "components.schemas", After: at["schemas"], Text: schemas},
	)
	if len(m.WebAuth) > 0 {
		provider, err := r.userAuthModule()
		if err != nil || provider == "" {
			return nil, fmt.Errorf("%w：web.auth 需要提供前台登录检查的模块（先跑 check 看 REPO-AUTH-MISSING）", ErrEnv)
		}
		m.WebAuthModule = provider
	}
	pl.Insert = append(pl.Insert, r.modulesInserts(m)...)
	return pl, nil
}

// specSections 返回 tags、paths、components.schemas 三段各自最后一行内容的行号：新内容插在它
// 们后面（跳过段末的空行和注释，注释通常属于下一段）。
func specSections(spec []byte) (map[string]int, error) {
	doc, err := specmap.Parse(spec)
	if err != nil {
		return nil, err
	}
	for _, k := range []string{"tags", "paths", "schemas"} {
		if doc.SectionEnd[k] == 0 {
			return nil, fmt.Errorf("找不到 %s 段，无法插入", k)
		}
	}
	return doc.SectionEnd, nil
}

// modulesInserts 是 internal/app/modules.go 里按硬规则 3 的固定几行。
func (r *Repo) modulesInserts(m *Model) []Insert {
	n := m.N
	deps := "DB: in.DBTx"
	if len(m.WebAuth) > 0 {
		deps += ", Auth: " + m.WebAuthModule + "M.Middleware()"
	}
	reg, list := "m."+n.ModuleField+".RegisterAdminHTTP,", "adminRegistrars"
	if !m.Admin {
		reg, list = "m."+n.ModuleField+".RegisterHTTP,", "httpRegistrars"
	}
	const f = "internal/app/modules.go"
	return []Insert{
		{File: f, Section: "import", Text: fmt.Sprintf("\t%q", r.GoMod+"/internal/modules/"+n.Module)},
		{File: f, Section: "Modules", Text: fmt.Sprintf("\t%s *%s.Module", n.ModuleField, n.Module)},
		{File: f, Section: "buildModules", Text: fmt.Sprintf("\t%s := %s.New(%s.Deps{%s})", n.ModuleVar, n.Module, n.Module, deps)},
		{File: f, Section: "return", Text: fmt.Sprintf("\t\t%s: %s,", n.ModuleField, n.ModuleVar)},
		{File: f, Section: list, Text: "\t\t" + reg},
		{File: "internal/app/migrate.go", Section: "import", Text: fmt.Sprintf("\t%q", r.GoMod+"/internal/modules/"+n.Module)},
		{File: "internal/app/migrate.go", Section: "migrationSources", Text: fmt.Sprintf("\t\t%s.Migrations,", n.Module)},
	}
}

// insertModules 按 modules.go 的约定找到每一行的位置并插入，然后 gofmt。
func insertModules(src string, ins []Insert) (string, error) {
	lines := strings.Split(src, "\n")
	find := func(section string) (int, error) {
		last := func(re *regexp.Regexp, from, to int) int {
			at := -1
			for i := from; i < to && i < len(lines); i++ {
				if re.MatchString(lines[i]) {
					at = i
				}
			}
			return at
		}
		block := func(open string) (int, int) { // open 所在行，以及它的闭合行
			for i, l := range lines {
				if strings.HasPrefix(strings.TrimSpace(l), open) {
					indent := len(l) - len(strings.TrimLeft(l, "\t"))
					for j := i + 1; j < len(lines); j++ {
						if lines[j] == strings.Repeat("\t", indent)+"}" {
							return i, j
						}
					}
				}
			}
			return -1, -1
		}
		switch section {
		case "import":
			if i := last(regexp.MustCompile(`^\t".*/internal/modules/`), 0, len(lines)); i >= 0 {
				return i + 1, nil
			}
		case "Modules":
			if _, j := block("type Modules struct {"); j >= 0 {
				return j, nil
			}
		case "buildModules":
			if i := last(regexp.MustCompile(`^\t\w+M := `), 0, len(lines)); i >= 0 {
				return i + 1, nil
			}
		case "return":
			if _, j := block("return &Modules{"); j >= 0 {
				return j, nil
			}
		case "migrationSources":
			if _, j := block("func migrationSources()"); j >= 0 {
				return j - 1, nil
			}
		default: // 注册切片
			if s, e := block("func (m *Modules) " + section + "()"); s >= 0 {
				for k := e - 1; k > s; k-- {
					if lines[k] == "\t}" {
						return k, nil
					}
				}
			}
		}
		return -1, fmt.Errorf("modules.go 里找不到 %s 的位置：它不再符合硬规则 3 的约定", section)
	}
	for _, in := range ins {
		at, err := find(in.Section)
		if err != nil {
			return "", err
		}
		lines = slices.Insert(lines, at, in.Text)
	}
	out, err := format.Source([]byte(strings.Join(lines, "\n")))
	if err != nil {
		return "", fmt.Errorf("插入后 modules.go 不能编译：%w", err)
	}
	return string(out), nil
}

// insertLines 把多段文本按行号插入（从后往前插，行号不会错位）。
func insertLines(src string, ins []Insert) string {
	lines := strings.Split(src, "\n")
	sorted := slices.Clone(ins)
	slices.SortStableFunc(sorted, func(a, b Insert) int { return b.After - a.After })
	for _, in := range sorted {
		text := strings.Split(strings.TrimRight(in.Text, "\n"), "\n")
		lines = slices.Insert(lines, in.After, text...)
	}
	return strings.Join(lines, "\n")
}

// Apply 写入文件和插入内容，然后跑生成和基本检查，最后留下快照。
func (r *Repo) Apply(pl *Plan, descriptor []byte, log func(string)) (*Result, error) {
	m := pl.model
	if err := r.CheckWorkspace(createdRoots(pl)); err != nil {
		return nil, err
	}
	res := &Result{Plan: pl, Revert: "go run ./tools/gen revert <描述文件>"}
	// 先算好所有共享文件的新内容，全部成功再写。
	shared := map[string]string{}
	var specIns []Insert
	goIns := map[string][]Insert{}
	for _, in := range pl.Insert {
		if in.File == m.N.Spec {
			specIns = append(specIns, in)
		} else {
			goIns[in.File] = append(goIns[in.File], in)
		}
	}
	spec, err := os.ReadFile(r.path(m.N.Spec))
	if err != nil {
		return nil, err
	}
	shared[m.N.Spec] = insertLines(string(spec), specIns)
	for file, inserts := range goIns {
		content, err := os.ReadFile(r.path(file))
		if err != nil {
			return nil, err
		}
		if shared[file], err = insertModules(string(content), inserts); err != nil {
			return nil, err
		}
	}
	// 写入前留一份原样：revert 按它还原，生成前已有的未提交改动不会丢。
	if err := r.saveBefore(pl, slices.Sorted(maps.Keys(shared))); err != nil {
		return nil, err
	}
	for _, f := range pl.files {
		if err := os.MkdirAll(filepath.Dir(r.path(f.Path)), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(r.path(f.Path), []byte(f.Content), 0o644); err != nil {
			return nil, err
		}
	}
	for p, content := range shared {
		if err := os.WriteFile(r.path(p), []byte(content), 0o644); err != nil {
			return nil, err
		}
	}
	if err := r.snapshot(pl, descriptor); err != nil {
		return nil, err
	}
	steps := [][]string{
		{"go", "generate", "./..."},
		{"go", "build", "./..."},
		{"go", "test", "./internal/archtest/", "./internal/app/"},
	}
	for _, s := range steps {
		log("$ " + strings.Join(s, " "))
		if out, err := r.run(s...); err != nil {
			return res, fmt.Errorf("%s 失败（文件已经写入，回退：%s）：\n%s", strings.Join(s, " "), res.Revert, out)
		}
	}
	if m.Admin {
		if exists(r.path(AdminDir + "node_modules")) {
			if err := r.frontend(pl, descriptor, log); err != nil {
				return res, fmt.Errorf("%w（文件已经写入，回退：%s）", err, res.Revert)
			}
		} else {
			res.Todo = append(res.Todo, Todo{Kind: "frontend", File: AdminDir + "src/api/schema.d.ts", Instruction: "web/admin 还没有 pnpm install，没能重新生成前端的接口类型和路由树、格式化生成的页面：cd web/admin && pnpm install && pnpm gen:api && pnpm gen:routes && pnpm format，再 pnpm typecheck"})
		}
	}
	res.Todo = append(res.Todo, todos(m)...)
	if err := r.recordApplied(pl.Names.Resource); err != nil {
		return res, err
	}
	return res, nil
}

// recordApplied 在 apply 成功结束时记下要还原的文件的内容摘要：之后这些文件再被改过（手改、又生成了别的资源），
// revert 会拒绝整文件还原，不悄悄抹掉后来的改动。
func (r *Repo) recordApplied(resource string) error {
	path := filepath.Join(BeforeDir(r.Root, resource), "manifest.json")
	m, err := readManifest(path)
	if err != nil {
		return err
	}
	m.Applied = map[string]string{}
	for _, p := range m.Restore {
		data, err := os.ReadFile(r.path(p))
		if err != nil {
			return err
		}
		m.Applied[p] = secrandHash(data)
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func readManifest(path string) (revertManifest, error) {
	var m revertManifest
	data, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(data, &m)
}

func secrandHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// frontend 重新生成接口类型和路由树，格式化生成的页面（快照随之更新为格式化后的内容），再做类型检查。
func (r *Repo) frontend(pl *Plan, descriptor []byte, log func(string)) error {
	var ts []string
	for _, f := range pl.files {
		if rel, ok := strings.CutPrefix(f.Path, AdminDir); ok {
			ts = append(ts, rel)
		}
	}
	steps := [][]string{
		{"pnpm", "gen:api"},
		{"pnpm", "gen:routes"},
		append([]string{"pnpm", "exec", "oxfmt"}, ts...),
	}
	for _, s := range steps {
		log("$ " + strings.Join(s, " ") + "（web/admin）")
		if out, err := r.runIn(AdminDir, s...); err != nil {
			return fmt.Errorf("%s 失败：\n%s", strings.Join(s, " "), out)
		}
	}
	for i, f := range pl.files {
		if strings.HasPrefix(f.Path, AdminDir) {
			data, err := os.ReadFile(r.path(f.Path))
			if err != nil {
				return err
			}
			pl.files[i].Content = string(data)
		}
	}
	if err := r.snapshot(pl, descriptor); err != nil {
		return err
	}
	log("$ pnpm typecheck（web/admin）")
	if out, err := r.runIn(AdminDir, "pnpm", "typecheck"); err != nil {
		return fmt.Errorf("pnpm typecheck 失败：生成的页面有类型错误，是生成器的 bug：\n%s", out)
	}
	return nil
}

// createdRoots 是这次新建的目录和文件：工作区检查只看它们下面有没有未跟踪的文件，回退时整个删掉。
func createdRoots(pl *Plan) []string {
	roots := []string{"internal/modules/" + pl.Names.Module}
	if pl.model.Admin {
		roots = append(roots, AdminDir+"src/features/"+pl.model.Feature(), AdminDir+"src/routes/_authed/"+pl.model.Feature())
	}
	return roots
}

func (r *Repo) runIn(dir string, args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = r.path(dir)
	// 生成器不装依赖：pnpm 在运行脚本前检查依赖是否最新，不是最新时会自动安装。
	cmd.Env = append(os.Environ(), "pnpm_config_verify_deps_before_run=false")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *Repo) run(args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = r.Root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// revertManifest 记下一次生成新建了哪些路径、改了哪些已有文件（原样存在 BeforeDir 下）。
type revertManifest struct {
	Created []string `json:"created"`
	Restore []string `json:"restore"`
	// Applied 是 apply 成功结束时每个要还原的文件的内容摘要。为空表示 apply 没有走完，revert 无条件还原。
	Applied map[string]string `json:"applied,omitempty"`
}

// BeforeDir 是一次生成写入前的原样：被插入的共享文件和会被重新生成的派生文件。
func BeforeDir(root, resource string) string {
	return filepath.Join(root, ".gen", resource, "before")
}

// saveBefore 保存将被修改的已有文件的原样。派生文件（meta.gen.ts、schema.d.ts、routeTree.gen.ts）也存：
// 还原原样比重新生成可靠，不依赖 web/admin 装没装依赖。
func (r *Repo) saveBefore(pl *Plan, shared []string) error {
	restore := slices.Clone(shared)
	if pl.model.Admin {
		restore = append(restore, AdminDir+"src/api/meta.gen.ts", AdminDir+"src/api/schema.d.ts", AdminDir+"src/routeTree.gen.ts")
	}
	dir := BeforeDir(r.Root, pl.Names.Resource)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	var kept []string
	for _, p := range restore {
		data, err := os.ReadFile(r.path(p))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, "files", p)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
		kept = append(kept, p)
	}
	m, err := json.MarshalIndent(revertManifest{Created: createdRoots(pl), Restore: kept}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), m, 0o644)
}

// Revert 精确回退一次生成：删掉新建的路径，把改过的已有文件还原成生成前的原样，然后删掉这次的 .gen 记录。
// 生成之后又手改了共享文件的话，那些改动会随还原丢失。不用 git checkout / git clean：它们会连带丢掉或删掉
// 与这次生成无关的改动和文件。
func (r *Repo) Revert(resource string) ([]string, error) {
	dir := BeforeDir(r.Root, resource)
	m, err := readManifest(filepath.Join(dir, "manifest.json"))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w：没有 %s 的生成记录（%s），无法精确回退", ErrEnv, resource, dir)
	}
	if err != nil {
		return nil, err
	}
	var changed []string
	for p, want := range m.Applied {
		data, err := os.ReadFile(r.path(p))
		if err != nil || secrandHash(data) != want {
			changed = append(changed, p)
		}
	}
	if len(changed) > 0 {
		slices.Sort(changed)
		return nil, fmt.Errorf("%w：生成之后这些文件又被改过（手改，或又生成了别的资源），整文件还原会抹掉那些改动，没有回退任何东西：\n  %s\n先撤掉后来的改动（或后来生成的资源），再回退；也可以按 .gen/%s/before/ 手工还原",
			ErrEnv, strings.Join(changed, "\n  "), resource)
	}
	var done []string
	for _, p := range m.Created {
		if err := os.RemoveAll(r.path(p)); err != nil {
			return done, err
		}
		done = append(done, "删除 "+p)
	}
	for _, p := range m.Restore {
		orig, err := os.ReadFile(filepath.Join(dir, "files", p))
		if err != nil {
			return done, err
		}
		if err := os.WriteFile(r.path(p), orig, 0o644); err != nil {
			return done, err
		}
		done = append(done, "还原 "+p)
	}
	return done, os.RemoveAll(filepath.Join(r.Root, ".gen", resource))
}

// SnapshotDir 是一次生成的快照目录：gen drift 拿它和现在的代码比较。
func SnapshotDir(root, resource string) string {
	return filepath.Join(root, ".gen", resource, "snapshot")
}

func (r *Repo) snapshot(pl *Plan, descriptor []byte) error {
	dir := SnapshotDir(r.Root, pl.Names.Resource)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	for _, f := range pl.files {
		p := filepath.Join(dir, "files", f.Path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			return err
		}
	}
	meta, _ := json.MarshalIndent(pl, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "plan.json"), meta, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "descriptor.yaml"), descriptor, 0o644)
}

const customReference = "条件和更新写在同一条 UPDATE 里，没更新到行时再查一次区分不存在和条件不满足；Repository 接口、Postgres 实现、内存 fake 和契约用例一起加"

func todos(m *Model) []Todo {
	var out []Todo
	svc := "internal/modules/" + m.N.Module + "/domain/service.go"
	test := "internal/modules/" + m.N.Module + "/domain/service_test.go"
	for _, a := range m.Actions {
		if a.Declarative {
			continue
		}
		ins := a.Custom
		if len(a.Errors) > 0 {
			var es []string
			for _, e := range a.Errors {
				es = append(es, e.Var+"（"+e.Code+"）")
			}
			ins += "。会返回的错误：" + strings.Join(es, "、")
		}
		if len(a.Whens) > 0 {
			ins += "。前置条件写进同一条 SQL，不满足时返回 ErrInvalidState"
		}
		out = append(out, Todo{Kind: "custom_action", File: svc, Symbol: "(*Service)." + a.Go, Instruction: ins,
			Test: test + ": Test" + m.N.Type + "_" + a.Go + "_NotImplemented", Reference: customReference})
	}
	for _, r := range m.Rules {
		sym := "(*Service)." + r.Symbol()
		ins := r.Custom
		if len(r.Errors) > 0 {
			var es []string
			for _, e := range r.Errors {
				es = append(es, e.Var+"（"+e.Code+"）")
			}
			ins += "。会返回的错误：" + strings.Join(es, "、")
		}
		out = append(out, Todo{Kind: "rule", File: svc, Symbol: sym, Instruction: ins,
			Test:      fmt.Sprintf("%s: Test%s_Rule%d_NotImplemented", test, m.N.Type, r.N),
			Reference: "需要和写入原子完成的检查（如删除时没有引用）放进同一个事务并加行锁，见 docs/agents/transactions.md"})
	}
	return out
}

// ReadSnapshotPlan 读出快照里的规划（gen drift 用）。
func ReadSnapshotPlan(root, resource string) (*Plan, error) {
	data, err := os.ReadFile(filepath.Join(SnapshotDir(root, resource), "plan.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w：没有 %s 的快照（.gen/%s/snapshot/）：只有在这台机器上 gen apply 过才有；跨机器审核时用 tools/gen/testdata 里的描述文件在临时副本里重新生成", ErrEnv, resource, resource)
	}
	if err != nil {
		return nil, err
	}
	var pl Plan
	return &pl, json.Unmarshal(data, &pl)
}
