# 后台前端：web/admin

事实来源：`web/admin/`、`api/admin.yaml`、`internal/platform/httpserver/admin.go`、`internal/app/admin_ui.go`、`internal/app/admin_spec.go`

改 `web/admin/` 或 `api/admin.yaml` 之前读这篇。后台接口、会话和权限见 [http.md](http.md) 的
“后台接入面”和 [auth.md](auth.md) 的“后台员工”。

## 工具链

`web/admin/` 是独立的 pnpm 项目（仓库根目录没有 `package.json`），工具链：Node 24、pnpm（`packageManager` 固定版本，`pnpm-workspace.yaml` 里
`minimumReleaseAge: 1440`、`allowBuilds` 白名单）、TypeScript 7、Vite 8、React 19、
Tailwind CSS 4、shadcn/ui 的 Base UI 风格（`components.json` 的 `style` 为 `base-vega`）、
TanStack Router / Query / Table（v9）/ Form、zod、openapi-fetch，检查用 oxlint、oxfmt、knip，
测试用 vitest + happy-dom 和 Playwright。

```bash
cd web/admin
pnpm install
pnpm dev            # 开发服务器，/admin/v1 代理到 ADMIN_SERVER（默认 http://127.0.0.1:8081）
pnpm gen:api        # 改了 api/admin.yaml 之后重新生成 src/api/schema.d.ts（meta.gen.ts 由仓库根目录的 make generate 生成）
pnpm typecheck && pnpm lint && pnpm format:check && pnpm test && pnpm check:unused
pnpm build          # 输出到 dist/，重新编译 api 后由后台 server 托管；先跑 vite（顺带重新生成路由树）再做类型检查
pnpm e2e            # 端到端测试，见下文“测试”
```

## API 客户端

- `pnpm gen:api`（`web/admin/scripts/gen-api.ts`）用 openapi-typescript 从 `api/admin.yaml`
  生成 `src/api/schema.d.ts`，只有类型，提交入库；请求用 openapi-fetch（`src/api/client.ts`），
  query hooks 手写。只生成类型而不是每个接口一个请求函数：运行时只有 openapi-fetch 一个很小的库，
  不随接口数量增长，spec 的改动也不会带来成片的生成代码 diff。数组查询参数按逗号分隔序列化（`?status=pending,failed`），和 `admin.yaml`
  里的 `explode: false` 一致；spec 里的数组查询参数都要这样写。
- openapi-typescript 依赖 TypeScript 5 的编译器 API，在 TS 7 下跑不起来，所以生成脚本用
  `pnpm dlx --package=openapi-typescript@<版本> --package=typescript@5.x` 单独运行，项目本身
  仍用 TS 7。两个版本号写在 `gen-api.ts` 顶部，升级时一起改。

## 字段文案与校验（meta.gen.ts）

`src/api/meta.gen.ts` 由 `tools/gen/meta` 从 `api/admin.yaml` 生成（`make generate`，指令在
`api/embed.go`），提交入库，不手改。`go test ./tools/gen/...`（含在 `go test ./...` 里）检查它和
spec 一致，并检查 spec 符合下面依赖的约定（见 [http.md](http.md) 的"后台接入面"）。改字段的
文案、约束、枚举文案只改 spec，页面不用跟着改。

导出名是 lowerCamel：`<schema>` 和 `<operationId>` 的首字母小写再接 `Meta`，如 `userMeta`、
`orderStatusMeta`、`listUsersMeta`。

| 导出 | 来自 | 用法 |
|---|---|---|
| `<schema>Meta.fields.<字段>` | 每个 schema 的属性：`title`，`min` / `max`（字符数或数值），枚举的 `labels` | 表单的 `label`、表头、详情页的标签、徽标和筛选项的文案、说明文字里的数字（如"至少 12 个字符"） |
| `<枚举 schema>Meta.values` / `.labels` | 独立定义的枚举（如 `OrderStatus`）的取值和 `x-enum-labels` | `EnumFilter` 的选项、`EnumCell` 的文案 |
| `<请求体 schema>Meta.form` | 作为请求体的 schema 的约束 | 表单的 zod schema：`validators.onSubmit` 直接用，`parse` 的结果就是请求体 |
| `<operationId>Meta.search` / `.pageSize` | 带查询参数的 GET 操作 | 路由的 `validateSearch`；`pageSize` 是 `size` 参数的默认值，查询和 `PagePager` 都用它 |

