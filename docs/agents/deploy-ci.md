# 配置、部署与 CI

事实来源：`internal/config/`、`deploy/`、`configs/`、`.github/`、`Makefile`、`go.mod`、`scripts/check-versions.sh`、`scripts/outdated.sh`、`scripts/upgrade.sh`、`scripts/lib-deps.sh`

改 `internal/config/`、`deploy/`、`configs/`、`Makefile`、`.github/` 之前读这篇。
改受保护的检查文件（`Makefile`、CI 工作流、`scripts/check-*.sh` 等，完整清单只在
[workflow.md](workflow.md) 的“规则变更的标记”）属于规则变更，先看那一节。

## 配置

- 环境变量优先，`CONFIG_FILE` 指向的可选 YAML 覆盖非敏感默认值。`internal/config`
  只加载基础设施配置和共享 Argon2 参数；模块在自己的包里定义 `Config` / `LoadConfig`，
  经 `config.Source.Section` 认领同名顶层段，再用 `Source.Env` 覆盖环境变量。组合根加载全部
  模块后调用 `Source.Finish()`，未认领的顶层段启动失败；
- 必需字段缺失、`CONFIG_FILE` 读不了、值格式错误（比如 duration 没写单位）都在启动时
  直接报错退出，不静默用零值或旧值；`CONFIG_FILE` 严格加载（`yaml.WithKnownFields()`、
  `yaml.WithUniqueKeys()`，只允许单个文档），拼错的键、重复键、多个文档、空文件、
  `yaml:"-"` 的密钥字段写进文件都会报错；
- 可插拔能力（如微信各登录入口）的配置是可选项，按项启用（见 ARCHITECTURE.md 的
  "能力和启用分开"）：启动校验只拦"配错"（一个入口的凭据只配一半），不拦"没配"，也不要求
  "至少启用一个"，启用哪些由项目的真实业务决定。只有所有部署都离不开的配置才是必需项；
  需要填占位值才能启动，说明必需项定错了；
- secrets 不进 git：JWT 签名密钥、轮换用的旧密钥和微信各应用的 AppID / Secret 只能来自环境变量
  （字段是 `yaml:"-"`）；
  数据库 DSN 和 Redis 密码技术上也能写进 `CONFIG_FILE`，但含密码的配置文件不能提交，
  生产环境用环境变量注入。`configs/.env.example` 只放占位符，本地用 `.env`（godotenv
  加载，不提交）；
- `APP_ENV` 只用来给日志/指标打标签，不允许拿它做代码分支。多环境靠同一套 key 注入
  不同的值；
- Redis 可选：配置了 `REDIS_ADDR` 时缓存（模块按需接入 `internal/platform/cache`）和限流计数用 Redis，
  否则缓存退回 Noop、限流按进程内存计数（启动时会 Warn 一次）。

## 进程与部署

- `cmd/api`、`cmd/worker` 只做"加载配置 + healthcheck 子命令 + 调 `app.RunAPI` /
  `app.RunWorker`"，不知道任何业务模块；`api migrate` 在全量配置校验前分派，只读数据库配置；
  `api queue cancel-orphans` 需要完整配置（它构造模块、从实际注册的 worker 得出任务类型）；其余带参数调用交给 `app.RunCommand`，
  执行模块提供的子命令（`api help` 列出全部）。容器里用
  `docker compose run --rm api <模块> <动作> [参数]` 运行（镜像的 `ENTRYPOINT` 是
  `/app/api`），需要和 api 相同的环境变量。退出码：0 成功，1 执行出错，2 用法错误；
- 进程内的长期组件由 `internal/kit/lifecycle` 统一启停：任一组件出错或提前退出，
  其余组件按逆序停止，进程以非零退出码结束；
- `deploy/Dockerfile` 多阶段构建，最终镜像是 distroless nonroot（不用 root 跑是安全
  基线），`CGO_ENABLED=0`；Node 阶段构建后台前端，产物经 go:embed 打进 api 二进制，worker
  镜像不经过这个阶段（见 [admin.md](admin.md) 的“构建与部署”）。根目录的 `.dockerignore`
  排除本地的 `web/admin/node_modules`、`dist`；
- `deploy/docker-compose.yml`：postgres → `migrate`（执行 `api migrate up`，只需 `DB_DSN`）→
  api / worker，长期运行的 service 都声明了资源上限。api/worker 不会自己建表；
