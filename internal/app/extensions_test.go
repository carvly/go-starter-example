package app

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"app.local/internal/platform/cli"
	"app.local/internal/platform/queue"
)

// 子命令和定时任务的完整性检查，对应 HTTP 路由的 TestRoutes_MatchOpenAPISpec 和
// 事件的 TestWorkers_EveryEventKindHasWorker：模块写了 Commands()/PeriodicJobs()
// 却忘了在 modules.go 登记时，编译能过、功能静默不生效，只能靠这里发现。
func TestExtensions_EveryExtensionIsRegistered(t *testing.T) {
	mods := buildModules(newZeroInfra(), testModuleConfigs(t))
	for _, p := range checkExtensions(mods, commands(mods), periodicJobs(mods), newWorkers(mods).Kinds()) {
		t.Error(p)
	}
}

type commandProvider interface{ Commands() []cli.Command }
type periodicProvider interface{ PeriodicJobs() []queue.Periodic }

// checkExtensions 遍历 mods（*Modules 或测试里的同构结构体）的每个字段，
// 字段名转小写就是模块名。
func checkExtensions(mods any, registeredCmds []cli.Command, registeredJobs []queue.Periodic, workerKinds []string) []string {
	var problems []string

	cmdNames := map[string]int{}
	for _, c := range registeredCmds {
		cmdNames[c.Name]++
	}
	for name, n := range cmdNames {
		if n > 1 {
			problems = append(problems, fmt.Sprintf("命令 %q 登记了 %d 次：命令名必须唯一", name, n))
		}
	}
	// 定时任务按 kind 计数比较：同一个 kind 可以有多个调度（如每小时一次加每天一次），
	// 只登记了其中一部分、或者同一个模块登记了两次，计数都会对不上。
	registeredKinds := map[string]int{}
	for _, j := range registeredJobs {
		if j.Args == nil {
			problems = append(problems, "有定时任务的 Args 为 nil：queue.Periodic 必须给出入队的参数")
			continue
		}
		registeredKinds[j.Args.Kind()]++
	}
	providedKinds := map[string]int{}
	providerOf := map[string]string{}

	v := reflect.ValueOf(mods).Elem()
	for i := range v.NumField() {
		// 字段名是模块目录名首字母大写（tools/starter 的删除执行器按此推导），
		// 这里做它的逆变换。
		fieldName := v.Type().Field(i).Name
		module := strings.ToLower(fieldName[:1]) + fieldName[1:]
		field := v.Field(i).Interface()

		if p, ok := field.(commandProvider); ok {
			for _, c := range p.Commands() {
				words := strings.Fields(c.Name)
				if len(words) != 2 || strings.Join(words, " ") != c.Name {
					problems = append(problems, fmt.Sprintf("%s 模块的命令 %q 格式不对：必须是 \"<模块> <动作>\" 两个词、单个空格分隔", module, c.Name))
				} else if words[0] != module {
					problems = append(problems, fmt.Sprintf("%s 模块的命令 %q 必须以模块名 %q 开头：删掉模块时它的命令随之消失，不同模块不会撞名", module, c.Name, module))
				}
				if cmdNames[c.Name] == 0 {
					problems = append(problems, fmt.Sprintf("%s 模块提供了命令 %q 但没有登记：在 internal/app/modules.go 的 commandRegistrars 里加一行 m.%s.Commands", module, c.Name, fieldName))
				}
			}
		}
		if p, ok := field.(periodicProvider); ok {
			for _, j := range p.PeriodicJobs() {
				if j.Args == nil {
					problems = append(problems, fmt.Sprintf("%s 模块有定时任务的 Args 为 nil", module))
					continue
				}
				providedKinds[j.Args.Kind()]++
				providerOf[j.Args.Kind()] = fieldName
			}
		}
	}

	for kind, n := range providedKinds {
		if registeredKinds[kind] != n {
			problems = append(problems, fmt.Sprintf("定时任务 %q 模块提供了 %d 个、登记了 %d 个：在 internal/app/modules.go 的 periodicJobRegistrars 里登记 m.%s.PeriodicJobs，且只登记一次", kind, n, registeredKinds[kind], providerOf[kind]))
		}
	}
	for kind := range registeredKinds {
		if !slices.Contains(workerKinds, kind) {
			problems = append(problems, fmt.Sprintf("定时任务 %q 没有对应的 worker：在提供它的模块的 RegisterJobs 里 queue.AddWorker，并确认该模块在 modules.go 的 jobRegistrars 里", kind))
		}
	}
	return problems
}