- 表单的 key 就是请求体的字段名（如金额字段叫 `price_cents`，表单里的值是元的字符串，`parse`
  后是分）。spec 表达不了的规则在 feature 里补：对 `form` 用 `.extend()`（如用户名的格式）或
  `.refine()`（如增减量不能为 0、两次输入的新密码一致），不改生成的文件。
- `search` 里每页条数和 cursor 不进 URL；时间范围（`date-time` 的查询参数）在 URL 里是日期，
  请求前用 `dayRange` 换算。
- 生成器只支持 spec 里实际用到的写法，遇到不支持的（如请求体里的日期字段）报错，在
  `tools/gen/meta` 里补上对应的规则和测试，以及 `src/lib/field-schemas.ts`、`src/lib/search.ts`
  里的零件。

## 本地开发

1. 起后端，打开后台 server：`HTTP_ADMIN_ADDR=127.0.0.1:8081`、
   `HTTP_ADMIN_ORIGIN=http://localhost:5173`。前台登录的 `USER_PUBLIC_BASE_URL`（如
   `http://localhost:8080`）是必需项，缺了 api 起不来；`WECHAT_*` 只开发后台时不用配
   （变量见 `configs/.env.example`，compose 里已经给了默认值）。首个管理员用
   `api staff create -username root` 创建；
2. `cd web/admin && pnpm dev`，浏览器打开 `http://localhost:5173`。用 `localhost` 而不是
   `127.0.0.1`：写请求的 `Origin` 必须等于 `HTTP_ADMIN_ORIGIN`。

浏览器只访问 Vite，`/admin/v1` 由 Vite 代理到后台 server，同源，不需要 CORS；代理原样转发
`Set-Cookie`。会话 cookie 是 `__Host-admin_session`，`__Host-` 前缀要求 `Secure`，各浏览器
在 `http://localhost` 上的表现（Playwright 1.64 实测，2026-11-08）：

| 浏览器 | 结果 |
|---|---|
| Chromium | 接受，后续请求带上 cookie；`document.cookie` 读不到（HttpOnly） |
| Firefox | 同上 |
| WebKit（Safari 的内核） | **不接受**：登录返回 200，但 cookie 没有保存，下一个请求 401 |

所以本地开发用 Chrome 或 Firefox。Safari 只在 HTTPS 下能用（生产环境本来就是 HTTPS）；
需要在本地用 Safari 调试时，给 Vite 配 HTTPS 证书，并把 `HTTP_ADMIN_ORIGIN` 改成
`https://localhost:5173`。

## 目录

```
web/admin/src/
  routes/          TanStack Router 文件路由，只做路由声明；_authed.tsx 是需要登录的布局
  features/<功能>/  页面、zod schema、query hooks（auth（员工登录）、staff、roles、audit-log、settings、
                   accounts、roles…）
  components/      公共组件，按字段类型（generator.md 的"字段类型矩阵"）各提供表单字段、单元格、筛选控件：
                   form/（表单入口和字段）、data-table/（服务端分页表格、cells.tsx 单元格）、
                   filters/（筛选控件）、overlay/（表单对话框、详情列表）、layout/（布局、
                   侧边栏）、confirm-button.tsx（危险操作的确认框）、date-range-picker.tsx
  components/ui/   shadcn CLI 生成，不手改
  hooks/           shadcn CLI 生成，不手改
  lib/             与界面无关的逻辑：权限判断、错误映射、日期换算、金额换算、表单字段的 zod
                   schema、URL search 的校验、User-Agent 展示
  api/             schema.d.ts、meta.gen.ts（生成）、client.ts、errors.ts
```

`src/features/**` 和 `src/routes/**` 不能直接 import `@base-ui/react`、`@tanstack/react-table`、
`@tanstack/react-form`、`react-day-picker`，只能经 `src/components/` 使用（oxlint 的
`no-restricted-imports`），外观和行为才统一。页面不设最大宽度，随侧边栏展开、收起占满内容区；内容区（`SidebarInset`）带 `min-w-0`，
比内容区宽的表格在表格自己的容器里横向滚动，不把整页撑宽。

