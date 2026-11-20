# 生成器：新增一个资源（新表）

事实来源：`tools/gen/`、`.github/workflows/generator.yml`

新增一张表，开放到后台或前台时读这篇。AI 写一份描述文件，`gen apply` 一次生成各层代码（后台资源
连同后台前端的页面），再补上描述文件里的 `custom` 和 `rules`。生成的代码是普通代码，不依赖生成器运行。

不新增真相源：字段只在 spec 和 migration 两处声明；描述文件是一次性的输入，放在 `.gen/`、不入库，
所以不存在描述文件和代码不一致的问题。产物分三类，处理方式不同：

| 产物 | 处理 |
|---|---|
| 脚手架（模块、Service、Repository、handler、页面） | 生成一次，之后是普通代码；目标文件已存在就整体放弃 |
| 派生代码（`*.gen.*`） | 永远不手改，`make generate` 重新生成，CI 检查重新生成后没有 diff |
| migration | 追加式 |

## 什么时候用、什么时候不用

- **用**：新模块里的一个新资源，表还不存在，开放到后台（`admin`）或前台（`web`）其中一个。
- **不用**：
  - user 模块：前台登录的代码是手写的，生成器管理范围之外，不再对它重新
    `gen apply`；`tools/gen/testdata/fixture_user.yaml` 只作为回归夹具。
  - 已有的表（`user_sessions`、`staff_accounts`……）：它们的写操作要经过已有 Service 里的业务规则（硬规则 2）。
    这类页面手写，在已有 Service 上加 `adminapi/`，见 [modules.md](modules.md)、[http.md](http.md)。
  - 已有模块里加第二张表、同时开放到后台和前台、跨表引用：第一版不支持，手写。
  - 以动作为主、不是增删改查形状的页面。

## 流程

1. **理解需求**。确认是新表。按下面的"需求对照"翻译需求；标着"要问"的地方需求没说清，就先问用户。
2. **写描述文件**，放在被 git 忽略的 `.gen/` 目录。先运行 `go run ./tools/gen example admin`
   （或 `web`）照着填。用 `go run ./tools/gen check .gen/<资源>.yaml` 校验，改到没有错误为止。
   `check` 一次报出全部错误，每条写明位置、改法和规则编号。
3. **核对规划**：运行 `go run ./tools/gen plan .gen/<资源>.yaml`。看推出的名字对不对（不规则复数
   补写 `plural`），要新建哪些文件、插在哪里。
4. **生成**：运行 `go run ./tools/gen apply .gen/<资源>.yaml`。
   - 工作区可以有未提交的改动：生成器只要求要新建的路径（模块目录、后台页面目录）下没有任何文件。写入前
     把要改的已有文件（被插入的 spec、`modules.go`、`migrate.go`，以及会重新生成的 `meta.gen.ts`、
     `schema.d.ts`、`routeTree.gen.ts`）原样存进 `.gen/<资源>/before/`，回退按它还原。
   - 写完会自动运行 `go generate ./...`（含 `meta.gen.ts`）、`go build ./...`、archtest 和注册测试；
     后台资源还会在 `web/admin` 里运行 `pnpm gen:api`（接口类型）、`pnpm gen:routes`（路由树）、
     oxfmt（格式化生成的页面）和 `pnpm typecheck`。`web/admin` 还没有 `pnpm install`
     （没有 `node_modules`）时这几步不运行，换成一条 `frontend` 待办，写明要补跑的命令。
   - 最后打印待办清单和回退命令 `go run ./tools/gen revert .gen/<资源>.yaml`。
