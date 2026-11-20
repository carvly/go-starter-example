# 事务：UnitOfWork 与 dbtx.Handle

事实来源：`internal/kit/uow/`、`internal/platform/dbtx/`

写跨 Repository、跨方法或"业务写 + 入队"的原子操作之前读这篇。

## 规则

- 需要原子性时调用 `uow.UnitOfWork.Run(ctx, fn)`（接口在 `internal/kit/uow`，
  实现是 `internal/platform/dbtx` 的 `Handle`）。事务经 ctx 隐式传给 `fn` 内调用的
  Repository，不显式传 `tx` 参数。
- Repository 只能经 `handle.Resolve(ctx)` 取连接：在 `Run` 闭包里拿到的是当前事务，
  闭包外拿到的是连接池。`Handle` 不暴露 pool，所以不存在"忘了用事务"的写法。
- 需要具体 `pgx.Tx` 的调用方（river 的 `InsertTx`）用 `handle.ResolveTx(ctx)`；不在
  事务里时它返回 `dbtx.ErrNoTransaction`，入队因此必须写在 `Run` 里面。
- 嵌套 `Run`（闭包里又调用了一个自己也会 `Run` 的 Service 方法）走 SAVEPOINT：
  内层失败只回滚内层，不会静默开出第二个独立事务。
- `UnitOfWork` 是可选的：单表 CRUD 直接用 Repository，不需要 `Run`。
- `Run` 里的 panic 会先回滚再继续向上抛，由 HTTP 的 Recover 中间件或 river 兜底。
- 必须等数据对其它连接可见之后才能做的副作用（删缓存等）用 `uow.AfterCommit(ctx, fn)`
  登记：最外层事务提交后执行，事务或所在 SAVEPOINT 回滚时丢弃，不在事务里时立即执行；
  回调拿到的 ctx 不随调用方取消。回调是尽力而为的：panic 会被记录并吞掉，不影响已提交的
  结果和其它回调；`Commit` 返回错误时（事务可能其实已提交）回调不执行，删缓存这类回调由 TTL
  兜底。`uow.InTransaction(ctx)` 判断当前是否在事务里。新的 `UnitOfWork` 实现必须用 `uow.WithHooks` /
  `Hooks.Committed` 支持它（见 `internal/platform/dbtx/dbtx.go`）。入队不用它：入队要和
  业务写同事务。

## 例子

- `internal/modules/user/domain/account.go` 的 `DeleteMe`：注销写操作和事件入队同事务；
- `internal/modules/staff/domain/service.go` 的 `Disable`：在锁里检查至少留下一个超级管理员、
  改状态、吊销会话同事务；
- 一条语句就能写完的不需要开事务：`internal/modules/user/postgres/repository.go` 的 `Disable`
  用数据修改的 CTE 在同一条语句里改用户状态并吊销会话；
- `internal/modules/staff/domain/service.go` 的 `ResetPassword`：重设密码、启用账号、
  吊销会话和写操作日志同事务。

## 测试

- domain 测试用 `internal/kit/uow/uowtest` 的 `Immediate`（直接执行 fn，不开事务）；
- 提交、回滚、SAVEPOINT、`AfterCommit` 的真实行为由 `internal/platform/dbtx/dbtx_integration_test.go`
  在真实 Postgres 上验证。
