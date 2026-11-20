package resource

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Rules 是全部规则编号和一句话说明。编号发布后不改名、不复用；每个编号在
// docs/agents/generator-rules.md 里有同名的小节，并至少被一个测试用例触发（rules_test.go 检查）。
var Rules = map[string]string{
	"YAML-SYNTAX":      "YAML 语法错误",
	"YAML-UNKNOWN-KEY": "没有这个键",
	"YAML-TYPE":        "值的类型不对",

	"TOP-REQUIRED":      "缺少必填的顶层键",
	"TOP-MODULE-NAME":   "模块名不合法",
	"TOP-RESOURCE-NAME": "资源名不合法",
	"TOP-PLURAL-NAME":   "复数不合法",
	"TOP-DISPLAY":       "display 不是 required 的 string 字段",
	"TOP-SURFACE":       "admin 和 web 要写且只写一个",
	"TEXT-TITLE-CHARS":  "名称里有不能用的字符",

	"FIELD-NAME":                 "字段名不合法",
	"FIELD-TYPE":                 "字段类型不认识",
	"FIELD-TITLE-REQUIRED":       "字段缺少 title",
	"FIELD-MODIFIER":             "修饰不能用在这种类型上",
	"FIELD-MAX-REQUIRED":         "string、text 必须写 max",
	"FIELD-MIN-MAX":              "min、max 的取值不对",
	"FIELD-MONEY-NAME":           "money 字段名必须以 _cents 结尾",
	"FIELD-ENUM-VALUES":          "enum 的取值不对",
	"FIELD-DEFAULT":              "default 不符合字段的类型或约束",
	"FIELD-IMMUTABLE-READONLY":   "immutable 和 readonly 不能同时写",
	"FIELD-READONLY-UNREACHABLE": "readonly 字段没有动作能改",

	"ADMIN-OPERATION":    "admin.operations 的取值不对",
	"ADMIN-LIST-FIELD":   "列表引用了不存在或类型不支持的字段",
	"ADMIN-DEFAULT-SORT": "default_sort 不在可排序的字段里",
	"ADMIN-NAV":          "admin.nav 的取值不对",

	"ACTION-NAME":         "动作名不合法",
	"ACTION-RESERVED":     "动作名或参数名和生成的名字冲突",
	"ACTION-TITLE":        "动作缺少 title",
	"ACTION-KIND":         "set 和 custom 要二选一",
	"ACTION-INPUT":        "动作的参数不合法",
	"ACTION-SET":          "set 写的列或值不对",
	"ACTION-WHEN":         "when 的字段或取值不对",
	"ACTION-ERROR":        "动作声明的错误不合法",
	"ACTION-SET-EDITABLE": "set 写的字段也能在编辑表单里改（提示）",

	"RULE-ON":     "rules 的 on 不对",
	"RULE-CUSTOM": "rules 缺少 custom",
	"RULE-ERROR":  "rules 声明的错误不合法",

	"WEB-OPERATION": "web.operations 的取值不对",
	"WEB-AUTH":      "web.auth 不是 operations 的子集",

	"INDEX-COLUMN":    "索引引用了不存在的列",
	"INDEX-FOR":       "索引缺少 for",
	"INDEX-REDUNDANT": "冗余索引",
	"INDEX-MISSING":   "有排序或筛选但没有索引（提示）",

	"REPO-MODULE-EXISTS": "模块已存在",
	"REPO-TABLE-EXISTS":  "表已存在",
	"REPO-SPEC-CONFLICT": "路径、tag、权限码或 schema 与 spec 已有的重复",
	"REPO-AUTH-MISSING":  "web.auth 需要的前台登录检查没有唯一的提供者",
}

