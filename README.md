# go-starter

生产级 Go 后端 starter：基于 [Echo v5](https://echo.labstack.com/)，按业务模块
垂直切分（每个模块自带 domain、存储、HTTP、任务），模块之间只经组合根粘合，依赖方向
由架构测试强制。在此之上补齐了鉴权、缓存、可观测性、HTTP 安全与流量控制、队列/多实例
协调、健康检查、Docker 部署产物、CI 等"几乎每个新 Go 后端服务都要回答一次"的问题。

先读 [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)（结构、依赖方向、已实现能力），
要改代码时按 [`AGENTS.md`](AGENTS.md) 的索引读 `docs/agents/` 下对应的主题文档。
README 只负责"如何跑起来"。

## 目录结构

```
cmd/api/              HTTP API 入口
cmd/worker/           后台任务入口（消费队列、定时任务）
internal/app/         组合根：唯一知道有哪些模块的地方
internal/modules/     业务模块（user、staff；每个模块自带 migrations）
internal/platform/    与业务无关的基础设施（Postgres、Redis、队列、HTTP 服务器等）
internal/kit/         只依赖标准库的通用件（错误、日志上下文、事务契约、生命周期）
internal/archtest/    依赖方向的架构测试
api/openapi.yaml      前台接口唯一真相源（spec-first，按 tag 为每个模块生成代码）
api/admin.yaml        后台接口唯一真相源（独立的后台 server，未配置时不启动）
web/admin/            后台前端（React + Vite，构建产物嵌入 api 二进制）
deploy/               Dockerfile、docker-compose.yml
configs/              .env.example
```

## 快速开始

**方式一：docker compose（推荐，一条命令跑通 api+worker+postgres）**

compose 给必需配置填了开发用的默认值。微信登录的四个入口默认都不启用，要用哪个入口就在
compose 文件同级的 `.env` 里配上它的 appid 和 secret（变量见 `configs/.env.example`）。

```bash
docker compose -f deploy/docker-compose.yml up -d
# 或用 Podman（见 deploy/docker-compose.yml 文件末尾的说明）：
podman compose -f deploy/docker-compose.yml up -d
```

**方式二：本地运行**

```bash
cp configs/.env.example .env   # 按需修改，标 [必需] 的字段不能留空
export DB_DSN=postgres://app:app@localhost:5432/app?sslmode=disable
make migrate-up
make run-api      # 另开一个终端
make run-worker
```

跑起来之后，浏览器打开 `http://localhost:8080/docs` 看 Redoc 版 API 文档；
`HTTP_API_DOCS_ENABLED=false` 可以整体关掉这组只读文档路由。

compose 栈里的后台在 `http://localhost:8081`，先创建首个超级管理员，用输出的临时密码登录：

```bash
docker compose -f deploy/docker-compose.yml run --rm api staff create -username root
```

本地运行时后台前端要先构建（`cd web/admin && pnpm install && pnpm build`），开发用
`pnpm dev`，见 `docs/agents/admin.md`。

## 常用命令

```bash
make setup             # 安装提交钩子（lefthook）
make test              # 单元测试 + 架构测试，秒级，不需要 Docker
make test-integration  # 真实 Postgres（testcontainers）上的集成测试，需要 Docker、Podman 或 socktainer
make lint              # golangci-lint
make generate          # 改了 api/openapi.yaml 后重新生成各模块的 oapi 代码
make check-modules     # 对每个模块演练一次删除
make ci                # 本地复现 CI 主流程
make migrate-up        # 需要先 export DB_DSN
```

完整 target 列表见 `Makefile`。各能力的实现状态见 `docs/ARCHITECTURE.md` 的"已实现 /
规划中"一节。

## 新增一个业务模块

新增一张表并开放到后台或前台，用生成器：写一份资源描述文件，一次生成 migration、模块代码、
spec 和组合根的注册，再补上描述文件里写明的业务逻辑。

```bash
go run ./tools/gen example admin > .gen/category.yaml   # 照着示例填写
go run ./tools/gen check .gen/category.yaml
go run ./tools/gen apply .gen/category.yaml
```

不需要改 `cmd/*/main.go`。完整说明见 `docs/agents/generator.md`；手写模块见 `docs/agents/modules.md`。

## git blame

纯搬迁和批量注释清理的提交登记在 `.git-blame-ignore-revs`，本地执行一次
`git config blame.ignoreRevsFile .git-blame-ignore-revs` 让 `git blame` 跳过它们。

## 许可证

MIT，见 `LICENSE`。
