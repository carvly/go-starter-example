package postgres

// Tables 是本模块拥有的表：本模块的 SQL 只能读写这里登记的表，其它模块和 platform
// 不能读写它们。internal/archtest 解析这个声明来检查，所以必须是字符串字面量列表。
var Tables = []string{"orders", "order_processed_events"}