var (
	reModule   = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	reSnake    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	reIcon     = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	fieldTypes = []string{"string", "text", "int", "money", "bool", "enum", "date", "datetime"}
	// 与组合根、生成的代码里的包名冲突，或者是 Go 关键字。
	reservedModules = strings.Fields(`break case chan const continue default defer else fallthrough for func go
		goto if import interface map package range return select struct switch type var
		echo river app api domain httpapi adminapi postgres events jobs cli queue errs page uow`)
	systemFields = []string{"id", "created_at", "updated_at", "version"}
	sqlReserved  = strings.Fields(`all and any array as asc between both case cast check collate column constraint
		create cross current_date current_time current_timestamp current_user default desc distinct do else end
		except false fetch for foreign from grant group having in index inner insert intersect into is join key
		leading left like limit not null offset on only or order outer primary references right select
		session_user set some table then to trailing true union unique update user using values when where window with`)
)

type checker struct {
	p        *Parsed
	problems []Problem
}

func (c *checker) add(rule, path, msg, fix string) {
	line, col := c.p.Pos(path)
	c.problems = append(c.problems, problem(rule, c.p.File, line, col, path, msg, fix))
}

func (c *checker) hint(rule, path, msg, fix string) {
	c.add(rule, path, msg, fix)
	c.problems[len(c.problems)-1].Hint = true
}

// Validate 检查描述文件本身（不看仓库现状），返回全部错误和提示。
func Validate(p *Parsed) []Problem {
	c := &checker{p: p}
	d := &p.D
	c.top(d)
	for _, f := range d.Fields {
		c.field("fields."+f.Key, f.Key, f.Val, false)
	}
	c.readonly(d)
	if d.Admin != nil {
		c.admin(d)
	}
	if d.Web != nil {
		c.web(d)
	}
	c.rules(d)
	c.indexes(d)
	c.titles(d)
	return c.problems
}

// reTitleUnsafe 是名称（title、枚举文案）里不能用的字符：名称原样写进生成的 JSX 文本和模板字符串。
var reTitleUnsafe = regexp.MustCompile("[\"'`{}<>\\$]")

func (c *checker) titles(d *Descriptor) {
	check := func(path, v string) {
		if reTitleUnsafe.MatchString(v) {
			c.add("TEXT-TITLE-CHARS", path, fmt.Sprintf("%q 里有不能用的字符", v), "名称里不要用引号、反引号、花括号、尖括号、反斜杠和 $；需要说明的内容写进 help、confirm 或 description")
		}
	}
	check("title", d.Title)
	fields := func(prefix string, fs Ordered[Field]) {
		for _, f := range fs {
			check(prefix+f.Key+".title", f.Val.Title)
			for _, v := range f.Val.Values {
				check(prefix+f.Key+".values."+v.Key, v.Val)
			}
		}
	}
	fields("fields.", d.Fields)
	if d.Admin != nil {
		for _, a := range d.Admin.Actions {
			check("admin.actions."+a.Key+".title", a.Val.Title)
			fields("admin.actions."+a.Key+".input.", a.Val.Input)
		}
	}
}