5. **补足**：按待办清单逐项完成。
   - 每个 `custom` 动作：加上 Repository 方法，同时改 Postgres 实现、内存 fake 和契约用例，再实现
     Service 方法。
   - 每条 `rules`：实现 `checkRuleN`。
   - `custom` 动作要读写本模块的另一张表时（如停用用户时吊销 `user_sessions` 里的会话）：手写这张表的
     migration，在 `postgres/tables.go` 登记；能写成一条语句的用数据修改的 CTE，否则用
     `UnitOfWork.Run`（[transactions.md](transactions.md)）。这类改动汇报时说明：改了生成的已有
     声明（如 `tables.go` 的 `Tables`）会出现在 `gen drift` 里，新建的文件（另一张表的 migration、
     新的 `postgres/*.go`）不会，要用 `git status` 补列。
   - 把对应的 `NotImplemented` 测试换成覆盖每条规则、每个声明的错误的测试。
   - 需求里有事件、缓存时，按 [queue.md](queue.md)、[database.md](database.md) 补上。
6. **跑全部检查**：
   ```bash
   go build ./... && go vet ./... && go test ./...
   make lint-new
   make test-integration              # 或 scripts/test-integration.sh ./internal/modules/<模块>/...
   make check-modules                 # 删除演练
   cd web/admin && pnpm typecheck && pnpm lint && pnpm test && pnpm build   # 后台资源
   ```
7. **汇报**：按下面的"汇报模板"。
8. **审核与反哺**：按下面的"审核清单"，由另一个没参与这次生成的上下文审核，再把发现落到生成器上。
   最后运行 `go run ./tools/gen fixture .gen/<资源>.yaml`，把描述文件存为回归夹具，和这次的改动一起提交。

要改描述文件重新生成：先 `go run ./tools/gen revert .gen/<资源>.yaml`，再 `apply`。回退删掉新建的路径，
把改过的已有文件还原成生成前的原样（生成前已有的未提交改动保留）。生成之后这些已有文件又被改过（手改，或又生成了
别的资源）时，revert 拒绝并列出文件，什么都不动；新建路径里补足的代码会随回退删除，所以补足之前先确认生成结果。

## 命令

```bash
go run ./tools/gen check   <描述文件> [--json]   # 只校验，一次报出全部错误
go run ./tools/gen plan    <描述文件> [--json]   # 推出的名字、新建的文件、插入的位置，不写文件
go run ./tools/gen apply   <描述文件> [--json]   # 生成
go run ./tools/gen drift   <描述文件> [--json]   # 生成之后改过的地方（和 .gen/<资源>/snapshot/ 比较）
go run ./tools/gen revert  <描述文件>            # 精确回退一次生成（按 .gen/<资源>/before/ 还原）
go run ./tools/gen fixture <描述文件>            # 存为回归夹具 tools/gen/testdata/fixture_<资源>.yaml
go run ./tools/gen example admin|web             # 带注释的完整描述文件
go run ./tools/gen schema                        # 描述文件的 JSON Schema（只管结构，语义规则只有 check 查）
go run ./tools/gen meta <admin.yaml> <meta.gen.ts>   # 从后台 spec 生成前端的字段文案与校验（make generate 调用，不用手动跑）
```

- 必须在仓库根目录运行。
- 退出码：
  - 0：成功；
  - 1：描述文件有错误；
  - 2：环境问题和用法错误（不在仓库根目录、要新建的路径下已有文件、目标
    文件已存在、没有可回退的生成记录、已有同名的路由文件 `web/admin/src/routes/_authed/<tag>.tsx`、描述文件读不到、
    参数不对）。
- `--json` 的字段：
  - `check`：`{ok, problems: [{rule, file, line, col, path, message, fix, doc, hint}]}`；`hint` 为 true
    的只是提示，不影响退出码。
  - `plan`：`{names, create, insert: [{file, section, after_line, text}], hints}`。
  - `apply`：在 `plan` 的基础上加 `todo: [{kind, file, symbol, instruction, test, reference}]` 和
    `revert`（回退命令）。
  - `drift`：`{items: [{file, symbol, kind, expected, detail}]}`。`kind` 取 `changed`、`removed`、
    `added`、`file_changed`、`file_removed`、`insert_changed`；`expected` 为 false 的就是待办清单之外的修改。
    覆盖范围：只比较生成器写过的文件（`plan` 的 `create`）和插入的内容。生成文件里新增的声明一律
    算 `expected: true`；生成之后新建的文件完全不出现，要用 `git status` 补列。

