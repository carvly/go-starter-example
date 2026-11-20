# go-starter 架构概览

事实来源：`internal/app/modules.go`、`internal/archtest/boundaries_test.go`、`internal/platform/`、`internal/kit/`

第一次了解这个项目时读这篇：它回答"代码是怎么组织的、依赖往哪个方向走、哪些能力已经
实现"。要改某一类代码时，读 [`AGENTS.md`](../AGENTS.md) 链接的 `docs/agents/` 主题文档；
分层、模块和运行时的示意图在 `docs/diagrams/`（浏览器打开），画法约定见
[`docs/agents/diagrams.md`](agents/diagrams.md)。

## 目标

生产级 Go 后端 starter：把鉴权、缓存、可观测性、部署、多实例 worker 协调这类几乎每个
新后端服务第一周都要回答一次的问题提前决定好，并给出最小可用的实现。判断要不要放进来
有两条标准：是不是几乎每个服务都要回答；放进来之后是不是仍然只需要改几行配置或照抄
一个例子。

## 设计原则

| 原则 | 含义 | 落点 |
|---|---|---|
| 边界清晰 | 一个功能的代码集中在一个模块目录；模块之间单向依赖 | `internal/modules/<m>/`、依赖方向 |
| 约定尽可能机器校验 | 依赖、归属、生成一致性等客观规则由工具检查，报错写明正确做法；语义复用、适度抽象等判断由评审和 Agent 自检覆盖 | `internal/archtest`、`internal/app` 的注册测试、`scripts/check-*.sh` |
| 文档按需可取 | 每次只读一份短索引，按任务链接到主题文档；文档描述现状，规划只写在本地提案里、不入库也不被引用 | `AGENTS.md`、`docs/agents/`、`scripts/check-docs.sh` |
| 显式优于隐式 | 依赖关系可以靠"跳转到定义"追到底：不用反射、`init()` 注册、`context.Value` 传业务依赖、按字符串运行时查找 | `internal/app/modules.go`、构造函数注入 |
| 能力和启用分开 | starter 把同一类的可插拔能力做全（登录入口、第三方集成等），每个项目按真实业务只启用用到的；启用由配置决定，没配就不启用 | `internal/config` 的启动校验、`docs/agents/deploy-ci.md` 的"配置" |

局部性的价值是检索精度（一个关键词只落在一个模块里，改动影响范围可预测），不是节省上下文。
检查越多，越要防止"让检查通过"代替"让代码正确"，所以规则本身也受保护（见
[`docs/agents/workflow.md`](agents/workflow.md)）。

### 长期演化与项目防腐

这些约束适用于每次模块、组合根、通用工具和规范的设计。目标不是追求机制代码最少，而是把分散、隐式、
重复的复杂度改成集中、显式、可测试的复杂度。评价设计时既看第一次实现，也看模块和升级次数增加后
变化落在哪里。

- **先修架构，再考虑补丁**：发现问题先找根因属于哪一类——边界划错、名字与职责不符、同一事实有两份、
  能力只做了一半——修正根因，让同类问题不再出现。局部特例、额外检查、解释性注释、兼容分支都是补丁，
  只在修正根因的代价明显不成比例时使用，并登记到 `docs/agents/known-friction.md` 等待收敛；
- **边界先行**：顺序是模块边界 → 文件与数据归属 → 公开契约 → 通用工具 → 检查机制。检查只能证明
  边界未被破坏，不能代替边界设计；发现两方需要频繁修改同一文件时，先调整边界或扩展点；
- **事实唯一，声明优先**：归属、模块能力和依赖只有一份声明，工具和检查从声明与实际代码推导。能推导出来
  的第二份事实直接删除，不加检查去绑定两份事实，也不用会与代码漂移的豁免标志代替事实。新增模块应只
  增加声明和组合根的固定接线；通用工具需要 `if/switch`、专用删除规则或升级例外时，先重新审视边界；
- **名字一致**：一个概念在目录、包名、配置段与环境变量前缀、迁移源、任务 kind、日志前缀里用同一个名字
  （模块名）；HTTP 路径按能力或资源命名，外部系统凭证按外部系统命名。名字与职责不符时改名，不靠注释
  解释对应关系；