func (c *checker) top(d *Descriptor) {
	for _, k := range []struct{ key, val string }{{"module", d.Module}, {"resource", d.Resource}, {"title", d.Title}} {
		if k.val == "" {
			c.add("TOP-REQUIRED", k.key, "缺少 "+k.key, "在文件开头加上 "+k.key+": <值>；不知道怎么写时看 go run ./tools/gen example admin")
		}
	}
	if len(d.Fields) == 0 {
		c.add("TOP-REQUIRED", "fields", "缺少 fields", "至少写一个字段（id、created_at、updated_at 是系统字段，不用写）")
	}
	if d.Module != "" && (!reModule.MatchString(d.Module) || slices.Contains(reservedModules, d.Module)) {
		c.add("TOP-MODULE-NAME", "module", fmt.Sprintf("模块名 %q 不合法", d.Module), "只用小写字母和数字、小写字母开头（不带下划线），也不能是 Go 关键字或 "+strings.Join(reservedModules[25:], "、")+" 这些已有的包名")
	}
	if d.Resource != "" && !reSnake.MatchString(d.Resource) {
		c.add("TOP-RESOURCE-NAME", "resource", fmt.Sprintf("资源名 %q 不合法", d.Resource), "用单数的 snake_case，如 category、order_item")
	}
	if d.Plural != "" && (!reSnake.MatchString(d.Plural) || d.Plural == d.Resource) {
		c.add("TOP-PLURAL-NAME", "plural", fmt.Sprintf("复数 %q 不合法", d.Plural), "用 snake_case，且和 resource 不同；默认规则推得对时删掉这一行")
	}
	switch {
	case d.Admin == nil && d.Web == nil:
		c.add("TOP-SURFACE", "", "admin 和 web 都没写", "写 admin（后台管理）或 web（前台接口）其中一个")
	case d.Admin != nil && d.Web != nil:
		c.add("TOP-SURFACE", "web", "第一版只能开放到一个接入面", "只保留 admin 或 web；同时开放到两边不支持，另一边手写")
	}
	if d.Admin != nil {
		f, ok := d.Fields.Get(d.Display)
		switch {
		case d.Display == "":
			c.add("TOP-REQUIRED", "display", "有 admin 时必须写 display", "写 display: <字段名>，选一个 required 的 string 字段（通常是名称），确认框和提示里用它代表这条记录")
		case !ok || f.Type != "string" || !f.Required:
			c.add("TOP-DISPLAY", "display", fmt.Sprintf("display 的 %s 不是 required 的 string 字段", d.Display), "换成一个 required 的 string 字段，或者给这个字段加上 required: true")
		}
	}
}

