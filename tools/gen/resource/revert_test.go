package resource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 生成前共享文件已有未提交的改动：apply 之后 revert，改动原样保留，新建的路径和 .gen 记录都删掉。
// 改动前的回退命令是 git checkout，会把这些改动一起丢掉，所以 apply 只能要求工作区干净。
func TestRevert_RestoresPreGenerationContent(t *testing.T) {
	root := fakeRepo(t, true)
	repo, err := OpenRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	p, probs := Parse("x.yaml", ExampleAdmin)
	if len(probs) > 0 {
		t.Fatal(probs)
	}
	pl, err := repo.MakePlan(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(root, pl.Names.Spec)
	edited := "# 用户生成前未提交的改动\n"
	original, _ := os.ReadFile(spec)
	if err := os.WriteFile(spec, append(original, edited...), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(spec)

	// 模拟 apply 的写入：先留原样，再新建文件、改共享文件。
	if err := repo.saveBefore(pl, []string{pl.Names.Spec}); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(root, "internal/modules", pl.Names.Module, "module.go")
	if err := os.MkdirAll(filepath.Dir(created), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, append(before, "# 生成器插入的内容\n"...), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Revert(pl.Names.Resource); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(spec); string(got) != string(before) || !strings.Contains(string(got), edited) {
		t.Errorf("共享文件没有还原成生成前的原样（含未提交的改动）：\n%s", got)
	}
	if exists(filepath.Join(root, "internal/modules", pl.Names.Module)) {
		t.Error("新建的模块目录没有删掉")
	}
	if exists(filepath.Join(root, ".gen", pl.Names.Resource)) {
		t.Error(".gen 记录没有删掉")
	}
	if _, err := repo.Revert(pl.Names.Resource); err == nil {
		t.Error("没有生成记录时 revert 应报错")
	}
}

// 要新建的路径下已有文件时拒绝生成；其它文件有未提交改动不影响。
func TestCheckWorkspace_OnlyCreatedPaths(t *testing.T) {
	root := fakeRepo(t, true)
	repo, _ := OpenRepo(root)
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.email=t@e", "-c", "user.name=t", "commit", "-qm", "base"}} {
		if _, err := repo.git(args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "api/admin.yaml"), []byte("openapi: 3.2.0\n# 改了\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.CheckWorkspace([]string{"internal/modules/fresh"}); err != nil {
		t.Errorf("共享文件有未提交改动不应拒绝：%v", err)
	}
	if err := repo.CheckWorkspace([]string{"internal/modules/existing"}); err == nil {
		t.Error("要新建的路径已存在时应拒绝")
	}
}

// apply 成功之后共享文件又被改过（这里模拟又生成了另一个资源 B 往 spec 里插了内容）：revert A 必须拒绝，
// 不能整文件还原把 B 的接线抹掉；什么都不动。
func TestRevert_RefusesWhenSharedFilesChangedAfterApply(t *testing.T) {
	root := fakeRepo(t, true)
	repo, err := OpenRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := Parse("x.yaml", ExampleAdmin)
	pl, err := repo.MakePlan(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(root, pl.Names.Spec)
	if err := repo.saveBefore(pl, []string{pl.Names.Spec}); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(root, "internal/modules", pl.Names.Module)
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	applied, _ := os.ReadFile(spec)
	applied = append(applied, "# A 插入的内容\n"...)
	if err := os.WriteFile(spec, applied, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.recordApplied(pl.Names.Resource); err != nil {
		t.Fatal(err)
	}
	later := append(append([]byte{}, applied...), "# B 插入的内容\n"...)
	if err := os.WriteFile(spec, later, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = repo.Revert(pl.Names.Resource)
	if err == nil || !strings.Contains(err.Error(), pl.Names.Spec) {
		t.Fatalf("err = %v, want 拒绝并列出 %s", err, pl.Names.Spec)
	}
	if got, _ := os.ReadFile(spec); string(got) != string(later) {
		t.Error("拒绝时不能改动共享文件")
	}
	if !exists(created) {
		t.Error("拒绝时不能删除新建的路径")
	}
}
