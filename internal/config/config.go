// Package config 实现环境变量优先、可选 YAML 文件覆盖非敏感默认值的配置加载。
// Config 按关注点拆嵌套子结构，不挤成一个扁平 struct。
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 是应用运行需要的全部配置。字段来自环境变量，`CONFIG_FILE`
// 指向的可选 YAML 文件可以覆盖非敏感默认值，环境变量优先级最高。
type Config struct {
	// AppEnv 只读，仅用于给日志/指标打环境标签，代码里不允许拿它做
	// if/switch 分支。
	AppEnv   string `yaml:"app_env"`
	LogLevel string `yaml:"log_level"`

	HTTP   HTTPConfig   `yaml:"http"`
	DB     DBConfig     `yaml:"db"`
	Redis  RedisConfig  `yaml:"redis"`
	CORS   CORSConfig   `yaml:"cors"`
	Argon2 Argon2Params `yaml:"argon2"`
	Source *Source      `yaml:"-"`
}

// HTTPConfig 是 cmd/api 的 HTTP 服务器与安全/流量控制中间件配置。
type HTTPConfig struct {
	Addr string `yaml:"addr"`

	// BodyLimitBytes 是请求体大小上限，默认 2MB。
	BodyLimitBytes int64 `yaml:"body_limit_bytes"`
	// RequestTimeout 包在业务 handler 最内层。
	RequestTimeout time.Duration `yaml:"request_timeout"`

	// HSTSMaxAge 为 0 时关闭 HSTS（本地开发用 HTTP 时不该被强制跳转
	// HTTPS）；生产环境必须显式打开。
	HSTSMaxAge time.Duration `yaml:"hsts_max_age"`

	// TrustedProxy 为 true 时用 echo.ExtractIPFromXFFHeader()（信任回环
	// 地址和内网网段发来的 X-Forwarded-For，适配"反代跟服务在同一个内网/
	// 同一台机器"这类最常见的部署拓扑）；为 false 时用
	// echo.ExtractIPDirect()，完全不信任请求头。这个值不能凭感觉猜，
	// 必须匹配真实部署拓扑——反代来自公网 IP 段这类更复杂的拓扑需要在
	// server.go 里换成 echo.TrustIPRange 显式指定信任网段。
	TrustedProxy bool `yaml:"trusted_proxy"`

	// GlobalRateLimit 是按 IP 的全局限流，单位：次/秒。
	GlobalRateLimitPerSecond float64 `yaml:"global_rate_limit_per_second"`

	// InternalAddr 是 /healthz、/metrics（cmd/worker 场景）监听的内部
	// 端口，不发布到宿主机。
	InternalAddr string `yaml:"internal_addr"`

	// AdminAddr 是后台 server 的监听地址，为空时不启动后台 server。它和 Addr
	// 是两个独立的 Echo 实例：可以只绑内网或 localhost，公网入口上没有 /admin/v1。
	AdminAddr string `yaml:"admin_addr"`
	// AdminOrigin 是后台前端的 origin（如 https://admin.example.com），配置了
	// AdminAddr 时必填：后台会话用 cookie，写请求的 Origin 必须等于它。
	AdminOrigin string `yaml:"admin_origin"`

	// APIDocsEnabled 控制要不要挂 GET /openapi.yaml 和 /docs（Redoc）这组只读
	// 文档路由，默认打开（本地开发/演示环境方便直接打开浏览器看接口）；生产
	// 环境如果不想对外暴露，配置成 false 即可。
	APIDocsEnabled bool `yaml:"api_docs_enabled"`
}

// DBConfig 是 Postgres 连接池配置。默认值是可以直接跑起来的起点，不是
// 最终生产数值——真实并发量、Postgres 实例规格都会影响最终该配多少。
type DBConfig struct {
	DSN               string        `yaml:"dsn"`
	MinConns          int32         `yaml:"min_conns"`
	MaxConns          int32         `yaml:"max_conns"`
	MaxConnLifetime   time.Duration `yaml:"max_conn_lifetime"`
	HealthCheckPeriod time.Duration `yaml:"health_check_period"`
}

// RedisConfig 的 Addr 为空时不连 Redis：缓存退回 Noop、限流按进程内存计数。
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
}

// Argon2Params 对应 argon2id.Params，显式写进配置而不是隐式依赖库默认
// 值——这个值直接决定暴力破解一次哈希要多少内存和时间。
type Argon2Params struct {
	MemoryKiB   uint32 `yaml:"memory_kib"`
	Iterations  uint32 `yaml:"iterations"`
	Parallelism uint8  `yaml:"parallelism"`
	SaltLength  uint32 `yaml:"salt_length"`
	KeyLength   uint32 `yaml:"key_length"`
}