// field 检查一个字段定义；input 为 true 时是动作的参数。
func (c *checker) field(path, name string, f Field, input bool) {
	rule := func(r string) string {
		if input {
			return "ACTION-INPUT"
		}
		return r
	}
	if !reSnake.MatchString(name) || (!input && (slices.Contains(systemFields, name) || slices.Contains(sqlReserved, name))) {
		c.add(rule("FIELD-NAME"), path+".$key", fmt.Sprintf("名字 %q 不合法", name), "用 snake_case；不能是系统字段（id、created_at、updated_at、version），也不能是 SQL 保留字（如 order、user、group），换成 sort_order、owner 这类名字")
	}
	if input && f.Like != "" {
		if _, ok := c.p.D.Fields.Get(f.Like); !ok {
			c.add("ACTION-INPUT", path+".like", fmt.Sprintf("like 的 %s 不是已声明的字段", f.Like), "写 fields 里的字段名，或者去掉 like 写完整的定义")
		}
		if f.Type != "" {
			c.add("ACTION-INPUT", path+".type", "写了 like 就不能再写 type", "去掉 type，类型沿用 like 的字段")
		}
		for _, k := range []struct {
			on  bool
			key string
		}{{f.Unique, "unique"}, {f.Immutable, "immutable"}, {f.Readonly, "readonly"}, {f.Default != nil, "default"}} {
			if k.on {
				c.add("ACTION-INPUT", path+"."+k.key, "动作的参数不能写 "+k.key, "去掉 "+k.key+"：参数不是表里的列")
			}
		}
		return
	}
	if f.Like != "" {
		c.add("FIELD-MODIFIER", path+".like", "like 只能用在动作的 input 里", "写完整的字段定义")
	}
	if input && f.Type == "enum" {
		c.add("ACTION-INPUT", path+".type", "动作的参数不能单独定义枚举", "用 like 沿用字段的枚举（如 { like: status }）：枚举类型和文案只在字段上定义一次")
	}
	if f.Title == "" {
		c.add(rule("FIELD-TITLE-REQUIRED"), path, "缺少 title", "加上 title: <中文名>，它是表单标签、表头和 spec 属性的 title")
	}
	if !slices.Contains(fieldTypes, f.Type) {
		c.add(rule("FIELD-TYPE"), path+".type", fmt.Sprintf("类型 %q 不认识", f.Type), "只能是 "+strings.Join(fieldTypes, "、"))
		return
	}
	if input {
		for _, k := range []struct {
			on  bool
			key string
		}{{f.Unique, "unique"}, {f.Immutable, "immutable"}, {f.Readonly, "readonly"}, {f.Default != nil, "default"}} {
			if k.on {
				c.add("ACTION-INPUT", path+"."+k.key, "动作的参数不能写 "+k.key, "去掉 "+k.key+"：参数不是表里的列")
			}
		}
	}
	t := f.Type
	type mod struct {
		on      bool
		key     string
		allowed []string
	}
	for _, m := range []mod{
		{f.Required, "required", []string{"string", "text", "int", "money", "enum", "date", "datetime"}},
		{f.Default != nil, "default", []string{"string", "text", "int", "money", "bool", "enum"}},
		{f.Min != nil, "min", []string{"string", "text", "int", "money"}},
		{f.Max != nil, "max", []string{"string", "text", "int", "money"}},
		{f.Unique, "unique", []string{"string", "int"}},
		{len(f.Values) > 0, "values", []string{"enum"}},
	} {
		if m.on && !slices.Contains(m.allowed, t) {
			fix := "去掉 " + m.key + "（" + m.key + " 只能用于 " + strings.Join(m.allowed, "、") + "）"
			if m.key == "required" && t == "bool" {
				fix = "去掉 required：bool 总是非空，不写时是 false"
			}
			c.add(rule("FIELD-MODIFIER"), path+"."+m.key, t+" 类型不能写 "+m.key, fix)
		}
	}
	if (t == "string" || t == "text") && f.Max == nil {
		def := "50"
		if t == "text" {
			def = "500"
		}
		c.add(rule("FIELD-MAX-REQUIRED"), path, t+" 类型必须写 max", "加上 max: "+def+" 这类长度上限（需求没说时名称类 50、text 500）")
	}
	if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
		c.add(rule("FIELD-MIN-MAX"), path+".min", "min 大于 max", "把 min 改成不大于 max 的值")
	}
	if (t == "string" || t == "text") && ((f.Min != nil && *f.Min < 0) || (f.Max != nil && *f.Max < 1)) {
		c.add(rule("FIELD-MIN-MAX"), path+".max", "长度的 min 不能为负，max 至少为 1", "改成合理的字符数")
	}
	if t == "money" && !strings.HasSuffix(name, "_cents") {
		c.add(rule("FIELD-MONEY-NAME"), path+".$key", "money 字段名必须以 _cents 结尾", "改名为 "+strings.TrimSuffix(name, "_cents")+"_cents（单位是分，名字里带上单位）")
	}
	if t == "enum" {
		if len(f.Values) == 0 {
			c.add(rule("FIELD-ENUM-VALUES"), path, "enum 缺少 values", "加上 values: { <取值>: <中文>, ... }，取值用 snake_case")
		}
		for _, v := range f.Values {
			if !reSnake.MatchString(v.Key) || v.Val == "" {
				c.add(rule("FIELD-ENUM-VALUES"), path+".values."+v.Key, fmt.Sprintf("取值 %q 不合法或没有中文", v.Key), "取值用 snake_case，后面写中文，如 enabled: 启用")
			}
		}
	}
	if f.Default != nil {
		if msg := checkValue(f, f.Default); msg != "" {
			c.add(rule("FIELD-DEFAULT"), path+".default", "default "+msg, "改成符合类型和约束的值")
		}
	}
	if !input && f.Immutable && f.Readonly {
		c.add("FIELD-IMMUTABLE-READONLY", path, "immutable 和 readonly 不能同时写", "创建后完全不能改写 immutable；只能经动作改写 readonly")
	}
}

