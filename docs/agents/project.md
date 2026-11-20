# 业务项目：创建、开发与修改 starter

事实来源：`starter-owned.txt`、`tools/starter/`、`scripts/starter-upgrade-entry.sh`、`project/`

## 开启新项目

1. 必须 clone 保留共同历史，从最新 starter tag 开始，不能用模板复制文件：
   `git clone <starter> myshop && cd myshop && git checkout -B main <tag>`，再把 origin 改名为
   starter，并添加项目自己的 origin。Go module path 保持 `app.local`，避免全仓 import 冲突。
2. 按需执行 `make remove-modules M="user"` 或 `make remove-modules M="staff"`。工具自动带上
   依赖闭包、删除 spec/前端/迁移并写 `starter-removed.yaml`；已上线项目的表不会自动删除。
3. 项目身份只改 `README.md`、`AGENTS.project.md`、`docs/PROJECT.md` 和 `docs/diagrams/`；项目自己的变更记录写在根目录
   `CHANGELOG.md`（项目所有，starter 不提供也不覆盖；starter 的发版说明是 `starter-changelog.md`）。
   项目规则不要写进 starter 的 `AGENTS.md` 或 `docs/ARCHITECTURE.md`。
4. 照 `configs/.env.example` 写 `.env`，只填保留模块的必需配置。执行 `make setup`、
   `go run ./cmd/api migrate up`；保留 staff 时执行 `api staff create -username <名字>`。
5. 项目模块的 CI 配置放 `project/ci/env`，业务冒烟放 `project/ci/smoke.sh`，E2E 放
   `web/admin/e2e/project/`。最后跑 `make ci`、`make check-modules` 并提交项目起点。

## 开发业务模块

按“配置 → 已有扩展点 → 已有业务模块 → 新模块”的顺序选择。新模块用生成器，不能和 starter
模块重名。每个模块都应有独立的迁移、配置和 `module.yaml` 前端归属，能被干净删除。

- 用户数据按 `user_id` 存在业务模块，不建外键；经 `GetPublicProfiles` 读取公开资料，
  登记 `userM.AddDataCleaner(...)` 并提供每日清扫。
- 前台接口挂 `userM.Middleware()`，敏感操作再挂 `RequireRecentAuth()`；后台操作写 `x-permission`。
- 跨模块同步调用经 `internal/app/adapters_*.go`；构造行推导不出的必需依赖写在可选的 `moduleRequires` 里（只在有这类依赖时写）。
- 模块配置在自己的包里定义 `Config` / `LoadConfig`，在组合根加一行；CI 配置写 `project/ci/env`。
- 集成测试传本模块 `Migrations`；业务链路放项目冒烟，项目文档更新 `docs/PROJECT.md`。

## 所有权

starter 所有的文件由两处推出，不再另列：`starter-owned.txt` 的每一行按前缀匹配；其中的模块目录行
（`internal/modules/<m>/`）再并入该模块 `module.yaml` 的 `web_paths`。所以 starter 模块的后台页面只写在
`module.yaml` 里。以下文件即使匹配也不算 starter 所有，升级时不按 starter 版本覆盖：派生文件（`*.gen.*`、
`schema.d.ts`）、锁文件（`go.sum`、`pnpm-lock.yaml`），以及项目自己的 CI 工作流——业务项目新增的工作流
命名为 `.github/workflows/project-*.yml`。

## 需要改 starter 时

按“配置 → 扩展点 → 业务模块 → 向 starter 提改动 → 临时本地补丁”的顺序处理。
业务项目默认不改 `starter-owned.txt` 中的文件。临时补丁必须同时向 starter 提改动，并在
`project/starter-patches.md` 记录文件、原因和上游 PR/issue；升级预检会列出这些漂移。
只需某个修复时优先 `git cherry-pick`，后续正常升级会自然合并它。