- **能力完整，不留半成品**：starter 可以提供暂无模块使用的能力，也可以预留扩展点。预留的空位必须同时
  满足：链路接通（从入口到实现每一段都能传递，只差具体实现）、不可误用（声明支持却没接通时明确报错）、
  形状有依据（来自标准或真实需求）、有替身实现的测试证明能走通。做不到就接通或删除；
- **只表达真实状态**：流程和状态机只覆盖实际存在的阶段，不为假想需求预建插件系统、通用事件总线或
  流程框架；
- **共享最小化**：来源文件明确唯一修改方，双方都修改的才共享，派生文件从来源重新生成；
- **兼容成本按真实下游评估**：没有线上数据和下游项目时直接改到目标形态，不做兼容层；有了之后，迁移源名、
  任务与事件 kind、配置项、HTTP 路径这些持久化或对外的标识变更必须带升级路径。

项目防腐包括两层：

1. **文件边界**：上游 starter 与业务项目各有明确的修改范围，starter 升级不能覆盖项目所有的内容；
   项目需要的能力优先经配置和公开扩展点接入，本地修改上游文件只能是登记在案、等待上游吸收的临时措施；
2. **语义边界**：业务 domain 只定义自己的模型和窄端口，跨模块能力由 `internal/app` 适配器连接并翻译
   模型和错误。业务 domain 不依赖对方的 Repository、数据库表、HTTP 生成类型或内部实现；公开事件和
   扩展点是稳定契约，内部重构不应要求业务模块跟着修改。

防腐不等于给所有调用再包一层：模块端口和组合根适配器已经是边界，没有模型、状态或错误语义差异的简单
调用不新增转发层。每次改动同时考虑接入、测试、删除和升级，而不只考虑功能能否运行。

### Go 模块与数据边界

- 非公开实现放在 `internal/`，但 `internal/` 只限制仓库外导入，模块间边界仍由 `archtest` 保证；
- 模块是围绕业务能力组织的垂直切片。组合根是唯一知道完整模块清单并连接模块的地方，platform 不含
  业务模块特例；
- 接口由消费方按需定义，只包含真正使用的方法。跨模块同步调用使用消费方端口和组合根适配器，不在业务
  包内实例化其他模块的具体实现；
- `httpapi`、`adminapi`、`jobs` 调用本模块 `domain.Service`；`postgres` 实现 domain 定义的 Repository
  端口；`module.go` 显式构造并注入具体实现。domain 不 import 具体 postgres 实现；
- 每张业务表只有一个所属模块，其他模块不得直接读写、Join 或建立跨模块外键。多步写操作有明确的
  `UnitOfWork` 边界，事件与业务写入在同一事务入队；
- 数据库存储结构不得绕过 OpenAPI 直接成为 HTTP 契约。请求和响应以 spec 为准，由生成类型和 handler
  完成边界转换；不强制增加 PO/DO 双层模型，只有存储结构与领域语义确实不同才增加转换；
- 使用显式构造函数、`Deps` 和 `Config` 表达依赖，必需依赖不能藏进可选参数。Functional Options 只适合
  少量真正可选的配置，不用于掩盖依赖过多或规避必要的签名变化；
- 优先组合，谨慎使用结构体嵌入，不因嵌入意外扩大模块的公开方法面。

### 后台前端边界

Feature 划分、状态位置和类型契约规则见 [`docs/agents/admin.md`](agents/admin.md) 的"Feature 与状态边界"。

### Agent 编写代码前自检

1. **归属**：代码是否有唯一所属模块？是否把业务特例写进了 platform 或通用工具？
2. **Go 依赖**：是否跨模块直接 import？接口是否由消费方定义？domain 是否接触具体存储实现？
3. **数据边界**：是否直接读写其他模块的表？是否让存储结构绕过 OpenAPI 成为 HTTP 契约？
4. **事务与事件**：多步写入是否有事务边界？事件是否同事务入队并保持契约稳定？
5. **React 边界**：是否深入引用其他 Feature 的私有实现？通用目录是否反向依赖 Feature？
6. **状态位置**：服务端状态是否交给 TanStack Query？局部状态是否被不必要地全局化？
7. **类型安全**：是否使用 `any`、无依据的断言或手工复制 DTO 逃避契约检查？
8. **长期演化**：这是在修根因还是在打补丁？同类模块能否只加声明接入、删除和升级？是否新增模块专用分支、
   第二份事实或与职责不符的名字？
