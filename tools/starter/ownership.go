package main

import (
	"fmt"
	"maps"
	"slices"

	"app.local/tools/internal/specmap"
)

// ownershipDrift 列出项目相对基线改过的 starter 操作和它们引用的 schema。
func ownershipDrift(root, base string) ([]string, error) {
	current := repository{root: root}
	tags, err := starterTags(current)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, name := range []string{"api/openapi.yaml", "api/admin.yaml"} {
		before, err := parseAt(repository{root: root, target: base}, name)
		if err != nil {
			return nil, err
		}
		after, err := parseAt(current, name)
		if err != nil {
			return nil, err
		}
		beforeOps, afterOps := taggedOperations(before, tags), taggedOperations(after, tags)
		keys := map[string]bool{}
		for key := range beforeOps {
			keys[key] = true
		}
		for key := range afterOps {
			keys[key] = true
		}
		for _, key := range slices.Sorted(maps.Keys(keys)) {
			if beforeOps[key] != afterOps[key] {
				problems = append(problems, fmt.Sprintf("%s: starter operation changed: %s", name, key))
			}
		}
		beforeSchemas, afterSchemas := schemaTexts(before), schemaTexts(after)
		var roots []string
		for _, block := range slices.Concat(slices.Collect(maps.Values(beforeOps)), slices.Collect(maps.Values(afterOps))) {
			for _, match := range referenced(block) {
				roots = append(roots, match[1])
			}
		}
		all := schemaClosure(roots, beforeSchemas)
		maps.Copy(all, schemaClosure(roots, afterSchemas))
		for _, schemaName := range slices.Sorted(maps.Keys(all)) {
			if beforeSchemas[schemaName] != afterSchemas[schemaName] {
				problems = append(problems, fmt.Sprintf("%s: starter schema changed: %s", name, schemaName))
			}
		}
	}
	return problems, nil
}

func parseAt(repo repository, name string) (*specmap.Doc, error) {
	content, err := repo.Read(name)
	if err != nil {
		return nil, err
	}
	doc, err := specmap.Parse([]byte(content))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return doc, nil
}