// CORSConfig 的 AllowOrigins 必须显式列出信任域名，不用 "*"。
type CORSConfig struct {
	AllowOrigins []string `yaml:"allow_origins"`
}

// Load 按"环境变量 > 可选 YAML 文件 > 默认值"的优先级加载配置。
// CONFIG_FILE 指向的文件不存在/必需字段缺失时必须直接报错退出，不能
// 静默退化——配置来源被显式指定就必须真的生效。
func Load() (*Config, error) {
	cfg := defaults()
	src, err := loadSource()
	if err != nil {
		return nil, err
	}
	cfg.Source = src
	for _, section := range []struct {
		name string
		out  any
	}{
		{"app_env", &cfg.AppEnv},
		{"log_level", &cfg.LogLevel},
		{"http", &cfg.HTTP},
		{"db", &cfg.DB},
		{"redis", &cfg.Redis},
		{"cors", &cfg.CORS},
		{"argon2", &cfg.Argon2},
	} {
		if err := src.Section(section.name, section.out); err != nil {
			return nil, err
		}
	}
	if err := applyEnvOverrides(cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadDB 只加载迁移命令需要的数据库配置，不校验业务模块。数据库配置的解析与 Load 是同一条路径
// （配置段 db + applyDBEnv），只是不读其余部分。
func LoadDB() (DBConfig, error) {
	src, err := loadSource()
	if err != nil {
		return DBConfig{}, err
	}
	cfg := defaults().DB
	if err := src.Section("db", &cfg); err != nil {
		return DBConfig{}, err
	}
	if err := applyDBEnv(&cfg); err != nil {
		return DBConfig{}, fmt.Errorf("config: %w", err)
	}
	if cfg.DSN == "" {
		return DBConfig{}, fmt.Errorf("config: missing required env var(s): DB_DSN")
	}
	return cfg, nil
}

func defaults() *Config {
	return &Config{
		AppEnv:   "development",
		LogLevel: "info",
		HTTP: HTTPConfig{
			Addr:                     ":8080",
			BodyLimitBytes:           2 * 1024 * 1024,
			RequestTimeout:           10 * time.Second,
			HSTSMaxAge:               0,
			TrustedProxy:             false,
			GlobalRateLimitPerSecond: 20,
			InternalAddr:             ":9090",
			APIDocsEnabled:           true,
		},
		DB: DBConfig{
			MinConns:          2,
			MaxConns:          25,
			MaxConnLifetime:   time.Hour,
			HealthCheckPeriod: time.Minute,
		},
		Argon2: Argon2Params{
			MemoryKiB:   64 * 1024,
			Iterations:  1,
			Parallelism: 2,
			SaltLength:  16,
			KeyLength:   32,
		},
	}
}

// applyEnvOverrides 让环境变量成为优先级最高的配置来源——即使
// CONFIG_FILE 已经覆盖过默认值，环境变量仍然可以再覆盖一次。返回的
// error 汇总了所有解析失败的字段（用 errors.Join，不是遇到第一个就
// 提前退出）——一次性把所有配错的环境变量都报出来，不用改一个、
// 重新启动一次、再发现下一个。
func applyEnvOverrides(cfg *Config) error {
	setString(&cfg.AppEnv, "APP_ENV")
	setString(&cfg.LogLevel, "LOG_LEVEL")
	setString(&cfg.HTTP.Addr, "HTTP_ADDR")
	setString(&cfg.HTTP.InternalAddr, "HTTP_INTERNAL_ADDR")
	setString(&cfg.HTTP.AdminAddr, "HTTP_ADMIN_ADDR")
	setString(&cfg.HTTP.AdminOrigin, "HTTP_ADMIN_ORIGIN")
	setStringSlice(&cfg.CORS.AllowOrigins, "CORS_ALLOW_ORIGINS")
	setString(&cfg.Redis.Addr, "REDIS_ADDR")
	setString(&cfg.Redis.Password, "REDIS_PASSWORD")
	return errors.Join(
		setInt64(&cfg.HTTP.BodyLimitBytes, "HTTP_BODY_LIMIT_BYTES"),
		setDuration(&cfg.HTTP.RequestTimeout, "HTTP_REQUEST_TIMEOUT"),
		setDuration(&cfg.HTTP.HSTSMaxAge, "HTTP_HSTS_MAX_AGE"),
		setBool(&cfg.HTTP.TrustedProxy, "HTTP_TRUSTED_PROXY"),
		setBool(&cfg.HTTP.APIDocsEnabled, "HTTP_API_DOCS_ENABLED"),
		setFloat64(&cfg.HTTP.GlobalRateLimitPerSecond, "HTTP_GLOBAL_RATE_LIMIT_PER_SECOND"),
		applyDBEnv(&cfg.DB),
		setUint32(&cfg.Argon2.MemoryKiB, "ARGON2_MEMORY_KIB"),
		setUint32(&cfg.Argon2.Iterations, "ARGON2_ITERATIONS"),
		setUint8(&cfg.Argon2.Parallelism, "ARGON2_PARALLELISM"),
	)
}

// applyDBEnv 是数据库配置的环境变量覆盖，Load 与 LoadDB 共用。
func applyDBEnv(db *DBConfig) error {
	setString(&db.DSN, "DB_DSN")
	return errors.Join(
		setInt32(&db.MinConns, "DB_MIN_CONNS"),
		setInt32(&db.MaxConns, "DB_MAX_CONNS"),
		setDuration(&db.MaxConnLifetime, "DB_MAX_CONN_LIFETIME"),
		setDuration(&db.HealthCheckPeriod, "DB_HEALTH_CHECK_PERIOD"),
	)
}

// validate 校验所有必需字段非空。缺哪个就报"missing FOO env var"这类
// 明确错误直接让启动失败，不能让零值静默流过去。
func validate(cfg *Config) error {
	var missing []string
	if cfg.DB.DSN == "" {
		missing = append(missing, "DB_DSN")
	}
	if len(cfg.CORS.AllowOrigins) == 0 {
		missing = append(missing, "CORS_ALLOW_ORIGINS")
	}
	if cfg.HTTP.AdminAddr != "" && cfg.HTTP.AdminOrigin == "" {
		missing = append(missing, "HTTP_ADMIN_ORIGIN（配置了 HTTP_ADMIN_ADDR 时必填）")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: missing required env var(s): %s", strings.Join(missing, ", "))
	}

	// ARGON2_ITERATIONS/ARGON2_PARALLELISM 被手滑设成 0 时，
	// golang.org/x/crypto/argon2 会在第一次真正调用哈希时 panic
	// （"number of rounds too small"/"parallelism degree too low"）——
	// 那时候已经是运行期，会被 Recover 中间件接住变成每个
	// 需要哈希密码的请求（员工登录、改密）的 500，而不是启动时就报错退出。这类配置错误必须
	// 在这里挡住，跟其它必需字段缺失是同一类"启动时就要炸出来"的要求。
	if cfg.Argon2.Iterations < 1 {
		return fmt.Errorf("config: ARGON2_ITERATIONS must be >= 1, got %d", cfg.Argon2.Iterations)
	}
	if cfg.Argon2.Parallelism < 1 {
		return fmt.Errorf("config: ARGON2_PARALLELISM must be >= 1, got %d", cfg.Argon2.Parallelism)
	}

	// 下面三项的零值或负数会让服务"看起来正常启动、健康检查也正常"，故障只在真实请求时暴露
	// （BodyLimit 让所有带 body 的写请求 413；限流额度和请求超时没有合理的零值），所以在启动时挡住。
	// 默认值只在 defaults() 一处，下游不再各自回落。
	if cfg.HTTP.BodyLimitBytes <= 0 {
		return fmt.Errorf("config: HTTP_BODY_LIMIT_BYTES must be > 0, got %d", cfg.HTTP.BodyLimitBytes)
	}
	if cfg.HTTP.GlobalRateLimitPerSecond <= 0 {
		return fmt.Errorf("config: HTTP_GLOBAL_RATE_LIMIT_PER_SECOND must be > 0, got %v", cfg.HTTP.GlobalRateLimitPerSecond)
	}
	if cfg.HTTP.RequestTimeout <= 0 {
		return fmt.Errorf("config: HTTP_REQUEST_TIMEOUT must be > 0, got %s", cfg.HTTP.RequestTimeout)
	}
	if cfg.HTTP.AdminAddr != "" && cfg.HTTP.AdminAddr == cfg.HTTP.Addr {
		return fmt.Errorf("config: HTTP_ADMIN_ADDR must differ from HTTP_ADDR (%q): the admin server is a separate listener", cfg.HTTP.Addr)
	}
	if o := cfg.HTTP.AdminOrigin; o != "" && !IsCanonicalOrigin(o) {
		return fmt.Errorf("config: HTTP_ADMIN_ORIGIN must be exactly what browsers send as Origin, like https://admin.example.com "+
			"(lowercase scheme://host[:port], no default port, no path, no user info), got %q", o)
	}
	return nil
}

// IsCanonicalOrigin 判断 o 是否和浏览器发送的 Origin 头逐字节相同。后台按字符串比较
// Origin，配成别的写法（大写、带默认端口、结尾斜杠、路径）时所有写请求都会被拒绝，
// 而读请求看起来一切正常。
func IsCanonicalOrigin(o string) bool {
	u, err := url.Parse(o)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return false
	}
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		return false
	}
	return o == u.Scheme+"://"+strings.ToLower(u.Host)
}

