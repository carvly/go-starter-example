package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// 表归属检查：每个模块在 internal/modules/<m>/postgres/tables.go 的 Tables 里登记自己拥有的
// 表，只有它能读写这些表，表之间也不能有跨模块外键。这样模块随时可以拆到独立的数据库，
// 拆分时不会发现某条 SQL 早就 JOIN 了别人的表。
//
// 静态分析只能看见写在字符串字面量、pgx.Identifier{...} 和 tableNameArgs 登记的辅助函数参数
// 里的表名：用变量拼出来的 SQL 查不到；辅助函数的表名参数不是字符串字面量时直接报错。

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

const identPattern = `((?:"?[A-Za-z_][A-Za-z0-9_]*"?\s*\.\s*)?"?[A-Za-z_][A-Za-z0-9_]*"?)`

var (
	// 字符串、行注释、块注释在同一次从左到右的扫描里识别：谁先出现就按谁处理，
	// 字符串里的 -- 或 /* 不会被当成注释，注释里的引号也不会被当成字符串。
	sqlNoiseRe = regexp.MustCompile(`'(?:[^']|'')*'|--[^\n]*|(?s:/\*.*?\*/)`)

	createTableRe = regexp.MustCompile(`(?i)\bCREATE\s+(?:(?:GLOBAL|LOCAL)\s+)?(?:(?:UNLOGGED|TEMP|TEMPORARY)\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?` + identPattern)
	alterTableRe  = regexp.MustCompile(`(?i)\bALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?` + identPattern + `(?:\s+RENAME\s+TO\s+` + identPattern + `)?`)
	dropTableRe   = regexp.MustCompile(`(?i)\bDROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(` + identPattern + `(?:\s*,\s*` + identPattern + `)*)`)
	referencesRe  = regexp.MustCompile(`(?i)\bREFERENCES\s+` + identPattern)
	statementEnd  = regexp.MustCompile(`;`)

	sqlStatementRe = regexp.MustCompile(`(?is)\b(?:select\b.*\bfrom|insert\s+into|update\b.*\bset|delete\s+from|truncate|merge\s+into` +
		`|copy\s+[\w."]+(?:\s*\([^)]*\))?\s+(?:from|to)|lock\s+table|(?:alter|drop)\s+table)\b`)
	identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
)

// tableNameArgs 是 internal/platform 里把表名作为单独参数传入的辅助函数：函数名 → 表名参数的
// 下标。按函数名匹配，不看包名；新增这类辅助函数时登记在这里。
var tableNameArgs = map[string]int{
	"ListPage": 2, // platformpg.ListPage(ctx, q, table, columns, ...)
}

// normalizeIdent 去掉 schema 前缀和引号：public."Betas" → betas。建表规范要求表名是
// 小写 snake_case，所以不区分带引号的大小写。
func normalizeIdent(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(strings.Trim(strings.TrimSpace(s), `"`))
}

func stripSQLNoise(sql string) string {
	return sqlNoiseRe.ReplaceAllStringFunc(sql, func(m string) string {
		if m[0] == '\'' {
			return "''"
		}
		return " "
	})
}

type migrationFile struct {
	name    string
	content string
}

type foreignKey struct {
	file     string
	from, to string
}

