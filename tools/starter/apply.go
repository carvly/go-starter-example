package main

import (
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

type applyOptions struct {
	NoRecord bool
	DryRun   bool
}

func apply(root string, plan DeletionSet, options applyOptions) error {
	if options.DryRun {
		for _, path := range plan.Paths {
			fmt.Println(path)
		}
		return nil
	}
	for _, path := range plan.Paths {
		if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			return err
		}
	}
	for _, module := range plan.Modules {
		if err := removeModuleLines(root, module); err != nil {
			return err
		}
	}
	for _, tag := range plan.HTTPTags {
		if err := editSpec(root, "api/openapi.yaml", tag); err != nil {
			return err
		}
	}
	for _, tag := range plan.AdminTags {
		if err := editSpec(root, "api/admin.yaml", tag); err != nil {
			return err
		}
	}
	if plan.AdminSurface {
		if err := removeAdminSurface(root); err != nil {
			return err
		}
	}
	if !options.NoRecord {
		return recordRemoved(root, plan.Modules)
	}
	return nil
}

func editSpec(root, name, tag string) error {
	path := filepath.Join(root, filepath.FromSlash(name))
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	edited, err := removeFromSpec(string(data), tag)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return os.WriteFile(path, []byte(edited), 0o644)
}

var adminRegistrarLineRe = regexp.MustCompile(`\bm\.[A-Z][A-Za-z]*\.RegisterAdminHTTP,`)

func removeAdminSurface(root string) error {
	path := filepath.Join(root, "internal/app/modules.go")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if !adminRegistrarLineRe.MatchString(line) {
			out = append(out, line)
		}
	}
	formatted, err := format.Source([]byte(strings.Join(out, "\n")))
	if err != nil {
		return fmt.Errorf("format modules.go after removing admin surface: %w", err)
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return err
	}
	entries, err := filepath.Glob(filepath.Join(root, "internal/modules/*/adminapi/oapi/config.yaml"))
	if err != nil {
		return err
	}
	for _, config := range entries {
		content, err := os.ReadFile(config)
		if err != nil {
			return err
		}
		if tag := includeTag(string(content)); tag != "" {
			if err := editSpec(root, "api/admin.yaml", tag); err != nil {
				return err
			}
		}
	}
	return nil
}

type removedRecord struct {
	RemovedModules []string `yaml:"removed_modules"`
}

func removedModules(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, "starter-removed.yaml"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record removedRecord
	if err := yaml.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return record.RemovedModules, nil
}

func recordRemoved(root string, modules []string) error {
	previous, err := removedModules(root)
	if err != nil {
		return err
	}
	all := unique(append(previous, modules...))
	line := fmt.Sprintf("removed_modules: [%s]\n", strings.Join(all, ", "))
	return os.WriteFile(filepath.Join(root, "starter-removed.yaml"), []byte(line), 0o644)
}

// generateAndTest 重新生成并跑测试；后台接入面被整体删掉时（adminSurfaceRemoved）没有前端可查。
func generateAndTest(root string, adminSurfaceRemoved bool) error {
	for _, command := range [][]string{{"go", "generate", "./api/"}, {"go", "vet", "./..."}, {"go", "test", "./..."}} {
		if err := run(root, command...); err != nil {
			return err
		}
	}
	if adminSurfaceRemoved {
		return nil
	}
	if _, err := os.Stat(filepath.Join(root, "web/admin/node_modules")); os.IsNotExist(err) && os.Getenv("CI") != "" {
		return fmt.Errorf("web/admin dependencies are required in CI")
	}
	if _, err := os.Stat(filepath.Join(root, "web/admin/node_modules")); err == nil {
		for _, command := range [][]string{{"pnpm", "gen:api"}, {"pnpm", "gen:routes"}, {"pnpm", "typecheck"}} {
			if err := run(filepath.Join(root, "web/admin"), command...); err != nil {
				return err
			}
		}
	}
	return nil
}

func run(dir string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "pnpm_config_verify_deps_before_run=false")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

func check(root string, modules []string) error {
	for _, module := range modules {
		plan, err := compute(repository{root: root}, []string{module})
		if err != nil {
			return err
		}
		work, err := os.MkdirTemp("", "starter-check-")
		if err != nil {
			return err
		}
		if err := copyWorktree(root, work); err != nil {
			_ = os.RemoveAll(work)
			return err
		}
		err = apply(work, plan, applyOptions{NoRecord: true})
		if err == nil {
			err = generateAndTest(work, plan.AdminSurface)
		}
		if err != nil {
			_ = os.RemoveAll(work)
			return fmt.Errorf("check %s: %w", module, err)
		}
		if err := os.RemoveAll(work); err != nil {
			return err
		}
		work, err = os.MkdirTemp("", "starter-negative-")
		if err != nil {
			return err
		}
		if err := copyWorktree(root, work); err != nil {
			_ = os.RemoveAll(work)
			return err
		}
		if err := os.RemoveAll(filepath.Join(work, "internal/modules/"+module)); err != nil {
			return err
		}
		cmd := exec.Command("go", "build", "./...")
		cmd.Dir = work
		if out, err := cmd.CombinedOutput(); err == nil {
			_ = os.RemoveAll(work)
			return fmt.Errorf("check %s: deleting only the module directory still builds: %s", module, out)
		}
		if err := os.RemoveAll(work); err != nil {
			return err
		}
		fmt.Printf("check %s: ok\n", module)
	}
	return nil
}

func copyWorktree(root, destination string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	cmd := exec.Command("git", "-C", root, "ls-files", "-co", "--exclude-standard")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git ls-files: %w\n%s", err, out)
	}
	if info, err := os.Stat(filepath.Join(root, "web/admin/node_modules")); err == nil && info.IsDir() {
		if err := os.MkdirAll(filepath.Join(destination, "web/admin"), 0o755); err != nil {
			return err
		}
		if err := os.Symlink(filepath.Join(root, "web/admin/node_modules"), filepath.Join(destination, "web/admin/node_modules")); err != nil && !os.IsExist(err) {
			return err
		}
	}
	for _, name := range strings.Fields(string(out)) {
		info, err := os.Lstat(filepath.Join(root, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		target := filepath.Join(destination, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func allModules(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "internal/modules"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	slices.Sort(out)
	return out, nil
}
