package main

import (
	"slices"
	"strings"

	"app.local/tools/internal/modinfo"
)

// starterOwnedPaths 是 target 版本里 starter 所有的文件：starter-owned.txt 的每一行按前缀匹配，模块目录行
// 再并入该模块 module.yaml 的 web_paths。清单和文件都取自同一个版本。removed 里的模块（及其依赖闭包）
// 已被项目删除，它们的路径不算。派生文件和锁文件不算（见 docs/agents/project.md 的"所有权"）。
func starterOwnedPaths(root, target string, removed []string) ([]string, error) {
	repo := repository{root: root, target: target}
	owned, err := repo.Read("starter-owned.txt")
	if err != nil {
		return nil, err
	}
	prefixes := ownedPrefixes(owned)
	for _, module := range ownedModules(owned) {
		manifest, err := modinfo.ReadManifest(repo, module)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, manifest.WebPaths...)
	}
	files, err := repo.Files()
	if err != nil {
		return nil, err
	}
	var excluded []string
	if len(removed) > 0 {
		plan, err := compute(repo, removed)
		if err != nil {
			return nil, err
		}
		excluded = plan.Paths
	}
	var out []string
	for _, file := range files {
		if !hasAnyPrefix(file, prefixes) || derivedOrLock(file) || hasAnyPrefix(file, excluded) {
			continue
		}
		out = append(out, file)
	}
	slices.Sort(out)
	return out, nil
}

func ownedPrefixes(owned string) []string {
	var out []string
	for line := range strings.SplitSeq(owned, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

func hasAnyPrefix(file string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(file, p) {
			return true
		}
	}
	return false
}

// derivedOrLock 是从来源重新生成的文件、锁文件，以及项目自己的 CI 工作流（.github/workflows/project-*）。
func derivedOrLock(file string) bool {
	return strings.Contains(file, ".gen.") || strings.HasSuffix(file, "schema.d.ts") || strings.HasSuffix(file, "go.sum") ||
		strings.HasSuffix(file, "pnpm-lock.yaml") || strings.HasPrefix(file, ".github/workflows/project-")
}
