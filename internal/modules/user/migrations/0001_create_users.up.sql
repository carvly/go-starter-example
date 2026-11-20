CREATE TABLE users (
    id UUID,
    display_name TEXT NOT NULL,
    avatar_url TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    last_login_at TIMESTAMPTZ NOT NULL,
    signup_client TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ck_users_display_name CHECK (char_length(display_name) <= 64),
    CONSTRAINT ck_users_avatar_url CHECK (char_length(avatar_url) <= 1024),
    CONSTRAINT pk_users PRIMARY KEY (id)
);

-- 服务后台用户列表的排序：ORDER BY <排序字段> DESC, id DESC LIMIT/OFFSET
CREATE INDEX idx_users_created_at_id ON users (created_at DESC, id DESC);
CREATE INDEX idx_users_last_login_at_id ON users (last_login_at DESC, id DESC);

COMMENT ON TABLE users IS '用户：前台用户。账号由登录创建，后台只能查看、停用和启用';
COMMENT ON COLUMN users.id IS '用户 ID，应用层生成的 UUIDv7';
COMMENT ON COLUMN users.display_name IS '昵称';
COMMENT ON COLUMN users.avatar_url IS '头像';
COMMENT ON COLUMN users.status IS '状态；取值 active 正常、disabled 已停用、deleted 已注销';
COMMENT ON COLUMN users.last_login_at IS '最近登录时间';
COMMENT ON COLUMN users.signup_client IS '建号时的客户端；取值 web、app、miniprogram';
COMMENT ON COLUMN users.created_at IS '创建时间';
COMMENT ON COLUMN users.updated_at IS '最后更新时间';
