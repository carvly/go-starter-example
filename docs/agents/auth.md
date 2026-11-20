# 鉴权

事实来源：`internal/modules/user/`、`internal/modules/staff/`、`internal/platform/crypto/`、`internal/platform/oauth/`、`internal/platform/httpserver/identity.go`

改 `internal/modules/user/`、`internal/modules/staff/`、`internal/platform/crypto/`、
`internal/platform/oauth/` 或给接口加鉴权之前读这篇。

## 前台用户（user 模块）

`internal/modules/user/` 管前台用户：账号的后台管理、登录（微信四个入口，按需启用）、
会话、重新验证、绑定/解绑、注销，以及供其它模块挂载的前台鉴权中间件。

- **用户的后台管理**：`users` 表和后台的列表、详情、停用 / 启用（`user:read`、
  `user:disable`、`user:enable`）由生成器生成（描述文件存为夹具
  `tools/gen/testdata/fixture_user.yaml`，**已冻结**：登录代码是生成器管理范围之外的
  手写代码，不再对 user 模块重新 `gen apply`，见 [generator.md](generator.md)）。生成之后
  手写加了用户详情页的登录方式列表（`listUserIdentities`）、会话列表（`listUserSessions`，
  都是 `user:read`）和踢下线（`kickUserSession`，`user:kick`）。账号只能经登录创建，
  后台不能新建或编辑。停用
  （`Repository.Disable`）在同一条语句里改状态并吊销该用户全部未吊销的会话
  （`user_sessions`），会话校验每次都查库，停用立即生效。
- **登录方式**：`user_identities` 表，`(provider, subject)` 唯一定位，每种 provider
  每个账号最多一个，绑定新的就替换旧的。现在只有微信（`subject` 是 unionid）；
  邮箱/手机号只能由验证码流程创建，第三方声明的邮箱不落库也不成为登录方式。微信各入口的 openid 单独存 `wechat_openids`（支付、模板消息用），
  换绑成另一个 unionid 时先删该账号全部 openid 再写入。
- **登录入口**：网站应用扫码、服务号网页授权走浏览器 OAuth 流程
  （`internal/platform/oauth/` 的 `Provider`：一次性 state + 浏览器绑定 cookie 防
  登录 CSRF，登录多一步兑换）；小程序 `wx.login`、App SDK 走原生接口
  （`Native`）。第三方的 code 换身份在事务之外同步完成；"解析账号"的安全规则
  写在 `domain` 的 `ResolveLogin`/`ResolveBind` 里，所有入口只能经过它。
- **通用链路**：流程按 provider 的能力声明工作，新增 provider 不改流程、表结构和 spec。
  - 浏览器：`Provider.Capabilities()` 返回 `BrowserCapabilities{PKCE, Nonce}`。声明了的，发起时由
    `domain` 的 `StartOAuthFlow` 生成 verifier（43 个字符）和 nonce，随 state 行存进 `oauth_states`
    的 payload；授权链接带 `oauth.S256Challenge(verifier)` 和 nonce；回调一次性取出 state 行，把
    verifier、nonce 和回调的全部参数（`ExchangeParams.Callback`）交给 `Exchange`。重新验证时
    `AuthorizeParams.Reauthenticate` 为真，provider 自行加 `prompt` 之类的参数。provider 专有的回调参数
    （如微信快照页的 `is_snapshotuser`）由 provider 自己处理，流程只把哨兵错误映射成结果码。
  - 回调同时接受 GET 和表单 POST（`response_mode=form_post`），两者走同一个 `handleOAuthCallback`。
  - 原生：`Native.Capabilities()` 返回 `NativeCapabilities{Nonce}`。声明了 nonce 的，客户端先调
    `POST /v1/auth/native/{provider}/nonce`（`oauth_states` 的 `kind=nonce`，绑定 provider，5 分钟），
    再随凭证提交；流程一次性核销后交给 provider 比对 id_token，截获的 id_token 因此无法重放。原生请求
    体的 `code`、`id_token`、`nonce`、`name` 都是可选的，缺了 provider 需要的字段返回
    `oauth.ErrMissingCredential`；所有原生入口都经 `httpapi` 的 `verifyNative`。
  - 校验失败对客户端一律是登录失败；`oauth.ErrNoUnionID`（应用没绑定开放平台）按错误级别记录，便于报警。
