# 工作流：改完跑什么、检查失败怎么办

事实来源：`lefthook.yml`、`.golangci.yml`、`Makefile`、`.github/workflows/`、`scripts/`、`internal/archtest/exceptions.go`

## 改完先跑什么

只有改完就会跑的检查才能引导你一步步改对，慢检查只能在最后拦截。按层使用，
默认用最内层的快循环，不要每次都跑 `make ci`：

| 时机 | 检查 | 预算 |
|---|---|---|
| 每改一次 | `go build ./...`、`go test ./<改动的包>/...`、`go test ./internal/archtest/` | ≤ 10 秒 |
| 提交前（pre-commit 钩子） | `gofmt`（暂存文件，自动修复）、`make lint-new`、`go vet`（带 `integration`、`genfixture` tag）、`scripts/doc-impact.sh --staged`（只提示）；暂存了 `.sh` 文件时跑 `shellcheck`（本机没装时跳过）；暂存了 `.github/workflows/` 下的文件时跑 `make actionlint`；暂存了 `web/admin/` 下的文件时还跑 `web-lint`（oxlint）、`web-format`（oxfmt，会直接改写文件）、`web-ui`（`pnpm ui:check`），需要先在 `web/admin` 里 `pnpm install` | ≤ 15 秒 |
| 推送前（pre-push 钩子） | `go test ./...`、`scripts/check-docs.sh`、`scripts/check-versions.sh`、`go mod tidy -diff`、生成代码一致性（`go generate ./...` 后派生文件不能有改动，不一致时重新生成的结果留在工作区） | ≤ 60 秒 |
| CI：推送到 main、PR | `.github/workflows/ci.yml`：静态检查、单元测试、集成测试、镜像构建、compose 端到端 | 分钟级 |
| CI 按路径触发 | `.github/workflows/modules.yml`（`make check-modules`）、`admin-web.yml`（后台前端检查和端到端）、`generator.yml`（生成器夹具，也每天定时跑），见 [deploy-ci.md](deploy-ci.md) 的“CI” | 分钟级 |

`make ci` 在本地复现 ci.yml 的主流程。它会检查 `go.mod`/`go.sum` 和生成代码是否已经提交，
有未提交的改动时必然失败，所以在提交之后运行；编辑过程中用上面两层的命令。

钩子由 `make setup` 安装（`lefthook.yml`）。改了模块边界（新增/删除模块、改
`internal/app/modules.go`）时，本地也跑一次 `make check-modules`。新增一个由 spec 派生、提交入库
并有一致性检查的文件（如 `web/admin/src/api/meta.gen.ts`）时也要跑：删除演练会删掉 spec 里的操作，
派生文件和它的检查要能跟着变。

每个 bug fix 都应该有一个改动前失败、改动后通过的测试。

## 高风险改动的任务拆分与验收

涉及外部版本、merge、状态恢复、删除或权限的改动，任务拆分时先列出关键不变量，以及对应的正常、
异常和恢复场景。优先先写会失败的持久测试，再实现；只有正常路径的 `complete` 或手工演示不能作为
验收依据。至少检查：

- 目标版本变化时实际执行的是目标代码；
- 非冲突型工具故障立即停止，冲突解决后可以幂等续跑；
- 不同目标或状态不会被误续跑；
- 删除、重放和重复执行保持幂等，不留下额外文件或记录；
- 可执行权限、符号链接、文件路径和派生文件在临时副本中保持正确；
- 受影响包的测试先通过，再按现有层级运行全量检查。

## 检查失败时：修代码，不修检查

检查的报错信息写明了正确做法，照着改代码。以下做法等于把规则悄悄删了，不允许：

- 修改测试的期望值让它通过；
- 往 `internal/archtest/exceptions.go` 加例外、给代码加 `//nolint`；
- 改 `scripts/check-*.sh`、`internal/app/registry_test.go`、`Makefile`、`lefthook.yml`、
  `.golangci.yml`、`.github/` 下的工作流，让检查变宽松或跳过某些内容；
- 用 `--no-verify` 跳过提交钩子。

如果你认为检查本身错了，停下来向用户说明理由，由用户决定是否改规则。

## 规则变更的标记

改动以下路径的 PR 属于规则变更，CI 的 guard-rules job 要求 PR 带 `rule-change` 标签，
并且 PR 描述里有"规则变更说明"一节。这是受保护文件的唯一清单，其它文档引用这里：

```
internal/archtest/**   internal/app/registry_test.go   .golangci.yml   lefthook.yml
Makefile   scripts/check-*.sh   .github/workflows/**   .github/dependabot.yml
.redocly.lint-ignore.yaml
```

检查在 `.github/workflows/guard-rules.yml`，打标签、改 PR 描述后会自动重跑；改名挪走受保护
文件同样会被识别。dependabot 的 PR 只升级版本号，不受此限制。这是减速带，不是安全边界：
它让"改规则"在 review 里无法被忽略。

- archtest 例外必须写 `Reason` 和到期日 `Until`，过期后测试失败；
- `//nolint` 必须指明具体 linter 并写理由（`nolintlint` 的 `require-specific`、
  `require-explanation`）。

## 任务后的摩擦回顾

每个改了代码或文档的任务结束时，按下表过一遍这次遇到的摩擦。信号尽量取工具给出的客观结果，
工具看不到的（读错了文档、被用户纠正）靠回顾：