## 需求对照

"不问时的默认"可以直接用，但汇报时要列出。

| 需求里的说法 | 写成 | 要问用户 | 不问时的默认 |
|---|---|---|---|
| "必填" | `required: true` | — | — |
| "默认是 X" | `default` | — | — |
| "不超过 N 个字" | `max: N` | — | 名称类 `string` 50，`text` 500 |
| "不能重复" | `unique: true` | 是否不区分大小写（要的话写进 `rules`） | 区分大小写 |
| "创建后不能改" | `immutable: true` | — | — |
| "是否……""开关" | `bool` | — | false |
| "状态有 A、B、C" | `enum` + `values` | 默认值是哪个 | 第一个取值 |
| "只能通过某个操作改"（如状态、库存） | `readonly: true` + 动作；新建时要填的再加 `required` | — | — |
| "金额""价格" | `money`，字段名 `<名>_cents` | 是否允许为 0 | 允许 0 |
| "日期""时间" | `date` / `datetime` | 是否精确到时刻 | — |
| "备注""说明" | `text` | — | — |
| "停用""上架" | 声明式动作 `set` | 哪些状态下可以做（`when`） | — |
| "调整""计算" | 自定义动作 `custom` + `errors` | 规则的边界 | — |
| "删除" | `admin.operations` 加 `delete` | 删除前要检查什么（写进 `rules`） | 不提供删除 |
| "X 不能早于 Y" | `rules` + `errors` | — | — |
| "按 X 筛选 / 排序 / 搜索" | `admin.list.filter` / `sort` / `search` | 数据量大概多少（决定 `indexes`） | 按创建时间倒序 |
| "前台能看到""用户能提交" | `web`，`auth` 写需要登录的操作 | 哪些操作要登录 | 写操作要登录 |

## 描述文件

完整示例看 `go run ./tools/gen example admin` 和 `example web`（源文件在
`tools/gen/resource/examples/`）。解码是严格的：未知键报错。

### 写法

- 长文本（`custom`、`confirm`、`help`）用块样式单独起行（`custom: >-`）。
- 值里有英文冒号加空格、`#`，或者以 `$` 以外的特殊字符开头时，加引号。
- 行内花括号只用于短的字段定义。

### 顶层

| 键 | 必填 | 说明 |
|---|---|---|
| `module` | 是 | 新模块名，同时是 Go 包名和目录名：小写字母开头，只含小写字母和数字 |
| `resource` | 是 | 资源名，单数 snake_case |
| `plural` | 否 | 复数。默认规则：辅音 + `y` 结尾改 `ies`，`s`/`x`/`z`/`ch`/`sh` 结尾加 `es`，其余加 `s` |
| `title` | 是 | 中文名：页面标题、tag 的 summary（权限页的分组名）、操作 summary 的前缀 |
| `description` | 否 | 一句话说明：tag 的说明、表注释 |
| `display` | 有 `admin` 时必填 | 代表一条记录给人看的字段，必须是 required 的 string |
| `fields` | 是 | 字段，按书写顺序 |
| `rules` | 否 | 新建、修改、删除时的业务规则 |
| `indexes` | 否 | 主键和唯一约束以外的索引 |
| `admin` / `web` | 写且只写一个 | 接入面 |

系统字段不用写：`id`（UUIDv7）、`created_at`、`updated_at`；有 `update` 时还有 `version`（编辑的乐观锁）。

### 推出的名字

| 名字 | 推法 | 例子（`catalog` / `category`） |
|---|---|---|
| 表名 | 复数 | `categories` |
| 路径、tag | 复数，`_` 换成 `-` | `/categories`、`categories` |
| 权限码 | `<资源>:<操作或动作>` | `category:read`、`category:disable` |
| Go 类型 | 资源的 PascalCase | `Category` |
| operationId | `list<复数>`、`get<资源>`、`create<资源>`、`update<资源>`、`delete<资源>`，动作是 `<动作><资源>` | `listCategories`、`createCategory`、`disableCategory` |
| 错误码 | `<资源>_not_found`、`_conflict`、`_invalid_state`、`_<字段>_taken`、`_invalid_<字段>`，声明的错误加资源前缀 | `category_name_taken` |
| 约束 | `pk_<表>`、`uk_<表>_<列>`、`ck_<表>_<列>`、`idx_<表>_<列...>` | `uk_categories_name` |

