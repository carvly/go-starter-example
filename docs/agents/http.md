# HTTP：spec-first、路由、错误、中间件

事实来源：`api/`、`internal/platform/httpserver/`、`internal/kit/errs/`、`internal/modules/*/httpapi/`、`internal/modules/*/adminapi/`

改 `api/openapi.yaml`、`api/admin.yaml`、任何模块的 `httpapi/`、`adminapi/` 或
`internal/platform/httpserver/` 之前读这篇。
鉴权相关见 [auth.md](auth.md)。

## spec-first 流程

`api/openapi.yaml`（前台）和 `api/admin.yaml`（后台，见下文“后台接入面”）是接口的唯一
真相源。固定流程：

1. 先改 spec；
2. `go generate ./...`：每个模块按自己的 tag 生成
   `internal/modules/<m>/httpapi/oapi/api.gen.go`（配置在同目录 `config.yaml`，
   `include-tags` 只生成本模块的操作和它引用的 schema），禁止手改生成文件；
3. 编译器报出需要跟进的 handler，再改 handler。

不允许先写 handler 再补 spec，也不允许绕过生成代码手写业务路由：
`internal/app/registry_test.go` 会把实际注册的路由和 spec 双向比对，多一条少一条都失败。

规则：

- 每个 operation 必须带 `tags: [<模块的 tag>]`，否则没有模块会注册它；
- 路径不带 `/v1`（spec 的 `servers.url` 是 `/v1`，`httpserver.APIPrefix` 统一加）；
- 全局 `security` 默认要求 bearer token，公开接口显式写 `security: []`；
- **请求体**字段的约束（必填、长度、格式）**必须同时**写进 `x-oapi-codegen-extra-tags:
  {validate: "..."}`，生成到结构体的 `validate` 标签上，由 handler 里的
  `c.Validate()` 校验。请求体字段只写 `minLength`/`format` 不加 extra-tags 不会报错，
  只是不生效。路径参数和查询参数不同：`type`（如 `integer`）和 `format: uuid` 由生成
  的绑定代码强制检查，不合法直接 400；`minimum`/`maximum` 等其它约束不检查。不用
  `oapi-codegen/echo-middleware`：它只支持 Echo v4；
- id 字段（路径参数、请求体、响应体）的写法见 [database.md](database.md) 的“主键”一节：
  `format: uuid`，请求体和响应体加 `x-go-type: string`，请求体里的 id 在 handler 用
  `strings.ToLower` 规范化；
- 前台列表接口从 v1 起就带 cursor 分页（`cursor`/`limit` 参数，响应带 `next_cursor`），
  `limit` 默认 20；不在 1–100 内时回落到默认值 20（不是截到 100）。后台表格要页码和总数的
  列表用页码分页，见下文"后台接入面"；
- `/healthz`、`/readyz` 是运维探针，不写进 spec；
- schema 字段用 `examples: [...]` 写示例值（3.1 起 `example` 已废弃），Redoc 的请求、
  响应示例和"Copy for LLM"都用它，oapi-codegen 只把它生成成注释；tag 用 `summary`
  写侧边栏显示的中文名，`name` 仍是模块的 include-tags 键，不能改；
- CI 用 redocly 校验 spec 本身是合法的 OpenAPI 3.2，按推荐规则集检查，警告也要修掉：
  接口实际会返回的状态码都写进 `responses`（挂了限流的写 429，`#/components/responses/TooManyRequests`），
  示例要满足字段约束。规则确实不适用的（如只会 302 的 OAuth 跳转接口没有 2xx），逐条豁免在
  `.redocly.lint-ignore.yaml` 里并写明原因，它在受保护清单里。
  注意 3.2 独有的 `additionalOperations`/`in: querystring` 等语法目前不能用：
  oapi-codegen 只为固定 HTTP 动词生成代码，这些操作会被静默忽略（路由/spec
  双向比对也只检查标准动词，抓不住这种遗漏）。