// checkValue 检查一个常量是否符合字段的类型和约束，符合时返回空串。
func checkValue(f Field, v *Raw) string {
	s := v.String()
	switch f.Type {
	case "string", "text":
		n := int64(len([]rune(s)))
		if f.Max != nil && n > *f.Max {
			return fmt.Sprintf("%q 超过了 max %d 个字符", s, *f.Max)
		}
	case "int", "money":
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v.Node.Tag != "!!int" {
			return fmt.Sprintf("%q 不是整数", s)
		}
		lo := f.Min
		if lo == nil && f.Type == "money" {
			lo = new(int64(0))
		}
		if (lo != nil && i < *lo) || (f.Max != nil && i > *f.Max) {
			return fmt.Sprintf("%d 不在 min、max 的范围内", i)
		}
	case "bool":
		if v.Node.Tag != "!!bool" {
			return fmt.Sprintf("%q 不是 true 或 false", s)
		}
	case "enum":
		if _, ok := f.Values.Get(s); !ok {
			return fmt.Sprintf("%q 不是取值之一（%s）", s, strings.Join(f.Values.Keys(), "、"))
		}
	}
	return ""
}

func (c *checker) readonly(d *Descriptor) {
	for _, f := range d.Fields {
		if !f.Val.Readonly {
			continue
		}
		reachable := false
		if d.Admin != nil {
			for _, a := range d.Admin.Actions {
				if _, ok := a.Val.Set.Get(f.Key); ok || a.Val.Custom != "" {
					reachable = true
				}
			}
		}
		if !reachable {
			c.add("FIELD-READONLY-UNREACHABLE", "fields."+f.Key+".readonly", "readonly 字段没有任何动作能改它", "加一个 set 写它的声明式动作（如 disable: { set: { "+f.Key+": ... } }），或者一个 custom 动作；不需要改就换成 immutable")
		}
	}
}

