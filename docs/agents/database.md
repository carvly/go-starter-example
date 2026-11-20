# 数据库：建表规范、migration、Repository、缓存、集成测试

事实来源：`internal/modules/*/migrations/`、`internal/platform/queue/migrations/`、`internal/platform/migrate/`、`internal/platform/postgres/`、`internal/platform/cache/`、`internal/modules/*/postgres/`、`internal/archtest/tables_test.go`、`scripts/test-integration.sh`

设计表结构、改 `internal/modules/<m>/migrations/`、任何模块的 `postgres/` 子包或缓存逻辑之前读这篇。事务边界见
[transactions.md](transactions.md)。

本篇的“建表规范”和“写 migration”两节是**规范**：新表、新 migration 一律按它写。

## 选型（不重新讨论）

- 只有 Postgres（≥ 18，与 `deploy/docker-compose.yml` 一致），驱动是原生 `jackc/pgx/v5`
  （`pgxpool.Pool` / `pgx.Tx`），不经 `database/sql`，不提供 SQLite；
- schema 由 `golang-migrate` 管理。每个模块的迁移在 `internal/modules/<m>/migrations/`，
  独立编号、使用 `schema_migrations_<m>` 版本表；river 等 platform 迁移在
  `internal/platform/queue/migrations/`，使用 `schema_migrations_platform` 并最先执行。
  `api migrate up|down|version|force` 按组合根的 `migrationSources()` 执行；
- 连接池在 `internal/platform/postgres/db.go` 构造，整个进程只有一个 pool，
  `MaxConnLifetime` 带 10% 抖动。默认值只是起点，按压测调整。

## 建表规范

总原则：**数据库只承担“应用层在并发下做不到或做不可靠”的事**——唯一性、原子扣减、非空、
不随业务演化的不变量；一切会演化的规则（枚举取值、格式校验、引用关系）放应用层：应用层
改一次是发代码，数据库改一次是发 migration。评价一条规则看四点：发版自由度、演进成本、
故障半径、认知负担。

### 表的类型

| 类型 | 主键 | 例子 |
| --- | --- | --- |
| 实体表：一行是一个可被单独引用的业务对象 | `id uuid`（应用层生成 UUIDv7） | 商品、订单、用户 |
| 关系 / 去重表：一行只表达“A 与 B 有关系”或“某件事处理过” | 业务键本身（单列或复合），不加 `id` | `order_processed_events`（`order_id`）、`wechat_openids`（`(user_id, appid)`） |
| 自然键表：业务上天然有稳定且不可变的唯一键 | 该键 | `oauth_states`（`id_hash`） |

- 行会被 `UPDATE` 的表必须有 `updated_at`，与表类型无关；所有表都有 `created_at`。
- 只有“需要单独引用或对外暴露这一行”才给 `id`；不为了“每张表都有 id”的形式统一加列。

### 主键

先选类型。**默认 `uuid`（v7）**，以下几条**全部**满足才用 `bigint GENERATED ALWAYS AS IDENTITY`：

| 判断 | 选 `uuid` | 可以选 `bigint` |
| --- | --- | --- |
| id 会不会出现在 URL、响应体、JWT、事件 payload、日志等外部可见的地方 | 会：自增 id 可被枚举，还泄露业务量（相邻两天的订单号一减就是日单量） | 从不出库，只在本库内部引用 |
| 写库之前要不要拿到 id（同事务里的事件、幂等键、fake 仓库） | 要：domain 先生成，不依赖数据库往返 | 不要：只靠 `INSERT ... RETURNING id` |
| 会不会被其它模块、外部系统引用，或将来拆库、合库 | 会：全局唯一，迁移数据不撞号 | 不会 |
| 写入量和体积是不是主要矛盾 | 一般业务表不是 | 是：日志、流水、明细这类行数远大于实体表的表，8 字节更省索引 |