- **入口按需启用**：四个入口是组合关系，产品用到哪个就配哪个应用的凭据。
  `WECHAT_<WEB|MP|MINI|APP>_APP_ID` 和 `_SECRET` 成对配置才启用
  （`WeChatAppCredentials.Enabled`），都留空就不启用，只配一半启动失败。`providers.go` 的
  `wechatRegistry` 只登记启用的入口；请求未启用的入口（登录、绑定、重新验证）和未知入口
  一样返回 400。注册表的装配在 `providers.go`。
- **会话**：每次登录建一行 `user_sessions`。access token 是 HS256 的 JWT，header 带
  `kid`，claims 只有 iss/aud/sub/sid/iat/exp，有效期 `USER_ACCESS_TOKEN_TTL`（默认
  15 分钟）；**每个请求按 `sid` 查会话行**（带 `users.status`）：吊销、过期、空闲超时、
  账号停用立即生效，查询失败也拒绝，不加缓存。refresh token 按 `refresh_family` 定位，
  轮换后上一代在 `USER_REFRESH_GRACE_PERIOD`（默认 10 秒）内仍可用；`refresh_family`
  对得上、随机串既不是当前值也不是宽限期内的上一代，而会话仍然有效时，视为泄露、吊销
  整个会话（`domain/session.go` 的 `Refresh`）；客户端必须串行刷新。refresh token 按
  请求 `Origin` 交付：名单内（`USER_COOKIE_DELIVERY_ORIGINS`）只进 httpOnly +
  SameSite=Strict cookie（Path `/v1/auth/session`），名单外（App、小程序）走响应体。
- **登录接口的防护**：`/v1/auth/*` 的操作（发起授权、回调、兑换、原生登录、签发原生 nonce、刷新、登出）在
  全局限流之外再按 IP 限流，每秒 5 次（`module.go` 的常量 `authRatePerSecond`，不可配置），配了
  Redis 时多实例共享计数；带请求体的操作只接受 `application/json`，表单等其它类型返回
  415（`httpapi/json_only.go`，挂在哪些操作上见 `httpapi/routes.go`），跨站页面不触发
  预检的表单请求到不了 handler。唯一的例外是表单 POST 回调（`oauthCallbackForm`）：第三方本来就用跨站
  表单回调，防护由一次性 state 和绑定 cookie 承担——绑定 cookie 是 `SameSite=None; Secure`，只在发起
  授权的浏览器里有，跨站表单带不来别人的。
- **必需配置**：`USER_PUBLIC_BASE_URL` 是浏览器 OAuth 流程 redirect_uri 的 origin，必须是
  浏览器会发出的规范 origin（小写 `scheme://host[:port]`，不带默认端口、路径和用户信息），
  `USER_COOKIE_DELIVERY_ORIGINS` 的每一项同样要求，不规范时启动失败。`WECHAT_*` 不是
  必需项，见上面的"入口按需启用"（校验在 `internal/modules/user/config.go` 的 `LoadConfig`）。
- **我的会话、我的登录方式**：`GET /v1/me/sessions`（只列未吊销的）和
  `GET /v1/me/identities` 按 cursor 分页，按创建时间倒序（`PageByUser`）；后台用户详情页
  的两个列表和账号内部逻辑用全量的 `ListByUser`。
- **重新验证**：† 操作要求 `auth_time` 在 `USER_REAUTH_WINDOW`（默认 10 分钟）内，
  由 `RequireRecentAuth` 中间件检查（见"给操作加鉴权"）。现在账号只有微信登录
  方式，重新验证只能走微信（小程序入口静默完成，客户端在注销/换绑前自己弹确认框）。
- **注销**：`DELETE /v1/me`（†）在一个事务里置 `status=deleted`、清空昵称头像、
  吊销全部会话、删除全部登录方式和 openid，并发布 `user_deleted` 事件
  （worker 在 user 的 `jobs/` 里，清理器在组合根通过 `userM.AddDataCleaner(...)` 登记，
  见 [queue.md](queue.md)）。`users.last_login_at` 在每次登录时更新，`users.signup_client`
  记录建号时的客户端；后台列表可以按最近登录时间排序和筛选。
- **过期行清理**：`user_cleanup` 定时任务（user 的 `PeriodicJobs()`，每小时 +
  启动时一次）删除 `oauth_states` 过期行、`user_sessions` 过期行和吊销超过 7 天的行。