type fakeExtModule struct {
	cmds []cli.Command
	jobs []queue.Periodic
}

func (f *fakeExtModule) Commands() []cli.Command        { return f.cmds }
func (f *fakeExtModule) PeriodicJobs() []queue.Periodic { return f.jobs }

type fakeExtModules struct {
	Staff *fakeExtModule
}

type fakeCleanupArgs struct{}

func (fakeCleanupArgs) Kind() string { return "fake_staff_cleanup" }

// 检查本身要能失败：每种漏写都必须被报出来，否则上面的通过不说明任何问题。
func TestCheckExtensions_ReportsEachMistake(t *testing.T) {
	cmd := func(name string) cli.Command { return cli.Command{Name: name} }
	job := queue.Periodic{Schedule: river.PeriodicInterval(time.Hour), Args: fakeCleanupArgs{}}
	daily := queue.Periodic{Schedule: river.PeriodicInterval(24 * time.Hour), Args: fakeCleanupArgs{}}
	withWorker := func() []string { return []string{fakeCleanupArgs{}.Kind()} }

	tests := []struct {
		name       string
		module     fakeExtModule
		registered []cli.Command
		regJobs    []queue.Periodic
		workers    []string
		want       string
	}{
		{"命令没登记", fakeExtModule{cmds: []cli.Command{cmd("staff create")}}, nil, nil, nil, "没有登记"},
		{"命令前缀不是模块名", fakeExtModule{cmds: []cli.Command{cmd("user create")}}, []cli.Command{cmd("user create")}, nil, nil, "必须以模块名"},
		{"命令只有一个词", fakeExtModule{cmds: []cli.Command{cmd("staff")}}, []cli.Command{cmd("staff")}, nil, nil, "格式不对"},
		{"命令名重复", fakeExtModule{}, []cli.Command{cmd("staff create"), cmd("staff create")}, nil, nil, "登记了 2 次"},
		{"定时任务没登记", fakeExtModule{jobs: []queue.Periodic{job}}, nil, nil, withWorker(), "periodicJobRegistrars"},
		{"同一 kind 的两个调度只登记了一个", fakeExtModule{jobs: []queue.Periodic{job, daily}}, nil, []queue.Periodic{job}, withWorker(), "提供了 2 个、登记了 1 个"},
		{"定时任务登记了两次", fakeExtModule{jobs: []queue.Periodic{job}}, nil, []queue.Periodic{job, job}, withWorker(), "提供了 1 个、登记了 2 个"},
		{"定时任务没有 worker", fakeExtModule{jobs: []queue.Periodic{job}}, nil, []queue.Periodic{job}, nil, "没有对应的 worker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := checkExtensions(&fakeExtModules{Staff: &tt.module}, tt.registered, tt.regJobs, tt.workers)
			if !strings.Contains(strings.Join(problems, "\n"), tt.want) {
				t.Errorf("problems = %q, want 包含 %q", problems, tt.want)
			}
		})
	}

	t.Run("全部登记时没有问题", func(t *testing.T) {
		m := fakeExtModule{cmds: []cli.Command{cmd("staff create")}, jobs: []queue.Periodic{job}}
		if problems := checkExtensions(&fakeExtModules{Staff: &m}, m.cmds, m.jobs, withWorker()); len(problems) != 0 {
			t.Errorf("problems = %q, want none", problems)
		}
	})
}