- api 进程可以同时跑后台 server：配置 `HTTP_ADMIN_ADDR`（为空不启动）和
  `HTTP_ADMIN_ORIGIN`（后台前端的 origin，配置了地址时必填）。后台地址建议只绑内网或
  localhost，不发布到公网入口；它和前台共用数据库连接池，连接数上限按两者合计规划。后台前端用
  自己的主机名：cookie 不按端口隔离，和前台同主机不同端口时，后台会话 cookie 也会被发到前台 server。
  后台端口上的 `/metrics`、`/docs`、`/admin.yaml` 不需要登录：反代给员工访问时只转发
  `/admin/v1` 和前端页面，生产环境用 `HTTP_API_DOCS_ENABLED=false` 关掉文档。首个超级管理员
  用 `docker compose run --rm api staff create -username root` 创建（见 auth.md 的“后台员工”），
  会话和日志参数是 `STAFF_*` 环境变量。前台用户的登录会话和微信凭据是 `AUTH_*` /
  `WECHAT_*` 环境变量（见 auth.md 的“前台用户”和 `configs/.env.example`）。其中
  `USER_PUBLIC_BASE_URL` 是必需项，缺了 api 和 worker 都起不来；`WECHAT_*` 四个入口按产品
  需要组合，每个入口的 `_APP_ID` / `_SECRET` 成对配置才启用，只配一半启动失败；其余 `AUTH_*`
  时长有默认值，`USER_COOKIE_DELIVERY_ORIGINS`、`USER_TRUSTED_IDENTITY_HEADER`（网关信任 header 模式，见
  auth.md 的"前台鉴权"）可选。`HTTP_GLOBAL_RATE_LIMIT_PER_SECOND`、`HTTP_REQUEST_TIMEOUT`、
  `HTTP_BODY_LIMIT_BYTES` 必须为正数，否则启动失败；默认值只在 `internal/config` 一处。
  `USER_PUBLIC_BASE_URL` 和 `USER_COOKIE_DELIVERY_ORIGINS` 里的每一项都必须是规范的 origin
  （小写 `scheme://host[:port]`，不带默认端口和路径），否则启动失败。compose 在 `x-user-env` 里给了开发默认值（微信各入口默认不启用，
  要用的入口在 compose 文件同级的 `.env` 里配上凭据），新增必需配置时同样要在 compose 里补默认值。compose 里
  后台监听 8081（页面和接口都在这个端口，
  只发布到宿主机的 127.0.0.1），`HTTP_ADMIN_ORIGIN` 默认 `http://localhost:8081`；
- 用 Podman 跑 compose 时（`podman compose` 调用 podman-compose）有两处和 Docker 不同：
  `up -d --build` 会重建镜像，但**不替换正在运行的容器**，要加 `--force-recreate`
  （`podman compose -f deploy/docker-compose.yml up -d --build --force-recreate --no-deps api`），否则跑的
  还是旧镜像；在终端里执行 `compose run` 会分配 TTY，输出的行尾带 `\r`，脚本解析输出（如
  `staff create` 打印的临时密码）前先 `tr -d '\r'`；
- api 只监听 HTTP，TLS 由上游反代/LB 终止；`HTTP_HSTS_MAX_AGE` 只在确实有上游终止 TLS
  时打开；
- Postgres 备份/PITR 是基础设施职责，不在应用代码里，但部署时必须确认已经覆盖。

### 前台登录的部署约束

- **web 前端和 API 必须同站**（同一个可注册域名），推荐反代成同源：refresh cookie 是
  SameSite=Strict，浏览器授权流程的绑定 cookie 是 SameSite=None，跨站时会被浏览器的第三方 cookie
  限制拦掉；
- **两张来源名单分开配**：`CORS_ALLOW_ORIGINS` 列 web 和混合 App 的 origin；
  `USER_COOKIE_DELIVERY_ORIGINS` 只列 web（名单内的请求 refresh token 只进 cookie）；
- **微信 API 的 IP 白名单**：各类微信应用开启 API IP 白名单后，服务端的出口 IP 要逐个登记，否则
  报 40164；部署时固定出口 IP（NAT）；
- **平台侧的回调配置都指向 API 域名**：服务号"网页授权域名"填全域名、不带协议，域名需要备案并在根
  目录放校验文件；开放平台网站应用填授权回调域；网站应用的"业务域名"校验文件和授权回调域是两回事；
- **网关的职责**：网关只做 TLS 终止、同源反代、`X-Forwarded-For`（配合 `HTTP_TRUSTED_PROXY=true`）、
  粗粒度限流，转发前删掉客户端带来的 `X-User-ID`。不在网关里做登录和 JWT 校验：那样吊销要等 token
  过期才生效，† 操作也拿不到 `auth_time`（见 [auth.md](auth.md) 的"前台鉴权"）。

## 版本固定