例子：商品、订单、用户都暴露给客户端，用 `uuid`；只被本模块后台任务按时间扫的操作流水
可以用 `bigint`。拿不准就用 `uuid`：`uuid` 多出的 8 字节很少成为非改不可的问题，而
`bigint` 一旦需要对外暴露，换成 `uuid` 要改主键和所有引用列。`bigint` 表以后要对外暴露某一行时，加一列 `uuid` 唯一键对外，不暴露自增值。

- 实体表主键用原生 `uuid` 类型，值由 domain 层用标准库 `uuid` 包的 `uuid.NewV7().String()`
  生成（不返回 error）。`github.com/google/uuid` 只经 oapi-codegen 生成的
  `openapi_types.UUID` 间接出现在 handler 层，domain 和 Repository 不 import 它。PG18 自带的
  `uuidv7()` 不用作列默认值：id 要在写库之前由 domain 生成（同一事务里的事件、fake 仓库都要用它）：
  - v7 按时间有序，插入集中在 B-tree 末尾，不像 v4 那样随机分裂页；
  - `uuid` 16 字节，`TEXT` 存同样的值要 37 字节，主键和每个引用它的索引都更小；
  - Go 侧仍然是 `string`：pgx v5 把 Go `string` 参数一律按文本格式发送，`uuid` 列也能直接
    扫描到 `string`，Repository 不需要任何转换；
  - 外部传入的 id 必须在 HTTP 层校验是合法 UUID，否则非法字符串到了数据库会报类型错误
    （`22P02`）变成 500：
    - 路径参数写 `format: uuid`：生成的参数类型变成 `openapi_types.UUID`，非法值由生成的
      包装函数直接返回 400；handler 用 `id.String()` 转回 `string` 再交给 domain；
    - 请求体字段写 `format: uuid` 的同时加 `x-go-type: string`（保持 `string`，domain 不用
      转换）和 `x-oapi-codegen-extra-tags: {validate: "required,uuid"}`（见 `http.md`，
      只写 `format` 不生效）。`uuid` 校验不限大小写，handler 用 `strings.ToLower` 转成
      标准形式再交给 domain，否则响应、事件里的值和从 `uuid` 列读回的不一致；
    - 响应体里的 id 字段同样写 `format: uuid` + `x-go-type: string`，文档和生成的客户端
      才知道它是 UUID。
  - 对外需要更短的写法时（分享链接、客服口述、短信），在 HTTP 层把 UUID 的 128 位转成
    Crockford Base32：固定 26 个字符，不含容易认错的 I、L、O、U，解码不区分大小写。
    只是同一个值的另一种写法，不另外存列；库、domain、事件里仍然是标准 UUID 字符串，
    解码失败按非法 id 返回 400。
- 整数主键只用 `bigint GENERATED ALWAYS AS IDENTITY`：不用 `integer`（溢出后改类型要重写
  全表），不用 `serial` / `bigserial`（identity 是 SQL 标准写法，权限和序列归属更清楚）。
  引用它的列也是 `bigint`。`DecodeCursor` 只接受 UUID，这类表要做 cursor 分页时另写编解码。
- 不用业务字段（邮箱、用户名、订单号）做实体表主键：它们会变。

### 列类型

| 场景 | 用 | 不用 |
| --- | --- | --- |
| 时间点 | `timestamptz` | `timestamp`（无时区）、字符串、epoch 整数 |
| 只有日期 | `date` | `timestamptz` 截断 |
| 金额 | 最小货币单位的 `bigint`（分），列名带单位后缀 `_cents` | 浮点、`numeric`（全项目统一，不混用） |
| 计数、数量 | `bigint` | `integer`（溢出后改类型要重写全表） |
| 文本 | `text`；真有业务上限时加 `CHECK (char_length(col) <= n)` | `varchar(n)`（改上限要改列类型） |
| 枚举 | `text` + 代码常量，取值写进 spec、domain 和列注释 | Postgres `ENUM` 类型、`CHECK (col IN (...))`（加取值要发 migration，`ENUM` 删值几乎不可能） |
| 开关 | `boolean` | `smallint` 0/1 |
| 半结构化、不参与过滤的数据 | `jsonb` | `json`；把会被 `WHERE` / `JOIN` 的字段塞进 `jsonb` |

