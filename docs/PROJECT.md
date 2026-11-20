# 当前项目模块与链路

本文件归业务项目所有；starter 通用架构见 `docs/ARCHITECTURE.md`。

## 模块清单

- `user`：前台用户、登录、会话、注销和前台鉴权中间件。
- `staff`：后台员工、角色权限、操作日志和后台鉴权。
- `product`：商品（带缓存）。
- `order`：订单，发布 `order_created` 事件。
- `fulfillment`：消费 `order_created`，扣库存并确认或收口订单。

## 下单与事件链路

**下单**：`POST /v1/orders` → `httpserver` 的全局中间件链 → order 模块按 tag 生成的路由 →
`httpapi.Handler.CreateOrder` → `domain.Service.Create`：经 `ProductLookup`（组合根用
product 的 Service 适配）查单价、算总价，在一个事务里写订单并经 `queue.Inserter`
入队 `order_created`（带 request_id）。返回时订单是 `pending`。

**确认**：worker 进程的 river client 取到任务 → `fulfillment/jobs` 重建带 request_id 的
logger → `fulfillment.Process.OnOrderCreated` 在一个事务里：写去重记录（重放直接跳过）→
扣库存（组合根适配器把“库存不足/商品不存在”翻译成永久失败）→ 确认订单或收口为 failed。
临时故障返回错误交给 river 重试。

## 业务后台页面

- `/products`：商品列表与编辑；`product:read` 可读，`product:create`、`product:update`、
  `product:adjust_stock` 分别控制新建、编辑和调库存。
- `/orders`、`/orders/$id`：`order:read` 可查看订单列表和详情。

## 拆服务时的项目特有前置项

fulfillment 目前在一个数据库事务里去重、扣 product 库存、确认 order。拆到不同数据库前要改成
跨服务 Saga：product 消费 `order_created` 扣库存后发 `stock_reserved` / `stock_rejected`，order
再确认或收口；各自维护去重表，需要时加补偿事件。