## Feature 与状态边界

- 业务代码按 `web/admin/src/features/<feature>/` 和对应路由组织，页面、查询、表单和测试随 Feature 内聚；
- Feature 之间不深入对方内部目录引用组件、Hooks 或状态。Feature 确实需要对外提供能力时才建公开出口
  `features/<名字>/index.ts`（如 `features/roles/index.ts`、`features/staff/index.ts`），其它 Feature 只能
  `import ... from "@/features/<名字>"`；没有外部使用者的 Feature 不建。`routes/` 是页面组合点，可以引用
  Feature 的页面和路由所需查询，不属于跨 Feature 的业务复用；
- 当前员工、权限码、登出是后台外壳的会话契约，放在 `src/lib/session.ts`，布局、路由守卫和各 Feature
  都从这里读；
- 跨 Feature 复用先判断是否具有稳定的通用语义。“出现三次”是检查重复的信号，不是机械门槛；
  `components/`、`lib/` 等通用目录不得反向 import `features/`；
- 基础 UI 组件只负责展示和交互事件，不直接发起业务 API 请求或读取业务全局状态。Feature 组件可以承担
  页面组合和少量局部交互，不为了形式把所有逻辑都抽成 Hook；
- 数据查询、缓存失效和复杂异步流程集中在 Feature 的查询模块或 Custom Hooks 中。表单校验和局部状态靠近
  使用位置，只有真实复用或复杂度需要时才提取；不按机械行数制造细碎组件；
- 服务端状态由 TanStack Query 管理，URL 表达可导航、可分享的界面状态，局部状态就近使用 `useState`，
  只有跨路由、跨页面且确属客户端会话的状态才考虑全局 Store；
- TypeScript 类型从 OpenAPI 生成，不手工维护重复 DTO。不用 `any` 绕过检查，外部输入优先用 `unknown`
  加运行时校验，类型断言必须能说明依据；
- 兼容性按操作语义判断，不是新增字段一律可选：请求新增必需字段通常破坏兼容；响应新增字段要考虑
  客户端处理；删除、重命名、改变含义、收紧校验、修改权限和错误码都要显式评估。

两条依赖方向由 oxlint 的 `no-restricted-imports` 检查（`.oxlintrc.json`）：`src/features/**` 不能引用
`@/features/*/*`（只能经公开出口），`src/components/**`、`src/lib/**` 不能引用 `@/features`。

## 页面、权限与菜单

数据库里没有菜单和权限定义：权限码只有 `admin.yaml` 的 `x-permission` 一个来源，菜单写在前端路由里，
都随代码发布、可以 review，不会出现数据库配置和代码不一致；角色只存权限码字符串。

- 页面路由用 `page({ permission, nav })`（`src/lib/guard.ts`，后台外壳所有）生成 `staticData` 和
  `beforeLoad`：权限只写一处，路由守卫和侧边栏都从这里取。`permission` 取值同
  `x-permission`：不写或 `authenticated` 登录即可，`superadmin` 只有超级管理员，其余是权限码。
- 侧边栏从路由树收集带 `nav` 的路由，按当前员工的权限过滤、按 `group` / `order` 排列（分组的
  先后固定为常用、业务、系统，见 `permissions.ts` 的 `groupOrder`）
  （`src/lib/permissions.ts` 的 `visibleNav`）。新增页面只在路由上写 `nav`，不改共享的菜单数据。
- 当前员工和权限码只来自 `meQuery`、`permissionsQuery`（`src/lib/session.ts`，后台外壳的会话契约），不用
  全局状态库。登录时清空全部缓存，换身份不会看到上一个人的数据。
- 员工和角色之间的引导：员工列表的"角色"列和顶部提示标出没有角色的启用员工（`needsRole`，
  接口的员工列表带 `role_ids`）和没勾选权限的角色；新建普通员工后，临时密码对话框引导去分配
  角色（还没有角色时去新建）；员工详情、角色页标出"未分配角色""未设置权限"；首页列出当前员工
  能进入的页面（和侧边栏同一份数据，`useVisibleNav`），一个都没有时说明原因、让他联系超级
  管理员。流程说明只写在 `src/features/staff/role-guide.tsx`，各处共用。