func (c *checker) admin(d *Descriptor) {
	a := d.Admin
	for i, op := range a.Operations {
		if !slices.Contains([]string{"create", "update", "delete"}, op) {
			c.add("ADMIN-OPERATION", fmt.Sprintf("admin.operations[%d]", i), fmt.Sprintf("%q 不是可选的操作", op), "只能写 create、update、delete（列表和详情总是有）")
		}
	}
	sys := map[string]string{"created_at": "datetime", "updated_at": "datetime"}
	typeOf := func(name string) (string, bool) {
		if t, ok := sys[name]; ok {
			return t, true
		}
		f, ok := d.Fields.Get(name)
		return f.Type, ok
	}
	if len(a.List.Columns) == 0 {
		c.add("ADMIN-LIST-FIELD", "admin.list.columns", "缺少 admin.list.columns", "列出列表要显示的字段，如 [name, status, created_at]")
	} else if d.Display != "" && !slices.Contains(a.List.Columns, d.Display) {
		c.add("ADMIN-LIST-FIELD", "admin.list.columns", fmt.Sprintf("列表里没有 display 字段 %s", d.Display), "把 "+d.Display+" 加进 columns：列表里它是进入详情页的链接")
	}
	for _, g := range []struct {
		key   string
		names []string
		types []string
	}{
		{"columns", a.List.Columns, nil},
		{"search", a.List.Search, []string{"string", "text"}},
		{"filter", a.List.Filter, []string{"enum", "bool", "date", "datetime"}},
		{"sort", a.List.Sort, []string{"string", "int", "money", "date", "datetime"}},
	} {
		for i, n := range g.names {
			path := fmt.Sprintf("admin.list.%s[%d]", g.key, i)
			t, ok := typeOf(n)
			switch {
			case !ok:
				c.add("ADMIN-LIST-FIELD", path, fmt.Sprintf("字段 %q 不存在", n), "写 fields 里的字段名，或 created_at、updated_at")
			case g.types != nil && !slices.Contains(g.types, t):
				c.add("ADMIN-LIST-FIELD", path, fmt.Sprintf("%s 是 %s 类型，不能用于 %s", n, t, g.key), g.key+" 只支持 "+strings.Join(g.types, "、"))
			}
		}
	}
	if s := strings.TrimPrefix(a.List.DefaultSort, "-"); s != "" && s != "created_at" && !slices.Contains(a.List.Sort, s) {
		c.add("ADMIN-DEFAULT-SORT", "admin.list.default_sort", fmt.Sprintf("%s 不在 admin.list.sort 里", s), "把它加进 sort，或者改成 sort 里的字段")
	}
	if a.Nav.Icon != "" && !reIcon.MatchString(a.Nav.Icon) {
		c.add("ADMIN-NAV", "admin.nav.icon", fmt.Sprintf("图标名 %q 不合法", a.Nav.Icon), "写 lucide-react 导出的组件名（PascalCase），如 Users、Tags、Package；图标列表见 https://lucide.dev/icons")
	}
	if a.Nav.Group != "" && strings.TrimSpace(a.Nav.Group) != a.Nav.Group {
		c.add("ADMIN-NAV", "admin.nav.group", "分组名首尾有空格", "去掉空格；已有的分组是 常用、业务、系统（web/admin/src/lib/permissions.ts 的 groupOrder）")
	}
	editable := hasOp(a.Operations, "update")
	for _, e := range a.Actions {
		path := "admin.actions." + e.Key
		act := e.Val
		if !reSnake.MatchString(e.Key) {
			c.add("ACTION-NAME", path+".$key", fmt.Sprintf("动作名 %q 不合法", e.Key), "用 snake_case，如 disable、adjust_stock")
		}
		if act.Title == "" {
			c.add("ACTION-TITLE", path, "动作缺少 title", "加上 title: <按钮文字>")
		}
		if slices.Contains([]string{"create", "update", "delete", "list", "get"}, e.Key) {
			c.add("ACTION-RESERVED", path+".$key", fmt.Sprintf("动作名 %q 和增删改查的操作重名", e.Key), "换一个说明业务含义的名字，如 archive、publish；增删改查写在 admin.operations 里")
		}
		for _, in := range act.Input {
			if slices.Contains(systemFields, in.Key) {
				c.add("ACTION-RESERVED", path+".input."+in.Key+".$key", fmt.Sprintf("参数名 %q 和系统字段重名", in.Key), "换一个名字：id 是路径里的记录 ID，version、created_at、updated_at 由系统维护")
			}
		}
		for _, in := range act.Input {
			c.field(path+".input."+in.Key, in.Key, in.Val, true)
		}
		switch {
		case len(act.Set) == 0 && act.Custom == "":
			c.add("ACTION-KIND", path, "set 和 custom 都没写", "能用常量或参数表达的写 set（完整生成）；要计算或有额外规则的写 custom（生成桩，由 AI 补足）")
		case len(act.Set) > 0 && act.Custom != "":
			c.add("ACTION-KIND", path, "set 和 custom 不能同时写", "二选一：去掉 set 改由 custom 实现，或去掉 custom")
		}
		if len(act.Set) > 0 && len(act.Errors) > 0 {
			c.add("ACTION-ERROR", path+".errors", "errors 只能用于 custom 动作", "声明式动作的错误是生成的（不存在 404、条件不满足 409），去掉 errors")
		}
		for _, s := range act.Set {
			sp := path + ".set." + s.Key
			f, ok := d.Fields.Get(s.Key)
			if !ok || f.Immutable {
				c.add("ACTION-SET", sp+".$key", fmt.Sprintf("%s 不是可以改的字段", s.Key), "写 fields 里不是 immutable 的字段")
				continue
			}
			if name, isParam := s.Val.Param(); isParam {
				in, ok := act.Input.Get(name)
				if !ok {
					c.add("ACTION-SET", sp, fmt.Sprintf("参数 $%s 不在 input 里", name), "在 input 里声明 "+name+"（可以写 { like: "+s.Key+" } 沿用字段的定义）")
				} else if t := resolveInput(d, in).Type; t != f.Type {
					c.add("ACTION-SET", sp, fmt.Sprintf("参数 $%s 是 %s，%s 是 %s", name, t, s.Key, f.Type), "让参数的类型和字段一致，或者用 like: "+s.Key)
				}
				continue
			}
			if msg := checkValue(f, s.Val); msg != "" {
				c.add("ACTION-SET", sp, "值 "+msg, "set 只接受常量或 $参数，不支持表达式；要计算的改成 custom 动作")
			}
			if editable && !f.Readonly && !f.Immutable {
				c.hint("ACTION-SET-EDITABLE", sp, s.Key+" 也能在编辑表单里改，这个动作可能是多余的", "只想经动作改就给字段加 readonly: true")
			}
		}
		for _, w := range act.When {
			wp := path + ".when." + w.Key
			f, ok := d.Fields.Get(w.Key)
			if !ok || (f.Type != "enum" && f.Type != "bool") {
				c.add("ACTION-WHEN", wp+".$key", fmt.Sprintf("%s 不是 enum 或 bool 字段", w.Key), "when 只能用 enum、bool 字段的取值")
				continue
			}
			for _, v := range w.Val {
				if (f.Type == "enum" && !slices.Contains(f.Values.Keys(), v)) || (f.Type == "bool" && v != "true" && v != "false") {
					c.add("ACTION-WHEN", wp, fmt.Sprintf("%q 不是 %s 的取值", v, w.Key), "写字段的合法取值")
				}
			}
		}
		if act.Custom != "" {
			c.errors("ACTION-ERROR", path+".errors", act.Errors, func(f string) bool { _, ok := act.Input.Get(f); return ok }, "input 里的参数")
		}
	}
}