// parseMigrations 按顺序回放全部 up 迁移，得到最终存在的表和全部外键引用。
func parseMigrations(files []migrationFile) (tables map[string]bool, fks []foreignKey) {
	tables = map[string]bool{}
	type event struct {
		pos  int
		kind string
		m    []string
	}
	for _, f := range files {
		sql := stripSQLNoise(f.content)
		var events []event
		for kind, re := range map[string]*regexp.Regexp{
			"create": createTableRe, "alter": alterTableRe, "drop": dropTableRe, "references": referencesRe, "end": statementEnd,
		} {
			for _, idx := range re.FindAllStringSubmatchIndex(sql, -1) {
				m := make([]string, len(idx)/2)
				for i := range m {
					if idx[2*i] >= 0 {
						m[i] = sql[idx[2*i]:idx[2*i+1]]
					}
				}
				events = append(events, event{idx[0], kind, m})
			}
		}
		slices.SortFunc(events, func(a, b event) int { return a.pos - b.pos })

		// REFERENCES 归属于同一条语句里的 CREATE/ALTER TABLE；语句结束就清空，认不出的
		// 语句里的 REFERENCES 宁可不归属，也不记到上一张表名下。
		current := ""
		for _, e := range events {
			switch e.kind {
			case "create":
				current = normalizeIdent(e.m[1])
				tables[current] = true
			case "alter":
				current = normalizeIdent(e.m[1])
				if e.m[2] != "" {
					delete(tables, current)
					current = normalizeIdent(e.m[2])
					tables[current] = true
				}
			case "drop":
				for name := range strings.SplitSeq(e.m[1], ",") {
					delete(tables, normalizeIdent(name))
				}
				current = ""
			case "end":
				current = ""
			case "references":
				if current != "" {
					fks = append(fks, foreignKey{f.name, current, normalizeIdent(e.m[1])})
				}
			}
		}
	}
	return tables, fks
}

func loadMigrations() ([]migrationFile, error) {
	var names []string
	for _, pattern := range []string{
		filepath.Join(repoRoot(), "internal", "platform", "queue", "migrations", "*.up.sql"),
		filepath.Join(repoRoot(), "internal", "modules", "*", "migrations", "*.up.sql"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		names = append(names, matches...)
	}
	slices.Sort(names)
	var files []migrationFile
	for _, name := range names {
		b, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(repoRoot(), name)
		if err != nil {
			return nil, err
		}
		files = append(files, migrationFile{filepath.ToSlash(rel), string(b)})
	}
	return files, nil
}

// referencedTables 返回一段 SQL 里出现的已知表名。不是 SQL 语句的字符串（日志、错误文案）
// 直接跳过；SQL 里的字符串常量、注释和 AS 后面的别名不算。
func referencedTables(sql string, known map[string]bool) []string {
	if !sqlStatementRe.MatchString(sql) {
		return nil
	}
	var out []string
	prev := ""
	for _, w := range identRe.FindAllString(stripSQLNoise(sql), -1) {
		w = strings.ToLower(w)
		if prev != "as" && known[w] && !slices.Contains(out, w) {
			out = append(out, w)
		}
		prev = w
	}
	return out
}

// sqlFragment 是源码里可能指向表的一处写法：SQL 字符串，或 pgx.Identifier{...}
// （CopyFrom 等 API 用它传表名，这时 text 就是表名）。
type sqlFragment struct {
	pos        string
	text       string
	identifier bool
	// unresolved 表示 tableNameArgs 里的辅助函数收到的表名不是字符串字面量，静态检查看不出是哪张表。
	unresolved bool
}

func (f sqlFragment) tables(known map[string]bool) []string {
	if f.identifier {
		if name := normalizeIdent(f.text); known[name] {
			return []string{name}
		}
		return nil
	}
	return referencedTables(f.text, known)
}

func sqlFragments(fset *token.FileSet, file *ast.File, root string) []sqlFragment {
	pos := func(p token.Pos) string {
		position := fset.Position(p)
		rel, err := filepath.Rel(root, position.Filename)
		if err != nil {
			rel = position.Filename
		}
		return fmt.Sprintf("%s:%d", filepath.ToSlash(rel), position.Line)
	}
	var out []sqlFragment
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			idx, ok := tableNameArgs[calleeName(n.Fun)]
			if !ok || idx >= len(n.Args) {
				return true
			}
			if lit, ok := n.Args[idx].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil {
					out = append(out, sqlFragment{pos: pos(lit.Pos()), text: s, identifier: true})
				}
			} else {
				out = append(out, sqlFragment{pos: pos(n.Args[idx].Pos()), text: calleeName(n.Fun), unresolved: true})
			}
		case *ast.CompositeLit:
			sel, ok := n.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Identifier" || len(n.Elts) == 0 {
				return true
			}
			// Identifier 的最后一段是表名，前面是 schema。
			if lit, ok := n.Elts[len(n.Elts)-1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil {
					out = append(out, sqlFragment{pos: pos(lit.Pos()), text: s, identifier: true})
				}
			}
			return false
		case *ast.BasicLit:
			if n.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(n.Value); err == nil {
				out = append(out, sqlFragment{pos: pos(n.Pos()), text: s})
			}
		}
		return true
	})
	return out
}