- 页面里的按钮按权限显隐用 `useCan(权限码)`（`src/lib/session.ts`），规则同路由守卫。
- 前端的权限判断只决定显示什么，真正的拦截在后端。

## 错误处理

请求经 `call()`（`src/api/errors.ts`）把错误转成 `ApiError { status, code }`。`main.tsx` 的
QueryCache / MutationCache 统一处理（`src/lib/server-error.ts`）：会话失效跳登录页（带回跳
地址）、`password_change_required` 跳改密页、读数据 403 跳无权限页，其余弹提示。错误码到中文
提示的映射在 `server-error.ts` 的 `messages`，没有列出的用后端的 message，5xx 不透出细节。
自己在表单里显示错误的 mutation 设 `meta.handlesErrors`：`true` 跳过全部统一处理（登录、改密，
连会话失效跳登录也由表单自己处理）；错误码数组只跳过列出的码（如调库存的 `insufficient_stock`），
其余照常统一处理。

## 表单

表单只用 `src/components/form` 的 `useAppForm` 和字段组件（`<form.AppField>` 里的 `f.XxxField`，
`<form.AppForm>` 里的 `form.SubmitButton`）。字段组件按类型一种一个：

| 类型 | 字段组件 | 表单里的值 | zod |
|---|---|---|---|
| 短文本 / 长文本 | `TextField`（`PasswordField`）/ `TextareaField` | 字符串 | `textInput({ title, required, min, max })` |
| 整数 | `NumberField` | 字符串 | `intInput({ title, required, min, max })` |
| 金额 | `MoneyField`（按元输入） | 字符串 | `moneyInput({ title, required, min, max })`，换成分 |
| 开关 | `SwitchField` | 布尔 | `z.boolean()` |
| 枚举 | `SelectField`（`options`，用 `src/lib/enum-options.ts` 的 `enumOptions(<枚举>Meta)`；非必填的加 `clearable`，可以清回未选择） | 字符串，空表示没选 | `enumInput(values, { title, required })` |
| 日期 | `DateField` | `yyyy-mm-dd` | `dateInput({ title, required })` |
| 时间点 | `DateTimeField`（浏览器时区选日期和时分） | RFC 3339 | `dateTimeInput({ title, required })` |

这些 zod 规则在 `src/lib/field-schemas.ts`（`required: true` 时输出类型不含 `undefined`）：输入框里的内容原样存成字符串，
提交时才校验和转换，所以能提示"库存要填整数"，金额也不经过浮点数；提示文字按字段的 `title` 和
约束生成，统一写在这里。表单的 zod schema 用 `meta.gen.ts` 的 `<请求体>Meta.form`（见上文），
直接作为 `validators.onSubmit`；表单的初始值写成 `z.input<typeof <请求体>Meta.form>` 类型（输入框里的
字符串），漏了字段或类型不对时编译报错。

新建、编辑、带输入的动作（如调库存）都用 `src/components/overlay` 的 `FormDialog` +
`useDialog()`：`onOpen` 在每次打开时把表单重置成当前数据或空白，提交成功后调 `close()`。编辑带
乐观锁的数据时，表单值里带上 `version`，传 `onConflict`：保存时接口返回 409 `<资源>_conflict`
（打开表单之后别人改过），`FormDialog` 调 `onConflict` 取回最新数据重置表单（直接读库的详情接口，
`staleTime: 0`），对话框保持打开并说明发生了什么；这个错误码写进 mutation 的 `meta.handlesErrors`。`<form>` 一律写 `onSubmit={onFormSubmit(form)}`：
`onSubmit` 里 await 的 mutation 失败时，TanStack Form 会把错误再抛出来，`onFormSubmit` 吞掉已经
处理过的 `ApiError`，不让它变成未捕获的异常（其它异常照常抛出）。

