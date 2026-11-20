# 队列：事件、消费者、定时任务

事实来源：`internal/platform/queue/`、`internal/modules/*/events/`、`internal/modules/*/jobs/`、`internal/app/worker.go`

发布或消费事件、新增定时任务之前读这篇。

## 选型

`riverqueue/river`（Postgres 上的任务队列）。入队和业务写入在同一个事务里提交，
语义上替代手写 outbox 表；重试（指数退避）、唯一性、定时任务都用 river 自带能力，
不另接 cron 库。river 的表由 `internal/platform/queue/migrations/0001_river_queue.up.sql` 创建。

不在 river 之外另写 outbox 表或自己的轮询消费者：两套投递机制并存，重试和幂等要各自保证一遍。

river 不做逐任务的心跳续租：`Work` 的 ctx 在任务超时后取消（`internal/platform/queue/` 没有配置
`JobTimeout`，用 river 的默认值 1 分钟），返回的错误按重试处理。单个任务要在这个时间内做完，耗时更长的
拆成多个任务，或者在 worker 上实现 `Timeout()`。

## 事件契约

- 事件类型放在发布方模块的 `events/` 包（`internal/modules/user/events/user_deleted.go`），
  只有纯数据类型，只依赖标准库，其它模块可以直接 import。
- **`Kind()` 发布后不能改名**：队列里的存量任务按它找 worker，改名后这些任务永远没人
  消费。每个事件都要在消费它的 `jobs/` 测试里用字面量锁死 `Kind()`：
  `internal/modules/user/jobs/user_deleted_test.go` 锁死 `user_deleted`，
  `internal/modules/user/jobs/` 的测试锁死 `user_deleted`。`internal/app` 的测试只检查
  每个 Kind 都有 worker，不检查值。
- JSON 字段名同样是存量任务的格式：只能新增可选字段，不能改名或删除。
- 事件带 `RequestID`，发布方从 ctx 读出写进事件，消费方据此重建带 request_id 的 logger。

## 发布

- 在 domain 里定义 `Publisher` 端口，`Publish...` 必须在 `UnitOfWork.Run` 里调用；
- 实现用 `internal/platform/queue` 的 `Inserter.InsertTx`（与业务无关，只接收
  `river.JobArgs`），适配代码放在发布方模块的根包（`internal/modules/user/publisher.go`）。

## 消费

- 业务规则写在不依赖 river 的函数里（`internal/modules/user/jobs/user_deleted.go`），
  用内存 fake 覆盖全部分支；`jobs/` 里的 `river.Worker` 只做参数转换、日志关联和
  panic 兜底。
- **必须幂等**：至少一次投递下同一事件可能被处理多次。状态置位天然幂等，扣库存这类
  计数操作不是，要用去重表（`order_processed_events`，`INSERT ... ON CONFLICT DO NOTHING`）
  和业务写在同一个事务里。
- 业务规则用内存 fake 覆盖全部分支；涉及事务回滚和真实入队的性质由组合根在真实
  Postgres 上的集成测试验证。
- **区分永久失败和临时故障**：永久失败要在同一事务里把业务状态收口并返回 nil；
  临时故障返回错误交给 river 重试。跨模块错误由组合根适配器翻译成消费方自己的 sentinel。
- 在消费模块的 `RegisterJobs` 里 `queue.AddWorker`，并把模块加进
  `internal/app/modules.go` 的 `jobRegistrars`。`internal/app` 的测试会检查每个
  `events` 包里的 `Kind()` 都有 worker。
- **一个事件牵动多个模块的写操作**（如 `user_deleted` 要各模块清理自己的数据）：
  发布方在 `events/` 里定义事件并在业务事务里入队；**worker 放在发布方的 `jobs/`
  包、经它的 `RegisterJobs` 注册**（删除演练要求 worker 随发布方模块一起消失），
  但它是编排角色——被调用的模块实现 `user.DataCleaner`，在全部模块构造完成后
  由组合根用一行 `userM.AddDataCleaner(memberM.UserDataCleaner())` 登记，不把编排塞进
  发布方的 domain。登记行不是构造依赖；业务模块的每日清扫经适配器调用 user 时才形成
  真实依赖。worker 逐个调用，任一失败整体重试，每个实现必须幂等。每个持有用户数据的
  模块还必须有每日清扫，补偿滚动部署和重试耗尽造成的遗漏。

## 定时任务

定时任务由模块提供，`internal/app` 不写具体任务：

1. 模块实现 `PeriodicJobs() []queue.Periodic`，每项给出 `Schedule`（如
   `river.PeriodicInterval(time.Hour)`）和每次入队都相同的 `Args`，需要时再给 `Opts`
   （`*river.PeriodicJobOpts`，如 `RunOnStart: true` 让进程启动时先跑一次）。截止时间之类在
   `Work` 里按当前时间计算，不放进参数；
2. 在同一个模块的 `RegisterJobs` 里 `queue.AddWorker` 注册它的 worker；
3. 在 `internal/app/modules.go` 的 `periodicJobRegistrars` 里加一行 `m.<M>.PeriodicJobs`，
   模块也要在 `jobRegistrars` 里。

`internal/app` 的测试会检查模块提供的定时任务都已登记、每个都有 worker。多实例部署时
river 只在选出的 leader 上调度，不会重复入队。例子：

- `internal/modules/staff/module.go` 的 `PeriodicJobs()`：每天清理超过保留期的操作日志，
  worker 在 `internal/modules/staff/jobs/`；
- `internal/modules/user/module.go` 的 `PeriodicJobs()`：每小时清理过期的 `oauth_states`
  和 `user_sessions`，worker 在 `internal/modules/user/jobs/cleanup.go`。

两者都设了 `RunOnStart`。

## 进程

worker 进程（`internal/app/worker.go`）运行两个组件：river 消费 client 和只有
`/healthz`、`/metrics` 的内部 server，由 `internal/kit/lifecycle` 统一启停。任一组件
异常退出，整个进程以非零退出码结束。收到 SIGTERM 时 river 先优雅停止，等正在执行的
任务最多 10 秒（`internal/platform/queue/client.go` 的 `SoftStopTimeout`），之后取消它们的
ctx；整个进程的关停上限是 20 秒（`internal/app/api.go` 的 `shutdownTimeout`），到期还没
停完就调用 `StopAndCancel`，再最多等 5 秒（`internal/platform/queue/component.go` 的
`hardStopTimeout`）。

## 删除模块后的存量任务

模块用 `queue.AddWorker` 注册 worker，`queue.Workers` 同时记下 kind：实际注册的 worker 是"本二进制能
处理哪些任务"的唯一来源，没有另一份需要手工同步的清单。worker 启动时用它找出没有 worker 的非终态任务并
打 Warn。所有实例都换成新版本后，执行 `api queue cancel-orphans`：它和 worker 进程一样构造模块、注册
worker（需要完整配置，部署环境里本来就有），取消所有 `finalized_at IS NULL` 且当前二进制没有 worker 的
任务（包括崩溃旧实例留下的 `running`），并按 kind 输出取消数量。终态任务不受影响。

## 外部消息队列

只有出现独立部署、需要订阅这里事件的其它服务时，才引入外部 MQ（默认推荐 NATS
JetStream）。做法是新增一个 river worker 作为 relay，把事件转发出去；relay 自己也
必须幂等。不预先接入。