### NULL 与默认值

- 列默认 `NOT NULL`：Go 结构体不用指针，查询不用想三值逻辑。
- 默认值只给有合理缺省的列（计数 `0`、布尔显式 `FALSE` / `TRUE`、`created_at` /
  `updated_at` 的 `now()`），以及 expand 阶段给已有表新加的列（旧代码不写它，必须有默认值
  才能 `NOT NULL`）。必填业务字段（`username`、`email`、`customer_name`）只写 `NOT NULL`
  不加 `DEFAULT ''`：漏写时应当报 `23502` 失败，而不是静默写入空字符串。
- **可选文本存空串**：可选的 `text` 列写 `NOT NULL DEFAULT ''`，空串就是"没填"，不用 NULL
  （如 `users.avatar_url`、`staff_audit_logs.login_key`）。可选又唯一时用部分唯一索引，只对
  非空的值唯一：`CREATE UNIQUE INDEX uk_<表>_<列> ON <表> (<列>) WHERE <列> <> ''`。
- 可选的数值、日期、时间列（没有合理缺省值）允许 NULL，NULL 表示"没填 / 还没发生 / 未知"，
  并在列注释里写明 NULL 的含义（如 `user_sessions.revoked_at`、`user_identities.last_login_at`）。
  生成器按同一条规则生成（见 [generator.md](generator.md) 的“字段类型矩阵”）。
- `UNIQUE` 允许多个 NULL；需要“最多一个 NULL”时用 `UNIQUE NULLS NOT DISTINCT`。

### 约束

- **唯一性交给数据库**：凡是“同一个 X 只能有一行”的业务规则都建唯一约束，代码用
  `INSERT ... ON CONFLICT` 处理并发，不“先查再插”；Repository 按约束名把冲突翻译成业务
  错误：`platformpg.UniqueViolation(err)` 返回违反的约束（或唯一索引）名，不是唯一冲突时
  返回空串（例：`internal/modules/staff/postgres/repository.go`）。
- 大小写不敏感的唯一（邮箱、用户名）：首选在 domain 层统一规范化（`strings.ToLower` +
  `TrimSpace`）后用普通唯一约束；无法保证所有写入方都规范化时，改用表达式唯一索引
  `CREATE UNIQUE INDEX uk_accounts_lower_email ON accounts (lower(email))`。表达式索引不是
  约束：`ON CONFLICT ON CONSTRAINT` 用不了，要写 `ON CONFLICT ((lower(email)))`；
  `23505` 错误的 `ConstraintName` 是索引名，Repository 照样按名字翻译。两种方式二选一，
  不混用：Go 的 `strings.ToLower` 和 Postgres 的 `lower()` 对非 ASCII 字符（土耳其 `İ`
  等）折叠结果不一定相同。不引入 `citext` 扩展。
- `CHECK` 只写不随业务演化的不变量（`price_cents > 0`、`quantity > 0`、
  `char_length(name) <= 200`），不写枚举取值、状态流转这类会变的规则。
- **不用外键**。写入路径收口在各模块的 Repository，引用完整性由代码保证；FK 会让
  expand-contract、删除归档、按模块拆分 schema、将来拆库都多一层依赖编排，也让模块在
  数据库层互相耦合（模块之间代码不互相 import，表之间也不该互相约束）。配套要求：
  - 引用列只有在被查询（`WHERE` / `JOIN`）时才建索引，不为“它是外键”建索引；
  - 删除被引用的行由业务决定：要么禁止（Service 先检查），要么软失效（状态列），要么
    由消费方处理“引用对象已不存在”；
  - 这个取舍的前提是写入收口；出现多写入方（脚本直连、多服务写同一张表）时重新评估。

  模块内的外键靠这条规范约束；跨模块外键另由 `internal/archtest` 检查（见下面“表归属”）。