提交失败时，能归到某个字段的服务端错误（如改密时的 `wrong_password`）写进该字段的
`errorMap.onServer`，显示在字段下面，用户一改这个字段就消失：这些错误码写成常量（如
`<资源>_ERRORS`），同时放进 mutation 的 `meta.handlesErrors`，表单里用 `fieldError(e, 常量)`
取提示文案，取不到就重新抛出交给统一处理；一个表单有多个字段错误时写成"错误码 → 字段名"的映射，
用 `serverField(e, 映射)` 同时取出字段名和文案（生成的页面都这样写）；归不到字段的（如登录失败）用
`FormError` 显示在表单顶部。不要用裸的 `Alert` 显示表单错误：只有边框和红字时，看起来像一个
输入框。

## 说明与引导

补充说明和操作指引放进标题、表头旁的 ⓘ（`src/components/info-tip.tsx` 的 `InfoTip`、
`TitleWithTip`），页面上只直接显示必须一直看到的提示（如"N 个员工还没有分配角色"）。`InfoTip`
用 Popover：悬停、点按、键盘聚焦都能打开，里面可以放链接；不用 Tooltip：触屏上打不开。

提示文字说清楚三件事：现在是什么状态、会有什么影响、怎么处理（带上能直接点的入口）。不写
"没有用"这类只有结论的话；不规定没有必要的先后顺序（权限每次请求实时检查，分配角色和给角色
勾选权限谁先谁后都立即生效）；不用"并集""权限码""x-permission"这类开发者的词，说"功能"。

## 表格与危险操作

- 单元格用 `src/components/data-table/cells.tsx`：`TextCell`（`truncate` 截断长文本）、`NumberCell`、
  `MoneyCell`、`BoolCell`、`EnumCell`（中文文案徽标，`variants` 按取值区分颜色）、`DateCell`、
  `DateTimeCell`，空值统一显示"—"；行末的按钮用 `actionsColumn(row => …)`，按钮用 `useCan` 显隐，
  一个都没有时不加这一列。`DataTable` 把列定义的 `cell` 当普通函数调用（不像 `FlexRender` 那样当
  组件渲染），所以在组件里按权限拼列定义时，列表刷新也不会重新挂载单元格、关掉里面打开的对话框；
  代价是 `cell` 函数里不能用 hook，需要状态的放进它返回的组件里。详情页的"标签：值"用 `overlay` 的 `DescriptionList` / `DescriptionItem`。
- 筛选控件用 `src/components/filters`，放进 `FilterBar`：`SearchInput`（关键字，回车或点搜索才查）、
  `EnumFilter`（枚举多选，接口参数逗号分隔）、`BoolFilter`（全部 / 是 / 否）、`DateRangeFilter`、
  `SelectFilter`（选项来自数据，如操作日志的员工）、`SortSelect`（排序方式，选回默认排序时回调
  `undefined`，默认排序不进 URL）。没选等于不筛选，回调 `undefined`。
- 列表用 `src/components/data-table` 的 `DataTable`、`DataTableColumn`（列定义）和
  `useCursorPager` + `Pager`：后端是 cursor 分页，只给下一页的 cursor，上一页靠记住走过的
  cursor；把序列化的筛选条件作为 `useCursorPager(resetKey)` 的参数，条件一变就在同一次渲染里
  回到第一页（不要在 `useEffect` 里重置：那样会先用旧 cursor 配新条件发一次请求）。筛选条件放在 URL 的 search 里（路由的
  `validateSearch`，不合法的值丢掉），刷新和分享链接时保留。
- 页码分页的列表（后端返回 `total` / `total_capped`）用 `PagePager`，页码和筛选条件一起放在
  URL 的 search 里，筛选条件一变就去掉页码回到第一页；`total_capped` 时显示"10000+ 条"。
  路由的 `validateSearch` 用 `meta.gen.ts` 的 `<operationId>Meta.search`，由 `src/lib/search.ts`
  的 `pageSearch`（不超过能翻到的最后一页）、`daySearch`（真实存在的日期）、`enumListSearch`
  （枚举多选，去掉不认识的取值）、`textSearch`、`uuidSearch` 组成，不合法的值丢掉：URL 可能被
  手改。
- 金额在接口里是整数"分"：显示用 `MoneyCell`（`src/lib/money.ts` 的 `formatCents`），表单里用
  `MoneyField` 按"元"输入，`moneyInput` 用 `parseYuan` 换成分（只做字符串处理，不经过浮点数）。