### 字段

`<字段名>: { type: <类型>, title: <中文名>, <修饰>... }`

- 字段名用 snake_case。不能是系统字段，也不能是 SQL 保留字。
- `money` 字段名以 `_cents` 结尾。
- `help` 写进 spec 的 `description`、列注释。
- `enum` 另写 `values: { <取值>: <中文>, ... }`。

| 修饰 | string | text | int | money | bool | enum | date | datetime |
|---|---|---|---|---|---|---|---|---|
| `required` | ✓ | ✓ | ✓ | ✓ | — | ✓ | ✓ | ✓ |
| `default` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | — |
| `min` / `max` | 字符数，`max` 必填 | 字符数，`max` 必填 | 值 | 分，不写 `min` 时为 0 | — | — | — | — |
| `unique` | ✓ | — | ✓ | — | — | — | — | — |
| `immutable` / `readonly` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

- **可空**只有一条规则：`string` / `text` 不填时存空串，`bool` 不填时是 false；其它类型既不是
  `required` 也没有 `default` 时可空（存 `NULL`，Go 里是指针）。
- `immutable` 和 `readonly` 不能同时写：`immutable` 创建后完全不能改；`readonly` 编辑时不能改，
  只能经动作改，并且要有动作能改它。
- `readonly` 字段新建时：required 且没有 `default` 的要填（如初始库存），其余的取 `default` 或为空
  （如状态）。
- **被动作改的字段要写 `readonly`**：否则编辑表单会整份提交它，把编辑期间动作改出的结果覆盖掉
  （动作不递增 `version`，乐观锁拦不住）。例如库存只能经"调整库存"改，就写
  `stock: { type: int, required: true, readonly: true, ... }`。
- 可选又唯一的字符串用部分唯一索引（只对非空的值唯一）。

### 字段类型矩阵

| 类型 | Postgres | Go | spec | 校验（validate 标签 / Service） | 前端表单（meta.gen.ts） |
|---|---|---|---|---|---|
| `string` | `TEXT` + `CHECK (char_length <= max)` | `string` | `string` + `maxLength` | `required,max=` / 去首尾空格后检查长度 | `textInput` |
| `text` | 同上 | `string` | 同上 + `x-field: text` | 同上 | `textInput` |
| `int` | `BIGINT` + 范围 `CHECK` | `int64` | `integer/int64` + `minimum`/`maximum` | `gte=,lte=` / 范围 | `intInput` |
| `money` | 同上 | `int64` | 同上 + `x-field: money` | 同上 | `moneyInput` |
| `bool` | `BOOLEAN NOT NULL` | `bool` | `boolean` | — | `z.boolean()` |
| `enum` | `TEXT`（取值不写 `CHECK`，见 database.md） | 具名 `string` 类型 + 常量 + `Valid()` | `$ref` 到 `<资源><字段>` 枚举 schema，带 `x-enum-labels` | `oneof=` / `Valid()` | `enumInput` |
| `date` | `DATE` | `time.Time` | `string/date` | — | `dateInput` |
| `datetime` | `TIMESTAMPTZ` | `time.Time` | `string/date-time` | — | `dateTimeInput` |

可空时 Go 类型是指针，spec 里不在 `required` 中。required 的字符串只写 `NOT NULL`，不加
`DEFAULT ''`（database.md：漏写时应当报错）。

### 后台接入面 `admin`