- 不做软删除（`deleted_at`）作为默认手段：它让每条查询和每个唯一约束都要带条件。
  业务上需要“作废但保留”时用明确的状态列或归档表，唯一性用部分唯一索引
  （`CREATE UNIQUE INDEX ... WHERE status <> 'archived'`）。它和表达式索引一样不是约束：
  要写 `ON CONFLICT (col) WHERE status <> 'archived'`，谓词必须和索引一致，否则报
  “no unique or exclusion constraint matching the ON CONFLICT specification”。

### 命名

- 表名：snake_case 复数名词（`invoices`、`user_sessions`）；关系表用两个对象名
  （`product_tags`）。不加任何前缀（模块名、`t_`、`tbl_` 都不加）。
- 列名：snake_case；引用列 `<对象>_id`；时间点 `<动作>_at`；布尔用形容词或过去分词
  （`enabled`、`verified`），不加 `is_`；金额带单位后缀（`_cents`）。
- 约束和索引**全部显式命名**，类型前缀开头，Repository 和运维脚本都按名字引用：

  | 对象 | 名字 | 例子 |
  | --- | --- | --- |
  | 主键 | `pk_<表>` | `CONSTRAINT pk_users PRIMARY KEY (id)` |
  | 唯一约束 / 唯一索引 | `uk_<表>_<列...>` | `CONSTRAINT uk_users_email UNIQUE (email)` |
  | 普通索引 | `idx_<表>_<列...>` | `CREATE INDEX idx_invoices_created_at_id ON invoices (created_at DESC, id DESC)` |
  | CHECK | `ck_<表>_<列>` | `CONSTRAINT ck_products_price_cents CHECK (price_cents > 0)` |

  Postgres 自动生成的名字是后缀风格（`users_pkey`、`users_email_key`），不写 `CONSTRAINT
  <名字>` 就会混进这种风格，所以主键、唯一、CHECK 都要显式写名字。例外是 `NOT NULL`：PG18
  起它也是 `pg_constraint` 里的具名约束，每个列一个，沿用自动名 `<表>_<列>_not_null`，
  不显式命名。名字超过 63 字节会被截断，超长时缩写列名。

### 索引

- 按查询建，不预建投机索引（每个索引都在给每次写入征税）：每个索引在 migration 的注释里
  写明服务哪条查询。
- 复合索引等值列在前、范围 / 排序列在后，排序方向与查询一致（如
  `(created_at DESC, id DESC)` 支撑 cursor 分页）。
- 只查一小部分行时用部分索引（`WHERE status = 'pending'`）；只为避免回表时才用
  `INCLUDE`。
- 不建被主键、唯一约束或其它复合索引**前缀**覆盖的冗余索引。

### 注释

表和每个列都写 `COMMENT ON TABLE` / `COMMENT ON COLUMN`：行内 `--` 注释执行后就丢了，
数据库里的注释在 `\d+` 和所有读 schema 的工具（包括 AI agent）里都能看到。枚举列列全
取值，可空列写明 NULL 的含义，数值列写明单位。

## 表归属

每张业务表属于且只属于一个模块，登记在该模块的 `postgres/tables.go`
（如 `internal/modules/user/postgres/tables.go`）：`var Tables = []string{...}`，必须是
字符串字面量。为什么：模块随时可能拆到独立的数据库，一条 JOIN 了别人表的 SQL 在拆分时
才暴露，代价最大。`internal/archtest/tables_test.go` 检查：

- 模块的 SQL（含测试）只读写本模块登记的表；需要别的模块的数据，经组合根适配器调用
  对方的 Service（见 [modules.md](modules.md) 的“跨模块协作”）；
- `internal/platform`、`internal/kit`、`internal/config`、`cmd` 不读写任何模块的表，
  platform 的集成测试自己建表（参考 `internal/platform/dbtx/dbtx_integration_test.go`）；
  `internal/app` 是组合根，跨模块集成测试可以直接准备多个模块的数据；