- 停用、重设密码、删除这类不可撤销或影响他人的操作用 `ConfirmButton`，先弹确认框。shadcn 的
  `AlertDialogAction` 不会自己关闭对话框，`ConfirmButton` 已经处理。
- 时间显示用 `DateTimeCell`（`src/lib/format.ts` 的 `formatDateTime`，浏览器时区）。按日期筛选用
  `DateRangeFilter`（包着 `DateRangePicker`：shadcn 的 Calendar + Popover），值是含两端的 `yyyy-mm-dd`，放进 URL；
  请求时用 `dayRange` 换成后端的半开区间 `[from, to)`；`format: date` 的字段（不带时区）用
  `dateRange`，只把止日加一天。不用 `<input type="date">`：各浏览器
  外观不一，和其它控件不统一。

## 已有页面

| 路由 | 权限 | 说明 |
|---|---|---|
| `/login`、`/change-password` | 公开 / 登录 | 登录；首次登录或密码被重设后强制改密，改完回到原来要去的页面 |
| `/accounts`、`/accounts/$id` | superadmin | 员工列表（只剩一个启用的超级管理员时提示）、新建（一次性显示临时密码）、停用 / 启用、重设密码、踢下线、超级管理员标记、分配角色 |
| `/roles` | superadmin | 角色增删改、按模块勾选权限码（每项显示它允许的操作，取自 `admin.yaml` 的 tag 和操作的 `summary`；spec 里已删除的权限码标为"已失效"）。超级管理员是员工上的标记、不是角色，所以没建角色时列表是空的，页面上写明了这一点 |
| `/audit-logs` | superadmin | 操作日志，按员工和日期筛选 |
| `/users`、`/users/$id` | user:read | 前台用户（生成器生成，见 [generator.md](generator.md) 的"后台页面"）：按昵称搜索、按状态和注册日期筛选；详情里停用（user:disable，同时吊销对方的全部会话）、启用（user:enable）；详情里还有生成后手写加的登录方式列表、登录会话列表（user:read）和踢下线（user:kick，吊销指定会话） |
| `/settings` | 登录 | 个人设置：修改密码、登录设备，从用户菜单进入。登录设备是最近 50 次登录的历史，含已退出、已过期的（不认识的登录即使已失效也值得看到）；状态用接口返回的 `status`（空闲超时只有服务端知道），设备名由 `src/lib/user-agent.ts` 从 User-Agent 换算，只用于展示 |

## shadcn/ui

`components.json` 由 `shadcn init -b base -p vega` 生成。shadcn CLI 写入的文件
（`src/components/ui/`、`src/hooks/`、`src/lib/utils.ts`）不手改，只经下面两个命令修改，
`ui.lock.json` 记录它们的 sha256：

```bash
pnpm ui:add <组件...>   # 安装组件（及依赖），重写 ui.lock.json
pnpm ui:update          # 重新生成全部已安装组件，重写 ui.lock.json
pnpm ui:check           # 校验没有被改动
```

`pnpm ui:check` 在提交钩子和 CI 里都会运行。

oxlint、oxfmt、knip 都忽略这些文件。主题改 `src/index.css` 里的 CSS 变量，其它定制在
`src/components/` 里包一层。Base UI 的菜单要求 `DropdownMenuLabel` 放在 `DropdownMenuGroup`
里，否则打开菜单时报错。

## 测试

- `pnpm test`：vitest + happy-dom，测逻辑和组件行为（权限过滤、错误映射、表单 schema、字段和
  对话框的交互），文件放在被测代码旁边（`*.test.ts(x)`）。happy-dom 不做布局，宽度、溢出这类
  问题放进 e2e；
