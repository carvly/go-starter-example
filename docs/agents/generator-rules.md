# 生成器规则编号

事实来源：`tools/gen/resource/rules.go`、`tools/gen/resource/repo.go`

`go run ./tools/gen check` 报出的每条错误都带规则编号和这里的链接（`#<编号小写>`）。流程、描述文件写法和
生成结果见 [generator.md](generator.md)。

报错格式：`<文件>:<行>:<列> <路径>: <问题>。改法：<怎么改>（<规则编号>，<本文的锚点>）`。
编号发布后不改名、不复用。

### YAML-SYNTAX

YAML 语法错误。常见原因是缩进不对、冒号后面少了空格，或者长文本没有用块样式（`custom: >-`）。

### YAML-UNKNOWN-KEY

没有这个键，多半是拼错了，报错里会给出最接近的键。描述文件用严格模式解码，不认识的键不会被静默忽略。

### YAML-TYPE

值的类型不对：该写列表的写成了字符串、`max` 写成了非整数、`required` 不是 true/false。

### TOP-REQUIRED

缺少必填的顶层键：`module`、`resource`、`title`、`fields`，以及有 `admin` 时的 `display`。

### TOP-MODULE-NAME

模块名同时是 Go 包名和目录名，只能用小写字母和数字（小写字母开头）。也不能是 Go 关键字，或者
组合根里已有的包名（`domain`、`postgres`、`echo`……），否则 `modules.go` 的 import 会冲突。

### TOP-RESOURCE-NAME

资源名用单数的 snake_case，它是表名、路径、权限码和 Go 类型名的来源。

### TOP-PLURAL-NAME

`plural` 用 snake_case，并且不能和 `resource` 相同。默认规则推得对时不要写。

### TOP-DISPLAY

`display` 必须是 required 的 `string` 字段：确认框、提示里用它代表一条记录，为空时就没法说清是哪一条。

### TOP-SURFACE

`admin` 和 `web` 要写且只写一个。同时开放到两边不支持，另一边手写。

### TEXT-TITLE-CHARS

名称（资源、字段、动作的 `title` 和枚举的文案）原样写进生成的页面（JSX 文本、模板字符串），所以不能有
引号、反引号、花括号、尖括号、反斜杠和 `$`。需要说明的内容写进 `help`、`confirm`、`description`，
它们按字符串字面量写出，没有这个限制。

### FIELD-NAME

字段名同时是列名和 JSON 字段名，用 snake_case。不能是系统字段（`id`、`created_at`、`updated_at`、
`version`），也不能是 SQL 保留字（`order`、`user`、`group`……）。

### FIELD-TYPE

字段类型只能是 `string`、`text`、`int`、`money`、`bool`、`enum`、`date`、`datetime`。扩展一种类型
要在生成器、前端零件和夹具里各补一处。

### FIELD-TITLE-REQUIRED

每个字段都要有 `title`：它是表单标签、表头和 spec 属性的 `title`（前端的文案从 spec 生成）。

### FIELD-MODIFIER

修饰用在了不支持的类型上，见"字段"的矩阵。例如 `bool` 不能写 `required`（总是非空），
`text` 不能写 `unique`。

### FIELD-MAX-REQUIRED

`string`、`text` 必须写 `max`：数据库、接口、前端三处的长度上限都从它来，没有上限的文本会被塞进
任意大的内容。

### FIELD-MIN-MAX

`min` 大于 `max`，或者长度的 `min` 为负、`max` 小于 1。

### FIELD-MONEY-NAME

`money` 的字段名以 `_cents` 结尾：单位是分，名字里带上单位（database.md 的命名规范），前端也按这个
后缀识别金额。

### FIELD-ENUM-VALUES

`enum` 要写 `values`，取值用 snake_case，每个取值都要有中文。

### FIELD-DEFAULT

`default` 要符合字段的类型和约束：整数在范围内、枚举是取值之一、字符串不超过 `max`。

### FIELD-IMMUTABLE-READONLY

`immutable`（创建后完全不能改）和 `readonly`（只能经动作改）不能同时写。

### FIELD-READONLY-UNREACHABLE

`readonly` 字段要有动作能改它：某个声明式动作的 `set` 写它，或者存在 `custom` 动作。否则它是永远
改不了的死字段；创建后不需要改的，写 `immutable`。

### ADMIN-OPERATION

`admin.operations` 只能写 `create`、`update`、`delete`。列表和详情总是有。

### ADMIN-LIST-FIELD