- 登记的表必须在全部模块迁移执行后存在，一张表不能登记在两个模块；模块迁移只能操作本模块的表，
  共享数据库对象只能放在 platform 迁移（`internal/archtest/migration_objects_test.go`）；
- 两个模块的表之间不能有外键；一端是模块的表、另一端是没有登记的业务表也算违规（防止忘了
  登记时漏检）。

新建表时同时在 `tables.go` 登记。river 的 `river_*` 表不属于任何模块，模块经
`internal/platform/queue` 使用队列，不直接写 SQL 访问它们。

检查是静态分析，不是 SQL 解析器：

- 能看见的：字符串字面量里的 SQL 语句（SELECT/INSERT/UPDATE/DELETE/TRUNCATE/MERGE/COPY/
  LOCK TABLE/ALTER TABLE/DROP TABLE），以及 `pgx.Identifier{...}`（`CopyFrom` 等用它传表名）。
  SQL 里的字符串常量、注释、`AS` 后面的别名不算表引用。
  `platformpg.ListPage` 等平台辅助函数的 `table` 参数也会被检查。
- 看不见的：用变量拼出来的表名，以及不含上述语句的 SQL 片段（单独的 `" JOIN user_sessions ..."`
  再拼接）。所以 SQL 一律写成完整的字面量，表名用字面量传给辅助函数。
- 可能误报：列名和别的模块的表同名（比如一列叫 `users`），或者一句文案恰好像
  “select ... from ...”。遇到误报按硬规则 1 处理：停下来说明理由，不改检查、不加例外。

删除模块时表不会消失（已发布的 migration 不改），检查只要求“被读写的表有归属”，
不要求“每张表都有归属”，删除演练因此不受影响。生成器（`tools/gen`）生成模块前会检查
表名是否已被占用。

## 写 migration

- **一个文件一个变更主题**，文件名写明动作和对象（如 `0011_add_orders_paid_at`）。同一个
  模块一起上线的建表可以放在同一个文件里（`0007_create_staff` 建了 staff 的 6 张表）。编号取
  `internal/modules/<m>/migrations/` 里现有的最大号 + 1，中间空着的号不复用。golang-migrate 把一个文件作为一次
  请求发送，文件内多条语句在同一个隐式事务里，失败整体回滚。
- **先问一句**：一个还没升级的旧版本 api/worker 实例对着新 schema 跑，会不会报错？会的话
  拆成 expand / contract 两次发布：先加新结构并让新代码兼容新旧两种状态，旧版本全部下线后
  再删旧结构、加约束。需要拆的变更：
  - 删列、删表、改列类型、重命名列或表（重命名 = 加新列 → 双写 → 回填 → 切读 → 删旧列）；
  - 给已有列加 `NOT NULL`、给已有数据加 `UNIQUE` / `CHECK`。
- **锁与耗时**（DDL 拿的是 `ACCESS EXCLUSIVE` 锁，排队时会挡住后面所有读写）：
  - 改已有表的文件第一行写 `SET lock_timeout = '5s';`、最后一行写 `RESET lock_timeout;`，
    拿不到锁就失败，不在线上排队。`SET` 对整个连接生效，golang-migrate 一次运行的所有
    文件共用一条连接，不 `RESET` 会带到后面的文件里。不要写显式 `BEGIN; ... COMMIT;`：
    中途失败时连接会停在 aborted 的事务里，golang-migrate 后续的解锁语句报
    “current transaction is aborted”，掩盖真正的错误。失败**不会自动重试**：版本被标为
    dirty，之后每次 `migrate up` 都拒绝执行。文件的隐式事务已整体回滚，确认没有长事务挡锁
    后 `api migrate force -module <模块> -version <上一个版本>` 再重跑；
  - `ADD COLUMN ... DEFAULT <常量或 now() 这类 STABLE 函数>` 只改元数据（PG11+ 只算一次
    并存为默认值），很快；`gen_random_uuid()`、`clock_timestamp()`、`random()` 这类
    VOLATILE 默认值要逐行求值，会重写全表，大表先加无默认值的列再回填；
  - 给已有数据加 `CHECK` / `NOT NULL`：先 `ADD CONSTRAINT ... NOT VALID`，再单独一个文件
    `VALIDATE CONSTRAINT`（只拿弱锁）。`NOT NULL` 在 PG18 里也是这样：
    `ADD CONSTRAINT <表>_<列>_not_null NOT NULL <列> NOT VALID`，不再需要 `CHECK` 垫底；
  - 给已有大表加唯一约束：`CREATE UNIQUE INDEX CONCURRENTLY`，再
    `ADD CONSTRAINT ... UNIQUE USING INDEX`；
  - 大批量回填不写进 migration：用可重入的分批任务或脚本（按主键范围、每批几千行）。