## handler

`internal/modules/<m>/httpapi/handler.go`：Bind → Validate → 调 Service → 序列化，
不含业务规则。`routes.go` 的 `Register` 按 operationId 声明操作级中间件（如鉴权），
模块的 `RegisterHTTP` 调它。需要当前用户时用 `httpserver.UserSessionFrom(c)` 取出，把需要的字段作为显式参数传给 Service，不把 Echo/JWT 类型带进 domain。

## 错误

- 业务错误在各模块 `domain/errors.go` 里用 `errs.New(kind, code, message)` 定义
  （`internal/kit/errs`），Kind 决定状态码：Invalid 400、Unauthorized 401、
  Forbidden 403、NotFound 404、Conflict 409、TooManyRequests 429。`errors.Is` 仍按指针相等判断。
- 需要等一段时间再试的错误（Kind 为 TooManyRequests）用 `ErrX.WithRetryAfter(d)` 带上剩余
  时间：返回的副本 `errors.Is(副本, ErrX)` 仍成立；错误处理器把剩余秒数（向上取整）写进
  `Retry-After` 响应头和响应体的 `retry_after`，spec 里引用 `TooManyRequests` 响应。
- 返回错误时用 `fmt.Errorf("...: %w", err)` 包装；错误处理器取出链上的业务错误，
  **响应只用它的 Code 和 Message**，不会带出包装链里的调用路径。
- Code 和 Message 必须可以安全地返回给客户端。只供模块内部使用、绝不能透给客户端
  的错误用 `KindInternal`（比如登录时的"账号不存在"，对外必须和密码错误统一成同一个错误），
  漏出去也只是一个固定文案的 500。
- 请求字段校验失败（`c.Validate()`）是 400，code 为 `invalid_request`，message 只列出
  JSON 字段路径和没通过的规则（`invalid request: product_id: uuid; quantity: gt=0`），
  不带 Go 结构体名和字段名（`internal/platform/httpserver/validator.go`）。
- `*echo.HTTPError` 和带 `StatusCode()` 的 Echo 内置错误按自身状态码返回，code 为
  `http_error`，**message 原样返回给客户端**：`c.Bind` 失败（handler 原样返回它的错误：JSON
  解码失败 400、请求体超过上限 413、Content-Type 不对 415，不要再包成 400，否则 413 会变成 400）、
  生成代码绑定路径参数失败（`Invalid format for parameter id: ...`）、BodyLimit 413。所以自己构造
  `echo.NewHTTPError` 时 message 也必须可以安全地返回给客户端；业务错误一律用 `errs.New`。
- 其它任何错误都是固定文案的 500，详细信息只进带 request_id 的日志。
- 新增模块、新增错误都不需要改 `internal/platform/httpserver`。

## 后台接入面

后台接口是**独立的 Echo 实例**（`httpserver.NewAdmin`，`internal/platform/httpserver/admin.go`），
监听 `HTTP_ADMIN_ADDR`，为空时不启动；前台端口上没有 `/admin/v1`。和前台共用二进制、进程和
模块的 Service，只是接入面分开：

- spec 是 `api/admin.yaml`（`servers.url` 为 `/admin/v1`），模块的后台 handler 在
  `internal/modules/<m>/adminapi/`，生成代码在 `adminapi/oapi/`（`config.yaml` 的
  `include-tags` 只取本模块的 tag，生成源换成 `admin.yaml`），由模块的 `RegisterAdminHTTP`
  挂载，经 `internal/app/modules.go` 的 `adminRegistrars` 汇总；
- 每个操作必须写 `x-permission`：`<资源>:<动作>` 格式的权限码，或保留值 `public`（只有员工
  登录）、`authenticated`、`superadmin`。`x-permission` 只出现在 `admin.yaml` 里；
- `internal/app/registry_test.go` 检查后台路由与 `admin.yaml` 双向一致、每个操作的
  `x-permission` 合法；
