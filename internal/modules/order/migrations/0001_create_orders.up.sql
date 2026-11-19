CREATE TABLE orders (
    id UUID,
    customer_name TEXT NOT NULL,
    product_id UUID NOT NULL,
    product_name TEXT NOT NULL,
    quantity BIGINT NOT NULL,
    unit_price_cents BIGINT NOT NULL,
    total_cents BIGINT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pk_orders PRIMARY KEY (id),
    CONSTRAINT ck_orders_quantity CHECK (quantity > 0),
    CONSTRAINT ck_orders_unit_price_cents CHECK (unit_price_cents > 0),
    CONSTRAINT ck_orders_total_cents CHECK (total_cents > 0)
);

-- 服务 List 的 cursor 分页：WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC。
CREATE INDEX idx_orders_created_at_id ON orders (created_at DESC, id DESC);

-- 服务后台按状态筛选的列表：WHERE status = $1 [AND created_at ...] ORDER BY created_at DESC, id DESC。
-- 不筛状态时用上面的 idx_orders_created_at_id。
CREATE INDEX idx_orders_status_created_at_id ON orders (status, created_at DESC, id DESC);

COMMENT ON TABLE orders IS '订单';
COMMENT ON COLUMN orders.id IS '订单 ID，应用层生成的 UUIDv7';
COMMENT ON COLUMN orders.customer_name IS '下单客户名称';
COMMENT ON COLUMN orders.product_id IS '商品 ID（products.id），无外键，由 order 模块经适配器校验存在';
COMMENT ON COLUMN orders.product_name IS '下单时的商品名称快照，不随商品改名变化';
COMMENT ON COLUMN orders.quantity IS '购买数量';
COMMENT ON COLUMN orders.unit_price_cents IS '下单时的单价快照，单位：分';
COMMENT ON COLUMN orders.total_cents IS '订单总额（unit_price_cents * quantity），单位：分';
COMMENT ON COLUMN orders.status IS '订单状态：pending（待处理）、confirmed（已扣库存）、failed（处理失败）';
COMMENT ON COLUMN orders.created_at IS '下单时间';
COMMENT ON COLUMN orders.updated_at IS '最后更新时间';