// calleeName 返回调用的函数名：pkg.F、pkg.F[T]、F 都返回 F。
func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.IndexExpr:
		return calleeName(f.X)
	case *ast.IndexListExpr:
		return calleeName(f.X)
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.Ident:
		return f.Name
	}
	return ""
}

func fileFragments(t *testing.T, root, path string) []sqlFragment {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	return sqlFragments(fset, f, root)
}

func walkGoFiles(t *testing.T, dir string, fn func(path string)) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			fn(path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// parseOwnedTables 读取 postgres/tables.go 里的 `var Tables = []string{...}`。
func parseOwnedTables(path string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			if len(vs.Names) != 1 || vs.Names[0].Name != "Tables" || len(vs.Values) != 1 {
				continue
			}
			cl, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				return nil, fmt.Errorf("Tables 必须写成 []string{...} 字面量")
			}
			var tables []string
			for _, elt := range cl.Elts {
				lit, ok := elt.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return nil, fmt.Errorf("Tables 的元素必须是字符串字面量")
				}
				s, _ := strconv.Unquote(lit.Value)
				tables = append(tables, normalizeIdent(s))
			}
			return tables, nil
		}
	}
	return nil, fmt.Errorf("没有找到 var Tables = []string{...}")
}

// ownerIndex 返回 表 → 模块。一张表登记在多个模块时按模块名排序取第一个，保证每次运行的
// 结果一致；重复登记本身由 TestTables_DeclarationsMatchMigrations 报错。
func ownerIndex(owned map[string][]string) map[string]string {
	idx := map[string]string{}
	for _, m := range slices.Sorted(maps.Keys(owned)) {
		for _, tbl := range owned[m] {
			if _, ok := idx[tbl]; !ok {
				idx[tbl] = m
			}
		}
	}
	return idx
}

// isInfraTable 判断不属于任何模块、由 internal/platform 管理的表。
func isInfraTable(name string) bool { return strings.HasPrefix(name, "river_") }

// crossModuleFKViolations 检查外键：两端属于不同模块，或者一端属于模块、另一端是没有登记
// 归属的业务表（忘了登记时跨模块外键不能因此被放过）。被删模块留下的表没有归属，但它们
// 原本就不允许和其它模块有外键，所以删除演练不受影响。
func crossModuleFKViolations(fks []foreignKey, owner map[string]string) []string {
	var out []string
	for _, fk := range fks {
		from, okFrom := owner[fk.from]
		to, okTo := owner[fk.to]
		switch {
		case okFrom && okTo && from != to:
			out = append(out, fmt.Sprintf("%s：表 %s（模块 %s）有外键引用表 %s（模块 %s）。跨模块外键会让两个模块无法拆到不同的数据库；"+
				"去掉外键，由 %s 经组合根适配器校验引用存在（参考 betas.alpha_id）", fk.file, fk.from, from, fk.to, to, from))
		case okFrom != okTo:
			unowned, m := fk.from, to
			if okFrom {
				unowned, m = fk.to, from
			}
			if isInfraTable(unowned) {
				continue
			}
			out = append(out, fmt.Sprintf("%s：表 %s 和模块 %s 的表之间有外键（%s → %s），但 %s 没有登记在任何模块的 postgres/tables.go。"+
				"先把它登记到所属模块；属于别的模块就去掉外键，改由代码校验引用", fk.file, unowned, m, fk.from, fk.to, unowned))
		}
	}
	return out
}

type tableFacts struct {
	known    map[string]bool
	fks      []foreignKey
	owned    map[string][]string // 模块 → 登记的表；解析失败的模块不在这里
	owner    map[string]string   // 表 → 模块
	declErrs []string            // tables.go 解析错误，只在一个测试里报告
}