- `admin.yaml` 也是后台前端文案和校验的来源（`make generate` 生成 `web/admin/src/api/meta.gen.ts`，
  见 [admin.md](admin.md) 的"字段文案与校验（meta.gen.ts）"），所以 `components.schemas` 里：
  每个属性写 `title`（中文名，表单标签、表头、详情页的字段名；`$ref` 的属性也写在 `$ref` 旁边）；
  每个枚举写 `x-enum-labels`，为每个取值写中文文案；金额字段名以 `_cents` 结尾并写
  `x-field: money`（整数，单位：分），用多行输入框的长文本写 `x-field: text`；密码写
  `format: password`（表单不去掉首尾空格）；请求体里的字符串放进 `required` 表示不能为空。缺了
  `go test ./tools/gen/...` 失败并写明位置和改法；
- 中间件链与前台相同，但不开 CORS（同源部署），带 CSP，指标的 subsystem 是
  `echo_starter_kit_admin`；`/admin/v1` 分组上先校验写请求的 `Origin` / `Sec-Fetch-Site`
  （会话用 cookie，防跨站请求伪造，必须等于 `HTTP_ADMIN_ORIGIN`），再执行 `AdminDeps.Auth`
  （会话与权限检查，由组合根的 `adminAuth()` 提供，可以有多个、按顺序执行），最后是 `AdminDeps.Middlewares`（如操作
  日志）。**有后台路由而 `Auth` 为空时 `NewAdmin` 报错**，不会把后台接口裸露出来；只挂了别的
  中间件不算鉴权。Origin 校验只是一层：不带 `Origin` 和 `Sec-Fetch-Site` 的写请求会放行，
  会话 cookie 还必须设 `SameSite`；
- 后台表格的列表用页码分页：`page` / `size` 参数（`components/parameters` 的 `Page`、
  `Size`），响应是 `PageTotal`（`total`、`total_capped`）加 `items`；handler 用
  `httpserver.PageRequest` 转成 `page.Request`（`internal/kit/page`）。总数最多数到 10000，超过时
  `total_capped` 为 true；只能翻到前 10000 条，更后面返回 400 `page_out_of_range`（大表上
  `count(*)` 和大 OFFSET 都要扫过前面的全部行）。数据量大、只需要往后翻的列表（如操作日志）
  仍可以用 cursor 分页；枚举多选的筛选参数是数组，写 `style: form`、`explode: false`（逗号分隔，
  如按状态筛选的 `status`），后台前端按同样的格式序列化；
- 编辑整份表单的接口带乐观锁（生成器的 `update<资源>`）：资源返回 `version`，修改请求必须带上读到的
  `version`，已经不是最新时返回 409 `<资源>_conflict`，并提供直接读库、不经过缓存的详情接口
  （`get<资源>`），冲突后前端用它取最新内容；表结构见 [database.md](database.md) 的"写
  Repository"；
- 员工身份用 `httpserver.StaffSessionFrom(c)` 读，和前台的 `UserSession` 是两套键和类型；
  会话、权限检查、操作日志的实现见 [auth.md](auth.md) 的“后台员工”；
- 可选托管后台前端的构建产物（`AdminDeps.Static`，组合根在 `internal/app/admin_ui.go` 接入，
  见 [admin.md](admin.md) 的“构建与部署”）：存在的文件原样返回，`assets/` 下缺失的文件 404，
  其余路径（前端路由）回退到 `index.html`；`assets/` 长期缓存，其余 `no-cache`；`/admin/v1`
  下不存在的路径仍是 JSON 404。这些静态文件不计入全局限流（`common.SkipRateLimit`），限流只管
  接口。

## 中间件顺序

全局链固定为：Recover → RequestID → BodyLimit → CORS → Secure → 全局限流 →
Prometheus 指标 → Timeout（`internal/platform/httpserver/server.go` 的 `newServer`，前台和
后台共用，后台没有 CORS）。鉴权是操作级中间件，由模块挂载；user 还给自己的操作挂了按 IP
的限流和只接受 JSON 请求体的中间件，见 [auth.md](auth.md)。