| 键 | 说明 |
|---|---|
| `operations` | `create`、`update`、`delete` 的子集；列表和详情总是有 |
| `list.columns` | 后台列表页显示的字段，可以含 `created_at`、`updated_at` |
| `list.search` | 关键字 `q` 搜哪些字段：`string`、`text` |
| `list.filter` | 筛选：`enum`（多选）、`bool`、`date`、`datetime`（`<字段>_from` / `_to`，半开区间） |
| `list.sort` | 可排序的字段：`string`、`int`、`money`、`date`、`datetime`、两个系统时间；`created_at` 总是可以排序 |
| `list.default_sort` | 默认排序，倒序写 `-<字段>`；不写时是 `-created_at` |
| `actions` | 业务动作 |
| `nav` | 侧边栏菜单项：`icon`（lucide-react 的图标名，默认 `Table`）、`group`（默认 `业务`）、`order`（默认 100） |

列表用 `platformpg.ListPage`：只放有值的条件，排序后总是再按 `id` 排，总数最多数到 10000。

### 动作 `admin.actions`

每个动作 `set` 和 `custom` 二选一。

| 键 | 说明 |
|---|---|
| `title` | 必填：按钮文字、操作 summary |
| `input` | 参数：完整的字段定义，或 `{ like: <字段> }` 沿用字段的类型和约束（可以覆盖 `title`、`min`、`max`、`required`） |
| `set` | 声明式：每列改成常量或 `$<参数>`，不支持表达式 |
| `when` | 前置条件：`enum`、`bool` 字段的取值，可以写多个。声明式动作写进同一条 SQL |
| `custom` | 自定义：要实现的规则、错误情形和并发要求 |
| `errors` | 只用于 `custom`：`<码>: { kind: invalid\|conflict, message: <中文>, field: <参数> }` |
| `confirm` | 后台页面确认框里说明后果的文字 |

- 路径：`POST /admin/v1/<复数>/{id}/<动作，_ 换成 ->`。返回改后的记录。
- 动作不校验也不递增 `version`。
- **声明式动作**完整生成：条件和更新在同一条 `UPDATE` 里，没更新到行时再查一次，不存在返回
  404，条件不满足返回 409 `<资源>_invalid_state`。
- **自定义动作**生成到 Service 方法签名为止，方法体返回 `ErrNotImplemented`，不生成 Repository
  方法（值怎么算生成器不知道，半成品只会误导）。`ErrNotImplemented` 是 `KindInternal`，客户端看到
  500；这可以接受，因为生成的 `NotImplemented` 测试在 pre-push 和 CI 里会失败，空壳上不了线。`custom` 的说明出现在四处：描述文件、方法的文档注释、`NotImplemented` 测试的失败信息、待办清单。

### 业务规则 `rules`

```yaml
rules:
  - on: [create, update]
    custom: 结束日期不能早于开始日期
    errors:
      invalid_period: { kind: invalid, message: 结束日期不能早于开始日期, field: ends_on }
```

- `on` 是已有操作的子集。
- `create` / `update` 的规则生成 `checkRuleN(ctx, v *<资源>)`，`v` 是要写入的记录：修改时由当前
  记录加上提交的字段合成。
- `delete` 的规则生成 `checkRuleNDelete(ctx, id)`。
- 需要和写入原子完成的检查（如删除时没有引用），放进同一个事务并加行锁，见 [transactions.md](transactions.md)。

### 前台接入面 `web`

| 键 | 说明 |
|---|---|
| `operations` | `list`、`get`、`create` 的子集（修改、删除涉及归属规则，第一版不生成） |
| `auth` | 需要登录的操作，在 `routes.go` 里按 operationId 挂组合根注入的鉴权中间件；其余在 spec 里写 `security: []` |

前台列表用 cursor 分页（`cursor` / `limit`），按创建时间倒序。没有索引服务它时，生成器自动加
`(created_at DESC, id DESC)`。

### 索引 `indexes`

```yaml
indexes:
  - columns: [status, -created_at]     # 倒序写 -<列>
    for: 后台按状态筛选：WHERE status = ANY($1) ORDER BY created_at DESC, id DESC
    where: status = 'active'            # 可选：部分索引
```