// loadTableFacts 只解析一次迁移和登记，三个检查共用。
var loadTableFacts = sync.OnceValues(func() (*tableFacts, error) {
	migrations, err := loadMigrations()
	if err != nil {
		return nil, err
	}
	facts := &tableFacts{owned: map[string][]string{}}
	facts.known, facts.fks = parseMigrations(migrations)

	modulesDir := filepath.Join(repoRoot(), "internal", "modules")
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(modulesDir, e.Name(), "postgres", "tables.go")
		if _, err := os.Stat(path); err != nil {
			continue // 没有 tables.go 的模块（如 gamma）不拥有表
		}
		tables, err := parseOwnedTables(path)
		if err != nil {
			facts.declErrs = append(facts.declErrs, fmt.Sprintf("internal/modules/%s/postgres/tables.go：%v", e.Name(), err))
			continue
		}
		facts.owned[e.Name()] = tables
	}
	facts.owner = ownerIndex(facts.owned)
	return facts, nil
})

func tableFactsFor(t *testing.T) *tableFacts {
	t.Helper()
	facts, err := loadTableFacts()
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestTables_DeclarationsMatchMigrations(t *testing.T) {
	facts := tableFactsFor(t)
	for _, msg := range facts.declErrs {
		t.Error(msg)
	}
	seen := map[string]string{}
	for _, m := range slices.Sorted(maps.Keys(facts.owned)) {
		for _, tbl := range facts.owned[m] {
			if prev, dup := seen[tbl]; dup {
				t.Errorf("表 %s 同时登记在模块 %s 和 %s：一张表只能属于一个模块。"+
					"另一个模块需要这些数据时，经 internal/app 的适配器调用拥有方的 Service", tbl, prev, m)
				continue
			}
			seen[tbl] = m
			if !facts.known[tbl] {
				t.Errorf("internal/modules/%s/postgres/tables.go 登记了表 %s，但 migrations/ 执行完后不存在这张表："+
					"新表先写建表 migration；表已删除或改名就同步更新登记", m, tbl)
			}
		}
	}
}

func TestTables_ModulesOnlyTouchOwnTables(t *testing.T) {
	root := repoRoot()
	facts := tableFactsFor(t)

	entries, err := os.ReadDir(filepath.Join(root, "internal", "modules"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := e.Name()
		walkGoFiles(t, filepath.Join(root, "internal", "modules", m), func(path string) {
			for _, frag := range fileFragments(t, root, path) {
				if frag.unresolved {
					t.Errorf("%s：模块 %s 调用 %s 时表名参数不是字符串字面量，表归属检查看不出访问的是哪张表。"+
						"直接写表名字面量（如 \"users\"），表要登记在 internal/modules/%s/postgres/tables.go", frag.pos, m, frag.text, m)
					continue
				}
				for _, tbl := range frag.tables(facts.known) {
					if slices.Contains(facts.owned[m], tbl) {
						continue
					}
					if o, ok := facts.owner[tbl]; ok {
						t.Errorf("%s：模块 %s 的 SQL 读写了模块 %s 的表 %s。模块之间不能直接访问对方的表"+
							"（拆成独立数据库后这条 SQL 无法执行）。请在 %s/domain 定义只包含所需方法的窄接口，"+
							"由 internal/app/adapters_%s_%s.go 用 %s 的 Service 实现", frag.pos, m, o, tbl, m, m, o, o)
					} else {
						t.Errorf("%s：模块 %s 的 SQL 读写了表 %s，但它没有登记在 internal/modules/%s/postgres/tables.go。"+
							"表属于本模块就登记进 Tables；river_* 等基础设施的表经 internal/platform 提供的能力访问", frag.pos, m, tbl, m)
					}
				}
			}
		})
	}

	// platform、kit 等与业务无关的层不能认识模块的表。internal/app 是组合根，
	// 它的跨模块集成测试可以直接准备多个模块的数据。
	for _, dir := range []string{"internal/platform", "internal/kit", "internal/config", "cmd"} {
		walkGoFiles(t, filepath.Join(root, dir), func(path string) {
			for _, frag := range fileFragments(t, root, path) {
				for _, tbl := range frag.tables(facts.known) {
					if o, ok := facts.owner[tbl]; ok {
						t.Errorf("%s：%s 的 SQL 读写了模块 %s 的表 %s。%s 不认识业务模块，也不能依赖模块的表；"+
							"测试需要一张表就在测试里自己建", frag.pos, dir, o, tbl, dir)
					}
				}
			}
		})
	}
}

func TestTables_NoCrossModuleForeignKeys(t *testing.T) {
	facts := tableFactsFor(t)
	for _, msg := range crossModuleFKViolations(facts.fks, facts.owner) {
		t.Error(msg)
	}
}

func TestParseMigrations(t *testing.T) {
	tables, fks := parseMigrations([]migrationFile{
		{"0001.up.sql", `
-- CREATE TABLE in_comment (id int);
CREATE TABLE alphas (id uuid PRIMARY KEY);
CREATE UNLOGGED TABLE IF NOT EXISTS public."Betas" (
    id uuid,
    alpha_id uuid REFERENCES alphas (id)
);
COMMENT ON TABLE alphas IS 'CREATE TABLE in_string';
CREATE TABLE old_name (id int);
ALTER TABLE old_name
    RENAME TO new_name;
ALTER TABLE alphas RENAME COLUMN id TO pid;
ALTER TABLE alphas ADD COLUMN owner uuid REFERENCES users (id);
CREATE TABLE gone_a (id int);
CREATE TABLE gone_b (id int);
`},
		{"0002.up.sql", `DROP TABLE IF EXISTS gone_a, gone_b;`},
		{"0003.up.sql", `
CREATE TABLE plain (id int);
ALTER FOREIGN TABLE remote ADD COLUMN pid uuid REFERENCES alphas (id);
CREATE GLOBAL TEMPORARY TABLE scratch (pid uuid REFERENCES alphas (id));
`},
	})

	want := []string{"alphas", "betas", "new_name", "plain", "scratch"}
	if got := slices.Sorted(maps.Keys(tables)); !slices.Equal(got, want) {
		t.Errorf("tables = %v, want %v", got, want)
	}
	wantFKs := []foreignKey{
		{"0001.up.sql", "betas", "alphas"},
		{"0001.up.sql", "alphas", "users"},
		{"0003.up.sql", "scratch", "alphas"}, // 上一条语句的 REFERENCES 不能记到 plain 名下
	}
	if !slices.Equal(fks, wantFKs) {
		t.Errorf("fks = %v, want %v", fks, wantFKs)
	}
}

func TestReferencedTables(t *testing.T) {
	known := map[string]bool{"betas": true, "alphas": true, "beta_processed_events": true}
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{"简单查询", `SELECT id FROM betas WHERE id = $1`, []string{"betas"}},
		{"JOIN 与 schema 前缀", `SELECT o.id FROM betas o JOIN public.alphas p ON p.id = o.alpha_id`, []string{"betas", "alphas"}},
		{"逗号连接", `select * from betas, alphas`, []string{"betas", "alphas"}},
		{"子查询", `UPDATE betas SET total = (SELECT price FROM alphas WHERE id = $1)`, []string{"betas", "alphas"}},
		{"写入", `INSERT INTO beta_processed_events (beta_id) VALUES ($1) ON CONFLICT DO NOTHING`, []string{"beta_processed_events"}},
		{"删除", `DELETE FROM alphas WHERE id = $1`, []string{"alphas"}},
		{"列名不是表名", `SELECT alpha_id, beta_id FROM betas`, []string{"betas"}},
		{"字符串常量不算", `SELECT id FROM betas WHERE note = 'alphas'`, []string{"betas"}},
		{"注释不算", "SELECT id FROM betas -- 以前查过 alphas", []string{"betas"}},
		{"不是 SQL 的文案", `postgres: list alphas`, nil},
		{"错误文案里出现 from", `load alphas from cache`, nil},
		{"字符串里的 -- 不是注释", `SELECT id FROM betas WHERE note <> 'a--b' AND alpha_id IN (SELECT id FROM alphas)`, []string{"betas", "alphas"}},
		{"字符串里的 /* 不是注释", "SELECT id FROM betas WHERE note = '/*' AND alpha_id IN (SELECT id FROM alphas) -- */", []string{"betas", "alphas"}},
		{"注释里的 /* 不吞后面的 SQL", "SELECT id -- 见 /*旧实现\nFROM betas JOIN alphas ON true -- */", []string{"betas", "alphas"}},
		{"转义的单引号", `SELECT id FROM betas WHERE note = 'it''s -- alphas'`, []string{"betas"}},
		{"COPY", `COPY alphas (id, name) FROM STDIN`, []string{"alphas"}},
		{"LOCK TABLE", `LOCK TABLE alphas IN EXCLUSIVE MODE`, []string{"alphas"}},
		{"ALTER TABLE", `ALTER TABLE alphas DISABLE TRIGGER ALL`, []string{"alphas"}},
		{"AS 别名不算表", `SELECT count(*) AS alphas FROM betas`, []string{"betas"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := referencedTables(tt.sql, known); !slices.Equal(got, tt.want) {
				t.Errorf("referencedTables(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}

func TestOwnerIndex_DuplicateIsDeterministic(t *testing.T) {
	owned := map[string][]string{"alpha": {"betas"}, "beta": {"betas"}, "user": {"users"}}
	for range 50 {
		if got := ownerIndex(owned)["betas"]; got != "alpha" {
			t.Fatalf("重复登记时应按模块名排序取第一个，got %q", got)
		}
	}
}

func TestParseOwnedTables_Normalizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tables.go")
	src := "package postgres\n\nvar Tables = []string{\"public.betas\", \"Beta_Processed_Events\"}\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := parseOwnedTables(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"betas", "beta_processed_events"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSQLFragments_PgxIdentifier(t *testing.T) {
	src := `package x

import "github.com/jackc/pgx/v5"

var copyTarget = pgx.Identifier{"public", "alphas"}
var query = "SELECT id FROM betas"
var notSQL = "alphas"
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{"betas": true, "alphas": true}
	var got []string
	for _, frag := range sqlFragments(fset, f, ".") {
		got = append(got, frag.tables(known)...)
	}
	if want := []string{"alphas", "betas"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSQLFragments_TableNameArgs(t *testing.T) {
	src := `package x

import platformpg "example/platform/postgres"

func a() { platformpg.ListPage(ctx, q, "alphas", columns, w, s, p, scan) }
func b() { platformpg.ListPage[row](ctx, q, "public.Betas", columns, w, s, p, scan) }
func c() { platformpg.ListPage(ctx, q, tableName, columns, w, s, p, scan) }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{"betas": true, "alphas": true}
	var got []string
	var unresolved int
	for _, frag := range sqlFragments(fset, f, ".") {
		got = append(got, frag.tables(known)...)
		if frag.unresolved {
			unresolved++
		}
	}
	if want := []string{"alphas", "betas"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if unresolved != 1 {
		t.Errorf("表名参数不是字符串字面量的调用应记为 1 处无法检查，got %d", unresolved)
	}
}

func TestCrossModuleFKViolations(t *testing.T) {
	owner := map[string]string{"betas": "beta", "beta_items": "beta", "alphas": "alpha"}
	tests := []struct {
		name string
		fk   foreignKey
		want bool
	}{
		{"跨模块", foreignKey{"m.sql", "betas", "alphas"}, true},
		{"同模块", foreignKey{"m.sql", "beta_items", "betas"}, false},
		{"未登记的表引用模块的表", foreignKey{"m.sql", "shipments", "betas"}, true},
		{"模块的表引用未登记的表", foreignKey{"m.sql", "betas", "shipments"}, true},
		{"两端都未登记", foreignKey{"m.sql", "a", "b"}, false},
		{"基础设施表之间", foreignKey{"m.sql", "river_client_queue", "river_client"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(crossModuleFKViolations([]foreignKey{tt.fk}, owner)) > 0; got != tt.want {
				t.Errorf("violation = %v, want %v", got, tt.want)
			}
		})
	}
}