- Recover 必须在最外层：Echo v5 自己不恢复 panic，没有 Recover 时靠 net/http 在连接级
  兜底——连接直接断开、客户端拿不到响应，panic 只进 `http.Server.ErrorLog`（Echo 把它接到了
  应用日志，但不带 request_id，也不经过错误处理器）。Recover 让 panic 渲染成固定文案的
  500，panic 内容带着 request_id 进结构化日志。Timeout 用的是 `ContextTimeout`，不另起
  goroutine，handler 里的 panic 仍在 Recover 的调用栈上；
- CORS 必须在限流前面，否则预检请求会被拦。前台 CORS 开了 `AllowCredentials: true`
  （refresh token 按 Origin 经 cookie 交付，见 [auth.md](auth.md)），所以 `AllowOrigins`
  必须显式列出，不能用 `*`；`AllowHeaders` 只放行 `Authorization` 和 `Content-Type`，
  新增自定义请求头要同时加进这个白名单（`server.go` 的 `New`）；
- `IPExtractor` 必须按部署拓扑配置（`HTTP_TRUSTED_PROXY`），否则按 IP 的限流可以被
  伪造 header 绕过；
- 全局限流的计数存储由 `internal/platform/ratelimit` 提供：配置了 Redis 时
  所有实例共享额度（GCRA），没配时退回进程内存，实际额度是"配置值 × 实例数"。
  Redis 故障时放行（fail open）；
- BodyLimit 默认 2MB，Timeout 默认 10 秒。Timeout 只让客户端提前拿到响应，释放服务端
  资源要靠 ctx 一路透传到数据库调用；
- `/healthz`、`/readyz`、`/metrics` 跳过 CORS、限流和 Timeout。

## API 文档

前台 server 的 `GET /openapi.yaml` 返回嵌入的 spec 原文（`api/embed.go`），`/docs` 是 Redoc
渲染的文档页；后台 server 换成 `GET /admin.yaml` 和它自己的 `/docs`，两边只暴露各自的 spec。
`HTTP_API_DOCS_ENABLED=false` 两边都不注册。

- 文档 UI 只有 Redoc 一个。曾经并存过 Swagger UI（echo-swagger），但 swaggo/files
  上游最新版停在 v2.0.2（2024-12，捆绑 swagger-ui 5.18.2），而 swagger-ui 到
  v5.32.0（2026-02）才官方支持 OpenAPI 3.2，已移除；
- Redoc 用的是 v3，JS bundle 从 CDN 加载，版本锁定在
  `internal/platform/httpserver/api_docs.go` 的 `redocBundleURL`，升级要显式改那里，
  不跟浮动 tag；页面必须带 `disable-telemetry="true"`（v3 默认向 Redocly 上报匿名
  使用数据；必须显式写 `="true"`，裸写属性的空字符串值不会被当作 true）；
- `/docs` 的 CSP 只放这一条路由：script-src 只放行 CDN 域，不需要 `unsafe-inline`
  （v3 是外联 ESM 脚本），connect-src 'self' 只够它 fetch 同源的 spec；
- Redoc 走 CDN 意味着 CDN 不可用时 `/docs` 白屏，但 `/openapi.yaml` 原文仍可取。

## 测试

- 通用行为（错误渲染、校验错误格式、panic 恢复、健康检查、API 文档）：
  `internal/platform/httpserver/server_test.go`；后台 server（Origin 校验、分组中间件、
  文档页、前端托管）：`internal/platform/httpserver/admin_test.go`；
- 通用请求校验、业务错误渲染、请求体过大：`internal/app/http_test.go`，用真实中间件链，
  只把 Repository 换成 `domaintest` 的 fake；
- 中间件自己的单测放在定义它的模块里（`internal/modules/user/httpapi/middleware_test.go`）。
