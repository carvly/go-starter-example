// Package specmap 用 yaml.Node 定位 OpenAPI 文件里各部分的行号范围：operation、tag 定义、schema、
// 顶层段落的末行。工具按行号编辑 spec，保留原文的格式和注释；定位交给 YAML 解析器，不依赖缩进或
// tags 的书写形式（流式 [a] 和块状 - a 都能识别）。
package specmap

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Range 是 1 起始、首尾都包含的行号范围。
type Range struct{ Start, End int }

// Operation 是 paths 下的一个操作。
type Operation struct {
	Path   string
	Method string
	Tags   []string
	Range  Range
}

// Key 是 "METHOD /path" 形式的操作标识。
func (o Operation) Key() string { return o.Method + " " + o.Path }

// Doc 是解析后的 spec。
type Doc struct {
	lines      []string
	Paths      map[string]Range // 整个 path item
	Operations []Operation
	TagDefs    map[string]Range // 顶层 tags 列表里的定义
	Schemas    map[string]Range // components.schemas 下的定义
	// SectionEnd 是 tags、paths、schemas 三段最后一行内容的行号（不含末尾空行和注释），不存在时为 0。
	SectionEnd map[string]int
}

var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace", "query"}

// Parse 解析 spec。
func Parse(spec []byte) (*Doc, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(spec, &root); err != nil {
		return nil, err
	}
	d := &Doc{
		lines:      strings.Split(string(spec), "\n"),
		Paths:      map[string]Range{},
		TagDefs:    map[string]Range{},
		Schemas:    map[string]Range{},
		SectionEnd: map[string]int{},
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("spec 顶层应该是映射")
	}
	doc := root.Content[0]
	docEnd := d.trimEnd(len(d.lines))
	for i, r := range d.entryRanges(doc, docEnd) {
		key, val := doc.Content[2*i], doc.Content[2*i+1]
		switch key.Value {
		case "tags":
			d.SectionEnd["tags"] = r.End
			if val.Kind == yaml.SequenceNode {
				for j, item := range val.Content {
					end := r.End
					if j+1 < len(val.Content) {
						end = d.trimEnd(d.headStart(val.Content[j+1].Line) - 1)
					}
					if name := mapValue(item, "name"); name != "" {
						d.TagDefs[name] = Range{d.headStart(item.Line), end}
					}
				}
			}
		case "paths":
			d.SectionEnd["paths"] = r.End
			if val.Kind != yaml.MappingNode {
				continue
			}
			for j, pr := range d.entryRanges(val, r.End) {
				path, item := val.Content[2*j].Value, val.Content[2*j+1]
				d.Paths[path] = pr
				if item.Kind != yaml.MappingNode {
					continue
				}
				for k, or := range d.entryRanges(item, pr.End) {
					method := item.Content[2*k].Value
					if !slices.Contains(httpMethods, method) {
						continue
					}
					d.Operations = append(d.Operations, Operation{
						Path: path, Method: strings.ToUpper(method), Tags: sequenceValues(mapNode(item.Content[2*k+1], "tags")), Range: or,
					})
				}
			}
		case "components":
			if val.Kind != yaml.MappingNode {
				continue
			}
			for j, cr := range d.entryRanges(val, r.End) {
				if val.Content[2*j].Value != "schemas" {
					continue
				}
				d.SectionEnd["schemas"] = cr.End
				schemas := val.Content[2*j+1]
				if schemas.Kind != yaml.MappingNode {
					continue
				}
				for k, sr := range d.entryRanges(schemas, cr.End) {
					d.Schemas[schemas.Content[2*k].Value] = sr
				}
			}
		}
	}
	return d, nil
}

// Text 是范围内的原文。
func (d *Doc) Text(r Range) string {
	if r.Start < 1 || r.End < r.Start || r.End > len(d.lines) {
		return ""
	}
	return strings.Join(d.lines[r.Start-1:r.End], "\n")
}

// Delete 返回删掉这些范围之后的原文。
func (d *Doc) Delete(ranges []Range) string {
	drop := map[int]bool{}
	for _, r := range ranges {
		for l := r.Start; l <= r.End; l++ {
			drop[l] = true
		}
	}
	out := make([]string, 0, len(d.lines))
	for i, line := range d.lines {
		if !drop[i+1] {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// entryRanges 是映射里每个键值对的范围：从键（连同紧挨着的头部注释）开始，到下一个键之前的
// 最后一行内容；最后一个键值对到 end 为止。
func (d *Doc) entryRanges(m *yaml.Node, end int) []Range {
	n := len(m.Content) / 2
	out := make([]Range, n)
	for i := range n {
		e := end
		if i+1 < n {
			e = d.trimEnd(d.headStart(m.Content[2*(i+1)].Line) - 1)
		}
		out[i] = Range{d.headStart(m.Content[2*i].Line), e}
	}
	return out
}

// headStart 把起始行向上扩到紧挨着的注释行。
func (d *Doc) headStart(line int) int {
	for line > 1 && strings.HasPrefix(strings.TrimSpace(d.lines[line-2]), "#") {
		line--
	}
	return line
}

// trimEnd 跳过末尾的空行和注释。
func (d *Doc) trimEnd(line int) int {
	for line > 0 {
		t := strings.TrimSpace(d.lines[line-1])
		if t != "" && !strings.HasPrefix(t, "#") {
			break
		}
		line--
	}
	return line
}

func mapNode(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func mapValue(m *yaml.Node, key string) string {
	if n := mapNode(m, key); n != nil && n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return ""
}

func sequenceValues(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	var out []string
	for _, item := range n.Content {
		out = append(out, item.Value)
	}
	return out
}
