package main

import (
	"regexp"
	"strings"

	"app.local/tools/internal/specmap"
)

var schemaRefRe = regexp.MustCompile(`\$ref:\s*["']?#/components/schemas/([A-Za-z0-9_]+)`)

func referenced(text string) [][]string {
	return schemaRefRe.FindAllStringSubmatch(text, -1)
}

// taggedOperations 是 spec 里带有任一指定 tag 的操作，键是 "METHOD /path"，值是原文。
func taggedOperations(doc *specmap.Doc, tags map[string]bool) map[string]string {
	out := map[string]string{}
	for _, op := range doc.Operations {
		for _, tag := range op.Tags {
			if tags[tag] {
				out[op.Key()] = doc.Text(op.Range)
				break
			}
		}
	}
	return out
}

// schemaTexts 是 components.schemas 下每个定义的原文。
func schemaTexts(doc *specmap.Doc) map[string]string {
	out := map[string]string{}
	for name, r := range doc.Schemas {
		out[name] = doc.Text(r)
	}
	return out
}

func schemaClosure(initial []string, definitions map[string]string) map[string]bool {
	seen := map[string]bool{}
	queue := append([]string(nil), initial...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		for _, match := range referenced(definitions[name]) {
			queue = append(queue, match[1])
		}
	}
	return seen
}

// starterTags 是 starter 所有的模块（starter-owned.txt 里的 internal/modules/<m>/ 行）在两份 spec 里的 tag。
func starterTags(repo repository) (map[string]bool, error) {
	owned, err := repo.Read("starter-owned.txt")
	if err != nil {
		return nil, err
	}
	tags := map[string]bool{}
	for _, module := range ownedModules(owned) {
		for _, api := range []string{"httpapi", "adminapi"} {
			content, err := repo.Read("internal/modules/" + module + "/" + api + "/oapi/config.yaml")
			if err != nil {
				continue
			}
			if tag := includeTag(content); tag != "" {
				tags[tag] = true
			}
		}
	}
	return tags, nil
}

// ownedModules 是所有权清单里以模块目录出现的模块名。
func ownedModules(owned string) []string {
	var out []string
	for line := range strings.SplitSeq(owned, "\n") {
		line = strings.TrimSpace(line)
		parts := strings.Split(strings.TrimSuffix(line, "/"), "/")
		if strings.HasSuffix(line, "/") && len(parts) == 3 && parts[0] == "internal" && parts[1] == "modules" {
			out = append(out, parts[2])
		}
	}
	return out
}