- **`CREATE INDEX CONCURRENTLY`** 不能在事务里执行，所以它所在的 migration 文件**只能有
  这一条语句**（否则会进入隐式事务而报错），也不写上面的 `lock_timeout`（它拿的是不挡
  读写的 `SHARE UPDATE EXCLUSIVE` 锁）。失败时会留下 INVALID 索引且 migration 版本被
  标为 dirty：先 `DROP INDEX CONCURRENTLY IF EXISTS <名字>`，再
  `api migrate force -module <模块> -version <上一个版本>` 后重跑。新建的空表直接 `CREATE INDEX`，不用 `CONCURRENTLY`。
- **项目上线前**还没有已发布的 migration：改表直接改原来建这张表的 migration（加列、改约束、
  换索引都写回原文件），不新增 migration，也不写兼容旧结构的代码；上面的 expand / contract
  和锁与耗时也不适用。上线后改用下一条规则，并删掉这一条。改完之后，已经执行过这个文件的本地库
  不会跟着变（golang-migrate 只记版本号，不重跑），api 会报"列不存在"之类的错，两种处理：
  - 要保留本地数据：在本地库里手动执行对应的改动（`ALTER TABLE ... ADD COLUMN IF NOT EXISTS`、
    `DROP INDEX IF EXISTS`、`CREATE INDEX IF NOT EXISTS`），内容和改后的 migration 一致；
  - 不需要本地数据：`docker compose down -v` 删掉数据卷后重建，所有 migration 从头执行。

  集成测试每次都从空库执行全部 migration，不受影响。
- 已发布的 migration 不再改语义；要改 schema 就新增一条。只修正 `--` 行注释可以改
  （golang-migrate 不校验文件内容）；`COMMENT ON` 是会执行的语句，改它要新增一条
  migration，否则已执行过的库永远拿不到新注释，新库和老库的列说明不一致。
- `.down.sql` 只用于本地开发回退；生产环境不执行 down（`DROP TABLE` 不会把数据还回来），
  出问题时新增一条前向 migration 修复。
- 新表、新列的 `COMMENT ON` 和建表 / 加列写在同一个 migration 里（要求见“注释”一节）。
- `internal/platform/queue/migrations/0001_river_queue.up.sql` / `.down.sql` 是 river 自己的队列表，分别由
  `river migrate-get --all --exclude-version 1 --up` / `--down` 导出，不要手改，也不受本篇
  建表规范约束。升级 river 后如果它新增了 schema 版本，用 `--version <新版本号> --up` /
  `--down` 导出成一条新 migration。
- 本地执行：`export DB_DSN=...` 后 `make migrate-up`；compose 里由 `migrate` service 执行。

## 写 Repository

- 实现放在 `internal/modules/<m>/postgres/`，接口在 `domain/ports.go`。一个聚合一个文件：
  只有一个 Repository 的模块写在 `repository.go`；user 按聚合拆成 `repository.go`（users）、
  `identity.go`、`session.go`、`openid.go`、`state.go`。
- 一律经 `handle.Resolve(ctx)` 取连接（需要具体 `pgx.Tx` 时用 `ResolveTx`），不持有
  `*pgxpool.Pool`：`dbtx.Handle` 把 pool 做成私有字段，在编译期保证 Repository
  不会绕过调用方的事务。
