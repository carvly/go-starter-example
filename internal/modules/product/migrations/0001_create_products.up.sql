CREATE TABLE products (
    id UUID,
    name TEXT NOT NULL,
    price_cents BIGINT NOT NULL,
    stock BIGINT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pk_products PRIMARY KEY (id),
    CONSTRAINT ck_products_price_cents CHECK (price_cents > 0),
    CONSTRAINT ck_products_stock CHECK (stock >= 0)
);

-- 服务 List 的 cursor 分页：WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC。
CREATE INDEX idx_products_created_at_id ON products (created_at DESC, id DESC);

COMMENT ON TABLE products IS '商品';
COMMENT ON COLUMN products.id IS '商品 ID，应用层生成的 UUIDv7';
COMMENT ON COLUMN products.name IS '商品名称';
COMMENT ON COLUMN products.price_cents IS '单价，单位：分';
COMMENT ON COLUMN products.stock IS '可售库存数量；扣减用 WHERE stock >= n 的条件 UPDATE，不会为负';
COMMENT ON COLUMN products.version IS '乐观锁版本号：后台编辑名称和单价时校验并加 1；调库存、下单扣减不改它';
COMMENT ON COLUMN products.created_at IS '创建时间';
COMMENT ON COLUMN products.updated_at IS '最后更新时间';