- `pnpm e2e`：Playwright（只用 Chromium）跑在真实后端上，访问嵌入了前端的后台 server，不经
  Vite。只覆盖关键路径（`e2e/admin.spec.ts`）：首次登录强制改密、超级管理员新建员工、普通员工
  看不到"系统"菜单、打开员工页被带到无权限页、直接调接口 403，以及窄屏上宽表格不把整页撑宽。
  首页也列出能进入的页面，菜单链接用 `menu(page, 名称)` 限定在侧边栏里找。测试会改掉超级管理员的
  临时密码，所以每次都要一个刚建的超级管理员（同一个库上换个用户名再建一个即可）：

  ```bash
  docker compose -f deploy/docker-compose.yml up -d --build
  docker compose -f deploy/docker-compose.yml run --rm api staff create -username e2e
  cd web/admin && pnpm exec playwright install chromium   # 第一次
  E2E_ROOT_USER=e2e E2E_ROOT_PASSWORD=<上面输出的临时密码> pnpm e2e
  ```

  `E2E_BASE_URL` 默认 `http://localhost:8081`（compose 里后台的端口）。在测试里手动
  `browser.newContext()` 时要显式传 `baseURL`：手动建的上下文不继承配置里的 `use`。

## 生成的页面

生成器为后台资源生成 `src/features/<tag>/` 和 `src/routes/_authed/<tag>/`（tag 是复数里的 `_`
换成 `-`）（结构和约定见
[generator.md](generator.md) 的"后台页面"）。生成之后是普通代码，按本篇的约定直接改。新增路由文件后
`pnpm gen:routes` 重新生成 `src/routeTree.gen.ts`（只启动路由插件，不打包；`pnpm dev`、`pnpm build`
也会生成）。

## 构建与部署

- `web/admin/embed.go` 用 `//go:embed all:dist` 把构建产物打进二进制，后台 server 在 `/` 托管
  页面（`AdminDeps.Static`），和 `/admin/v1` 同源。挂载代码只在 `internal/app/admin_ui.go`。
  目前不支持单独去掉后台前端：`api/embed.go` 的 `go:generate` 会写
  `web/admin/src/api/meta.gen.ts`，`tools/gen/meta` 的测试读这个文件，
  `internal/app/admin_ui_test.go` 也要求挂载了前端，删掉任何一处 `go test ./...` 都会失败。
- `web/admin/` 本身不是业务模块，不参与删除演练；但模块的后台页面（`src/features/<tag>`、
  `src/routes/_authed/<tag>`）会随模块一起演练删除（见 [modules.md](modules.md) 的"删除一个模块"）。
- `dist/` 里只提交 `dist/.placeholder`：没有构建前端时 `go build ./...` 照常通过，页面换成提示
  先构建前端的 `web/admin/unbuilt/index.html`。Vite 的 `emptyOutDir` 关掉了（否则会删掉占位
  文件），`pnpm build` 先用 `web/admin/scripts/clean-dist.ts` 清掉其余内容。
- `deploy/Dockerfile` 的 Node 阶段构建前端，产物拷进 api 的 Go 构建阶段；构建 worker 镜像时
  不执行 Node 阶段。compose 里的后台在 `http://localhost:8081`，端口只发布到宿主机的 127.0.0.1。
- 前端的静态文件不计入后台 server 的全局限流：冷启动打开页面要并发加载几十个资源文件，计入
  按 IP 的限流会让页面只加载一半。

## 检查

- 格式化：oxfmt 读 `web/admin/.editorconfig`（`root = true`），结果不受开发机上层目录的
  `.editorconfig` 影响，本地和 CI 一致。
- 提交钩子（`lefthook.yml`）只查暂存的 `web/admin` 文件：oxlint、oxfmt（自动修复后重新暂存）、
  `pnpm ui:check`。需要先在 `web/admin` 里 `pnpm install`。
- CI（`.github/workflows/admin-web.yml`，在 `web/admin/**`、`api/admin.yaml`、staff 模块、
  `internal/platform/httpserver/`、`deploy/` 等路径变化时运行）：lint、format:check、
  typecheck、test、knip、`ui:check`、重新生成 `schema.d.ts` 无差异、build、路由树无差异，
  以及在 compose 栈上跑端到端测试。`meta.gen.ts` 与 spec 一致由 Go 测试检查（见"字段文案与
  校验"）。
- Node 主版本（以 `package.json` 的 `engines` 为准）、pnpm 版本（以 `packageManager` 为准）、
  生成器的两个版本号由 `scripts/check-versions.sh` 检查。