9. **适度抽象**：是否为了假想需求引入 Options、Hook、公共组件或通用框架？预留的空位是否链路接通并有测试？
   是否多包了一层？
10. **验证与文档**：是否运行覆盖本次变化的检查，并同步更新受影响的文档、生成代码和测试？

## 依赖方向

```
cmd ──> app ──> modules/*/module.go ──> modules/*/{httpapi,postgres,jobs}
                                               │           │
                                               ▼           ▼
                                      modules/*/domain   platform/*
                                               │           │
                                               ▼           ▼
                                                  kit/*（仅标准库）
```

由 `internal/archtest` 从包路径推导并强制，新增/删除模块不需要改任何配置。完整规则（domain 的依赖、模块
之间只经组合根粘合、platform 不认识模块、模块只读写自己登记的表、接入包只经 Service 读写……）见
[`docs/agents/modules.md`](agents/modules.md) 的"依赖规则"。

## 目录速查

```
cmd/api, cmd/worker        进程入口：加载配置 → app.RunAPI / app.RunWorker
internal/
  app/                     组合根：infra.go、modules.go、migrate.go、module_configs.go、api.go、worker.go（跨模块调用时另有 adapters_*.go）
  kit/                     只依赖标准库的通用件
    errs/                  带 Kind 的业务错误，Kind 决定 HTTP 状态码
    logctx/                ctx 里的 logger 和 request_id
    uow/                   UnitOfWork 契约（uowtest 是测试替身）
    lifecycle/             进程内组件的统一启停
  platform/                与业务无关的基础设施
    postgres/              连接池、cursor 编解码（pgtest：集成测试用的容器化 Postgres）
    dbtx/                  UnitOfWork 实现：Resolve/ResolveTx、嵌套 SAVEPOINT
    queue/                 river client、事务内入队的 Inserter、worker 组件
    cache/                 RedisCache[T] / NoopCache[T]
    redis/                 Redis 客户端与探活
    ratelimit/             Echo 限流的计数存储（Redis GCRA / 内存）
    crypto/                Argon2id、JWT 签发与校验
    oauth/                 第三方登录：浏览器 OAuth 流程与原生 code 换身份（目前只有微信）
    httpserver/            Echo 构造（前台、后台两个 server）、中间件链、错误渲染、健康检查、API 文档
    logger/                slog 构造
    healthcheck/           二进制自带的探活子命令
    cli/                   api 子命令的查找与执行（命令由模块提供）
  modules/                 业务模块，每个目录自带 domain/postgres/httpapi[/events/jobs]、
                           migrations + module.go
    user/                  前台用户：登录（微信四个入口）、会话、重新验证、注销（发布
                           user_deleted 事件），后台的用户管理，提供前台鉴权中间件
    staff/                 后台员工：账号、会话、角色权限、操作日志，提供后台鉴权
  archtest/                依赖方向的架构测试
  config/                  配置加载
api/openapi.yaml           前台接口唯一真相源，operation 用 tag 归属到模块
api/admin.yaml             后台接口唯一真相源，每个操作带 x-permission
web/admin/                 后台前端（独立的 pnpm 项目），构建产物经 go:embed 打进 api 二进制
scripts/                   检查与升级脚本（check-docs.sh、check-versions.sh、check-module-removal.sh 等）
tools/gen/                 生成器（开发工具，不进运行时）：新资源的各层代码、前端的 meta.gen.ts
deploy/                    Dockerfile、docker-compose.yml
```

## 一个请求和一个事件怎么走