主键和唯一约束自动生成，其余索引都在这里写，每个都说明服务哪条查询。

**分析清单**：列出这个资源会执行的查询，每条要么对应一个索引，要么在汇报里说明为什么不需要
（枚举只有两三个取值、表很小）：

- 后台列表的默认排序；
- 常用的筛选组合；
- 前台列表（`created_at DESC, id DESC`）；
- 动作里的条件；
- 其它模块经 Service 的读取。

`check` 对"有排序或筛选、但没有以它开头的索引"给出提示（INDEX-MISSING）。

关键字搜索 `q` 是 `ILIKE '%…%'`，不建索引，`indexes` 也不支持 `pg_trgm`。数据量大到需要时，另写一条
migration 建三元组索引，并在汇报里说明。

## 生成了什么

以 `catalog` / `category` 为例：

```
internal/modules/catalog/migrations/<序号>_create_categories.{up,down}.sql
internal/modules/catalog/migrations.go
internal/modules/catalog/module.yaml        web_paths 前端归属声明
internal/modules/catalog/
  module.go                         New、RegisterAdminHTTP（前台是 RegisterHTTP）
  domain/entity.go                  Category、枚举、CategoryCreate / CategoryUpdate / CategoryFilter
  domain/errors.go                  错误码，以及声明的 errors
  domain/ports.go                   Repository 接口
  domain/service.go                 增删改查与动作、字段校验；custom 动作和 rules 的桩
  domain/service_test.go            校验的测试；每个 custom 动作、每条 rules 一个 NotImplemented 测试
  domain/domaintest/                内存 fake、ValidCategory() 夹具、Repository 契约
  postgres/                         tables.go、repository.go、集成测试
  adminapi/                         handler、routes、oapi 生成配置、全字段的 HTTP 测试
web/admin/src/features/categories/ 后台页面（只有 admin 时生成，见下面的"后台页面"）
  queries.ts                        类型、列表和详情的 queryOptions、各写操作的 mutation 与字段错误映射
  columns.tsx                       列表的列（display 字段链接到详情页）
  list-page.tsx                     标题、新建按钮、筛选栏（搜索、筛选、排序）、表格、分页
  detail.tsx                        详情：全部字段、修改按钮、动作和删除
  form.tsx                          新建、修改的对话框（有 create / update 时）
  actions.tsx                       动作和删除的按钮（有动作或 delete 时）
web/admin/src/routes/_authed/categories/
  index.tsx、$id.tsx                列表和详情的路由：权限、菜单（admin.nav）、URL 查询参数的校验
```

还会往共享文件里插入内容（只插入，不改写已有内容）：

- `api/admin.yaml`（前台是 `api/openapi.yaml`）的 `tags`、`paths`、`components.schemas`；
- `internal/app/modules.go` 里按硬规则 3 的固定几行，以及 `internal/app/migrate.go`
  里的模块 import 和 `migrationSources()` 一行；
- 后台资源重新生成的派生文件：`meta.gen.ts`、`schema.d.ts`、`routeTree.gen.ts`。

### 后台页面

生成的页面是显式组装的普通代码，不是读配置渲染的万能页面：要改某一页就直接改它的文件，不用学配置
语言，也不会遇到配置表达不了、只好绕过框架的情况。页面只组装公共组件（`src/components`、`src/lib`），
约定见 [admin.md](admin.md)：

- 文案、校验、枚举选项来自 `meta.gen.ts`：改 spec 后 `make generate`，页面不用改；
- 筛选条件、排序和页码在 URL 里（`validateSearch` 用 `meta.gen.ts` 的查询参数 schema）；日期范围在
  URL 里是 `yyyy-mm-dd`，请求前用 `dayRange`（datetime，按浏览器时区的日界）或 `dateRange`（date）
  换算成半开区间；
- 修改、动作、删除都在详情页：按钮按权限码（`useCan`）显示，有 `when` 的动作只在记录满足条件时
  显示；没有参数的动作弹确认框（`confirm` 是说明文字），有参数的弹表单；
