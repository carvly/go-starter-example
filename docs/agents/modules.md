# 业务模块：新增、删除、跨模块协作

事实来源：`internal/app/`、`internal/archtest/boundaries_test.go`、`tools/starter/`、`internal/modules/*/module.go`

改 `internal/modules/` 或 `internal/app/` 之前读这篇。

## 模块的形状

每个业务模块是 `internal/modules/<m>/` 下的一个目录，内部按职责拆子包：

```
internal/modules/<m>/
  module.yaml        web_paths：前端页面、路由和 E2E 的唯一归属声明
  migrations/        本模块独立编号的数据库迁移
  module.go          对组合根暴露的唯一入口：Deps、Module、New，以及按需实现的
                     RegisterHTTP / RegisterAdminHTTP / RegisterJobs / Commands / PeriodicJobs
  domain/            实体、错误（errs.New）、端口接口（ports.go）、Service、单元测试
    domaintest/      内存 fake + Repository 契约测试（RunRepositoryContract），只能在测试里导入
  postgres/          Repository 的 Postgres 实现 + 跑契约测试的集成测试
    tables.go        本模块拥有的表（见 database.md 的“表归属”）
  httpapi/           前台 handler、routes.go（操作级中间件清单）、oapi/（从 api/openapi.yaml 按 tag 生成）
  adminapi/          后台 handler，oapi/ 从 api/admin.yaml 生成（见 http.md 的“后台接入面”）
                     httpapi/ 和 adminapi/ 至少有一个（staff 和生成的纯后台模块只有 adminapi/）
  events/            （可选）对外公开的事件契约，只有纯数据类型
  jobs/              （可选）river worker 薄适配
```

参考实现：

- `internal/modules/user/`：前台用户（后台的用户管理由生成器生成），提供前台鉴权中间件（用法见
  [auth.md](auth.md) 的"给操作加鉴权"）；发布 `user_deleted` 事件并在自己的 `jobs/` 里编排
  各模块的清理（`AddDataCleaner` 扩展点，见下面"跨模块协作"），还有定时清理过期行的
  `PeriodicJobs()`；
- `internal/modules/staff/`：后台接入面（`adminapi/`）、提供后台鉴权（`AdminGuard`）、
  子命令（`Commands()`）和定时任务（`PeriodicJobs()`）。

### 命名

模块名 `<m>` 是唯一的命名来源（`docs/ARCHITECTURE.md`"名字一致"），新模块照这张表取名：

| 标识 | 规则 | user | staff |
|---|---|---|---|
| 目录 / 包 | `internal/modules/<m>` | `user` | `staff` |
| 组合根变量 / 字段 | `<m>M` / `Modules.<M>` / `moduleConfigs.<M>` | `userM` | `staffM` |
| 配置段 / 环境变量前缀 | `<m>:` / `<M>_*` | `user:` / `USER_*` | `staff:` / `STAFF_*` |
| 迁移源 | `migrate.Source{Name: "<m>"}`（版本表 `schema_migrations_<m>`） | `user` | `staff` |
| 定时任务 kind | `<m>_<动作>` | `user_cleanup` | `staff_audit_cleanup` |
| 日志前缀 | `<m>: ...` | `user:` | `staff:` |
| spec tag | 资源名，经模块 `oapi/config.yaml` 的 `include-tags` 归属模块 | 前台 `user`、后台 `users` | 后台 `staff` |

例外：HTTP 路径按能力或资源命名（前台登录在 `/v1/auth/*`，员工登录在 `/admin/v1/auth/login`）；
外部系统的凭证按外部系统命名（`JWT_*`、`WECHAT_*` 由 `user.LoadConfig` 认领）；事件 kind 按事件命名
（`user_deleted`）。

为什么拆子包而不是一个模块一个包：Go 的包边界由编译器免费强制（domain 无法访问
postgres 的未导出内容、不能循环 import），`internal/archtest` 只需要补编译器管不到
的规则。

## 依赖规则（由 `internal/archtest` 强制，违反时 `go test ./...` 失败）

