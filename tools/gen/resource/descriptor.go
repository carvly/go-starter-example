// Package resource 是资源描述文件：解析、校验、推出名字，并生成一个新模块的后端代码。
// 规则和用法见 docs/agents/generator.md。
package resource

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Descriptor 是一份资源描述文件。键名、类型来自 yaml 标签，gen schema 从这些类型导出 JSON Schema，
// 解码也按这些类型做，两者不会不一致。desc 标签是 JSON Schema 里的说明。
type Descriptor struct {
	Module      string         `yaml:"module" desc:"新模块名，同时是 Go 包名和目录名"`
	Resource    string         `yaml:"resource" desc:"资源名，单数，snake_case"`
	Plural      string         `yaml:"plural" desc:"复数，不规则时写；用于表名、tag、路径"`
	Title       string         `yaml:"title" desc:"中文名"`
	Description string         `yaml:"description" desc:"一句话说明"`
	Display     string         `yaml:"display" desc:"代表一条记录给人看的字段，required 的 string"`
	Fields      Ordered[Field] `yaml:"fields" desc:"字段，按书写顺序"`
	Rules       []Rule         `yaml:"rules" desc:"新建、修改、删除时的业务规则"`
	Indexes     []Index        `yaml:"indexes" desc:"主键和唯一约束以外的索引"`
	Admin       *Admin         `yaml:"admin" desc:"后台接入面"`
	Web         *Web           `yaml:"web" desc:"前台接入面"`
}

type Field struct {
	Type      string          `yaml:"type" desc:"string、text、int、money、bool、enum、date、datetime"`
	Title     string          `yaml:"title" desc:"中文名"`
	Help      string          `yaml:"help" desc:"说明文字"`
	Values    Ordered[string] `yaml:"values" desc:"enum 的取值和中文，按显示顺序"`
	Required  bool            `yaml:"required"`
	Default   *Raw            `yaml:"default"`
	Min       *int64          `yaml:"min" desc:"字符串是字符数，数值是值，金额单位是分"`
	Max       *int64          `yaml:"max"`
	Unique    bool            `yaml:"unique"`
	Immutable bool            `yaml:"immutable" desc:"创建后不能改"`
	Readonly  bool            `yaml:"readonly" desc:"只能经动作修改"`
	Like      string          `yaml:"like" desc:"只用于动作的 input：沿用某个字段的类型和约束"`
}

type Admin struct {
	Operations []string        `yaml:"operations" desc:"create、update、delete 的子集"`
	List       AdminList       `yaml:"list"`
	Actions    Ordered[Action] `yaml:"actions"`
	Nav        AdminNav        `yaml:"nav" desc:"侧边栏菜单项"`
}

type AdminNav struct {
	Group string `yaml:"group" desc:"侧边栏分组，不写时是 业务"`
	Icon  string `yaml:"icon" desc:"lucide-react 的图标名（PascalCase，如 Users），不写时是 Table"`
	Order *int64 `yaml:"order" desc:"同组内的顺序，小的在前；不写时是 100（排在手写页面之后）"`
}

type AdminList struct {
	Columns     []string `yaml:"columns"`
	Search      []string `yaml:"search"`
	Filter      []string `yaml:"filter"`
	Sort        []string `yaml:"sort"`
	DefaultSort string   `yaml:"default_sort" desc:"倒序写 -<字段>，不写时是 -created_at"`
}

type Action struct {
	Title   string             `yaml:"title"`
	Input   Ordered[Field]     `yaml:"input"`
	Set     Ordered[*Raw]      `yaml:"set" desc:"声明式：常量或 $参数"`
	When    Ordered[OneOrMore] `yaml:"when" desc:"前置条件，enum 或 bool 字段的取值"`
	Custom  string             `yaml:"custom" desc:"自定义：要实现的规则、错误情形和并发要求"`
	Errors  Ordered[ErrorDef]  `yaml:"errors"`
	Confirm string             `yaml:"confirm"`
}

type ErrorDef struct {
	Kind    string `yaml:"kind" desc:"invalid 或 conflict"`
	Message string `yaml:"message"`
	Field   string `yaml:"field"`
}

type Rule struct {
	On     []string          `yaml:"on" desc:"create、update、delete 的子集"`
	Custom string            `yaml:"custom"`
	Errors Ordered[ErrorDef] `yaml:"errors"`
}

type Index struct {
	Columns []string `yaml:"columns" desc:"列名，倒序写 -<列>"`
	For     string   `yaml:"for" desc:"服务哪条查询"`
	Where   string   `yaml:"where" desc:"部分索引的条件"`
}

type Web struct {
	Operations []string `yaml:"operations" desc:"list、get、create 的子集"`
	Auth       []string `yaml:"auth" desc:"需要登录的操作"`
}

// Entry 是 Ordered 的一项。
type Entry[T any] struct {
	Key string
	Val T
}