`admin.list` 引用了不存在的字段，或者类型不支持（`columns` 必须包含 `display` 字段：列表里它是进入
详情页的链接）：`search` 只支持文本，`filter` 只支持枚举、布尔和
日期，`sort` 不支持 `text`、`bool`、`enum`。`columns` 不能为空。

### ADMIN-DEFAULT-SORT

`default_sort` 必须在 `sort` 里（或者是 `created_at`）。

### ADMIN-NAV

`admin.nav.icon` 是 lucide-react 导出的组件名（PascalCase，如 `Users`），生成的路由文件直接 import 它；
名字不存在时前端的 typecheck 失败。`group` 不能有首尾空格，已有的分组见 `web/admin/src/lib/permissions.ts`
的 `groupOrder`，不在其中的分组排在后面。

### ACTION-NAME

动作名用 snake_case：它出现在权限码 `<资源>:<动作>` 和路径里。

### ACTION-RESERVED

动作名不能是 `create`、`update`、`delete`、`list`、`get`：它们和增删改查生成的 Service 方法、
operationId、前端的 hook 重名。参数名不能是系统字段（`id`、`version`、`created_at`、`updated_at`）：
`id` 是路径里的记录 ID，同名参数会在前端覆盖它，请求发到别的记录上。

### ACTION-TITLE

动作要有 `title`：它是按钮文字和权限页上的操作说明。

### ACTION-KIND

每个动作要在 `set`（声明式，完整生成）和 `custom`（自定义，生成桩由 AI 补足）里二选一。

### ACTION-INPUT

动作的参数不合法：名字、类型、修饰的规则同字段；参数不是表里的列，不能写 `unique`、`immutable`、
`readonly`、`default`；`like` 要指向已声明的字段，写了 `like` 就不能再写 `type`；参数是枚举时用
`like` 沿用字段的枚举，不单独定义（枚举类型和文案只在字段上生成一次）。

### ACTION-SET

`set` 的列必须是已声明且不是 `immutable` 的字段。值只能是符合字段约束的常量，或者 `input` 里类型
一致的 `$参数`。算不出来的写成 `custom` 动作。

### ACTION-WHEN

`when` 只能用 `enum`、`bool` 字段，值必须是合法的取值。

### ACTION-ERROR

动作声明的错误不合法：只有 `custom` 动作能声明错误；错误码用 snake_case（不带资源前缀，生成时
加）；`kind` 是 `invalid`（400）或 `conflict`（409）；要有给用户看的 `message`；`field` 要是 `input`
里的参数。

### ACTION-SET-EDITABLE

提示：`set` 写的字段也能在编辑表单里改，这个动作可能是多余的。只想经动作改的字段，加 `readonly: true`。

### RULE-ON

`rules` 的 `on` 只能写这个资源已有的操作。`delete` 要先加进 `admin.operations`。

### RULE-CUSTOM

每条规则要有 `custom`：一句话写清规则、出错时返回哪个错误、有没有并发要求。它会出现在桩代码的
注释、测试的失败信息和待办清单里。

### RULE-ERROR

规则声明的错误不合法，要求同 ACTION-ERROR，`field` 是 `fields` 里的字段。

### WEB-OPERATION

`web.operations` 只能写 `list`、`get`、`create`，不能为空。

### WEB-AUTH

`web.auth` 只能列 `operations` 里有的操作。

### INDEX-COLUMN

索引引用了不存在的列，或者没写 `columns`。

### INDEX-FOR

每个索引都要写 `for`，说明服务哪条查询（database.md：按查询建，不预建投机索引）。说不出就不建。

### INDEX-REDUNDANT

索引的列是主键、唯一约束或另一个索引的前缀，已经被覆盖了。部分索引不参与这项检查。

### INDEX-MISSING

提示：有排序或筛选的字段，但没有以它开头的索引。数据量大时补上索引；表很小或取值很少时，在
汇报里说明为什么不需要。

### REPO-MODULE-EXISTS

模块目录已经存在。第一版只支持新模块。

### REPO-TABLE-EXISTS

推出的表名已经存在：登记在某个模块的 `tables.go`，或者已在`internal/modules/<m>/migrations/` 里创建。已有的表不经生成器。

### REPO-SPEC-CONFLICT

推出的路径、tag、operationId、schema 名，或者权限码前缀，和 spec 里已有的重复。换资源名、
`plural` 或动作名。

### REPO-AUTH-MISSING

`web.auth` 需要唯一一个提供前台登录检查的模块。提供者从代码推导：`module.go` 里有
`func (m *Module) Middleware() echo.MiddlewareFunc` 的模块（`tools/internal/modinfo` 的
`UserAuthProvider`），生成的代码调用 `<m>M.Middleware()`。没有提供者或多于一个都报这条。
