// Command gen 是开发工具，不进运行时。用法和规则见 docs/agents/generator.md。
//
//	go run ./tools/gen check   <描述文件> [--json]   只校验，一次报出全部错误
//	go run ./tools/gen plan    <描述文件> [--json]   推出的名字、将要新建的文件和插入的位置，不写文件
//	go run ./tools/gen apply   <描述文件> [--json]   生成
//	go run ./tools/gen drift   <描述文件> [--json]   列出生成之后改过的地方
//	go run ./tools/gen revert  <描述文件>            精确回退一次生成（删新建的路径，共享文件还原成生成前的原样）
//	go run ./tools/gen fixture <描述文件>            把描述文件存为回归夹具（tools/gen/testdata/）
//	go run ./tools/gen example admin|web             打印带注释的完整描述文件
//	go run ./tools/gen schema                        打印描述文件的 JSON Schema
//	go run ./tools/gen meta <admin.yaml> <meta.gen.ts>   从后台 spec 生成前端的字段文案与校验（make generate 调用）
//
// 退出码：0 成功；1 描述文件或 spec 有错误（逐条列出位置和改法）；2 环境问题（不在仓库根目录、
// 目标文件已存在、用法错误）。
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"app.local/tools/gen/meta"
	"app.local/tools/gen/resource"
)

const usage = `用法：
  go run ./tools/gen check|plan|apply|drift <描述文件> [--json]
  go run ./tools/gen revert <描述文件>
  go run ./tools/gen fixture <描述文件>
  go run ./tools/gen example admin|web
  go run ./tools/gen schema
  go run ./tools/gen meta <admin.yaml> <meta.gen.ts>
规则和流程见 docs/agents/generator.md`

func main() {
	os.Exit(run(os.Args[1:]))
}

type exitErr struct {
	code int
	msg  string
}

func (e exitErr) Error() string { return e.msg }

func run(args []string) int {
	asJSON := slices.Contains(args, "--json")
	args = slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "--json" })
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	var err error
	switch cmd := args[0]; cmd {
	case "check", "plan", "apply", "drift", "fixture", "revert":
		if len(args) != 2 {
			err = exitErr{2, usage}
			break
		}
		err = descriptorCmd(cmd, args[1], asJSON)
	case "example":
		switch {
		case len(args) == 2 && args[1] == "admin":
			_, err = os.Stdout.Write(resource.ExampleAdmin)
		case len(args) == 2 && args[1] == "web":
			_, err = os.Stdout.Write(resource.ExampleWeb)
		default:
			err = exitErr{2, "用法：go run ./tools/gen example admin|web"}
		}
	case "schema":
		var s []byte
		if s, err = resource.JSONSchema(); err == nil {
			fmt.Println(string(s))
		}
	case "meta":
		if len(args) != 3 {
			err = exitErr{2, usage}
			break
		}
		err = metaCmd(args[1], args[2])
	default:
		err = exitErr{2, "不认识的命令 " + cmd + "\n" + usage}
	}
	if err == nil {
		return 0
	}
	fmt.Fprintln(os.Stderr, err)
	var e exitErr
	switch {
	case errors.As(err, &e):
		return e.code
	case errors.Is(err, resource.ErrEnv):
		return 2
	}
	return 1
}

func metaCmd(specPath, out string) error {
	spec, err := os.ReadFile(specPath)
	if err != nil {
		return exitErr{2, err.Error()}
	}
	ts, err := meta.Generate(spec)
	if err != nil {
		return exitErr{1, err.Error()}
	}
	return os.WriteFile(out, ts, 0o644)
}

func printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func descriptorCmd(cmd, file string, asJSON bool) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return exitErr{2, err.Error()}
	}
	repo, err := resource.OpenRepo(".")
	if err != nil {
		return err
	}
	p, problems := resource.Parse(file, data)
	if len(problems) == 0 {
		problems = append(problems, resource.Validate(p)...)
	}
	if cmd != "drift" && cmd != "fixture" && cmd != "revert" && !hasErrors(problems) {
		problems = append(problems, repo.Check(p)...)
	}
	var errs, hints []resource.Problem
	for _, pr := range problems {
		if pr.Hint {
			hints = append(hints, pr)
		} else {
			errs = append(errs, pr)
		}
	}
	if len(errs) > 0 || cmd == "check" {
		if asJSON {
			printJSON(map[string]any{"ok": len(errs) == 0, "problems": append(errs, hints...)})
		} else {
			for _, pr := range append(errs, hints...) {
				fmt.Println(pr)
			}
			if len(errs) == 0 {
				fmt.Printf("%s 没有错误（%d 条提示）\n", file, len(hints))
			}
		}
		if len(errs) > 0 {
			return exitErr{1, fmt.Sprintf("%d 个错误，改完再跑一次 check", len(errs))}
		}
		return nil
	}
	switch cmd {
	case "fixture":
		// 和手写的夹具同一套命名：fixture_<资源>.yaml；已有同名夹具时不覆盖，由人决定改名还是替换。
		dst := filepath.Join("tools/gen/testdata", "fixture_"+p.D.Resource+".yaml")
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("%w：%s 已经存在，先改名或删除旧夹具", resource.ErrEnv, dst)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, resource.FixtureNames(data), 0o644); err != nil {
			return err
		}
		fmt.Println("已存为回归夹具：" + dst + "（和这次的改动一起提交）")
		return nil
	case "revert":
		done, err := repo.Revert(p.D.Resource)
		for _, d := range done {
			fmt.Println(d)
		}
		return err
	case "drift":
		items, err := repo.Drift(p.D.Resource)
		if err != nil {
			return err
		}
		if asJSON {
			printJSON(map[string]any{"items": items})
			return nil
		}
		printDrift(items)
		return nil
	}
	pl, err := repo.MakePlan(p, hints)
	if err != nil {
		return err
	}
	if cmd == "plan" {
		if asJSON {
			printJSON(pl)
		} else {
			printPlan(pl)
		}
		return nil
	}
	res, err := repo.Apply(pl, data, func(s string) {
		if !asJSON {
			fmt.Println(s)
		}
	})
	if err != nil {
		return err
	}
	if asJSON {
		printJSON(res)
		return nil
	}
	printPlan(pl)
	fmt.Println("\n待办（逐项完成，跑通全部检查后按 generator.md 的汇报模板汇报）：")
	if len(res.Todo) == 0 {
		fmt.Println("  没有：生成的代码已经完整")
	}
	for i, t := range res.Todo {
		fmt.Printf("  %d. [%s] %s %s\n     %s\n", i+1, t.Kind, t.File, t.Symbol, t.Instruction)
		if t.Test != "" {
			fmt.Printf("     要替换的测试：%s\n", t.Test)
		}
		if t.Reference != "" {
			fmt.Printf("     %s\n", t.Reference)
		}
	}
	fmt.Println("\n回退（补足之前确认生成结果，回退会丢掉补足的代码和生成后对共享文件的改动）：\n  go run ./tools/gen revert " + file)
	return nil
}

func hasErrors(ps []resource.Problem) bool {
	return slices.ContainsFunc(ps, func(p resource.Problem) bool { return !p.Hint })
}

func printPlan(pl *resource.Plan) {
	n := pl.Names
	fmt.Println("推出的名字（不对就补写 plural 或换名字）：")
	for _, kv := range [][2]string{
		{"表名", n.Table}, {"路径", n.Path}, {"tag", n.Tag}, {"权限码前缀", n.Permission + ":"},
		{"Go 类型", n.Type}, {"spec", n.Spec}, {"operationId", strings.Join(slices.DeleteFunc([]string{n.OpList, n.OpGet, n.OpCreate, n.OpUpdate, n.OpDelete}, func(s string) bool { return s == "" }), "、")},
	} {
		fmt.Printf("  %-10s %s\n", kv[0], kv[1])
	}
	fmt.Println("新建的文件：")
	for _, f := range pl.Create {
		fmt.Println("  " + f)
	}
	fmt.Println("插入的位置：")
	for _, in := range pl.Insert {
		if in.After > 0 {
			fmt.Printf("  %s 的 %s（第 %d 行之后）\n", in.File, in.Section, in.After)
		} else {
			fmt.Printf("  %s 的 %s：%s\n", in.File, in.Section, strings.TrimSpace(in.Text))
		}
	}
	for _, h := range pl.Hints {
		fmt.Println(h)
	}
}

func printDrift(items []resource.DriftItem) {
	var outside, todo []resource.DriftItem
	for _, it := range items {
		if it.Expected {
			todo = append(todo, it)
		} else {
			outside = append(outside, it)
		}
	}
	fmt.Printf("待办清单之外的修改：%d 处（汇报里逐条说明原因；理想是 0）\n", len(outside))
	for _, it := range outside {
		fmt.Printf("  [%s] %s %s\n", it.Kind, it.File, it.Symbol)
		if it.Detail != "" {
			for l := range strings.SplitSeq(it.Detail, "\n") {
				fmt.Println("      " + l)
			}
		}
	}
	fmt.Printf("待办清单里的修改和新增：%d 处\n", len(todo))
	for _, it := range todo {
		fmt.Printf("  [%s] %s %s\n", it.Kind, it.File, it.Symbol)
	}
}
