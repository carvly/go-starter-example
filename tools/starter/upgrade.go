package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// upgradeState 保存在项目的 git 目录里（starter-upgrade/state.json），中断后用同一版本续跑。
type upgradeState struct {
	Target string `json:"target"`
	Base   string `json:"base"`
	Stage  string `json:"stage"`
	// Pending 是停下等待人工确认的点；--continue 只确认它这一个。
	Pending   string   `json:"pending,omitempty"`
	Confirmed []string `json:"confirmed,omitempty"`
}

// 人工确认点。前三个对应 starter-changelog.md 升级说明里的同名小节，drift 是检测到项目改过 starter 文件。
const (
	pointPrecheck      = "合并前"
	pointDrift         = "drift"
	pointBeforeMigrate = "合并后迁移前"
	pointAfterMigrate  = "迁移后"
)

type upgradeOptions struct {
	version     string
	continueRun bool
	projectRoot string
	targetRoot  string
}

func runUpgrade(args []string) error {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	version := flags.String("version", "", "target version")
	continueRun := flags.Bool("continue", false, "continue after manual steps")
	projectRoot := flags.String("project-root", ".", "project root")
	targetRoot := flags.String("target-root", "", "target worktree (prepared by scripts/starter-upgrade-entry.sh)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *version == "" || *targetRoot == "" {
		return fmt.Errorf("upgrade: --version and --target-root are required; run make starter-upgrade")
	}
	return upgrade(upgradeOptions{version: *version, continueRun: *continueRun, projectRoot: *projectRoot, targetRoot: *targetRoot})
}

func upgrade(options upgradeOptions) error {
	gitDir := func(name string) (string, error) { return gitPath(options.projectRoot, name) }
	statePath, err := gitDir("starter-upgrade/state.json")
	if err != nil {
		return err
	}
	mergeHead, err := gitDir("MERGE_HEAD")
	if err != nil {
		return err
	}
	state, err := loadUpgradeState(statePath)
	if err != nil {
		return err
	}
	if state.Target != "" && state.Target != options.version {
		return fmt.Errorf("upgrade: unfinished target %s", state.Target)
	}
	if state.Stage == "" {
		if out, err := gitOutput(options.projectRoot, "status", "--porcelain"); err != nil {
			return err
		} else if out != "" {
			return fmt.Errorf("upgrade: initial workspace must be clean")
		}
		if exists(mergeHead) {
			return fmt.Errorf("upgrade: initial merge already in progress")
		}
		state.Base, err = upgradeBase(options.projectRoot, options.version)
		if err != nil {
			return err
		}
		state.Stage = "precheck"
	}
	state.Target = options.version
	if options.continueRun && state.Pending != "" {
		state.Confirmed = append(state.Confirmed, state.Pending)
		state.Pending = ""
	}
	if err := saveUpgradeState(statePath, state); err != nil {
		return err
	}
	changelog, err := os.ReadFile(filepath.Join(options.targetRoot, "starter-changelog.md"))
	if err != nil {
		return err
	}
	notes := upgradeNotes(string(changelog), state.Base, state.Target)
	// stop 在需要人工确认且还没确认时记下停点并返回 true。
	stop := func(point string, needed bool, message string) (bool, error) {
		if !needed || slices.Contains(state.Confirmed, point) {
			return false, nil
		}
		state.Pending = point
		fmt.Println(message)
		fmt.Println("完成后用 CONTINUE=1 续跑，只确认这一处。")
		return true, saveUpgradeState(statePath, state)
	}
	for {
		fmt.Printf("升级基线：%s；目标：%s\n", state.Base, state.Target)
		switch state.Stage {
		case "precheck":
			printNotes(notes)
			if stopped, err := stop(pointPrecheck, notes[pointPrecheck] != "", "请先完成升级说明里的“合并前”事项。"); stopped || err != nil {
				return err
			}
			state.Stage = "drift"
		case "drift":
			drift, err := starterFileDrift(options, state.Base)
			if err != nil {
				return err
			}
			specDrift, err := ownershipDrift(options.projectRoot, state.Base)
			if err != nil {
				return err
			}
			drift = append(drift, specDrift...)
			if stopped, err := stop(pointDrift, len(drift) > 0, "项目改过以下 starter 文件，合并时会以 starter 版本为准，请确认已登记在 project/starter-patches.md：\n"+strings.Join(drift, "\n")); stopped || err != nil {
				return err
			}
			state.Stage = "merge"
		case "merge":
			if !exists(mergeHead) {
				if err := mergeTarget(options.projectRoot, options.version, mergeHead); err != nil {
					return err
				}
			}
			paths, err := starterOwnedPaths(options.targetRoot, options.version, nil)
			if err != nil {
				return err
			}
			for _, path := range paths {
				conflicted, err := gitOutput(options.projectRoot, "diff", "--name-only", "--diff-filter=U", "--", path)
				if err != nil {
					return err
				}
				if conflicted != "" {
					if err := run(options.projectRoot, "git", "checkout", options.version, "--", path); err != nil {
						return err
					}
					if err := run(options.projectRoot, "git", "add", "--", path); err != nil {
						return err
					}
				}
			}
			remaining, err := gitOutput(options.projectRoot, "diff", "--name-only", "--diff-filter=U")
			if err != nil {
				return err
			}
			if remaining != "" {
				fmt.Printf("请解决并暂存以下共享/项目文件冲突，然后用 CONTINUE=1 续跑：\n%s\n", remaining)
				return saveUpgradeState(statePath, state)
			}
			state.Stage = "replay"
		case "replay":
			remaining, err := gitOutput(options.projectRoot, "diff", "--name-only", "--diff-filter=U")
			if err != nil {
				return err
			}
			if remaining != "" {
				return fmt.Errorf("upgrade: unmerged paths: %s", remaining)
			}
			removed, err := removedModules(options.projectRoot)
			if err != nil {
				return err
			}
			if len(removed) > 0 {
				plan, err := compute(repository{root: options.projectRoot, target: options.version}, removed)
				if err != nil {
					return err
				}
				if err := apply(options.projectRoot, plan, applyOptions{NoRecord: true}); err != nil {
					return err
				}
			}
			for _, command := range [][]string{{"go", "mod", "tidy"}, {"go", "generate", "./..."}} {
				if err := run(options.projectRoot, command...); err != nil {
					return err
				}
			}
			if exists(filepath.Join(options.projectRoot, "web/admin/package.json")) {
				if !exists(filepath.Join(options.projectRoot, "web/admin/node_modules")) {
					if err := run(filepath.Join(options.projectRoot, "web/admin"), "pnpm", "install", "--frozen-lockfile"); err != nil {
						return err
					}
				}
				for _, command := range [][]string{{"pnpm", "gen:api"}, {"pnpm", "gen:routes"}} {
					if err := run(filepath.Join(options.projectRoot, "web/admin"), command...); err != nil {
						return err
					}
				}
			}
			state.Stage = "migrate"
		case "migrate":
			if stopped, err := stop(pointBeforeMigrate, notes[pointBeforeMigrate] != "", "请先完成升级说明里的“合并后迁移前”事项，再执行迁移。"); stopped || err != nil {
				return err
			}
			if err := run(options.projectRoot, "go", "run", "./cmd/api", "migrate", "up"); err != nil {
				return err
			}
			state.Stage = "after-migrate"
		case "after-migrate":
			if stopped, err := stop(pointAfterMigrate, notes[pointAfterMigrate] != "", "请完成升级说明里的“迁移后”事项。"); stopped || err != nil {
				return err
			}
			state.Stage = "verify"
		case "verify":
			if err := run(options.projectRoot, "go", "test", "./..."); err != nil {
				return err
			}
			modules, err := allModules(options.projectRoot)
			if err != nil {
				return err
			}
			if err := check(options.projectRoot, modules); err != nil {
				return err
			}
			state.Stage = "complete"
		case "complete":
			if n := notes["部署后"]; n != "" {
				fmt.Println("部署后事项：\n" + n)
			}
			fmt.Printf("升级流程完成：%s。\n", state.Target)
			return saveUpgradeState(statePath, state)
		default:
			return fmt.Errorf("upgrade: unknown stage %s", state.Stage)
		}
		if err := saveUpgradeState(statePath, state); err != nil {
			return err
		}
	}
}