// Ordered 是保留书写顺序的映射：字段、取值、动作的顺序就是生成结果里的顺序。
type Ordered[T any] []Entry[T]

func (o Ordered[T]) Get(key string) (T, bool) {
	for _, e := range o {
		if e.Key == key {
			return e.Val, true
		}
	}
	var zero T
	return zero, false
}

func (o Ordered[T]) Keys() []string {
	keys := make([]string, len(o))
	for i, e := range o {
		keys[i] = e.Key
	}
	return keys
}

// Raw 是原样保留的标量（默认值、set 的值），由校验按目标字段的类型解释。
type Raw struct {
	Node *yaml.Node
}

func (r *Raw) String() string { return r.Node.Value }

// IsParam 报告值是不是 $参数。
func (r *Raw) Param() (string, bool) {
	if r.Node.Tag != "!!str" {
		return "", false
	}
	return strings.CutPrefix(r.Node.Value, "$")
}

// OneOrMore 是一个值或一组值（when: { status: enabled } 或 when: { status: [a, b] }）。
type OneOrMore []string

// Problem 是一条校验结果。Rule 是规则编号；Hint 为 true 时只是提示，不阻止生成。
type Problem struct {
	Rule    string `json:"rule"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Path    string `json:"path"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
	Doc     string `json:"doc"`
	Hint    bool   `json:"hint,omitempty"`
}

func (p Problem) String() string {
	loc := p.File
	if p.Line > 0 {
		loc = fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
	}
	path := ""
	if p.Path != "" {
		path = " " + p.Path + ":"
	}
	kind := ""
	if p.Hint {
		kind = "提示："
	}
	return fmt.Sprintf("%s%s %s%s。改法：%s（%s，%s）", loc, path, kind, p.Message, p.Fix, p.Rule, p.Doc)
}

// Parsed 是解码后的描述文件，带着每个路径在文件里的位置。
type Parsed struct {
	File string
	D    Descriptor
	pos  map[string]*yaml.Node
}

// Parse 严格解码：未知键、类型不对都报错，并且一次报出全部问题。
func Parse(file string, data []byte) (*Parsed, []Problem) {
	p := &Parsed{File: file, pos: map[string]*yaml.Node{}}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		line, col := yamlErrPos(err)
		return p, []Problem{problem("YAML-SYNTAX", file, line, col, "", "YAML 语法错误："+err.Error(), "按报错的位置修正缩进、引号或冒号；长文本用块样式（custom: >-）单独起行")}
	}
	if len(root.Content) == 0 {
		return p, []Problem{problem("YAML-SYNTAX", file, 1, 1, "", "文件是空的", "参考 go run ./tools/gen example admin 的输出填写")}
	}
	d := decoder{file: file, pos: p.pos}
	d.decode("", root.Content[0], reflect.ValueOf(&p.D).Elem())
	return p, d.problems
}

// Pos 返回路径在文件里的位置；路径本身不存在时退回到最近的上级。
func (p *Parsed) Pos(path string) (int, int) {
	for {
		if n, ok := p.pos[path]; ok {
			return n.Line, n.Column
		}
		i := strings.LastIndexAny(path, ".[")
		if i < 0 {
			if n, ok := p.pos[""]; ok {
				return n.Line, n.Column
			}
			return 0, 0
		}
		path = path[:i]
	}
}

func yamlErrPos(err error) (int, int) {
	msg := err.Error()
	if _, rest, ok := strings.Cut(msg, "line "); ok {
		n, _, _ := strings.Cut(rest, ":")
		if line, e := strconv.Atoi(n); e == nil {
			return line, 1
		}
	}
	return 1, 1
}

type decoder struct {
	file     string
	pos      map[string]*yaml.Node
	problems []Problem
}

func (d *decoder) fail(n *yaml.Node, path, msg, fix string) {
	d.problems = append(d.problems, problem("YAML-TYPE", d.file, n.Line, n.Column, path, msg, fix))
}