前台请求都经过 `httpserver` 的全局中间件链、按 tag 生成的模块路由和模块 Service；
需要异步处理时，业务写入与 river 入队在同一个事务提交，worker 消费方按事件里的 request_id
重建日志上下文。当前项目的模块清单与链路见 `docs/PROJECT.md`。

## 技术选型

Go 1.27、Echo v5、pgx/v5 + Postgres、golang-migrate、riverqueue/river、oapi-codegen
（spec-first，按 tag 分模块生成）、go-playground/validator、go-redis、slog、Prometheus、
testcontainers-go。显式构造函数注入，不用 wire/fx。

几处取舍：

- 数据库直接用 pgx 的原生接口（`pgxpool`、`pgx.Tx`），不用 ORM：反射映射和按约定触发的 hook 在编译期
  看不出来，ORM 自带的事务 API 也接不进"Repository 经 `Resolve` 取连接"的机制
  （见 [`docs/agents/transactions.md`](agents/transactions.md)）；
- 队列用 river，不手写 outbox：事务内入队、指数退避重试、唯一性、定时任务和多实例调度都是现成的，手写
  就要自己保证领取、租约、死信的正确性；
- 接口 spec-first（oapi-codegen）：spec 是唯一真相源，路由和 spec 的一致性由测试检查，不从代码注释生成
  spec；
- 配置以环境变量为主，`CONFIG_FILE` 可选 YAML 覆盖非敏感默认值，不用 Viper：不需要多格式、远程配置中心；
- 显式构造函数注入，不用 wire/fx：依赖关系靠"跳转到定义"就能追到底。

## 已实现 / 规划中

| 能力 | 状态 |
|---|---|
| 模块化结构、架构测试、注册完整性测试、删除演练 | 已实现 |
| 前台会话鉴权中间件（JWT 验签 + 按 sid 查会话）/ 网关信任 header、Argon2id（后台员工密码） | 已实现 |
| 前台登录与会话：微信四个入口、服务端会话、refresh 轮换、重新验证、绑定/解绑、注销 | 已实现；第三方登录的通用链路（PKCE、nonce、表单回调、原生 id_token）已就绪，手机号、邮箱、密码和 GitHub/Google/Apple 等 provider 未实现 |
| 缓存工具（`RedisCache[T]` / `NoopCache[T]`，事务提交后删缓存） | 已提供，模块按需接入；singleflight 防击穿未实现，有热点时再加 |
| 全局限流（Redis 共享 / 内存） | 已实现 |
| river 队列、事务内入队、幂等消费、永久失败收口 | 已实现 |
| 后台接入面（独立 server、`api/admin.yaml`、`adminapi/` 约定） | 已实现 |
| 后台员工：账号、服务端会话、登录退避、角色与权限码、操作日志、CLI 创建管理员 | 已实现（`staff` 模块） |
| 后台前端：登录、强制改密、员工、角色、操作日志、个人设置，嵌入二进制 | 已实现（`web/admin/`）；TOTP、文件上传、任务面板等未实现，按需扩展 |
| 定时任务、模块子命令 | 已实现（模块的 `PeriodicJobs()` / `Commands()`），staff 是第一个使用者 |
| 集成测试（Repository 契约、dbtx、入队） | 已实现，需要 Docker |
| 日志、request_id 跨进程传播、Prometheus 指标、健康检查 | 已实现；OpenTelemetry tracing 未接入 |
| Docker 镜像、compose、CI、提交钩子、dependabot | 已实现；镜像漏洞扫描（trivy）未接入 |
| 手写 outbox | 不实现，river 已覆盖（见"技术选型"） |
| 外部消息队列 relay、跨服务 HTTP 调用封装、熔断 | 不预先实现，出现真实需求时再加 |

## 明确不放进来的

具体云厂商 SDK、LLM provider、通用 RBAC 框架（Casbin 这类）、多租户、前台前端。它们是
"需要时自己加"的扩展点，不是每个服务都要回答的问题。后台管理是例外：几乎每个业务服务都
需要一个运营后台，所以提供后台接入面、员工账号与权限码、后台前端。前台登录也内置了微信登录，
但只用 `net/http` 调微信接口，不引入 SDK。