func (c *checker) errors(rule, path string, errs Ordered[ErrorDef], validField func(string) bool, fieldDesc string) {
	for _, e := range errs {
		ep := path + "." + e.Key
		if !reSnake.MatchString(e.Key) {
			c.add(rule, ep+".$key", fmt.Sprintf("错误码 %q 不合法", e.Key), "用 snake_case，不带资源前缀（生成时自动加）")
		}
		if e.Val.Kind != "invalid" && e.Val.Kind != "conflict" {
			c.add(rule, ep+".kind", fmt.Sprintf("kind %q 不对", e.Val.Kind), "输入本身不合法写 invalid（400），与当前状态冲突写 conflict（409）")
		}
		if e.Val.Message == "" {
			c.add(rule, ep, "缺少 message", "写给用户看的中文提示：说清楚发生了什么、怎么处理")
		}
		if e.Val.Field != "" && !validField(e.Val.Field) {
			c.add(rule, ep+".field", fmt.Sprintf("field 的 %s 不存在", e.Val.Field), "写"+fieldDesc+"，或者去掉 field")
		}
	}
}

func (c *checker) web(d *Descriptor) {
	w := d.Web
	if len(w.Operations) == 0 {
		c.add("WEB-OPERATION", "web.operations", "缺少 web.operations", "写 list、get、create 的子集，如 [list, get, create]")
	}
	for i, op := range w.Operations {
		if !slices.Contains([]string{"list", "get", "create"}, op) {
			c.add("WEB-OPERATION", fmt.Sprintf("web.operations[%d]", i), fmt.Sprintf("%q 不是可选的操作", op), "只能写 list、get、create（前台的修改、删除涉及归属规则，第一版不生成）")
		}
	}
	for i, op := range w.Auth {
		if !slices.Contains(w.Operations, op) {
			c.add("WEB-AUTH", fmt.Sprintf("web.auth[%d]", i), fmt.Sprintf("%q 不在 web.operations 里", op), "auth 只能列 operations 里有的操作")
		}
	}
}

func (c *checker) rules(d *Descriptor) {
	var ops []string
	if d.Admin != nil {
		ops = append([]string{"create"}, d.Admin.Operations...)
	} else if d.Web != nil && slices.Contains(d.Web.Operations, "create") {
		ops = []string{"create"}
	}
	for i, r := range d.Rules {
		path := fmt.Sprintf("rules[%d]", i)
		if len(r.On) == 0 {
			c.add("RULE-ON", path, "缺少 on", "写 on: [create]、[create, update] 或 [delete]")
		}
		for j, op := range r.On {
			if !slices.Contains(ops, op) {
				c.add("RULE-ON", fmt.Sprintf("%s.on[%d]", path, j), fmt.Sprintf("这个资源没有 %s 操作", op), "on 只能写已有的操作："+strings.Join(ops, "、")+"（delete 要先加进 admin.operations）")
			}
		}
		if r.Custom == "" {
			c.add("RULE-CUSTOM", path, "缺少 custom", "一句话写清规则、出错时返回哪个错误、有没有并发要求")
		}
		c.errors("RULE-ERROR", path+".errors", r.Errors, func(f string) bool { _, ok := d.Fields.Get(f); return ok }, "fields 里的字段")
	}
}