var (
	rawType       = reflect.TypeFor[*Raw]()
	oneOrMoreType = reflect.TypeFor[OneOrMore]()
)

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func (d *decoder) decode(path string, n *yaml.Node, v reflect.Value) {
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	d.pos[path] = n
	switch {
	case v.Type() == rawType:
		if n.Kind != yaml.ScalarNode {
			d.fail(n, path, "应该是一个值（数字、true/false、字符串或 $参数）", "写成单个值")
			return
		}
		v.Set(reflect.ValueOf(&Raw{Node: n}))
		return
	case v.Type() == oneOrMoreType:
		var out []string
		switch n.Kind {
		case yaml.ScalarNode:
			out = []string{n.Value}
		case yaml.SequenceNode:
			for _, c := range n.Content {
				out = append(out, c.Value)
			}
		default:
			d.fail(n, path, "应该是一个取值或取值的列表", "写成 enabled 或 [enabled, draft]")
			return
		}
		v.Set(reflect.ValueOf(OneOrMore(out)))
		return
	}
	if n.Tag == "!!null" && v.Kind() != reflect.String && v.Kind() != reflect.Bool && v.Kind() != reflect.Int64 {
		return // 写了键但没写值（如 actions: 后面为空）：当作没写
	}
	switch v.Kind() {
	case reflect.Pointer:
		e := reflect.New(v.Type().Elem())
		d.decode(path, n, e.Elem())
		v.Set(e)
	case reflect.Struct:
		d.decodeStruct(path, n, v)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Struct && v.Type().Elem().NumField() == 2 && v.Type().Elem().Field(0).Name == "Key" {
			d.decodeOrdered(path, n, v)
			return
		}
		if n.Kind != yaml.SequenceNode {
			d.fail(n, path, "应该是列表", "写成 [a, b] 或每行一个 - a")
			return
		}
		s := reflect.MakeSlice(v.Type(), len(n.Content), len(n.Content))
		for i, c := range n.Content {
			d.decode(fmt.Sprintf("%s[%d]", path, i), c, s.Index(i))
		}
		v.Set(s)
	case reflect.String:
		if n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
			d.fail(n, path, "应该是字符串", "写成单行文本；长文本用块样式（>-）单独起行")
			return
		}
		if n.Style&yaml.FoldedStyle != 0 {
			v.SetString(unfoldCJK(n.Value))
		} else {
			v.SetString(n.Value)
		}
	case reflect.Bool:
		var b bool
		if n.Kind != yaml.ScalarNode || n.Decode(&b) != nil {
			d.fail(n, path, "应该是 true 或 false", "写成 true；不需要时整行删掉")
			return
		}
		v.SetBool(b)
	case reflect.Int64:
		var i int64
		if n.Kind != yaml.ScalarNode || n.Decode(&i) != nil {
			d.fail(n, path, "应该是整数", "写成不带引号的整数，如 500")
			return
		}
		v.SetInt(i)
	default:
		panic("resource: unsupported kind " + v.Kind().String())
	}
}

func (d *decoder) decodeStruct(path string, n *yaml.Node, v reflect.Value) {
	if n.Kind != yaml.MappingNode {
		d.fail(n, path, "应该是键值映射", "写成 key: value 的形式")
		return
	}
	fields := map[string]int{}
	var names []string
	for i := range v.NumField() {
		tag := v.Type().Field(i).Tag.Get("yaml")
		fields[tag] = i
		names = append(names, tag)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, val := n.Content[i], n.Content[i+1]
		idx, ok := fields[k.Value]
		if !ok {
			fix := "可用的键：" + strings.Join(names, "、")
			if s := closest(k.Value, names); s != "" {
				fix = "是不是 " + s + "？" + fix
			}
			d.problems = append(d.problems, problem("YAML-UNKNOWN-KEY", d.file, k.Line, k.Column, join(path, k.Value), "没有这个键", fix))
			continue
		}
		d.decode(join(path, k.Value), val, v.Field(idx))
	}
}

func (d *decoder) decodeOrdered(path string, n *yaml.Node, v reflect.Value) {
	if n.Kind != yaml.MappingNode {
		d.fail(n, path, "应该是键值映射", "写成 <名字>: <定义> 的形式")
		return
	}
	s := reflect.MakeSlice(v.Type(), 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, val := n.Content[i], n.Content[i+1]
		e := reflect.New(v.Type().Elem()).Elem()
		e.Field(0).SetString(k.Value)
		p := join(path, k.Value)
		d.pos[p+".$key"] = k
		d.decode(p, val, e.Field(1))
		s = reflect.Append(s, e)
	}
	v.Set(s)
}

// closest 找编辑距离最近的候选（拼错键名时提示），距离太远时不提示。
func closest(s string, candidates []string) string {
	best, bestD := "", 3
	for _, c := range candidates {
		if d := distance(s, c); d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func problem(rule, file string, line, col int, path, msg, fix string) Problem {
	return Problem{Rule: rule, File: file, Line: line, Col: col, Path: path, Message: msg, Fix: fix, Doc: docRef(rule)}
}

func docRef(rule string) string {
	return "docs/agents/generator-rules.md#" + strings.ToLower(rule)
}

// reFoldedCJK 是折叠块（>-）把换行换成的空格，两边都是中文字符或全角标点。
var reFoldedCJK = regexp.MustCompile(`([\p{Han}\x{3000}-\x{303F}\x{FF00}-\x{FFEF}]) ([\p{Han}\x{3000}-\x{303F}\x{FF00}-\x{FFEF}])`)

// unfoldCJK 去掉折叠块在两个中文字符之间留下的空格：长文本按规范用 >- 折行书写，YAML 把换行折成
// 空格，英文需要它，中文里它是多余的（"的行 置为"）。
func unfoldCJK(s string) string {
	for {
		t := reFoldedCJK.ReplaceAllString(s, "$1$2")
		if t == s {
			return s
		}
		s = t
	}
}