// Redacted 返回敏感字段脱敏成 "***" 的配置快照，供启动时打日志用。
func (c *Config) Redacted() map[string]any {
	return map[string]any{
		"app_env":            c.AppEnv,
		"log_level":          c.LogLevel,
		"http_addr":          c.HTTP.Addr,
		"http_internal_addr": c.HTTP.InternalAddr,
		"http_admin_addr":    c.HTTP.AdminAddr,
		"http_admin_origin":  c.HTTP.AdminOrigin,
		"cors_allow_origins": c.CORS.AllowOrigins,
		"db_dsn":             "***",
		"db_max_conns":       c.DB.MaxConns,
		"redis_addr":         c.Redis.Addr,
		"redis_password":     redactedIfSet(c.Redis.Password),
	}
}

func MergeRedacted(dst, src map[string]any) {
	for key, value := range src {
		dst[key] = value
	}
}

func redactedIfSet(s string) string {
	if s == "" {
		return ""
	}
	return "***"
}

func setString(dst *string, env string) {
	if v, ok := os.LookupEnv(env); ok {
		*dst = v
	}
}

func setStringSlice(dst *[]string, env string) {
	if v, ok := os.LookupEnv(env); ok {
		out := []string{}
		for p := range strings.SplitSeq(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		*dst = out
	}
}

// setStringMap 解析形如 "kid1:secret1,kid2:secret2" 的环境变量成
// map[string]string——目前只有 JWT_ADDITIONAL_KEYS 用这个格式。忽略
// 格式不对的单个 pair（没有冒号），不因为一个 pair 写错就让整个值
// 解析失败——密钥轮换时这个字段本来就允许为空。
func setStringMap(dst *map[string]string, env string) {
	v, ok := os.LookupEnv(env)
	if !ok {
		return
	}
	out := make(map[string]string)
	for pair := range strings.SplitSeq(v, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, val, found := strings.Cut(pair, ":")
		if !found {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(val)
	}
	*dst = out
}

// 下面这些 setXxx 解析失败时都返回一个 error，不能像早期版本那样直接
// 吞掉——环境变量被显式设置但格式不对（比如 HTTP_HSTS_MAX_AGE 抄
// "31536000" 这种裸数字，忘了 time.Duration 要求带单位写成 "31536000s"）
// 曾经会静默保留旧值/默认值，Load() 照样返回 nil error，服务"看起来
// 正常启动"，实际上这个配置项根本没生效。跟必需字段缺失是同一类"配置
// 错误必须在启动时就炸出来"的要求。

func setBool(dst *bool, env string) error {
	v, ok := os.LookupEnv(env)
	if !ok {
		return nil
	}
	switch v {
	case "true", "1":
		*dst = true
	case "false", "0":
		*dst = false
	default:
		return fmt.Errorf("invalid %s=%q: must be true/false/1/0", env, v)
	}
	return nil
}

func setInt32(dst *int32, env string) error {
	v, ok := os.LookupEnv(env)
	if !ok {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid %s=%q: %w", env, v, err)
	}
	*dst = int32(n)
	return nil
}

func setInt64(dst *int64, env string) error {
	v, ok := os.LookupEnv(env)
	if !ok {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid %s=%q: %w", env, v, err)
	}
	*dst = n
	return nil
}

func setUint32(dst *uint32, env string) error {
	v, ok := os.LookupEnv(env)
	if !ok {
		return nil
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid %s=%q: %w", env, v, err)
	}
	*dst = uint32(n)
	return nil
}

func setUint8(dst *uint8, env string) error {
	v, ok := os.LookupEnv(env)
	if !ok {
		return nil
	}
	n, err := strconv.ParseUint(v, 10, 8)
	if err != nil {
		return fmt.Errorf("invalid %s=%q: %w", env, v, err)
	}
	*dst = uint8(n)
	return nil
}

func setFloat64(dst *float64, env string) error {
	v, ok := os.LookupEnv(env)
	if !ok {
		return nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fmt.Errorf("invalid %s=%q: %w", env, v, err)
	}
	*dst = n
	return nil
}

func setDuration(dst *time.Duration, env string) error {
	v, ok := os.LookupEnv(env)
	if !ok {
		return nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf("invalid %s=%q: %w", env, v, err)
	}
	*dst = d
	return nil
}
