package resource

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Model 是渲染模板用的数据：描述文件通过校验后，每个字段在每一层的写法都在这里算好，
// 模板只做拼装，不含判断逻辑之外的计算。字段类型矩阵见 docs/agents/generator.md。
type Model struct {
	D       *Descriptor
	N       Names
	GoMod   string // go.mod 的 module 路径
	Admin   bool
	Fields  []*FieldM
	Enums   []*FieldM
	Create  []*FieldM // 新建请求里的字段（readonly 之外）
	Update  []*FieldM // 编辑请求里的字段（readonly、immutable 之外）
	Unique  []*FieldM
	Checked []*FieldM // Service 里有校验的字段

	HasCreate, HasUpdate, HasDelete    bool
	HasUpdateRules                     bool
	Declared                           []ErrorM  // actions 和 rules 声明的错误，按错误码去重
	NeedDeref, NeedDateOf, NeedDateRef bool      // handler 里要用到的辅助函数
	Columns                            []*FieldM // 列表显示的字段（不含系统字段）
	Search                             []*FieldM
	Filters                            []*FieldM
	SortFields                         []string
	DefaultSort                        string
	DefaultDesc                        bool
	Actions                            []*ActionM
	Rules                              []*RuleM
	Indexes                            []IndexM
	HasDate                            bool // 有 date 字段：handler 要用 openapi_types.Date
	HasNotImplemented                  bool
	HasInvalidState                    bool

	WebList, WebGet, WebCreate bool
	WebAuth                    []string // 需要登录的 operationId
	WebAuthModule              string   // 提供前台登录检查的模块（WebAuth 非空时由 MakePlan 从代码推导）

	Migration string // 新 migration 的序号前缀，如 0008
}

type FieldM struct {
	Name, Title, Help, Type string
	Go                      string // Go 字段名，同 oapi-codegen（price_cents → PriceCents）
	GoType                  string // domain 里的类型
	Nullable                bool   // 不填时存 NULL，Go 里是指针
	NotEmpty                bool   // string / text 的 required：去掉首尾空格后非空
	Required                bool
	Default                 *Raw
	Min, Max                *int64
	Unique, Immutable       bool
	Readonly                bool
	Values                  []EnumValue
	EnumType                string // 枚举的 Go 类型和 spec schema 名，如 CategoryStatus
	Index                   int    // 在 fields（或动作参数）里的位置，让各字段的样本值互不相同
}

type EnumValue struct{ Value, Label, Const string }

type ActionM struct {
	Name, Title, Op, Path, Go string
	Custom, Confirm           string
	Input                     []*FieldM
	Sets                      []SetM
	Whens                     []WhenM
	Errors                    []ErrorM
	Declarative               bool
}

type SetM struct {
	Field *FieldM
	Param *FieldM // $参数；为 nil 时是常量
	Const string  // 常量的 Go 表达式
}

type WhenM struct {
	Field  *FieldM
	Values []string // Go 表达式
	Other  string   // 一个不满足条件的取值（Go 表达式），没有时为空
}

type ErrorM struct {
	Var, Code, Kind, Message, Field string
}

type RuleM struct {
	N                  int
	Custom             string
	OnWrite, OnDelete  bool // create/update 的规则拿到要写入的记录；delete 的规则拿到 id
	OnCreate, OnUpdate bool
	Errors             []ErrorM
}

type IndexM struct {
	Name, Columns, For, Where string
}