同一个版本出现在多处时，`scripts/check-versions.sh` 以一处为准校验其余各处：
Go（以 `go.mod` 为准）、govulncheck / golangci-lint（以 `Makefile` 为准，golangci-lint
还要和生成器夹具测试 `tools/gen/fixture_test.go` 里的一致）、
Postgres 镜像（以 compose 为准）、Node 主版本（以 `web/admin/package.json` 的 `engines` 为准）、
pnpm（以 `packageManager` 为准），并要求后台前端的客户端生成器写确切版本。找不到应有的版本号、或者写成 `latest`，同样会失败。
oapi-codegen、lefthook 由 `go.mod` 的 `tool` 指令固定；本地的 golangci-lint 由 `Makefile`
用 `go run` 固定版本运行（第一次要编译约 1 分钟）。不用 `@latest`：版本漂移会让 CI 因为
上游发版而变色，与本仓库的改动无关。

依赖升级由 dependabot 每周合并成一个 PR（`.github/dependabot.yml`），覆盖 Go 模块、
GitHub Actions、后台前端（`web/admin`）的 npm 依赖和 Dockerfile 里的镜像，不提议 3 天内刚发布的
版本。Go 版本、Node 镜像的主版本、
compose 里的 postgres 镜像不交给 dependabot：它写在多处，要人一起改（postgres 跨大版本
还要处理数据目录升级）。dependabot 之外，`make upgrade` 可以随时在本地做同样的兼容升级。

### 依赖升级

- `make outdated`（`scripts/outdated.sh`）只读，列出所有能升级的版本：Go 直接依赖（标出发布
  日期，冷却期内的标"冷却中"）、Go 工具链、`Makefile` 固定的两个工具、后台前端依赖、镜像，
  每项附 changelog 链接；
- `make upgrade`（`scripts/upgrade.sh`）只做兼容范围内的升级：Go 直接依赖升到同一主版本内
  的最新版（跳过发布不满 3 天的，和 dependabot 一致，`COOLDOWN_DAYS` 可改），后台前端在
  `package.json` 的范围内 `pnpm update`（pnpm 按 `pnpm-workspace.yaml` 的 `minimumReleaseAge`
  跳过刚发布的版本）；然后 `go mod tidy`、`go generate ./...`，跑 `go build`/`vet`/`test` 和前端
  的 `typecheck`/`lint`/`test`，最后列出每项"旧版本 → 新版本 + changelog 链接"。开始前要求
  `go.mod`、`go.sum`、`web/admin/package.json`、`pnpm-lock.yaml` 没有未提交的改动，检查失败时
  打印放弃这次升级的命令；
- 跨大版本、Go 工具链、`Makefile` 固定的工具、镜像不自动升：它们写在多处或可能不兼容，按上面
  "版本固定"一起改，改完跑 `make check-versions`。

**看 changelog 是升级的一部分，不是可选项。** 对 `make upgrade` 列出的每一项（以及手动升级的
每一项），读当前版本到新版本之间的 changelog / release notes，逐条归到下面三类：

1. **不兼容变更、废弃的 API、行为变化**：查本项目是否用到，用到就在同一个改动里改掉；
2. **新特性、性能优化、安全修复**：判断能不能用来替换本项目手写的实现、简化代码、改善性能
   或安全性（例如 river 新的任务选项、pgx 的新能力、oxlint 新规则、vite 的构建优化）。改动小、
   收益明确的，在同一个改动里采用并补测试；改动大或需要取舍的，写进 `docs/proposals/` 或列给
   用户决定，不要顺手做大改；
3. **和本项目无关的**：不处理。

数据库驱动、river、testcontainers 这类和 Postgres 打交道的依赖升级后，还要跑
`make test-integration`。汇报时逐项写：升了什么、changelog 里和本项目相关的要点、采用了
什么、建议了什么。dependabot 的周 PR 合并前同样按这三类过一遍。

postgres 18 起官方镜像的 `PGDATA` 是 `/var/lib/postgresql/<大版本>/docker`，数据卷挂在
`/var/lib/postgresql`（挂在旧的 `.../data` 上容器会拒绝启动）。旧大版本的数据卷**不会**报错：
17 的集群文件在卷根目录，18 只看 `18/docker`，会静默初始化一个空库，看起来一切正常但数据
都不见了。所以升级前必须先处理旧卷：本地开发 `docker compose down -v` 后重建；有数据的环境
用 `pg_upgrade` 或 `pg_dump` / 恢复迁移。

## CI

`.github/workflows/ci.yml`：