- 唯一约束冲突、`rules` 和动作声明了 `field` 的错误显示在对应字段下（`queries.ts` 的
  `*_FIELD_ERRORS`，同时写进 mutation 的 `handlesErrors`）；修改时的 `<资源>_conflict` 由 `FormDialog`
  提示并刷新表单；其余错误由统一的错误处理弹出提示，message 是后端返回的中文；
- 菜单从路由派生（`admin.nav`），不改共享的菜单数据。

**防止漏改的测试**：

- 契约测试先检查 `ValidCategory()` 的每个字段都不是零值，再"写入 → 读出 → 逐个字段比较"：
  加了字段没补夹具，或者补了夹具没改 SQL，都会失败；
- HTTP 测试用全部字段创建再读出比较：handler 的映射漏了字段会失败；
- rules 还没补足时，经过它的用例跳过，由 NotImplemented 测试负责失败。

## 加字段

不提供"加字段"命令：它要改已经是普通代码的脚手架。按下面的清单改，漏改由派生代码和测试发现：

1. 改 migration（上线前直接改原文件）；
2. spec 加属性（带 `title`），然后 `make generate`；
3. 实体、`Create` / `Update` 结构体、Repository 的列和 SQL、handler 的映射、fake、`ValidXxx()` 各加一处：
   漏了任何一处，契约测试或 HTTP 测试会失败；
4. Service 的字段校验加一处：测试强制不了，用越界值调一次 Service 自检，汇报时列出改了哪些校验。

后台资源还要改前端：

5. `cd web/admin && pnpm gen:api` 更新接口类型（`make generate` 已经更新了 `meta.gen.ts`）；
6. `src/features/<tag>/form.tsx` 的 `blank` 和 `editValues` 各加一项（`pnpm typecheck` 会报出
   来），再在表单里加字段组件；
7. `detail.tsx` 的 `DescriptionList` 加一个 `DescriptionItem`：详情页按字段逐个写死，**漏了没有
   任何检查能发现**，汇报时说明；
8. 按需改 `columns.tsx`（列表列）和列表页的筛选：同样没有检查，按需求决定。

## 汇报模板

1. 描述文件全文；
2. "需求 → 键"的翻译决定，以及用了哪些默认值；
3. 生成了哪些文件；有哪些声明式动作；
4. 按 `custom` 和 `rules` 补足了什么、对应的测试；
5. **`gen drift` 列出的待办清单之外的修改，逐条说明为什么**：这是生成器缺什么的直接信号，理想是没有；
6. `indexes` 和理由；
7. 检查失败了几次、`check` 改了几轮；
8. 需要用户确认的问题；
9. 检查结果。

## 审核清单与反哺

审核由另一个没参与这次生成的上下文来做：

| 审核什么 | 看什么 |
|---|---|
| 描述文件与需求 | 需求的每一句都有对应的键；翻译决定和默认值合理；该问的问了 |
| 生成的代码 | 和项目约定、项目里已有的模块实现有没有结构分叉，有没有 bug |
| 待办清单之外的修改 | 从 `gen drift` 的输出出发，逐条看原因 |
| 补足的部分 | 是否满足 `custom` / `rules` 的说明；测试是否覆盖每条规则和每个声明的错误 |
| 过程 | `check` 改了几轮、卡在哪里；规范和报错有没有让 AI 走弯路 |

每个发现都要落到生成器的某一处，并留下防止复发的检查：

| 发现 | 反哺到 | 防止复发 |
|---|---|---|
| 生成的代码有 bug 或不符合约定 | 模板（`tools/gen/resource/templates/`） | 夹具复现，修复前夹具 CI 失败 |
| 待办之外改了生成的代码，且这类修改能声明 | 描述文件新增能力 | 夹具覆盖新能力 |
| 待办之外改了生成的代码，但声明不了 | 待办清单（`apply.go` 的 `todos`）和本文 | 生成器的测试 |
| 描述文件写错了而 `check` 没报 | 新的规则编号 | `rules_test.go` 的用例 |
| `check` 报了但照着改法没改对 | 改法的文字 | 用例断言新的改法 |
| 需求翻译偏差、该问没问 | 需求对照表 | 本文 |
| 公共组件或公共包不够用 | 公共层（`internal/kit/page`、`internal/platform/postgres`、`web/admin/src/components`） | 它们的测试 |