- 占位符是 `$1, $2`，不是 `?`。
- 写明列名，不用 `SELECT *` 和不带列名的 `INSERT`：expand 阶段新加的列不会让旧代码的
  `Scan` 列数对不上。
- **竞态敏感的判断写进同一条 SQL**，不要先 SELECT 再 UPDATE：
  扣库存用 `UPDATE ... WHERE stock >= $1` 看 `RowsAffected()`；状态机用
  `WHERE status = 'pending'`；去重用 `INSERT ... ON CONFLICT DO NOTHING`；读完就删的
  临时数据用 `DELETE ... WHERE ... RETURNING`（`internal/modules/user/postgres/state.go` 的
  `Take`），读完要留痕的用 `UPDATE ... RETURNING`。RowsAffected 为 0 时，只在失败路径多查
  一次来区分“不存在”和“条件不满足”。
- **后台编辑整份表单的表用乐观锁**：加 `version BIGINT NOT NULL DEFAULT 1`，编辑用
  `UPDATE ... SET ..., version = version + 1 WHERE id = $1 AND version = $n`，没更新到行时区分
  不存在（`ErrNotFound`）和已被修改（`ErrConflict`，409 `<资源>_conflict`）。只有编辑校验和递增
  `version`：调库存这类单个增减量的动作不会覆盖别人的修改，不动它，高频的动作也就不会让正在
  编辑的人频繁冲突。例子是带 `version` 的生成器资源；接口和前端的约定见 [http.md](http.md)、[admin.md](admin.md)。
- 不用触发器：手写的 `UPDATE` 和 `INSERT ... ON CONFLICT DO UPDATE` 必须显式带
  `updated_at = now()`。
- 后台的页码分页（见 [http.md](http.md) 的"后台接入面"）用 `platformpg.ListPage`
  （`internal/platform/postgres/list.go`）：条件用 `platformpg.Where` 收集（`Eq`、`In`、
  `Range`、`Contains`，值为空的条件不加，查询计划才能用上索引；`Contains` 内部用
  `ContainsPattern` 转义 `%` `_`），内部用 `LIMIT/OFFSET` 取一页、`CountCapped` 数总数（最多
  数到 10000，多数一行判断是否超过），起始位置由 `page.New` 限制在前 10000 条内。参考
  `internal/modules/user/postgres/repository.go` 的 `List`，生成器生成的 Repository 也是这样；
- 其余列表分页用 cursor（`created_at + id`，`internal/platform/postgres/pagination.go`），
  不用 offset；多取一行判断是否有下一页；`limit` 在 Repository 里再兜底一次，cursor
  解析失败返回模块自己的 `ErrInvalidCursor`（4xx）。cursor 里的 id 也是外部输入，
  `DecodeCursor` 会校验它是合法 UUID，否则伪造的 cursor 会在数据库报 `22P02` 变成 500。
- 找不到返回模块的 `ErrNotFound`，其它错误用 `fmt.Errorf("postgres: ...: %w", err)` 包装。

## Repository 契约测试

每个 Repository 接口一套契约用例，定义在模块的 `domain/domaintest/`：只有一个 Repository
的模块是 `contract.go` 的 `RunRepositoryContract`；user 另有 `contract_login.go` 的
`RunIdentityRepositoryContract`、`RunSessionRepositoryContract`、`RunOAuthStateRepositoryContract`。
同一套用例跑两遍：

- `domain/domaintest/contract_test.go` 跑内存 fake（`go test ./...`）；
- `postgres/repository_integration_test.go` 跑 Postgres 实现（`make test-integration`，
  需要 Docker、Podman 或 socktainer）。

改 Repository 行为时，先改契约用例，再让 fake 和 Postgres 实现都通过。fake 和真实
实现之间的不一致就是靠这套用例发现的，不要只改其中一边。