1. `modules/<m>/domain` 只能依赖标准库（ID 用标准库的 `uuid`）、`internal/kit/*`、
   本模块的 `events`。需要数据库、缓存、队列等能力时，在 domain 里定义窄接口，
   由本模块的 `postgres/` 或 `internal/platform` 实现，在 `module.go` 里注入。
2. 模块之间不能互相 import。唯一例外是其它模块的 `events` 包（公开契约）。
3. 只有 `internal/app` 可以导入模块；`internal/app` 只能被 `cmd/*` 导入。
4. `internal/platform/*` 不认识任何模块；`internal/kit/*` 只依赖标准库。
5. `events` 包只依赖标准库。
6. `domaintest`、`uowtest` 这类测试替身包只能在 `_test.go` 里导入。
7. 模块只读写自己在 `postgres/tables.go` 登记的表，模块的表之间没有外键
   （`internal/archtest/tables_test.go`，详见 [database.md](database.md) 的“表归属”）。
8. 接入包（`httpapi`、`adminapi`、`jobs`）只能经本模块的 Service 读写数据：不能 import
   本模块的 `postgres`，源码里也不能引用 `domain` 里名字以 `Repository` 结尾的接口
   （`internal/archtest/entry_test.go`）。handler / job 的构造函数只接收 `*Service`。

例外只能登记在 `internal/archtest/exceptions.go`，必须写理由和到期日；新增例外属于
规则变更（见 [workflow.md](workflow.md)）。

## 跨模块协作

**同步查询**（业务模块要读 user 的公开用户资料）：

1. 消费方在自己的 domain 里定义窄接口，只声明需要的 ID 和返回值；
2. 在 `internal/app/adapters_<消费方>_<提供方>.go` 里用提供方的 Service 实现它，并把
   提供方的错误翻译成消费方自己的错误；
3. 在 `internal/app/modules.go` 里把适配器传进消费方的 `Deps`。

**一个事件牵动多个模块的写操作**：

1. 发布方在 `events/` 里定义事件，在同一个事务里入队（见 [queue.md](queue.md)）；
2. 编排放进一个独立模块，它定义自己需要的窄端口，
   由组合根用各模块的 Service 适配；业务规则写在不依赖 river 的 `process.go`，
   `jobs/` 只做 river 适配；
3. 不要把编排塞进发布方或任一被写的模块的 domain：那会让它依赖另一方。

**发布方编排、各模块提供清理能力**（注销账号后各模块删掉自己存的这个用户的数据）：

1. 发布方（user）在 `events/` 里定义 `user_deleted`，在业务事务里入队；
2. worker 放在发布方的 `jobs/`、经它的 `RegisterJobs` 注册（模块删掉时 worker 随之消失）；
3. 被调用的模块实现 `user.DataCleaner`，全部模块构造完成后在 `modules.go`
   用独立一行 `userM.AddDataCleaner(memberM.UserDataCleaner())` 登记。登记本身不是
   构造依赖，删除任一模块时登记行随之删除；
4. 持有用户数据的模块每天分批清扫：经组合根适配器调用 `GetPublicProfiles`，删除已注销
   或已不存在用户的数据。清扫与即时清理共用同一个幂等实现。细节见 [queue.md](queue.md)。

判断用哪条：

- 只需要读另一个模块的信息 → 同步查询；
- 一个业务事件要按业务规则协调多个模块的写操作（扣库存失败要把订单收口）→ 事件 + 独立的
  编排模块；
- 发布方天然就是编排者，其它模块只提供互不相关的幂等能力（各删各的数据，没有跨模块的
  业务规则）→ 发布方编排 + 注入窄接口。

**新增持有前台用户数据的模块**时，要登记 `user.DataCleaner` 并提供每日清扫，
否则滚动部署或重试耗尽时可能留下已注销用户的数据。

## 新增一个模块

新模块以一张新表为中心时，用生成器（[generator.md](generator.md)），下面几步它都会做好。
手写时：

1. 建 `internal/modules/<name>/`（`domain/`、`postgres/`、`httpapi/` 或 `adminapi/`、`module.go`），
   结构参照生成器的产物或 `internal/modules/user`；