**每条反哺标明落在哪一层，优先放进公共层**：已经生成的模块是普通代码，生成器不会回头改它们。

- 公共层（公共包、公共组件）和派生代码（`meta.gen.ts` 等）：已有模块自动受益，首选；
- 模板：只影响以后生成的模块。确实要改模板时二选一：这是约定，就加一条覆盖全部模块的检查
  （archtest 或注册测试），并在同一个改动里把已有模块改过来；只是写法上的改进，就在汇报里写明
  "只对以后生成的模块生效"和理由。

不做"重新生成再三方合并"：那就是双向同步。

**自迭代的边界**：同 [workflow.md](workflow.md) 的"任务后的摩擦回顾"——只往更严、更清楚的方向直接做（加规则、
改清楚报错、补能力、补夹具），放宽或删除规则、删除夹具、改变已有规则的含义交给用户决定。

## 夹具与 CI

- `tools/gen/testdata/` 里是回归夹具。`fixture_widget.yaml` 覆盖全部字段类型和写法，
  `fixture_goods.yaml` 是商品的形状，`fixture_readonly.yaml`（只读、没有动作）和
  `fixture_edit_delete.yaml`（没有新建、改名动作、`like` 枚举参数、带特殊字符的说明）是前端模板的边界组合；每次真实使用后，用 `gen fixture` 存一份，文件名是 `fixture_<资源>.yaml`（如前台用户的 `fixture_user.yaml`），同名时报错不覆盖。
  存档时 `module` 加前缀 `fx`，`resource`、`plural` 加前缀 `fx_`（如 `fxuser`、`fx_user`）：真实
  使用过的模块、表和接口已经在仓库里，原样重新生成会冲突；夹具检查的是形状，名字不影响。
- `go test -tags genfixture -timeout 30m ./tools/gen/` 对每份夹具和两份示例：在仓库的临时副本里
  生成，然后跑：
  - `go vet`；
  - 跳过 NotImplemented 的全部测试；
  - 单独跑 NotImplemented 测试，断言每个桩各失败一次；
  - golangci-lint；
  - 后台资源：`web/admin` 的 typecheck、lint、format:check、test、check:unused、build（临时副本链接到
    `web/admin/node_modules`，要先 `pnpm install`）；
  - 删除演练。
- 加上 `GEN_FIXTURE_INTEGRATION=1` 再跑 Postgres 上的契约测试。
- 每份描述文件要跑几分钟。`GEN_FIXTURE_SHARD=i/n` 只跑编号对 n 取余等于 i 的那些；本地只想验证
  某一份时用它，或者用 `-run 'TestFixtures/<文件名>'`。
- CI（`.github/workflows/generator.yml`）在生成器、公共包、前端零件和配置、两份 spec、
  `.golangci.yml` 变化时运行，另外每周一定时跑一次；完整的触发路径以这个文件的 `paths` 为准。它分成
  7 片并行（`GEN_FIXTURE_SHARD=0/7` … `6/7`），总耗时约等于最慢的一份；新增描述文件不用改工作流。
  分片共享的构建缓存见 [deploy-ci.md](deploy-ci.md) 的"CI"。
  本地改了其中任何路径，提交前也跑一次。
- 生成器自己的单元测试（`go test ./tools/gen/...`）检查：
  - 每个规则编号至少被一个用例触发，报错带位置、改法和文档链接；
  - 每个规则编号在 [generator-rules.md](generator-rules.md) 有同名小节；
  - 两份示例能通过校验；
  - 插入位置正确；
  - drift 能区分待办内外的修改。

## 规则编号

每条错误的规则编号、含义和改法见 [generator-rules.md](generator-rules.md)。
