-- 前台用户的登录方式、微信各入口 openid、浏览器授权流程的临时数据。
-- 枚举列（provider、kind）按 docs/agents/database.md 用 text + 代码常量，取值写进列注释。

CREATE TABLE user_identities (
    id UUID,
    user_id UUID NOT NULL,
    provider TEXT NOT NULL,
    subject TEXT NOT NULL,
    profile JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pk_user_identities PRIMARY KEY (id),
    CONSTRAINT uk_user_identities_provider_subject UNIQUE (provider, subject),
    CONSTRAINT uk_user_identities_user_provider UNIQUE (user_id, provider)
);

-- (user_id, provider) 的唯一索引前缀覆盖"按 user 列登录方式"的查询，不再另建 (user_id) 索引。

COMMENT ON TABLE user_identities IS '登录方式：一个账号可绑定多个，(provider, subject) 唯一定位';
COMMENT ON COLUMN user_identities.id IS '登录方式 ID（user_identities.id），应用层生成的 UUIDv7';
COMMENT ON COLUMN user_identities.user_id IS '所属账号（users.id），无外键';
COMMENT ON COLUMN user_identities.provider IS '登录方式：email、phone、github、google、apple、wechat';
COMMENT ON COLUMN user_identities.subject IS '该 provider 下稳定的唯一 ID：微信是 unionid';
COMMENT ON COLUMN user_identities.profile IS '第三方的昵称、头像，只作展示；不存第三方邮箱';
COMMENT ON COLUMN user_identities.last_login_at IS '最近一次用该登录方式登录的时间；NULL 表示绑定后还没用过';
COMMENT ON COLUMN user_identities.created_at IS '绑定时间';
COMMENT ON COLUMN user_identities.updated_at IS '最后更新时间';

CREATE TABLE wechat_openids (
    user_id UUID NOT NULL,
    appid TEXT NOT NULL,
    openid TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pk_wechat_openids PRIMARY KEY (user_id, appid),
    CONSTRAINT uk_wechat_openids_appid_openid UNIQUE (appid, openid)
);

-- 主键前缀覆盖"按 user 查 openid"的查询，不再另建 (user_id) 索引。

COMMENT ON TABLE wechat_openids IS '微信各入口的 openid：支付、模板消息按 (appid, openid) 定位收件人';
COMMENT ON COLUMN wechat_openids.user_id IS '所属账号（users.id），无外键';
COMMENT ON COLUMN wechat_openids.appid IS '微信应用 appid：网站应用、服务号、小程序、移动应用各一个';
COMMENT ON COLUMN wechat_openids.openid IS '该 appid 下的 openid';
COMMENT ON COLUMN wechat_openids.created_at IS '首次记录时间';
COMMENT ON COLUMN wechat_openids.updated_at IS '最后更新时间';

CREATE TABLE oauth_states (
    id_hash TEXT,
    kind TEXT NOT NULL,
    provider TEXT NOT NULL,
    payload JSONB NOT NULL,
    binding_hash TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pk_oauth_states PRIMARY KEY (id_hash)
);

-- 清理任务按过期时间扫全表，不用状态列。
CREATE INDEX idx_oauth_states_expires_at ON oauth_states (expires_at);

COMMENT ON TABLE oauth_states IS '授权流程的临时数据：浏览器 state、登录兑换 result、原生登录的一次性 nonce；一次性读取用 DELETE ... RETURNING';
COMMENT ON COLUMN oauth_states.id_hash IS 'state/兑换码/nonce 的摘要，主键';
COMMENT ON COLUMN oauth_states.kind IS '取值 state、result、nonce';
COMMENT ON COLUMN oauth_states.provider IS '路由里的 provider（如 wechat-web）；nonce 绑定签发时的 provider';
COMMENT ON COLUMN oauth_states.payload IS '本次流程的上下文：intent、sid、return_to、兑换结果、PKCE verifier、nonce';
COMMENT ON COLUMN oauth_states.binding_hash IS '浏览器绑定 cookie 的摘要；nonce 没有';
COMMENT ON COLUMN oauth_states.expires_at IS '过期时间，到期由定时任务删除';
COMMENT ON COLUMN oauth_states.created_at IS '写入时间';
