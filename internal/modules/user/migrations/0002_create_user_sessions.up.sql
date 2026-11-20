-- 前台用户的登录会话，规则见 docs/agents/auth.md 的"会话"。登录时签发，每个请求按 sid
-- 校验；吊销（登出、踢下线、停用、注销）只标记 revoked_at，user 的定时清理任务再物理删除
-- 已过期、以及吊销超过保留期的行。
CREATE TABLE user_sessions (
    id UUID,
    user_id UUID NOT NULL,
    client TEXT NOT NULL,
    amr TEXT[] NOT NULL,
    login_identity_id UUID,
    auth_time TIMESTAMPTZ NOT NULL,
    refresh_family TEXT NOT NULL,
    refresh_token_hash TEXT NOT NULL,
    previous_refresh_token_hash TEXT,
    rotated_at TIMESTAMPTZ,
    device_name TEXT NOT NULL DEFAULT '',
    ip TEXT NOT NULL,
    user_agent TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT pk_user_sessions PRIMARY KEY (id),
    CONSTRAINT uk_user_sessions_refresh_family UNIQUE (refresh_family)
);

-- 服务"列出某用户的会话"和"吊销某用户的全部会话"（停用、注销、踢下线）。
CREATE INDEX idx_user_sessions_user_id ON user_sessions (user_id);

COMMENT ON TABLE user_sessions IS '前台用户的登录会话；吊销时标记 revoked_at，过期和吊销超过保留期的行由定时任务删除';
COMMENT ON COLUMN user_sessions.id IS '会话 ID（sid），应用层生成的 UUIDv7';
COMMENT ON COLUMN user_sessions.user_id IS '用户 ID（users.id），无外键';
COMMENT ON COLUMN user_sessions.client IS '客户端；取值 web、app、miniprogram';
COMMENT ON COLUMN user_sessions.amr IS '本会话用过的验证方式，如 otp_sms、wechat';
COMMENT ON COLUMN user_sessions.login_identity_id IS '登录用的登录方式（user_identities.id），解绑它时吊销本会话；NULL 表示没有';
COMMENT ON COLUMN user_sessions.auth_time IS '最近一次验证身份的时间（登录或重新验证）';
COMMENT ON COLUMN user_sessions.refresh_family IS 'refresh token 的定位键，只出现在 refresh token 里';
COMMENT ON COLUMN user_sessions.refresh_token_hash IS '当前 refresh token 的摘要，库里不存明文';
COMMENT ON COLUMN user_sessions.previous_refresh_token_hash IS '上一代 refresh token 的摘要，用于发现重放；NULL 表示还没轮换过';
COMMENT ON COLUMN user_sessions.rotated_at IS '最近一次轮换 refresh token 的时间；NULL 表示还没轮换过';
COMMENT ON COLUMN user_sessions.device_name IS '设备名';
COMMENT ON COLUMN user_sessions.ip IS '登录时的客户端 IP';
COMMENT ON COLUMN user_sessions.user_agent IS '登录时的 User-Agent';
COMMENT ON COLUMN user_sessions.created_at IS '登录时间';
COMMENT ON COLUMN user_sessions.updated_at IS '最后更新时间';
COMMENT ON COLUMN user_sessions.last_used_at IS '最后使用时间';
COMMENT ON COLUMN user_sessions.expires_at IS '绝对过期时间';
COMMENT ON COLUMN user_sessions.revoked_at IS '吊销时间；NULL 表示未吊销';