func (c *checker) indexes(d *Descriptor) {
	cols := map[string]bool{"id": true, "created_at": true, "updated_at": true}
	for _, f := range d.Fields {
		cols[f.Key] = true
	}
	// 主键和唯一约束：它们的列组合也算已有的"前缀"。
	existing := [][]string{{"id"}}
	for _, f := range d.Fields {
		if f.Val.Unique {
			existing = append(existing, []string{f.Key})
		}
	}
	for i, idx := range d.Indexes {
		path := fmt.Sprintf("indexes[%d]", i)
		var plain []string
		for j, col := range idx.Columns {
			name := strings.TrimPrefix(col, "-")
			plain = append(plain, name)
			if !cols[name] {
				c.add("INDEX-COLUMN", fmt.Sprintf("%s.columns[%d]", path, j), fmt.Sprintf("列 %q 不存在", name), "写 fields 里的字段或 id、created_at、updated_at")
			}
		}
		if len(idx.Columns) == 0 {
			c.add("INDEX-COLUMN", path, "缺少 columns", "写 columns: [<列>, ...]")
		}
		if idx.For == "" {
			c.add("INDEX-FOR", path, "缺少 for", "写这个索引服务的查询（WHERE / ORDER BY 的形状），说不出就不建（database.md：按查询建）")
		}
		if idx.Where == "" {
			for _, e := range existing {
				if isPrefix(plain, e) {
					c.add("INDEX-REDUNDANT", path, fmt.Sprintf("%v 是已有索引 %v 的前缀", plain, e), "删掉这个索引：主键、唯一约束或另一个索引已经覆盖了它")
					break
				}
			}
			existing = append(existing, plain)
		}
	}
	if d.Admin == nil {
		return
	}
	// 提示：排序和筛选的字段没有出现在任何索引的第一列。
	first := map[string]bool{"id": true}
	for _, idx := range d.Indexes {
		if len(idx.Columns) > 0 {
			first[strings.TrimPrefix(idx.Columns[0], "-")] = true
		}
	}
	for _, f := range d.Fields {
		if f.Val.Unique {
			first[f.Key] = true
		}
	}
	for _, s := range append(slices.Clone(d.Admin.List.Sort), d.Admin.List.Filter...) {
		if !first[s] {
			first[s] = true // 同一个字段只提示一次
			c.hint("INDEX-MISSING", "indexes", "按 "+s+" 排序或筛选的查询没有索引", "数据量大时加一个以 "+s+" 开头的索引；表很小或取值很少时在汇报里说明为什么不需要")
		}
	}
}

func isPrefix(a, b []string) bool {
	return len(a) <= len(b) && slices.Equal(a, b[:len(a)])
}

// resolveInput 展开 like：沿用字段的定义，再用参数里写了的键覆盖。
func resolveInput(d *Descriptor, in Field) Field {
	if in.Like == "" {
		return in
	}
	base, _ := d.Fields.Get(in.Like)
	base.Unique, base.Immutable, base.Readonly, base.Default = false, false, false, nil
	if in.Title != "" {
		base.Title = in.Title
	}
	if in.Help != "" {
		base.Help = in.Help
	}
	if in.Min != nil {
		base.Min = in.Min
	}
	if in.Max != nil {
		base.Max = in.Max
	}
	if in.Required {
		base.Required = true
	}
	base.Like = ""
	return base
}
