package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeTargetReturnsNonConflictFailure(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	git := filepath.Join(bin, "git")
	if err := os.WriteFile(git, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	if err := mergeTarget(root, "v0.2.0", filepath.Join(root, ".git", "MERGE_HEAD")); err == nil {
		t.Fatal("merge failure without MERGE_HEAD should stop")
	}
}

func TestMergeTargetAllowsConflictRecovery(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	git := filepath.Join(bin, "git")
	if err := os.WriteFile(git, []byte("#!/bin/sh\nmkdir -p .git\ntouch .git/MERGE_HEAD\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	if err := mergeTarget(root, "v0.2.0", filepath.Join(root, ".git", "MERGE_HEAD")); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeRejectsDifferentTarget(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	statePath, err := gitPath(root, "starter-upgrade/state.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveUpgradeState(statePath, upgradeState{Target: "v0.1.0", Stage: "merge"}); err != nil {
		t.Fatal(err)
	}
	err = upgrade(upgradeOptions{version: "v0.2.0", projectRoot: root, targetRoot: root})
	if err == nil || !strings.Contains(err.Error(), "unfinished target v0.1.0") {
		t.Fatalf("different target should be rejected, got %v", err)
	}
}

// 项目根是 git worktree 时 .git 是文件，状态和 MERGE_HEAD 的路径要经 git rev-parse --git-path 取。
func TestGitPathInWorktree(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	for _, args := range [][]string{{"init", "-q", main}, {"-C", main, "-c", "user.email=t@e", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "base"}, {"-C", main, "worktree", "add", "-q", filepath.Join(root, "wt")}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	p, err := gitPath(filepath.Join(root, "wt"), "starter-upgrade/state.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveUpgradeState(p, upgradeState{Target: "v1"}); err != nil {
		t.Fatalf("worktree 里保存状态失败：%v（路径 %s）", err, p)
	}
}

// 业务项目自己的 tag（v1.0.0）不是 starter 目标版本的祖先，不能被当成升级基线；否则升级说明整段被跳过。
func TestUpgradeBaseIgnoresProjectTags(t *testing.T) {
	root := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.email=t@e", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	g("init", "-q")
	g("commit", "-q", "--allow-empty", "-m", "starter v0.1.0")
	g("tag", "v0.1.0")
	g("checkout", "-q", "-b", "starter")
	g("commit", "-q", "--allow-empty", "-m", "starter v0.2.0")
	g("tag", "v0.2.0")
	g("checkout", "-q", "-")
	g("commit", "-q", "--allow-empty", "-m", "project release")
	g("tag", "v1.0.0")
	base, err := upgradeBase(root, "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if base != "v0.1.0" {
		t.Fatalf("base = %q, want v0.1.0（项目自己的 v1.0.0 不是目标版本的祖先）", base)
	}
}

// 漂移只报"合并会丢项目改动"的文件：项目已经提前同步成目标版本内容的（如上游修复），不算漂移；
// 和目标版本不同的改动照样报出。
func TestStarterFileDriftIgnoresFilesMatchingTarget(t *testing.T) {
	root := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.email=t@e", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g("init", "-q")
	write("starter-owned.txt", "synced.txt\ncustom.txt\n")
	write("synced.txt", "old\n")
	write("custom.txt", "old\n")
	g("add", "-A")
	g("commit", "-qm", "base")
	g("tag", "v0.1.0")
	g("checkout", "-q", "-b", "starter")
	write("synced.txt", "fixed upstream\n")
	g("commit", "-qam", "starter fix")
	g("tag", "v0.2.0")
	g("checkout", "-q", "-")
	write("synced.txt", "fixed upstream\n") // 提前同步了上游修复
	write("custom.txt", "project only\n")   // 项目自己的改动
	g("commit", "-qam", "project")

	drift, err := starterFileDrift(upgradeOptions{version: "v0.2.0", projectRoot: root, targetRoot: root}, "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(drift, ",") != "custom.txt" {
		t.Fatalf("drift = %v, want [custom.txt]（synced.txt 已与目标版本相同，合并不会丢东西）", drift)
	}
}