2. 往 `api/openapi.yaml`（后台是 `api/admin.yaml`）加带 `tags: [<tag>]` 的操作和 schema，跑 `go generate ./...`。
   后台操作还要写 `x-permission`（见 [http.md](http.md) 的"后台接入面"），再
   `cd web/admin && pnpm gen:api` 更新前端客户端；
3. 在 `internal/app/modules.go` 里加：import 一行、`Modules` 字段一行、`buildModules`
   里构造一行（变量名固定为 `<m>M`）、返回值一行、注册切片里各一行；在
   `internal/app/migrate.go` 加迁移来源一行；模块有配置时在自己的包里定义 `Config` /
   `LoadConfig`，并在 `internal/app/module_configs.go` 加加载一行；
4. 写建表 migration（见 [database.md](database.md)），表名登记在 `postgres/tables.go`；
5. 决定每个操作要不要鉴权：在 `httpapi/routes.go` 的 `OperationMiddlewares` 里按
   operationId 声明，spec 里对应操作去掉 `security: []`；
6. `go test ./...`：`internal/app` 的测试会指出漏注册的模块、漏写 tag 的操作、没有
   worker 消费的事件、没登记的子命令和定时任务。

组合根只改 `internal/app/modules.go`、`migrate.go`、`module_configs.go`（有跨模块同步调用时另建适配器
`adapters_*.go`），不改 `cmd/api/main.go`、`cmd/worker/main.go`。

## 删除一个模块

用 `make remove-modules M="<模块>"` 真实删除。`tools/starter` 统一计算依赖闭包、模块目录、
组合根适配器和 import 它的测试、spec tags 以及 `module.yaml` 的 `web_paths`；执行器删除这些路径、
组合根里的行、spec 操作和不再被引用的 schemas，再重新生成并运行检查。`--dry-run` 只预览。
依赖它的模块会一起删除：构造参数里引用了 `<m>M` 的模块依赖它；推导不出的依赖（如 A 发布的事件只有 B 消费）在
`internal/app/modules.go` 里用可选的 `moduleRequires` 声明，没有这类依赖时不写这个变量（写法见 modules.go 顶部注释）。工具默认把模块名写入 `starter-removed.yaml`，
数据库里已有的表不会自动删除；所有实例部署后还要执行 `api queue cancel-orphans`。

删掉提供后台鉴权的模块（`modules.go` 的 `adminAuth()` 里引用的模块，即 staff；`tools/internal/modinfo` 从代码解析）时，后台接入面整体
去掉：其它模块在 `adminRegistrars` 里的行和 `api/admin.yaml` 里的操作一并删除，它们的
`adminapi/` 代码保留但不再注册。没有鉴权的后台路由不能注册（`httpserver.NewAdmin` 会拒绝启动）。

`make check-modules` 对每个模块演练一次删除：删完必须 build/vet/test 全绿，后台前端的 typecheck
也要通过（后台接入面整体去掉时不检查；`web/admin` 没有 `pnpm install` 时本地跳过，CI 里失败）；只删目录、
保留注册时 build 必须失败（防止退化成 `init()` 隐式注册）。

## 组合根

`internal/app` 是唯一知道"有哪些模块"的地方：

- `infra.go`：连接池、Redis、只入队的 river client 等连接类资源，按配置决定是否创建；
- `modules.go`：模块清单，显式构造，不用运行时注册表、反射或 `init()`；
- `adapters_*.go`：跨模块适配器，有跨模块同步调用时新建（starter 目前没有）；
- `api.go` / `worker.go`：组装两个进程的组件列表，交给 `internal/kit/lifecycle` 统一启停；
- `command.go`：`api <模块> <动作>` 子命令的入口，构建和 api 相同的 Infra 与模块，不启动 HTTP。

模块对组合根的扩展点，每种在 `modules.go` 里有一个注册切片，每个用到它的模块占一行：

