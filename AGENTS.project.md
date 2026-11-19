# 项目补充规则

本文件归业务项目所有，记录当前项目特有的模块和规则；starter 通用规则见 `AGENTS.md`。

## 当前业务模块

- `product`、`order`、`fulfillment` 是示例业务模块，模块清单和请求链路见 `docs/PROJECT.md`。
- **order/product 是按生产要求实现的参考模块**：幂等、并发安全扣库存、金额溢出检查、
  cursor 分页都不是占位符，不要当作教学代码随意重写。
- 项目特有的 CI 冒烟放在 `project/ci/smoke.sh`，项目模块的 CI 环境变量放在 `project/ci/env`。