// mergeTarget 合并目标版本；有冲突时 git merge 失败但 MERGE_HEAD 存在，交给后续步骤处理。
func mergeTarget(root, version, mergeHead string) error {
	if err := run(root, "git", "merge", version); err != nil && !exists(mergeHead) {
		return err
	}
	return nil
}

// upgradeBase 是项目已经合并的最新 starter 版本：同时是 HEAD 和目标版本祖先的 tag。业务项目和 starter 共享
// tag 命名空间，项目自己的 tag（如 v1.0.0）不是目标版本的祖先，不会被选中。没有这样的 tag 时退回 merge-base。
func upgradeBase(root, target string) (string, error) {
	out, err := gitOutput(root, "tag", "--merged", "HEAD", "--sort=-v:refname")
	if err != nil {
		return "", err
	}
	for _, tag := range strings.Fields(out) {
		if tag == target {
			continue
		}
		if exec.Command("git", "-C", root, "merge-base", "--is-ancestor", tag, target).Run() == nil {
			return tag, nil
		}
	}
	return gitOutput(root, "merge-base", "HEAD", target)
}

// starterFileDrift 列出项目相对基线改过、且和目标版本也不一样的 starter 文件：合并时它们会被 starter 版本覆盖，
// 项目的改动会丢。内容已经和目标版本相同的（例如提前同步了上游的修复）合并不会丢任何东西，不算漂移。
func starterFileDrift(options upgradeOptions, base string) ([]string, error) {
	removed, err := removedModules(options.projectRoot)
	if err != nil {
		return nil, err
	}
	paths, err := starterOwnedPaths(options.targetRoot, base, removed)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, path := range paths {
		changed, err := gitOutput(options.projectRoot, "diff", "--name-only", base, "--", path)
		if err != nil {
			return nil, err
		}
		if changed == "" {
			continue
		}
		differsFromTarget, err := gitOutput(options.projectRoot, "diff", "--name-only", options.version, "--", path)
		if err != nil {
			return nil, err
		}
		if differsFromTarget != "" {
			out = append(out, changed)
		}
	}
	return out, nil
}

func loadUpgradeState(path string) (upgradeState, error) {
	var state upgradeState
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	return state, json.Unmarshal(data, &state)
}

func saveUpgradeState(path string, state upgradeState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// gitPath 是 git 目录里的路径；项目根是 git worktree 时 .git 是文件，不能直接拼接。
func gitPath(root, name string) (string, error) {
	p, err := gitOutput(root, "rev-parse", "--git-path", name)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	return p, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func gitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