| 模块方法 | 注册切片 | 用途 | 漏登记时谁发现 |
|---|---|---|---|
| `RegisterHTTP(*echo.Group)` | `httpRegistrars` | 挂载 spec 里本模块 tag 的操作 | `TestRoutes_MatchOpenAPISpec` |
| `RegisterAdminHTTP(*echo.Group)` | `adminRegistrars` | 挂载 `api/admin.yaml` 里本模块 tag 的后台操作 | `TestAdminRoutes_MatchAdminSpec` |
| `RegisterJobs(*queue.Workers)` | `jobRegistrars` | 用 `queue.AddWorker` 注册 worker（同时记下 kind） | `TestWorkers_EveryEventKindHasWorker` |
| `Commands() []cli.Command` | `commandRegistrars` | api 二进制的子命令，名字是 `<模块> <动作>` | `TestExtensions_EveryExtensionIsRegistered` |
| `PeriodicJobs() []queue.Periodic` | `periodicJobRegistrars` | 定时任务（见 [queue.md](queue.md)） | 同上，并检查每个定时任务都有 worker |

子命令的参数由命令自己用 `flag.NewFlagSet` 解析；结果写 `Run` 收到的 `out`（stdout），
日志写 stderr。命令不经过 HTTP 中间件，需要审计时自己记录。

模块对外只暴露 `Service` 或显式的窄能力（如 `user.Module.Middleware()`），
Repository 不是任何公开字段。

## 拆成独立部署的服务：前置条件

现在是模块化单体，默认不拆。前台（`httpapi/`）和后台（`adminapi/`）共用同一个模块的 Service，只分
接入包：不另起一个后台服务直连同一个库，那样会绕过拥有缓存的 Service，库存、金额、幂等这些规则要写
两份。将来拆服务按业务模块拆，不按前台、后台拆，前台和后台各自变成调用业务服务的薄接入层。

只有出现以下信号之一才考虑拆分：某个模块需要单独扩缩容、
不同团队的发布节奏互相阻塞、某个模块的故障必须和其它模块隔离。“代码多了”不是理由。

已经由检查保证、拆分时不用再处理的：模块之间不互相 import（`internal/archtest`）、
不读写对方的表也没有跨模块外键（表归属检查）、跨模块同步调用都经消费方定义的窄接口
（换成远程实现时 domain 不用改）、事件契约只依赖标准库（`events/`）。

迁移已按模块独立编号并使用独立版本表；拆库时把模块的表和对应版本表一起迁移，platform
迁移在每个库各执行一份。

拆分前必须先改的（现在不预先实现，出现信号再做）：

| 现状 | 拆分前要改成 | 位置 |
|---|---|---|
| 同步调用由组合根用对方的 Service 适配 | 远程客户端（oapi-codegen 按 tag 生成 client）：超时、只重试幂等请求、透传 request_id、把远端错误翻译成消费方的错误；本地和远程两个适配器跑同一套契约测试 | `internal/app/adapters_*.go` |
| river 只在同一个库里投递 | river 继续做本服务的 outbox，新增 relay worker 转发到外部 MQ | [queue.md](queue.md) 的“外部消息队列” |
| JWT 用 HS256 共享密钥 | 只校验不签发的服务出现时换 RS256/ES256 + JWKS，或后端统一用网关信任 header 模式 | `internal/platform/crypto/jwt.go`、[auth.md](auth.md) |
| 一个组合根装配全部模块 | 每个服务一个显式的组合根（`modules.go`），不在运行时自动判断对方在本进程还是远程 | `internal/app/modules.go` |
| 前台会话校验每个请求查 `user_sessions` | 拆出去的服务读不到 `user_sessions`：由网关（OpenResty `auth_request`）子请求 user 服务的内网端点校验会话，结果不缓存，user 服务不可用时需要登录的接口全部失败；各服务用信任 header 模式，并扩展为同时读会话 ID、`auth_time`、`amr`，† 操作才能用。需要拆分时再设计 | `internal/modules/user/httpapi/trusted_identity_middleware.go` |

明确不做：运行时服务注册表 / 自动识别本地或远程调用、能在 river 和外部 MQ 之间切换的
通用事件总线、为了“以后可能拆”让单体也走远程语义。本地调用和远程调用的事务、失败模式、
延迟都不同，把差异藏起来只会让问题更晚暴露。
