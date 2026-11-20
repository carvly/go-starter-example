package main

import (
	"slices"

	"app.local/tools/internal/specmap"
)

// removeFromSpec 删除带 tag 的操作、它的 tag 定义，以及因此不再被引用的 schema；其余原文不动。
func removeFromSpec(spec, tag string) (string, error) {
	doc, err := specmap.Parse([]byte(spec))
	if err != nil {
		return "", err
	}
	var drop []specmap.Range
	remaining := map[string]int{}
	removed := map[string]int{}
	for _, op := range doc.Operations {
		if slices.Contains(op.Tags, tag) {
			drop = append(drop, op.Range)
			removed[op.Path]++
		} else {
			remaining[op.Path]++
		}
	}
	for path, n := range removed {
		if n > 0 && remaining[path] == 0 {
			// 操作全删光的 path item 整个删掉，不留只剩 parameters 的空壳。
			drop = append(drop, doc.Paths[path])
		}
	}
	if r, ok := doc.TagDefs[tag]; ok {
		drop = append(drop, r)
	}
	return pruneUnreferencedSchemas(doc.Delete(drop))
}

// pruneUnreferencedSchemas 删除 schemas 里没有被 schemas 之外的内容（直接或间接）引用的定义。
func pruneUnreferencedSchemas(spec string) (string, error) {
	doc, err := specmap.Parse([]byte(spec))
	if err != nil {
		return "", err
	}
	if len(doc.Schemas) == 0 {
		return spec, nil
	}
	var schemaRanges []specmap.Range
	for _, r := range doc.Schemas {
		schemaRanges = append(schemaRanges, r)
	}
	outside := doc.Delete(schemaRanges)
	var roots []string
	for _, match := range referenced(outside) {
		roots = append(roots, match[1])
	}
	keep := schemaClosure(roots, schemaTexts(doc))
	var drop []specmap.Range
	for name, r := range doc.Schemas {
		if !keep[name] {
			drop = append(drop, r)
		}
	}
	if len(drop) == 0 {
		return spec, nil
	}
	return doc.Delete(drop), nil
}