- **换绑是账号所有权的变更**：同一种 provider 换成新的 subject 时，吊销用旧登录方式建立的其它
  会话，当前会话的 `login_identity_id` 改指向新的登录方式（`ResolveBind`）。以后即使改成原地更新
  `subject`，也必须吊销。同一个 unionid 从别的入口进来只 upsert 本入口的 openid；换成另一个
  unionid 才清空全部 openid。
- **解绑**：不能解绑最后一种登录方式（`ErrLastIdentity`）；解绑时吊销用它登录的其它会话。以后加
  密码时，解绑最后一个邮箱或手机号要同时清空密码（密码只能配合邮箱或手机号使用）。
- **重新验证必须属于当前账号**：第三方返回的 `(provider, subject)` 不属于当前账号就拒绝
  （`ErrReauthWrongAccount`）。第三方的重新验证强度弱（已授权过的会自动同意），以后有了密码或
  验证码，第三方重新验证只作为没有其它凭据的账号的兜底。
- **建号时的昵称和头像**：取第三方的昵称，为空时（小程序入口）用"微信用户"；昵称按字符截到 64，
  头像超过 1024 不存；原始值照常进 `profile`。
- **`return_to` 和 `redirect_uri`**：`return_to` 只接受以单个 `/` 开头的站内路径，整串不能有
  反斜杠和控制字符，解析后不能有 scheme 和 host（`ValidReturnTo`；浏览器会去掉制表符、把 `\` 当 `/`，只看前缀
  挡不住 `/<制表符>/evil.com`）；`redirect_uri` 由 `USER_PUBLIC_BASE_URL` 加固定路径拼出，和平台侧登记的值精确
  匹配。
- **为什么每个请求查库还保留 JWT + refresh 两层**：web 的长期凭据只在 httpOnly cookie 里；refresh
  轮换能发现盗用；签名先挡掉伪造的 token，不必为它们查库。泄露检测按 `refresh_family` 定位，因为
  `sid` 在 access token 里可读，按 `sid` 定位就能被用来踢别人下线。
- **客户端串行刷新**：web 用 Web Locks API，App、小程序在进程内做 single-flight。
- **不做的事**：不按邮箱自动合并账号，第三方声明的邮箱永远不成为 email 登录方式（攻击者可以用
  别人的邮箱抢先建号）；不做新设备登录通知；不保存第三方的 token；不做 App 内的网页授权。

## 微信平台要点

这些是微信平台的行为，代码里看不出来，接入和上线前逐条核对：

- **unionid 的前提**：启用的应用（网站应用、服务号、小程序、移动应用）都要绑定在**同一个**开放平台
  账号下，上线前完成；开放平台要先做开发者资质认证（付费）。网站应用和移动应用直接建在这个账号下；
  小程序在微信开发者平台"我的业务 - 开放平台 - 绑定关系"里绑定（不是"小程序 - 绑定关系"）；服务号在
  开放平台"管理中心 - 公众号"里绑定。拿不到 unionid 时登录失败（`oauth.ErrNoUnionID`），不退化成用
  openid。
- **服务号网页授权**：只有已认证的服务号能用；scope 必须是 `snsapi_userinfo`（`snsapi_base` 不返回
  unionid）；授权必须由用户点击触发，否则落进网页快照页，快照页的虚拟账号（`is_snapshotuser=1`）一律
  拒绝。官方文档把这个标记列为换 token 接口（`sns/oauth2/access_token`）的返回字段，userinfo 不一定
  带，所以换完 token、userinfo 返回、回调参数三处都检查；`state` 只能是 `[a-zA-Z0-9]`、不超过 128 字节，所以用 hex；授权链接的参数按官方顺序拼
  （现在 `url.Values.Encode` 按字母排序恰好一致，加参数时要核对）。
- **小程序**：`code2session` 返回 40226 是高风险用户被拦截，按登录失败处理；`session_key` 不下发也不
  保存；`wx.getUserProfile` 已回收，昵称头像由用户用"头像昵称填写能力"提交 `PATCH /v1/me`。
- **移动应用**：未上架的应用每天只能登录 100 次（10060），各平台要分别设为已上架。iOS App 要和
  Apple 登录同期上线（App Store 审核指南 4.8），所以现在只做 Android。
- **协议不标准**：出错时 HTTP 200 + `errcode`，所以手写实现，不用聚合多家登录的库。
- 部署相关的约束（IP 白名单、回调域名）见 [deploy-ci.md](deploy-ci.md) 的"前台登录的部署约束"。

## 接入新的登录方式

表结构、通用链路、"解析账号"、会话、解绑规则都不用改：

1. 在 `internal/platform/oauth/` 实现 `Provider`（浏览器跳转）或 `Native`（原生 SDK）：声明
   `Capabilities()`，在 `AuthorizeURL` 里放进 challenge、nonce 和自己专有的参数（如 `response_mode`），
   在 `Exchange` / `VerifyNative` 里校验（OIDC 的 id_token 签名、iss/aud/exp/nonce 由 provider 校验），
   从 `ExchangeParams.Callback` 读自己专有的回调字段；带用 httptest 模拟对方响应的测试；
2. `internal/modules/user/config.go` 加凭据和校验（按"能力和启用分开"：没配就不启用）；
3. 在 user 的 `providers.go` 的注册表里登记；
4. 在平台侧配置回调地址（指向 API 域名）；
5. `api/admin.yaml` 的登录方式枚举（已预留 email、phone、github、google、apple）补上显示名，
   `make generate`。

手机号、邮箱、密码登录和 GitHub、Google、Apple 等 provider 还没有实现。

## 业务怎么扩展用户

业务自己的用户资料放在业务模块里，按 `user_id` 关联，不建外键，需要时惰性初始化；
不要修改 user 的表或 Repository。user 提供两个组合根扩展点：

- `Service.GetPublicProfiles(ctx, ids) (map[string]PublicProfile, error)` 批量返回
  `id`、`display_name`、`avatar_url`、`status`，不存在的 ID 不在结果里；业务列表不要逐行查用户；
- `Service.WeChatOpenID(ctx, userID, appid) (string, error)` 查询指定微信应用下的 openid，
  供支付和订阅消息使用；不存在时返回 `domain.ErrNotFound`。

持有用户数据的模块还要同时提供两种幂等清理：

1. 实现 `user.DataCleaner`，全部模块构造完成后在 `internal/app/modules.go`
   用独立一行 `userM.AddDataCleaner(memberM.UserDataCleaner())` 登记，`user_deleted`
   worker 会即时调用；登记行不是构造依赖，删除任一模块时一起删除；
2. 提供每天一次的清扫任务，按本模块表里的 `user_id` 分批，经组合根适配器调用
   `GetPublicProfiles`，删除状态为 `deleted` 或结果里不存在的用户数据。它补偿滚动部署期间
   旧 worker 不认识新清理器、以及清理重试耗尽造成的遗漏；这个查询适配器让业务模块真实依赖
   user，所以删除 user 时该业务模块也要删除。

不推送注册、停用、启用事件：停用已由每个请求的会话查询实时生效，注销是终态。

## 前台鉴权：两种模式，二选一

- **默认：会话中间件**（`internal/modules/user/httpapi/middleware.go` 的
  `NewSessionMiddleware`）：验签（HS256 + `kid` 轮换，旧密钥放 `JWT_ADDITIONAL_KEYS`）
  → 校验 iss/aud/exp → 按 `sid` 查会话。吊销立即生效，† 操作拿得到 `auth_time`；
- **网关信任 header**（`trusted_identity_middleware.go`）：部署在已经校验过身份的
  网关后面时用。这个模式下 `UserSession` 只有 `UserID`，没有服务端会话：依赖会话的操作（† 操作、绑定、
  重新验证）一律返回 403 `session_unavailable`（fail closed），不返回 `recent_auth_required`，客户端不会
  陷入"去重新验证、再失败"的循环。中间件本身不做任何签名校验，
  前提是客户端无法绕过网关直连服务、网关会清掉客户端带来的同名 header。配错
  不会报错，只会让鉴权悄悄失效。

由配置切换：`USER_TRUSTED_IDENTITY_HEADER` 留空用会话中间件，填上网关透传的 header 名（如
`X-User-ID`）用信任 header 模式，选择逻辑在 `internal/modules/user/module.go` 的 `Middleware()`。两种实现都把
身份写进 `httpserver.SetUserSession`，handler 用 `httpserver.UserSessionFrom(c)` 读，
不需要跟着改。请求上下文里的身份按主体分类型（前台用户 `UserSession`，后台员工
`StaffSession`），一个 handler 读错了类型只会得到 401，不会把另一类主体的 ID 当成
用户 ID。

## 给操作加鉴权

前台接口：模块的 `Deps` 收一个 `Auth echo.MiddlewareFunc`，由组合根传入
`userM.Middleware()`（这样模块在 `modules.go` 里依赖 user，删除演练会连带处理）；在
`httpapi/routes.go` 的 `OperationMiddlewares` 里按 operationId 挂上，spec 里对应操作去掉
`security: []`。敏感操作（改绑登录方式、注销等，接口文档标 †）在 Auth 之后再挂
`mw.RequireRecentAuth`，由组合根传入 `userM.RequireRecentAuth()`（窗口是
`USER_REAUTH_WINDOW`）：

```go
// internal/modules/<m>/httpapi/routes.go
func Register(g *echo.Group, h *Handler, mw Middlewares) {
	oapi.RegisterHandlersWithOptions(g, h, oapi.RegisterHandlersOptions{
		OperationMiddlewares: map[string][]echo.MiddlewareFunc{
			"createThing":     {mw.Auth},
			"dangerousThing":  {mw.Auth, mw.RequireRecentAuth},
		},
	})
}
```

生成器的 `web.auth` 只生成 `{mw.Auth}` 这一种挂法，不挂 `RequireRecentAuth`
（`REPO-AUTH-MISSING` 检查有且只有一个模块提供 `Middleware()`）。

仓库里目前没有其它前台模块挂它：管理类操作（新建、修改、调整业务数据）放在后台接入面，由员工
的会话和权限码保护（见下文"后台员工"），不是"登录用户即管理员"。后台操作不需要上面这些，
写 `x-permission` 即可。

## 必须保持的安全性质

- 密码哈希是 Argon2id（`alexedwards/argon2id`，`internal/platform/crypto`），参数从配置
  （`ARGON2_*`）显式传入，不依赖库默认值，按部署环境内存和登录 QPS 压测调整。现在只有后台
  员工的密码用它，前台加密码登录时复用同一组参数；
- 停用前台用户必须同时吊销会话，并且两者原子完成；注销、踢下线同理；
- **吊销立即生效**：前台鉴权每个请求按 `sid` 查 `user_sessions`，不做 access token 主动
  吊销，也不缓存会话查询结果（缓存的"写后删"窗口会让吊销不成立）；查询失败必须拒绝
  （fail closed）；
- refresh token 只存随机串部分的摘要；family 对得上而随机串不对、会话仍有效时视为泄露，吊销
  整个会话；
  按 `refresh_family` 定位，只知道 `sid` 吊销不了别人的会话；
- 浏览器授权流程的一次性 state + 绑定 cookie 防登录 CSRF：没有绑定 cookie、或 cookie 摘要
  和流程行里登记的不一致，直接拒绝；
- 第三方登录失败（unionid 缺失、快照页、40226 等）对客户端统一返回"登录失败"，细节只进日志。

## 后台员工（staff 模块）

后台账号和前台用户是两套独立体系：账号只能由超级管理员（或 CLI）创建，用户名 + 密码登录，
服务端会话 cookie，没有 JWT 和 refresh。实现在 `internal/modules/staff/`，接口在
`api/admin.yaml`（tag `staff`）。

为什么不复用前台的登录体系：后台可能只有一两个人使用，不能依赖邮件、短信、企业 IdP 或第三方登录
（邮箱被盗就等于后台被盗）；后台只有一个同源的 web 客户端，用服务端会话 cookie 即可，不需要为 App
和小程序设计的 JWT + refresh。账号、会话、角色权限放在同一个 staff 模块：每个请求都要同时用到会话、
账号状态和权限，拆成两个模块就不能 JOIN（表归属规则），还要经适配器互相调用。

- **首个超级管理员**用 CLI 创建：`api staff create -username root`，输出临时密码和它的过期
  时间（`STAFF_TEMP_PASSWORD_TTL`，默认 24 小时），首次登录后必须改密；
  `api staff reset-password -username root` 是所有超级管理员都进不来时的恢复手段（在一个事务里
  重设密码、启用账号、吊销全部会话）。CLI 操作记操作日志，操作人为空；
- **会话**：登录成功下发 `__Host-admin_session`（HttpOnly、Secure、SameSite=Strict、Path=/），
  库里只存 token 的 sha256 摘要。每个请求用一条查询取出会话和账号状态，不缓存：吊销、停用、
  空闲超时（`STAFF_SESSION_IDLE_TIMEOUT`，默认 30 分钟）、绝对过期（`STAFF_SESSION_MAX_AGE`，
  默认 12 小时）立即生效，查询失败也拒绝。改密吊销其它会话，重设密码和停用吊销全部会话；
- **登录**：用户名不存在、密码错误、账号停用返回同一个 401 `invalid_credentials`，不存在时也
  做一次哑比较，响应时间不暴露账号是否存在。退避按"用户名摘要 + IP"计数，不管账号是否存在：
  同一用户名在同一 IP 连续失败 5 次开始等待，从 30 秒起逐次翻倍，最长 15 分钟，等待期内返回
  429 `login_throttled`（带 `retry_after`），也做一次哑比较。不存在的用户名一样进入等待，所以
  单独返回等待不会暴露账号是否存在；以后改退避逻辑时这一点不能丢。只数这个 IP 上次登录成功和
  上次设置密码之后的失败（别的 IP 登录成功不清零，重设密码清零），计数从 `staff_audit_logs`
  的 `login_key`（规范化用户名的 sha256，不存原文）里数，不另建表；不做硬锁定。存在的账号的
  登录在账号行锁上排队，并发请求绕不过阈值。后台登录页按情况提示（`web/admin/src/lib/server-error.ts`）：
  "用户名或密码不正确"，或"用户名或密码连续输错多次，请 N 分钟后再试"；
- **鉴权中间件**（`internal/modules/staff/adminapi/guard.go`，经组合根 `adminAuth()` 注入后台
  server）：按"请求方法 + 路由模板"查 `x-permission`（组合根从内嵌的 `admin.yaml` 解析，
  `internal/app/admin_spec.go`）→ 路径不存在 404、查不到映射拒绝 → `public` 放行 → 会话校验 →
  必须改密时只放行改密、登出、查看自己 → 权限检查（`authenticated` 放行，`superadmin` 只放行
  超级管理员，权限码每次查库，超级管理员短路）→ 为写操作记日志（方法 + 路由模板、路径参数、
  状态码、IP、request_id，不记请求体）：会话校验通过之后被拒绝的写请求也记，未登录的不记。
  路径存在但方法不对返回 405；
- **权限码**由代码定义（`admin.yaml` 的 `x-permission`），角色在运行时配置，只存权限码。权限码
  每次查库、不缓存：和会话一样，收回角色立即生效；Redis 是可选的，权限依赖缓存会让没配 Redis 或
  Redis 故障时整个后台不可用。超级管理员短路、员工和角色只有超级管理员能管理，不采用"只能授予自己
  已有的权限"：目标是个人或小团队，不下放管理权限，这样不存在普通员工给自己加权限的路径；
  spec 里删掉的权限码在检查和展示时忽略。员工和角色只有超级管理员能管理；超级管理员不能停用
  自己、不能取消自己的标记；停用或取消超级管理员时在锁里检查至少留下一个 active 的超级管理员
  （`Repository.LockActiveSuperAdmins`），两个超级管理员同时互相停用也不会全部失效。员工列表
  第一页返回的 `active_super_admins`（`CountActiveSuperAdmins`，不加锁）只用于页面提示，
  不能用来做这类检查；
- **操作日志**按保留期（`STAFF_AUDIT_RETENTION`，默认 180 天）由每天一次的定时任务删除。日志的
  `target` 可能是前台用户 ID，但不含其它个人信息，且按保留期删除，所以 staff 不登记注销清理端口。
  操作日志直接写库、不经 river：入队本身也是一次 INSERT，代价和直接写差不多，还多一次 worker 写入。

### 离职交接与应急恢复

系统的最终权限在能执行 CLI 的人（有部署环境和数据库权限）手里，不在后台的某个账号上。后台的
"至少留下一个 active 超级管理员"只能防止超级管理员一个都不剩，防不了"剩下的那个人走了"，
这一层靠流程：

- 平时至少保持两个 active 的超级管理员；
- 员工离职时，由另一个超级管理员先停用他的账号（停用会立即吊销全部会话），再交接；员工不能
  删除，操作日志和 `created_by` 仍然指向他；
- 唯一的超级管理员已经离开、或所有超级管理员都进不来时，运维执行
  `api staff reset-password -username <留任的员工>`（容器里是
  `docker compose run --rm api staff reset-password -username <留任的员工>`）：重设密码、
  启用账号、吊销旧会话（这个命令不改变超级管理员标记，所以要对一个超级管理员账号执行，比如
  之前被停用的另一个超级管理员）；留任的人里一个超级管理员都没有时，用
  `api staff create -username <新登录名>` 新建一个。登录改密后，停用离职者的账号；
- CLI 操作记入操作日志（动作 `cli:staff reset-password`，操作人为空），事后可查。
