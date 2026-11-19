-- 去重表：同一个 order_id 只能 INSERT 成功一次，让扣库存这类非幂等操作在事件重放时只执行一次。
CREATE TABLE order_processed_events (
    order_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pk_order_processed_events PRIMARY KEY (order_id)
);

COMMENT ON TABLE order_processed_events IS '已处理的订单事件，用于消费方幂等去重';
COMMENT ON COLUMN order_processed_events.order_id IS '订单 ID（orders.id），无外键';
COMMENT ON COLUMN order_processed_events.created_at IS '首次处理时间';
