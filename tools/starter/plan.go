package main

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"app.local/tools/internal/modinfo"
)

type DeletionSet struct {
	Modules      []string `json:"modules"`
	Paths        []string `json:"paths"`
	HTTPTags     []string `json:"http_tags"`
	AdminTags    []string `json:"admin_tags"`
	AdminSurface bool     `json:"admin_surface"`
}

// repository 读取工作区（target 为空）或某个 git 版本的文件，实现 modinfo.Source。
type repository struct {
	root   string
	target string
}

func (r repository) Read(name string) (string, error) {
	if r.target == "" {
		data, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(name)))
		return string(data), err
	}
	out, err := r.git("show", r.target+":"+name)
	if err != nil {
		return "", err
	}
	return out, nil
}

func (r repository) Files() ([]string, error) {
	if r.target != "" {
		out, err := r.git("ls-tree", "-r", "--name-only", r.target)
		if err != nil {
			return nil, err
		}
		return strings.Fields(out), nil
	}
	var files []string
	err := filepath.WalkDir(r.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", ".gen":
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(r.root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

// git 在仓库里执行 git 命令，返回原样输出（不去掉首尾空白，git show 的文件内容要保持原样）。
func (r repository) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

var (
	includeTagsRe    = regexp.MustCompile(`(?m)^\s*include-tags:\s*\[([^]]+)\]`)
	moduleRequiresRe = regexp.MustCompile(`"([^"]+)"\s*:\s*\{([^}]*)\}`)
	quotedRe         = regexp.MustCompile(`"([^"]+)"`)
	appModuleFileRe  = `^[a-z]+(_[a-z]+)*_%s(_[a-z_]+)?\.go$`
)

func includeTag(content string) string {
	match := includeTagsRe.FindStringSubmatch(content)
	if match == nil {
		return ""
	}
	return strings.ReplaceAll(match[1], " ", "")
}

// moduleRequires 解析 modules.go 里可选的 var moduleRequires：构造行推导不出的依赖。
func moduleRequires(content string) map[string][]string {
	out := map[string][]string{}
	start := strings.Index(content, "var moduleRequires")
	if start < 0 {
		return out
	}
	end := strings.Index(content[start:], "\n}")
	if end >= 0 {
		content = content[start : start+end]
	}
	for _, match := range moduleRequiresRe.FindAllStringSubmatch(content, -1) {
		for _, dep := range quotedRe.FindAllStringSubmatch(match[2], -1) {
			out[match[1]] = append(out[match[1]], dep[1])
		}
	}
	return out
}

func closure(modules []string, deps map[string][]string, removed []string) ([]string, error) {
	known := map[string]bool{}
	for _, module := range modules {
		known[module] = true
	}
	drop := map[string]bool{}
	for _, module := range removed {
		if !known[module] {
			return nil, fmt.Errorf("starter: unknown module %q: the target version has no internal/modules/%s (renamed? see starter-changelog.md, then update starter-removed.yaml)", module, module)
		}
		drop[module] = true
	}
	changed := true
	for changed {
		changed = false
		for _, module := range modules {
			if drop[module] {
				continue
			}
			for _, dep := range deps[module] {
				if drop[dep] {
					drop[module] = true
					changed = true
					break
				}
			}
		}
	}
	var out []string
	for module := range drop {
		out = append(out, module)
	}
	slices.Sort(out)
	return out, nil
}

func compute(repo repository, removed []string) (DeletionSet, error) {
	files, err := repo.Files()
	if err != nil {
		return DeletionSet{}, err
	}
	modules := modinfo.List(files)
	modulesGo, err := repo.Read(modinfo.ModulesGo)
	if err != nil {
		return DeletionSet{}, err
	}
	deps := moduleRequires(modulesGo)
	for _, module := range modules {
		line := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(module) + `M\s*:=\s*(.*)$`).FindStringSubmatch(modulesGo)
		if line == nil {
			return DeletionSet{}, fmt.Errorf("starter: constructor for %s not found", module)
		}
		for _, other := range modules {
			if other != module && regexp.MustCompile(`\b`+regexp.QuoteMeta(other)+`M\b`).MatchString(line[1]) {
				deps[module] = append(deps[module], other)
			}
		}
	}
	manifests := map[string]modinfo.Manifest{}
	webOwners := map[string]string{}
	for _, module := range modules {
		manifest, err := modinfo.ReadManifest(repo, module)
		if err != nil {
			return DeletionSet{}, fmt.Errorf("starter: %w", err)
		}
		for _, webPath := range manifest.WebPaths {
			if owner, exists := webOwners[webPath]; exists {
				return DeletionSet{}, fmt.Errorf("starter: web path %s claimed by both %s and %s", webPath, owner, module)
			}
			webOwners[webPath] = module
		}
		manifests[module] = manifest
	}
	adminAuth, err := modinfo.AdminAuthProviders(modulesGo)
	if err != nil {
		return DeletionSet{}, fmt.Errorf("starter: parse %s: %w", modinfo.ModulesGo, err)
	}
	drop, err := closure(modules, deps, removed)
	if err != nil {
		return DeletionSet{}, err
	}
	pathSet := map[string]bool{}
	var httpTags, adminTags []string
	adminSurface := false
	for _, module := range drop {
		pathSet["internal/modules/"+module+"/"] = true
		for _, path := range manifests[module].WebPaths {
			pathSet[path] = true
		}
		for _, item := range []struct {
			path string
			tags *[]string
		}{
			{"internal/modules/" + module + "/httpapi/oapi/config.yaml", &httpTags},
			{"internal/modules/" + module + "/adminapi/oapi/config.yaml", &adminTags},
		} {
			content, err := repo.Read(item.path)
			if err == nil {
				if tag := includeTag(content); tag != "" {
					*item.tags = append(*item.tags, tag)
				}
			}
		}
		if slices.Contains(adminAuth, module) {
			adminSurface = true
		}
	}
	for _, name := range files {
		if !strings.HasPrefix(name, "internal/app/") {
			continue
		}
		base := filepath.Base(name)
		for _, module := range drop {
			if regexp.MustCompile(fmt.Sprintf(appModuleFileRe, regexp.QuoteMeta(module))).MatchString(base) {
				pathSet[name] = true
			}
			if strings.HasSuffix(name, "_test.go") {
				content, err := repo.Read(name)
				if err == nil && strings.Contains(content, "/internal/modules/"+module) {
					pathSet[name] = true
				}
			}
		}
	}
	paths := slices.Sorted(maps.Keys(pathSet))
	return DeletionSet{Modules: drop, Paths: paths, HTTPTags: unique(httpTags), AdminTags: unique(adminTags), AdminSurface: adminSurface}, nil
}

func unique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}