集成测试用 `internal/platform/postgres/pgtest`：每个测试进程起一个 Postgres 容器
（镜像与 `deploy/docker-compose.yml` 一致，`scripts/check-versions.sh` 检查）。调用方用
`pgtest.New(t, <模块>.Migrations)` 声明来源；pgtest 总是先执行 platform 迁移，按来源集合缓存
模板库，每个测试再克隆独立数据库。组合根的跨模块测试传 `migrationSources()`。集成测试文件都带
`//go:build integration`。

`make test-integration` 经 `scripts/test-integration.sh` 运行，Docker、Podman 和 socktainer
（macOS 上把 Apple Container 包装成 Docker API）都能直接用：

- 已设置 `DOCKER_HOST` 时原样使用；
- 有 docker 命令时，testcontainers 不读 docker context，脚本把当前 context 的地址设成
  `DOCKER_HOST`（地址是默认的 `/var/run/docker.sock` 时不改）；
- 没有 docker 命令时用 Podman：`DOCKER_HOST` 指向它的 API socket（rootless 下按需启动
  `podman.socket`），并设置 `TESTCONTAINERS_RYUK_CONTAINER_PRIVILEGED=true`；
- socktainer 下同一次 `go test` 的各个包共用一个 ryuk，会撞名或被上一个包的 ryuk 清掉容器，
  所以带 `//go:build integration` 的包每个单独跑一次 `go test`，其余的包合在一次里跑。用
  socktainer 前先 `container system start`，socktainer 本身没在运行时 docker 命令连不上。

不要用 `TESTCONTAINERS_RYUK_DISABLED=true` 绕过 ryuk 起不来的问题：`pgtest` 的容器靠 ryuk
清理，没有 ryuk，测试结束后 Postgres 容器和匿名卷都不会被清理。只跑部分包：
`scripts/test-integration.sh ./internal/modules/user/...`；带值的 `go test` 参数写成
`-run=X` 这种形式（脚本把 `./`、`../` 开头的参数当作包）。

## 缓存

- 缓存是现成能力，目前没有模块接入；只有真的有热点读的模块才接。端口由模块自己在 `domain/ports.go` 定义，
  方法签名与 `internal/platform/cache` 一致即可直接满足：

  ```go
  type Cache interface {
  	Get(ctx context.Context, id string) (*Product, bool, error)
  	Set(ctx context.Context, id string, v *Product, ttl time.Duration) error
  	Delete(ctx context.Context, id string) error
  }
  ```

- 实现用 `internal/platform/cache` 的泛型 `RedisCache[T]` / `NoopCache[T]`，在 `module.go` 里按是否配置了
  Redis 选择（`cache.NewRedisCache[T](client, "<模块>", logger)` 或 `cache.NewNoopCache[T]()`）；
- key 必须是 `cache:<模块>:<id>`：限流也用同一个 Redis（`ratelimit:<规则>:<标识>`），
  不隔离会撞 key；
- 失效顺序固定为先写 DB、再删缓存（用 Delete，不用 Set 覆写）；删缓存一律包在
  `uow.AfterCommit(ctx, fn)` 里：写操作可能处在调用方的事务里，提交前删缓存，并发读会把
  旧值回填进缓存一直留到 TTL 过期（失效应包在 `uow.AfterCommit` 中，避免提交前删缓存后旧值回填）；
- 读缓存的方法在事务里（`uow.InTransaction(ctx)`）不读缓存、直接查库，否则删缓存推迟后读不到
  本事务自己的写入；回填缓存（Set）同样包在 `uow.AfterCommit` 里，不缓存未提交、可能回滚的值
  （避免回填未提交、可能回滚的值）；
- 缓存故障一律 fail open，当作未命中回源，不能让请求变成 500；
- TTL 只是兜底，失效靠“写后删”；
- 以后引入进程内的本地缓存时，必须有跨实例的失效通知（Redis pub/sub 或 Postgres `LISTEN/NOTIFY`）：
  "写后删"只删得到本实例的副本，其它实例会一直读旧值到 TTL 过期。