// BuildModel 在描述文件通过校验之后调用。
func BuildModel(d *Descriptor, goMod string, migration string) *Model {
	m := &Model{D: d, N: DeriveNames(d), GoMod: goMod, Admin: d.Admin != nil, Migration: migration}
	for i, e := range d.Fields {
		f := m.field(e.Key, e.Val)
		f.Index = i
		m.Fields = append(m.Fields, f)
		if f.EnumType != "" {
			m.Enums = append(m.Enums, f)
		}
		if f.Type == "date" {
			m.HasDate = true
		}
		if f.InCreate() {
			m.Create = append(m.Create, f)
		}
		if f.Unique {
			m.Unique = append(m.Unique, f)
		}
	}
	if a := d.Admin; a != nil {
		m.HasCreate = hasOp(a.Operations, "create")
		m.HasUpdate, m.HasDelete = hasOp(a.Operations, "update"), hasOp(a.Operations, "delete")
		if m.HasUpdate {
			for _, f := range m.Fields {
				if !f.Readonly && !f.Immutable {
					m.Update = append(m.Update, f)
				}
			}
		}
		for _, c := range a.List.Columns {
			if f := m.byName(c); f != nil {
				m.Columns = append(m.Columns, f)
			}
		}
		for _, s := range a.List.Search {
			m.Search = append(m.Search, m.byName(s))
		}
		for _, s := range a.List.Filter {
			m.Filters = append(m.Filters, m.filterField(s))
		}
		m.SortFields = slices.Clone(a.List.Sort)
		if !slices.Contains(m.SortFields, "created_at") {
			m.SortFields = append(m.SortFields, "created_at")
		}
		ds := cmpOr(a.List.DefaultSort, "-created_at")
		m.DefaultSort, m.DefaultDesc = strings.TrimPrefix(ds, "-"), strings.HasPrefix(ds, "-")
		for _, e := range a.Actions {
			m.Actions = append(m.Actions, m.action(e.Key, e.Val))
		}
	}
	if w := d.Web; w != nil {
		m.WebList, m.WebGet, m.WebCreate = hasOp(w.Operations, "list"), hasOp(w.Operations, "get"), hasOp(w.Operations, "create")
		m.HasCreate = m.WebCreate
		for _, op := range w.Auth {
			m.WebAuth = append(m.WebAuth, map[string]string{"list": m.N.OpList, "get": m.N.OpGet, "create": m.N.OpCreate}[op])
		}
	}
	if !m.HasCreate {
		// 没有新建操作：没有新建请求，各层（校验、表单、字段错误）都不该用到新建的字段。
		m.Create = nil
	}
	for i, r := range d.Rules {
		rm := &RuleM{N: i + 1, Custom: r.Custom}
		for _, op := range r.On {
			switch op {
			case "create":
				rm.OnCreate, rm.OnWrite = true, true
			case "update":
				rm.OnUpdate, rm.OnWrite = true, true
				m.HasUpdateRules = true
			case "delete":
				rm.OnDelete = true
			}
		}
		rm.Errors = m.errors(r.Errors)
		m.Rules = append(m.Rules, rm)
		m.HasNotImplemented = true
	}
	for _, idx := range d.Indexes {
		var names, cols []string
		for _, c := range idx.Columns {
			name := strings.TrimPrefix(c, "-")
			names = append(names, name)
			if strings.HasPrefix(c, "-") {
				cols = append(cols, name+" DESC")
			} else {
				cols = append(cols, name)
			}
		}
		m.Indexes = append(m.Indexes, IndexM{
			Name: "idx_" + m.N.Table + "_" + strings.Join(names, "_"), Columns: strings.Join(cols, ", "),
			For: idx.For, Where: idx.Where,
		})
	}
	// 只给经 Service 写入的字段生成校验：新建、编辑的字段和声明式动作的参数写入的字段。
	written := map[string]bool{}
	for _, f := range m.Update {
		written[f.Name] = true
	}
	if m.HasCreate {
		for _, f := range m.Create {
			written[f.Name] = true
		}
	}
	for _, a := range m.Actions {
		for _, s := range a.Sets {
			if s.Param != nil {
				written[s.Field.Name] = true
			}
		}
	}
	for _, f := range m.Fields {
		if written[f.Name] && f.Checks() {
			m.Checked = append(m.Checked, f)
		}
	}
	seen := map[string]bool{}
	add := func(es []ErrorM) {
		for _, e := range es {
			if !seen[e.Var] {
				seen[e.Var] = true
				m.Declared = append(m.Declared, e)
			}
		}
	}
	for _, a := range m.Actions {
		add(a.Errors)
	}
	for _, r := range m.Rules {
		add(r.Errors)
	}
	m.helpers()
	if !m.Admin && !m.hasCreatedAtIndex() {
		m.Indexes = append(m.Indexes, IndexM{
			Name: "idx_" + m.N.Table + "_created_at_id", Columns: "created_at DESC, id DESC",
			For: "前台列表的 cursor 分页：WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC",
		})
	}
	return m
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (m *Model) hasCreatedAtIndex() bool {
	for _, idx := range m.Indexes {
		if strings.HasPrefix(idx.Columns, "created_at") {
			return true
		}
	}
	return false
}

func (m *Model) byName(name string) *FieldM {
	for _, f := range m.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// filterField 返回筛选用的字段；created_at、updated_at 是系统字段，筛选时当作 datetime。
func (m *Model) filterField(name string) *FieldM {
	if f := m.byName(name); f != nil {
		return f
	}
	return &FieldM{Name: name, Go: pascal(name), Type: "datetime", GoType: "time.Time", Title: map[string]string{"created_at": "创建时间", "updated_at": "更新时间"}[name]}
}

func (m *Model) field(name string, f Field) *FieldM {
	fm := &FieldM{
		Name: name, Title: f.Title, Help: f.Help, Type: f.Type, Go: pascal(name),
		Required: f.Required, Default: f.Default, Min: f.Min, Max: f.Max,
		Unique: f.Unique, Immutable: f.Immutable, Readonly: f.Readonly,
	}
	if fm.Type == "money" && fm.Min == nil {
		fm.Min = new(int64(0))
	}
	fm.Nullable = f.Type != "string" && f.Type != "text" && f.Type != "bool" && !f.Required && f.Default == nil
	fm.NotEmpty = (f.Type == "string" || f.Type == "text") && f.Required
	switch f.Type {
	case "string", "text":
		fm.GoType = "string"
	case "int", "money":
		fm.GoType = "int64"
	case "bool":
		fm.GoType = "bool"
	case "date", "datetime":
		fm.GoType = "time.Time"
	case "enum":
		fm.EnumType = m.N.Type + fm.Go
		fm.GoType = fm.EnumType
		for _, v := range f.Values {
			fm.Values = append(fm.Values, EnumValue{Value: v.Key, Label: v.Val, Const: fm.EnumType + pascal(v.Key)})
		}
	}
	if fm.Nullable {
		fm.GoType = "*" + fm.GoType
	}
	return fm
}

// Checks 报告 Service 里要不要校验这个字段（有长度、范围、非空或枚举的约束）。
func (f *FieldM) Checks() bool {
	return f.NotEmpty || f.Min != nil || f.Max != nil || f.EnumType != ""
}

func (m *Model) action(name string, a Action) *ActionM {
	am := &ActionM{
		Name: name, Title: a.Title, Op: m.N.ActionOp(name), Path: m.N.ActionPath(name), Go: pascal(name),
		Custom: strings.TrimSpace(a.Custom), Confirm: a.Confirm, Declarative: len(a.Set) > 0,
	}
	for i, in := range a.Input {
		f := m.field(in.Key, resolveInput(m.D, in.Val))
		f.Index = 50 + i
		if lf := m.byName(in.Val.Like); lf != nil && lf.EnumType != "" {
			// 沿用字段的枚举类型（只在字段上生成），参数名和字段名不同时也引用同一个类型。
			f.EnumType, f.Values = lf.EnumType, lf.Values
			f.GoType = lf.EnumType
			if f.Nullable {
				f.GoType = "*" + f.GoType
			}
		}
		am.Input = append(am.Input, f)
	}
	for _, s := range a.Set {
		f := m.byName(s.Key)
		sm := SetM{Field: f}
		if p, ok := s.Val.Param(); ok {
			for _, in := range am.Input {
				if in.Name == p {
					sm.Param = in
				}
			}
		} else {
			sm.Const = goConst(f, s.Val.String())
		}
		am.Sets = append(am.Sets, sm)
	}
	for _, w := range a.When {
		f := m.byName(w.Key)
		wm := WhenM{Field: f}
		for _, v := range w.Val {
			wm.Values = append(wm.Values, goConst(f, v))
		}
		all := []string{"true", "false"}
		if f.EnumType != "" {
			all = nil
			for _, v := range f.Values {
				all = append(all, v.Value)
			}
		}
		for _, v := range all {
			if !slices.Contains(w.Val, v) {
				wm.Other = goConst(f, v)
				break
			}
		}
		am.Whens = append(am.Whens, wm)
		m.HasInvalidState = true
	}
	if am.Custom != "" {
		m.HasNotImplemented = true
		am.Errors = m.errors(a.Errors)
	}
	return am
}

func (m *Model) errors(errs Ordered[ErrorDef]) []ErrorM {
	var out []ErrorM
	for _, e := range errs {
		kind := "errs.KindInvalid"
		if e.Val.Kind == "conflict" {
			kind = "errs.KindConflict"
		}
		out = append(out, ErrorM{Var: "Err" + pascal(e.Key), Code: m.N.Resource + "_" + e.Key, Kind: kind, Message: e.Val.Message, Field: e.Val.Field})
	}
	return out
}

// goConst 是字段取值的 Go 表达式（在 domain 包外使用时由模板加包名前缀）。
func goConst(f *FieldM, v string) string {
	switch f.Type {
	case "enum":
		for _, ev := range f.Values {
			if ev.Value == v {
				return "domain." + ev.Const
			}
		}
	case "string", "text":
		return strconv.Quote(v)
	}
	return v
}

// --- 每一层的写法 ---

// SQLType 是列定义（不含约束名）。
func (f *FieldM) SQLType() string {
	t := map[string]string{"string": "TEXT", "text": "TEXT", "int": "BIGINT", "money": "BIGINT", "bool": "BOOLEAN", "enum": "TEXT", "date": "DATE", "datetime": "TIMESTAMPTZ"}[f.Type]
	switch {
	case f.Nullable:
		return t
	case f.Default != nil:
		return t + " NOT NULL DEFAULT " + f.sqlLiteral(f.Default.String())
	case f.Type == "bool":
		return t + " NOT NULL DEFAULT FALSE"
	case (f.Type == "string" || f.Type == "text") && !f.Required:
		return t + " NOT NULL DEFAULT ''"
	}
	return t + " NOT NULL"
}

func (f *FieldM) sqlLiteral(v string) string {
	switch f.Type {
	case "string", "text", "enum":
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	case "bool":
		return strings.ToUpper(v)
	}
	return v
}

// Check 是 CHECK 约束的条件，没有时为空：只写长度和数值范围这类不随业务演化的不变量，
// 不写枚举取值（database.md：枚举取值放应用层）。
func (f *FieldM) Check() string {
	var conds []string
	switch f.Type {
	case "string", "text":
		if f.Max != nil {
			conds = append(conds, fmt.Sprintf("char_length(%s) <= %d", f.Name, *f.Max))
		}
	case "int", "money":
		if f.Min != nil {
			conds = append(conds, fmt.Sprintf("%s >= %d", f.Name, *f.Min))
		}
		if f.Max != nil {
			conds = append(conds, fmt.Sprintf("%s <= %d", f.Name, *f.Max))
		}
	}
	return strings.Join(conds, " AND ")
}

// Comment 是列注释：枚举列列全取值，可空列写明 NULL 的含义，金额写明单位。
func (f *FieldM) Comment() string {
	c := f.Title
	if f.Help != "" {
		c += "：" + f.Help
	}
	switch {
	case f.EnumType != "":
		var vs []string
		for _, v := range f.Values {
			vs = append(vs, v.Value+" "+v.Label)
		}
		c += "；取值 " + strings.Join(vs, "、")
	case f.Type == "money":
		c += "，单位：分"
	}
	if f.Nullable {
		c += "；NULL 表示未填写"
	}
	return strings.ReplaceAll(c, "'", "''")
}

// SpecSchema 是属性在 spec 里的写法（不含 title），每行一项。
func (f *FieldM) SpecSchema() []string {
	var lines []string
	switch f.Type {
	case "string", "text":
		lines = append(lines, "type: string")
		if f.Min != nil && *f.Min > 0 {
			lines = append(lines, fmt.Sprintf("minLength: %d", *f.Min))
		}
		if f.Max != nil {
			lines = append(lines, fmt.Sprintf("maxLength: %d", *f.Max))
		}
		if f.Type == "text" {
			lines = append(lines, "x-field: text")
		}
	case "int", "money":
		lines = append(lines, "type: integer", "format: int64")
		if f.Min != nil {
			lines = append(lines, fmt.Sprintf("minimum: %d", *f.Min))
		}
		if f.Max != nil {
			lines = append(lines, fmt.Sprintf("maximum: %d", *f.Max))
		}
		if f.Type == "money" {
			lines = append(lines, "x-field: money")
		}
	case "bool":
		lines = append(lines, "type: boolean")
	case "enum":
		lines = append(lines, `$ref: "#/components/schemas/`+f.EnumType+`"`)
	case "date":
		lines = append(lines, "type: string", "format: date")
	case "datetime":
		lines = append(lines, "type: string", "format: date-time")
	}
	return lines
}

// Validate 是请求体字段的 validate 标签（handler 入口的校验），没有时为空。指针字段（请求里
// 可以不填）加 omitempty；int 不用 required：go-playground 的 required 把 0 当作没填。
func (f *FieldM) Validate(pointer bool) string {
	var tags []string
	switch f.Type {
	case "string", "text":
		if f.NotEmpty {
			tags = append(tags, "required")
		} else if f.Min != nil && *f.Min > 0 {
			tags = append(tags, "omitempty")
		}
		if f.Min != nil && *f.Min > 0 {
			tags = append(tags, fmt.Sprintf("min=%d", *f.Min))
		}
		if f.Max != nil {
			tags = append(tags, fmt.Sprintf("max=%d", *f.Max))
		}
	case "int", "money":
		if f.Min != nil {
			tags = append(tags, fmt.Sprintf("gte=%d", *f.Min))
		}
		if f.Max != nil {
			tags = append(tags, fmt.Sprintf("lte=%d", *f.Max))
		}
	case "enum":
		var vs []string
		for _, v := range f.Values {
			vs = append(vs, v.Value)
		}
		tags = append(tags, "oneof="+strings.Join(vs, " "))
	case "date", "datetime":
		if !pointer {
			tags = append(tags, "required")
		}
	}
	if pointer && len(tags) > 0 && tags[0] != "omitempty" {
		tags = append([]string{"omitempty"}, tags...)
	}
	return strings.Join(tags, ",")
}

// InCreateRequired 报告新建请求里这个字段是不是必须提供（spec 的 required，生成的结构体里
// 不是指针）。有 default 的、可空的和非必填的文本可以不填。
func (f *FieldM) InCreateRequired() bool { return f.Default == nil && f.InBodyRequired() }

// InBodyRequired 报告请求体（编辑、动作参数）里这个字段是不是必须提供。可空的字段和非必填的
// 文本可以不填：spec 的 required 对文本还表示"不能为空"（meta.gen.ts 据此生成表单校验），非必填的
// 文本列进 required 会让表单强制填写；不填时按空串处理，和数据库的默认值一致。
func (f *FieldM) InBodyRequired() bool { return !f.Nullable && !f.optionalText() }

func (f *FieldM) optionalText() bool { return (f.Type == "string" || f.Type == "text") && !f.NotEmpty }

// OapiType 是 oapi-codegen 生成的结构体里这个字段的类型（不含指针）。
func (f *FieldM) OapiType() string {
	switch f.Type {
	case "string", "text":
		return "string"
	case "int", "money":
		return "int64"
	case "bool":
		return "bool"
	case "date":
		return "openapi_types.Date"
	case "datetime":
		return "time.Time"
	}
	return "oapi." + f.EnumType
}

// BaseGoType 是去掉指针的 domain 类型（模板在 domain 包外使用时加 domain. 前缀）。
func (f *FieldM) BaseGoType() string { return strings.TrimPrefix(f.GoType, "*") }

// DomainType 是在 domain 包外引用这个字段的类型。
func (f *FieldM) DomainType() string {
	t := f.BaseGoType()
	if f.EnumType != "" {
		t = "domain." + t
	}
	if f.Nullable {
		return "*" + t
	}
	return t
}

// FromReq 是 handler 里把请求体字段换成 domain 值的表达式。pointer 为 true 时请求字段是指针。
func (f *FieldM) FromReq(expr string, pointer bool) string {
	switch {
	case f.Nullable:
		switch f.Type {
		case "enum":
			return "(*domain." + f.EnumType + ")(" + expr + ")"
		case "date":
			return "dateOf(" + expr + ")"
		}
		return expr
	case pointer:
		def := f.zeroOapi()
		if f.Default != nil {
			def = f.defaultOapi()
		}
		v := "deref(" + expr + ", " + def + ")"
		return f.convIn(v)
	}
	return f.convIn(expr)
}

func (f *FieldM) convIn(v string) string {
	switch f.Type {
	case "enum":
		return "domain." + f.EnumType + "(" + v + ")"
	case "date":
		return v + ".Time"
	}
	return v
}

func (f *FieldM) zeroOapi() string {
	switch f.Type {
	case "string", "text":
		return `""`
	case "bool":
		return "false"
	case "date":
		return "openapi_types.Date{}"
	case "datetime":
		return "time.Time{}"
	case "enum":
		return `oapi.` + f.EnumType + `("")`
	}
	return "0"
}

func (f *FieldM) defaultOapi() string {
	v := f.Default.String()
	switch f.Type {
	case "string", "text":
		return strconv.Quote(v)
	case "enum":
		return "oapi." + f.EnumType + "(" + strconv.Quote(v) + ")"
	}
	return v
}

// ToResp 是 handler 里把 domain 值换成响应字段的表达式。
func (f *FieldM) ToResp(expr string) string {
	switch {
	case f.Type == "enum" && f.Nullable:
		return "(*oapi." + f.EnumType + ")(" + expr + ")"
	case f.Type == "enum":
		return "oapi." + f.EnumType + "(" + expr + ")"
	case f.Type == "date" && f.Nullable:
		return "dateRef(" + expr + ")"
	case f.Type == "date":
		return "openapi_types.Date{Time: " + expr + "}"
	}
	return expr
}

// Sample 是一个满足全部约束的非零值（Go 表达式，domain 包外使用），夹具和测试用。
// pkg 是 domaintest 的包名前缀（在 domaintest 包内为空），n 让唯一字段的值彼此不同。
func (f *FieldM) Sample(pkg, n string) string {
	v := f.sampleBase(pkg, n)
	if f.Nullable {
		return "new(" + f.castSample(v) + ")"
	}
	return v
}

func (f *FieldM) castSample(v string) string {
	switch f.Type {
	case "int", "money":
		return "int64(" + v + ")"
	}
	return v
}

func (f *FieldM) sampleBase(pkg, n string) string {
	switch f.Type {
	case "string", "text":
		size := int64(8)
		if f.Min != nil && *f.Min > size {
			size = *f.Min
		}
		if f.Max != nil && *f.Max < size {
			size = *f.Max
		}
		return fmt.Sprintf("%sText(%s*100+%d, %d)", pkg, n, f.Index, size)
	case "int", "money":
		lo, hi := int64(1), int64(1_000_000)
		if f.Min != nil && *f.Min > lo {
			lo = *f.Min
		}
		if f.Max != nil {
			hi = *f.Max
		}
		if f.Max != nil && *f.Max < lo {
			lo = *f.Max
		}
		return fmt.Sprintf("%sInt(%s*100+%d, %d, %d)", pkg, n, f.Index, lo, hi)
	case "bool":
		return "true"
	case "enum":
		return "domain." + f.Values[f.Index%len(f.Values)].Const
	case "date":
		return "time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)"
	case "datetime":
		return "time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)"
	}
	return ""
}

// JSONSample 是请求体里的 JSON 值，和 Sample 对应。
func (f *FieldM) JSONSample() string {
	switch f.Type {
	case "string", "text":
		size := int64(8)
		if f.Min != nil && *f.Min > size {
			size = *f.Min
		}
		if f.Max != nil && *f.Max < size {
			size = *f.Max
		}
		return strconv.Quote(strings.Repeat(string(rune('a'+f.Index%26)), int(size)))
	case "int", "money":
		v := int64(2 + f.Index)
		if f.Min != nil && *f.Min > v {
			v = *f.Min
		}
		if f.Max != nil && *f.Max < v {
			v = *f.Max
		}
		return strconv.FormatInt(v, 10)
	case "bool":
		return "true"
	case "enum":
		return strconv.Quote(f.Values[0].Value)
	case "date":
		return `"2026-01-02"`
	case "datetime":
		return `"2026-01-02T03:04:05Z"`
	}
	return "null"
}

// Invalid 是 Service 校验要拒绝的值和对应的说明，每种约束一个。
func (f *FieldM) Invalid() [][2]string {
	var out [][2]string
	wrap := func(v string) string {
		if f.Nullable {
			return "new(" + f.castSample(v) + ")"
		}
		return v
	}
	switch f.Type {
	case "string", "text":
		if f.NotEmpty {
			out = append(out, [2]string{`"  "`, "只有空格"})
		}
		if f.Min != nil && *f.Min > 1 {
			out = append(out, [2]string{fmt.Sprintf("strings.Repeat(\"a\", %d)", *f.Min-1), "短于 min"})
		}
		if f.Max != nil {
			out = append(out, [2]string{fmt.Sprintf("strings.Repeat(\"a\", %d)", *f.Max+1), "长于 max"})
		}
	case "int", "money":
		if f.Min != nil && *f.Min > -9_000_000_000_000_000_000 {
			out = append(out, [2]string{wrap(strconv.FormatInt(*f.Min-1, 10)), "小于 min"})
		}
		if f.Max != nil && *f.Max < 9_000_000_000_000_000_000 {
			out = append(out, [2]string{wrap(strconv.FormatInt(*f.Max+1, 10)), "大于 max"})
		}
	case "enum":
		if f.Nullable {
			out = append(out, [2]string{"new(domain." + f.EnumType + `("nope"))`, "不是取值之一"})
		} else {
			out = append(out, [2]string{"domain." + f.EnumType + `("nope")`, "不是取值之一"})
		}
	}
	return out
}

// ErrInvalid 是字段校验失败的错误变量名。
func (f *FieldM) ErrInvalid() string { return "ErrInvalid" + f.Go }

// ErrTaken 是唯一约束冲突的错误变量名。
func (f *FieldM) ErrTaken() string { return "Err" + f.Go + "Taken" }

// Constraint 是唯一约束名。
func (m *Model) Constraint(f *FieldM) string { return "uk_" + m.N.Table + "_" + f.Name }

// helpers 算出 handler 里要生成哪些辅助函数（没用到的不生成，否则 unused 报错）。
func (m *Model) helpers() {
	if m.HasCreate {
		for _, f := range m.Create {
			if !f.InCreateRequired() && !f.Nullable {
				m.NeedDeref = true
			}
			if f.Type == "date" && f.Nullable {
				m.NeedDateOf = true
			}
		}
	}
	for _, f := range m.Update {
		if f.Type == "date" && f.Nullable {
			m.NeedDateOf = true
		}
		if f.optionalText() {
			m.NeedDeref = true
		}
	}
	for _, a := range m.Actions {
		for _, f := range a.Input {
			if f.Type == "date" && f.Nullable {
				m.NeedDateOf = true
			}
			if f.optionalText() {
				m.NeedDeref = true
			}
		}
	}
	for _, f := range m.Filters {
		if f.Type == "date" {
			m.NeedDateOf = true
		}
	}
	for _, f := range m.Fields {
		if f.Type == "date" && f.Nullable {
			m.NeedDateRef = true
		}
	}
}

// InputRequired 报告动作参数在 spec 里是不是 required（生成的结构体里不是指针），同 InBodyRequired。
func (f *FieldM) InputRequired() bool { return f.InBodyRequired() }

// DefaultGo 是 readonly 字段在新建时的值（domain 包内的 Go 表达式），没有 default 时为空。
func (f *FieldM) DefaultGo() string {
	if f.Default == nil {
		return ""
	}
	return strings.TrimPrefix(goConst(f, f.Default.String()), "domain.")
}

// InvalidCond 是"v 不合法"的 Go 条件（domain 包内）。
func (f *FieldM) InvalidCond(v string) string {
	if f.Nullable {
		return v + " != nil && " + f.invalidCond("(*"+v+")")
	}
	return f.invalidCond(v)
}

func (f *FieldM) invalidCond(v string) string {
	var conds []string
	switch f.Type {
	case "string", "text":
		lo := int64(0)
		if f.NotEmpty {
			lo = 1
		}
		if f.Min != nil && *f.Min > lo {
			lo = *f.Min
		}
		n := "utf8.RuneCountInString(" + v + ")"
		if lo > 0 && !f.NotEmpty {
			conds = append(conds, fmt.Sprintf("(%s > 0 && %s < %d)", n, n, lo))
		} else if lo > 0 {
			conds = append(conds, fmt.Sprintf("%s < %d", n, lo))
		}
		if f.Max != nil {
			conds = append(conds, fmt.Sprintf("%s > %d", n, *f.Max))
		}
	case "int", "money":
		if f.Min != nil {
			conds = append(conds, fmt.Sprintf("%s < %d", v, *f.Min))
		}
		if f.Max != nil {
			conds = append(conds, fmt.Sprintf("%s > %d", v, *f.Max))
		}
	case "enum":
		conds = append(conds, "!"+v+".Valid()")
	}
	if len(conds) > 1 && f.Nullable {
		return "(" + strings.Join(conds, " || ") + ")"
	}
	return strings.Join(conds, " || ")
}

// InvalidMessage 是字段校验失败时返回给客户端的 message。后台前端没有为生成的错误码另写文案，
// 直接显示它，所以用中文、带上字段的 title。
func (f *FieldM) InvalidMessage() string {
	switch f.Type {
	case "string", "text":
		lo := int64(0)
		if f.NotEmpty {
			lo = 1
		}
		if f.Min != nil && *f.Min > lo {
			lo = *f.Min
		}
		switch {
		case f.Max != nil && lo > 0:
			return fmt.Sprintf("%s要填 %d 到 %d 个字符", f.Title, lo, *f.Max)
		case f.Max != nil:
			return fmt.Sprintf("%s最多 %d 个字符", f.Title, *f.Max)
		}
		return fmt.Sprintf("%s至少 %d 个字符", f.Title, lo)
	case "int", "money":
		unit := ""
		if f.Type == "money" {
			unit = "（单位：分）"
		}
		switch {
		case f.Min != nil && f.Max != nil:
			return fmt.Sprintf("%s的范围是 %d 到 %d%s", f.Title, *f.Min, *f.Max, unit)
		case f.Min != nil:
			return fmt.Sprintf("%s不能小于 %d%s", f.Title, *f.Min, unit)
		case f.Max != nil:
			return fmt.Sprintf("%s不能大于 %d%s", f.Title, *f.Max, unit)
		}
	case "enum":
		var vs []string
		for _, v := range f.Values {
			vs = append(vs, v.Label)
		}
		return fmt.Sprintf("%s只能是%s", f.Title, strings.Join(vs, "、"))
	}
	return f.Title + "不合法"
}

// SortField 返回排序字段；created_at、updated_at 是系统字段。
func (m *Model) SortField(name string) *FieldM {
	if f := m.byName(name); f != nil {
		return f
	}
	return &FieldM{Name: name, Go: pascal(name), Type: "datetime", GoType: "time.Time"}
}

// WhenFails 是"v 不满足前置条件"的 Go 条件（domain 包外）。
func (m *Model) WhenFails(w WhenM, v string) string {
	f := w.Field
	x := v + "." + f.Go
	if f.Type == "bool" {
		if len(w.Values) == 2 {
			return "false"
		}
		if w.Values[0] == "true" {
			return "!" + x
		}
		return x
	}
	list := "[]domain." + f.EnumType + "{" + strings.Join(w.Values, ", ") + "}"
	if f.Nullable {
		return x + " == nil || !slices.Contains(" + list + ", *" + x + ")"
	}
	return "!slices.Contains(" + list + ", " + x + ")"
}

// SetValue 是 set 常量赋给字段的 Go 表达式（domain 包外），可空字段取地址。
func (m *Model) SetValue(s SetM) string {
	if !s.Field.Nullable {
		return s.Const
	}
	if s.Field.Type == "int" || s.Field.Type == "money" {
		return "new(int64(" + s.Const + "))"
	}
	return "new(" + s.Const + ")"
}

// WhenRaw 是前置条件的原始取值（SQL 参数用）。
func (w WhenM) Raw() []string {
	out := make([]string, len(w.Values))
	for i, v := range w.Values {
		out[i] = strings.TrimPrefix(v, "domain.")
		if w.Field.EnumType != "" {
			for _, ev := range w.Field.Values {
				if "domain."+ev.Const == v {
					out[i] = strconv.Quote(ev.Value)
				}
			}
		}
	}
	return out
}

// FirstSet 把字段设为第一个满足条件的取值；OtherSet 设为一个不满足条件的取值。
func (w WhenM) FirstSet() SetM { return SetM{Field: w.Field, Const: w.Values[0]} }
func (w WhenM) OtherSet() SetM { return SetM{Field: w.Field, Const: w.Other} }

// FirstOther 返回第一个有"不满足条件的取值"的前置条件，没有时为 nil。
func (a *ActionM) FirstOther() *WhenM {
	for i := range a.Whens {
		if a.Whens[i].Other != "" {
			return &a.Whens[i]
		}
	}
	return nil
}

// InvalidCase 是 Service 校验的一个用例：把字段改成 Value 后应当返回 Err。
type InvalidCase struct{ Name, Field, Value, Err string }

// CreateInvalid 是新建时每个字段的每种约束各一个违反的用例。
func (m *Model) CreateInvalid() []InvalidCase {
	var out []InvalidCase
	for _, f := range m.Create {
		for _, c := range f.Invalid() {
			out = append(out, InvalidCase{Name: f.Name + " " + c[1], Field: f.Go, Value: c[0], Err: f.ErrInvalid()})
		}
	}
	return out
}

// --- Repository 的 SQL ---

func (m *Model) SelectColumns() string {
	cols := []string{"id"}
	for _, f := range m.Fields {
		cols = append(cols, f.Name)
	}
	if m.HasUpdate {
		cols = append(cols, "version")
	}
	return strings.Join(append(cols, "created_at", "updated_at"), ", ")
}

func (m *Model) ScanArgs() string {
	args := []string{"&v.ID"}
	for _, f := range m.Fields {
		args = append(args, "&v."+f.Go)
	}
	if m.HasUpdate {
		args = append(args, "&v.Version")
	}
	return strings.Join(append(args, "&v.CreatedAt", "&v.UpdatedAt"), ", ")
}

func (m *Model) InsertColumns() string {
	cols := []string{"id"}
	for _, f := range m.Fields {
		cols = append(cols, f.Name)
	}
	return strings.Join(cols, ", ")
}

func (m *Model) InsertPlaceholders() string {
	var ps []string
	for i := range len(m.Fields) + 1 {
		ps = append(ps, fmt.Sprintf("$%d", i+1))
	}
	return strings.Join(ps, ", ")
}

func (m *Model) InsertArgs() string {
	args := []string{"v.ID"}
	for _, f := range m.Fields {
		args = append(args, "v."+f.Go)
	}
	return strings.Join(args, ", ")
}

func (m *Model) UpdateSet() string {
	var sets []string
	for i, f := range m.Update {
		sets = append(sets, fmt.Sprintf("%s = $%d", f.Name, i+2))
	}
	return strings.Join(sets, ", ")
}

func (m *Model) UpdateVersionArg() int { return len(m.Update) + 2 }

func (m *Model) HasEnumFilter() bool {
	for _, f := range m.Filters {
		if f.Type == "enum" {
			return true
		}
	}
	return false
}

func (m *Model) NeedExists() bool {
	if m.HasUpdate {
		return true
	}
	for _, a := range m.Actions {
		if a.Declarative && len(a.Whens) > 0 {
			return true
		}
	}
	return false
}

// ActionSQL 是声明式动作的 UPDATE（不含 RETURNING）。参数顺序：$1 是 id，然后是 set 的值，
// 最后是 when 的取值。
func (m *Model) ActionSQL(a *ActionM) string {
	n := 1
	var sets, conds []string
	for _, s := range a.Sets {
		n++
		sets = append(sets, fmt.Sprintf("%s = $%d", s.Field.Name, n))
	}
	for _, w := range a.Whens {
		n++
		if len(w.Values) == 1 {
			conds = append(conds, fmt.Sprintf("%s = $%d", w.Field.Name, n))
		} else {
			conds = append(conds, fmt.Sprintf("%s = ANY($%d)", w.Field.Name, n))
		}
	}
	sql := fmt.Sprintf("UPDATE %s SET %s, updated_at = now() WHERE id = $1", m.N.Table, strings.Join(sets, ", "))
	for _, c := range conds {
		sql += " AND " + c
	}
	return sql
}

func (m *Model) ActionArgs(a *ActionM) string {
	args := []string{"id"}
	for _, s := range a.Sets {
		if s.Param != nil {
			args = append(args, camel(s.Param.Name))
		} else {
			args = append(args, s.Const)
		}
	}
	for _, w := range a.Whens {
		raw := w.Raw()
		if len(raw) == 1 {
			args = append(args, raw[0])
		} else if w.Field.Type == "bool" {
			args = append(args, "[]bool{"+strings.Join(raw, ", ")+"}")
		} else {
			args = append(args, "[]string{"+strings.Join(raw, ", ")+"}")
		}
	}
	return strings.Join(args, ", ")
}

// ActionSegment 是动作路径的最后一段（_ 换成 -）。
func (m *Model) ActionSegment(a *ActionM) string { return strings.ReplaceAll(a.Name, "_", "-") }

// ActionBody 是动作请求体的 JSON（没有参数时为空）。
func (m *Model) ActionBody(a *ActionM) string {
	if len(a.Input) == 0 {
		return ""
	}
	var parts []string
	for _, in := range a.Input {
		parts = append(parts, strconv.Quote(in.Name)+":"+in.JSONSample())
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// --- spec ---

// PropM 是 spec 里的一个属性；Validate 是请求体字段的 validate 标签。
type PropM struct {
	F        *FieldM
	Validate string
}

func (m *Model) SortEnum() string {
	var vs []string
	for _, s := range m.SortFields {
		vs = append(vs, s, "-"+s)
	}
	return strings.Join(vs, ", ")
}

func (m *Model) CreateConflict() bool {
	if len(m.Unique) > 0 {
		return true
	}
	for _, r := range m.Rules {
		if r.OnCreate {
			return true
		}
	}
	return false
}

// HasCreateRules 报告新建时有没有要经过的 rules（还没补足时新建返回 ErrNotImplemented）。
func (m *Model) HasCreateRules() bool {
	for _, r := range m.Rules {
		if r.OnCreate {
			return true
		}
	}
	return false
}

func (m *Model) DeleteConflict() bool {
	for _, r := range m.Rules {
		if r.OnDelete {
			return true
		}
	}
	return false
}

func (m *Model) ResponseRequired() string {
	req := []string{"id"}
	for _, f := range m.Fields {
		if !f.Nullable {
			req = append(req, f.Name)
		}
	}
	if m.HasUpdate {
		req = append(req, "version")
	}
	return strings.Join(append(req, "created_at", "updated_at"), ", ")
}

func (m *Model) ResponseProps() []PropM {
	var ps []PropM
	for _, f := range m.Fields {
		ps = append(ps, PropM{F: f})
	}
	return ps
}

func (m *Model) CreateRequired() string {
	var req []string
	for _, f := range m.Create {
		if f.InCreateRequired() {
			req = append(req, f.Name)
		}
	}
	return strings.Join(req, ", ")
}

func (m *Model) CreateProps() []PropM {
	var ps []PropM
	for _, f := range m.Create {
		ps = append(ps, PropM{F: f, Validate: f.Validate(!f.InCreateRequired())})
	}
	return ps
}

func (m *Model) UpdateRequired() string {
	var req []string
	for _, f := range m.Update {
		if f.InBodyRequired() {
			req = append(req, f.Name)
		}
	}
	return strings.Join(append(req, "version"), ", ")
}

func (m *Model) UpdateProps() []PropM {
	var ps []PropM
	for _, f := range m.Update {
		ps = append(ps, PropM{F: f, Validate: f.Validate(!f.InBodyRequired())})
	}
	return ps
}

func (m *Model) InputRequired(a *ActionM) string {
	var req []string
	for _, f := range a.Input {
		if f.InputRequired() {
			req = append(req, f.Name)
		}
	}
	return strings.Join(req, ", ")
}

func (m *Model) InputProps(a *ActionM) []PropM {
	var ps []PropM
	for _, f := range a.Input {
		ps = append(ps, PropM{F: f, Validate: f.Validate(!f.InputRequired())})
	}
	return ps
}

// FakeNeed 报告内存 fake 要不要生成某个辅助函数（没用到的不生成，否则 unused 报错）。
func (m *Model) FakeNeed(name string) bool {
	switch name {
	case "cmpPtr", "cmpTime":
		for _, s := range m.SortFields {
			f := m.SortField(s)
			isTime := f.Type == "date" || f.Type == "datetime"
			if f.Nullable && isTime == (name == "cmpTime") {
				return true
			}
		}
	case "inRange":
		for _, f := range m.Filters {
			if f.Type == "date" || f.Type == "datetime" {
				return true
			}
		}
	}
	return false
}

// HasConstSet 报告声明式动作有没有写常量（契约测试据此检查结果）。
func (a *ActionM) HasConstSet() bool {
	for _, s := range a.Sets {
		if s.Param == nil {
			return true
		}
	}
	return false
}

// Mismatch 是"v 的这个字段不等于 set 的常量"的 Go 条件（契约测试用）。
func (s SetM) Mismatch(v string) string {
	x := v + "." + s.Field.Go
	switch {
	case s.Field.Nullable:
		return x + " == nil || *" + x + " != " + s.Const
	case s.Field.Type == "bool" && s.Const == "true":
		return "!" + x
	case s.Field.Type == "bool":
		return x
	}
	return x + " != " + s.Const
}

// Symbol 是规则的桩方法名：create/update 的是 checkRuleN，只有 delete 的是 checkRuleNDelete。
func (r *RuleM) Symbol() string {
	if r.OnWrite {
		return fmt.Sprintf("checkRule%d", r.N)
	}
	return fmt.Sprintf("checkRule%dDelete", r.N)
}

// InCreate 报告新建请求里有没有这个字段：readonly 字段编辑时不能改、只能经动作改，其中 required
// 且没有 default 的新建时要填（如初始库存），其余的新建时取 default 或为空。
func (f *FieldM) InCreate() bool { return !f.Readonly || (f.Required && f.Default == nil) }