| 信号 | 例子 |
|---|---|
| 违规发现得晚 | 到了 review 或用户才发现，而不是在编辑循环、提交钩子、CI |
| 同一个检查连续失败 | 照着报错里的改法改了，还是没过 |
| 规范没找到或读错了文档 | `AGENTS.md` 的索引没指到，或者文档和要做的事对不上 |
| 用户纠正了方向或结果 | 改法、措辞、范围被用户改过 |
| 文档和代码对不上 | `scripts/check-docs.sh` 失败，或读代码时发现文档过时 |
| 流程互相矛盾 | 两份文档、或文档和工具的报错要求的做法相反，照哪个做都违反另一个 |
| 工具和环境的坑 | 命令在本机的行为和文档不一致（如 Podman 与 Docker 的差异） |
| 又踩到了已知的或修过的坑 | 撞上 [known-friction.md](known-friction.md) 里的条目，或文档里已经写过的问题 |
| 生成器的产物被改动 | `gen drift` 列出待办清单之外的修改（见 [generator.md](generator.md) 的"汇报模板"） |

回顾之前先跑 `make doc-impact`，把列出的文档逐篇核对完，汇报里写明核对了哪几篇、改了什么。

处理：

- **能在同一个改动里修的直接修**，并留下防止复发的东西：文档靠 `check-docs`，报错靠断言报错
  文字的测试，规则靠 archtest 或注册测试。找不到规范就补 `AGENTS.md` 的索引或主题文档；用户
  纠正的是项目约定，写进对应的 `docs/agents/` 文档，纠正的只是个人偏好则不入库；
- **修不了的记进 [known-friction.md](known-friction.md)**，并在汇报里告诉用户；已经解决的条目
  删掉；
- **汇报里只写有的摩擦和处理结果**，一条都没有就写一句"本次没有摩擦"。

自迭代只往更严、更清楚的方向走：

| 可以直接做（在同一个改动里说明） | 交给用户决定 |
|---|---|
| 补文档、更新 known-friction.md、改清楚非受保护代码里的报错、补生成器的能力和夹具 | 放宽或删除检查、改变已有规则的含义、改硬规则 |

新增或修改检查（archtest、注册测试、`scripts/check-*.sh`）的规则时，同时加一条构造违规输入、断言
规则会报错的测试（如 `TestCheck_EntryPackagesCannotImportOwnPostgres`、
`TestPermissionProblems_ReportsEachMistake`）：规则因为重构退化成永远不报错时，只对真实代码跑的测试
照样全绿。

受保护文件（上一节列出的路径）的任何改动，包括加严检查和只改报错文字，照“检查失败时”和
“规则变更的标记”两节处理，先向用户说明。

## 文档

- `docs/agents/` 描述**当前**代码；改了哪块，同一个改动里更新对应的主题文档。每篇标题下
  有一行"事实来源："，列出它描述的代码路径；新增文档或文档覆盖的范围变了，同步改这一行；
- 文档在代码定稿之后写，照着最终的代码写，不照着提案或设想写。写进文档的函数名、配置项、
  命令、数字（超时、限额、默认值），都在代码里搜一遍确认；
- `scripts/check-docs.sh` 检查文档里用反引号写的仓库路径、Go 标识符（`pkg.Name`、
  `Type.Method`）、测试名、配置项和 `make` 目标都存在；数字和行为描述它查不出来；
- 改完代码跑 `make doc-impact`（`scripts/doc-impact.sh`，默认和 main 的分叉点比较）：它按
  "事实来源："、文档里引用的改动文件、这次删掉的函数和类型名，列出可能受影响的文档，逐篇对照
  代码核对，要改的在同一个改动里改。提交钩子对暂存区跑同一个检查，只提示、不拦截；
- 规划中、还没实现的内容写进 `docs/proposals/`，不写进 `docs/agents/`；
- 提案和既有设计是输入，不是定论：实施或引用前，逐条用 `docs/ARCHITECTURE.md`"长期演化与项目防腐"的
  当前原则重新判断，不因为"提案里写了""以前就这样"而照做；判断后不再成立的，在同一个改动里修订提案
  文本或改掉既有设计，并说明理由；
- 只属于某个模块的设计说明可以放在 `internal/modules/<m>/docs/`，随模块一起删除；
- 提案实施完后，把仍然有效的规则写进 `docs/agents/`、选型理由写进 `docs/ARCHITECTURE.md`，然后删除
  提案；没实施的部分改写成精简的提案留在 `docs/proposals/`。仓库里不保留归档文档，历史靠 git log 查；
- 代码注释只写"为什么"，不写"改了什么、修的是哪个问题"，历史放进 commit message。

## 脚本

`scripts/` 下的脚本要在 macOS 原装环境（bash 3.2、BSD 的 find/sed/grep/tar）和 Linux（GNU）上
都能跑：

- 不用 bash 4 的特性：关联数组、`mapfile`、`${x^}`、`local -A`；空数组在 `set -u` 下用
  `${a[@]+"${a[@]}"}` 展开；
- 变量后面紧跟中文时写成 `${x}`：bash 3.2 会把紧跟的多字节字符当成变量名的一部分；
- 不用两边写法不同的参数：`sed -i`、`find -printf`、`cp --parents`、`xargs -r`、`date -d`
  （改文件用 `perl -i`，复制目录树用 `tar --null -T`，日期两种写法都试）；
- `set -e` 下循环里最后一条 `a && b` 失败会让循环返回非零、脚本悄悄退出，用 `if`；子 shell
  （`$(...)`、`< <(...)`）里改的变量传不回来，跨子 shell 的失败标记写进临时文件。

改完用 macOS 原装工具跑一遍：`PATH=/usr/bin:/bin:<go、pnpm 所在目录> /bin/bash scripts/<脚本>`。

## 提交

只在用户明确要求时才创建 commit。docs、代码注释、commit 信息用中文，代码标识符、
路径、命令保持英文。