| job | 内容 |
|---|---|
| test | vet、golangci-lint（两者都带 `integration`、`genfixture` tag，见 `.golangci.yml` 的 `run.build-tags`）、`shellcheck -x scripts/*.sh`、govulncheck、`make actionlint`（workflow 文件）、`go mod verify`、生成代码一致性（`go generate ./...` 后，各模块 `httpapi/oapi`、`adminapi/oapi` 的 `api.gen.go` 和 `web/admin/src/api/meta.gen.ts` 不能有改动）、tidy 一致性、`go test ./...`（含架构测试）、`scripts/check-docs.sh`、`scripts/check-versions.sh`、构建 |
| integration | `go test -tags=integration ./...`，testcontainers 起真实 Postgres；先装后台前端依赖（升级工具和删除执行器的集成测试要重新生成前端） |
| gitleaks | 密钥泄露扫描：`make gitleaks`（开源 CLI，版本固定在 Makefile；不用 gitleaks-action，它对组织账号要求付费许可证） |
| openapi-lint | redocly 校验 spec |
| docker-build | api / worker 镜像能构建 |
| compose-integration | 起完整栈，等 api `/readyz` 与 worker 健康；按 `starter-removed.yaml` 跑 starter 通用冒烟（`scripts/ci-staff-smoke.sh`：CLI 建管理员、登录、改密、员工与角色、操作日志；worker 启动时执行 user 清理任务），再跑项目的 `project/ci/smoke.sh`（存在时） |

`.github/workflows/modules.yml`：只在 `internal/**`、`api/**`、`web/admin/src/**`、
`web/admin/scripts/**`、`tools/starter/**`（删除演练的执行器）变化时跑 `scripts/check-module-removal.sh`（约 1 分钟）。它先装好后台前端的
依赖：演练要删掉模块的后台页面再做类型检查，CI 里（设置了 `CI` 环境变量）没装依赖时脚本直接失败，
不会悄悄跳过。

`.github/workflows/admin-web.yml`：后台前端的检查和 compose 栈上的端到端测试，只在
`web/admin/**`、`api/admin.yaml`、`internal/modules/*/adminapi/**`、`internal/modules/*/domain/**`、
`internal/platform/httpserver/`、`deploy/` 等路径变化时运行（见 [admin.md](admin.md) 的“检查”）。
新增带后台页面的模块时，把它加进触发路径。

`.github/workflows/generator.yml`：生成器的夹具 CI（见 [generator.md](generator.md) 的“夹具与 CI”），
在仓库的临时副本里用 `tools/gen/testdata/` 的每份夹具和两份示例生成代码，再跑检查；按编号分成 7 片
并行，每片上限 25 分钟。
`tools/gen/**`、`tools/internal/**`（生成器从代码推导模块信息的共享包）、提供前台登录检查的 `internal/modules/user/module.go`、生成代码依赖的公共包和前端零件（`internal/platform/postgres/`、
`internal/platform/httpserver/`、`web/admin/src/lib/`、`web/admin/src/components/` 等，以工作流里的
`paths` 为准）、两份 spec、`.golangci.yml` 变化时运行，另外每周一定时跑一次。各分片共用 `warm-cache`
job 准备的缓存（Go 依赖、编译结果、golangci-lint 及其分析缓存），`go.sum` 和 `Makefile` 不变时直接命中。

`.github/workflows/guard-rules.yml`：PR 改动了检查规则时，要求 `rule-change` 标签和
"规则变更说明"（见 [workflow.md](workflow.md)）。

所有工作流共用的设置，新增工作流时照做：

- 顶层 `permissions: contents: read`，需要更多权限的 job 单独声明（如 gitleaks 的
  `pull-requests: read`）；
- `actions/checkout` 一律 `persist-credentials: false`：没有 job 需要推送，token 不留在
  `.git/config` 里给后续步骤（`npx`、`pnpm install` 的依赖脚本、`go run` 的工具）读到；
- 每个 job 设 `timeout-minutes`，卡住的 job 不跑满默认的 6 小时；
- `concurrency` 按工作流和 PR 分组，同一个 PR 连续推送时取消旧的运行，main 上的推送都跑完；
- action 用主版本 tag（如 `@v7`），由 dependabot 升级；
- `ci.yml` 不对纯文档改动加 `paths-ignore`：`scripts/check-docs.sh` 在其中检查文档引用的路径和
  标识符，跳过会让坏引用直接进 main。确实要省时间，先把文档检查拆成不带 `paths-ignore` 的独立 job。

本地 `make ci` 复现 test job，要求改动已经提交（它检查 `go.mod` 和生成代码是否已提交）。
改了 `api/openapi.yaml` 或 `api/admin.yaml` 后跑 `make generate`，把生成的文件一起提交。
