package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"app.local/internal/config"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DSN", "postgres://user:pass@localhost:5432/app")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://example.com")
}

func TestLoad_MissingRequiredFields(t *testing.T) {
	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing required fields error")
	}
}

func TestLoad_Argon2ParamsZero_Rejected(t *testing.T) {
	for _, env := range []string{"ARGON2_ITERATIONS", "ARGON2_PARALLELISM"} {
		t.Run(env, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(env, "0")
			if _, err := config.Load(); err == nil {
				t.Fatalf("Load() error = nil, want %s=0 rejected", env)
			}
		})
	}
}

func TestLoad_BodyLimitBytesZero_Rejected(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("HTTP_BODY_LIMIT_BYTES", "0")
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() error = nil, want HTTP_BODY_LIMIT_BYTES=0 rejected")
	}
}

func TestLoad_MalformedEnvVar_Rejected(t *testing.T) {
	t.Run("duration without unit", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("HTTP_HSTS_MAX_AGE", "31536000")
		if _, err := config.Load(); err == nil {
			t.Fatal("Load() error = nil, want malformed duration rejected")
		}
	})
	t.Run("multiple malformed vars are all reported", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("HTTP_HSTS_MAX_AGE", "31536000")
		t.Setenv("DB_MAX_CONNS", "not-a-number")
		_, err := config.Load()
		if err == nil || !strings.Contains(err.Error(), "HTTP_HSTS_MAX_AGE") || !strings.Contains(err.Error(), "DB_MAX_CONNS") {
			t.Fatalf("Load() error = %v, want both malformed vars", err)
		}
	})
}

func TestLoad_DefaultsAndRequiredFields(t *testing.T) {
	setRequiredEnv(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Addr != ":8080" || cfg.HTTP.BodyLimitBytes != 2*1024*1024 || !cfg.HTTP.APIDocsEnabled {
		t.Errorf("HTTP defaults = %+v", cfg.HTTP)
	}
	if cfg.DB.MinConns != 2 || cfg.DB.MaxConns != 25 {
		t.Errorf("DB defaults = %+v", cfg.DB)
	}
	if cfg.Argon2.MemoryKiB != 64*1024 {
		t.Errorf("Argon2.MemoryKiB = %d, want 65536", cfg.Argon2.MemoryKiB)
	}
	if len(cfg.CORS.AllowOrigins) != 1 || cfg.CORS.AllowOrigins[0] != "https://example.com" {
		t.Errorf("CORS = %v", cfg.CORS.AllowOrigins)
	}
}

func TestLoad_EnvOverridesDefaultAndConfigFile(t *testing.T) {
	setRequiredEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("http:\n  addr: \":7070\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", path)
	t.Setenv("HTTP_ADDR", ":6060")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Addr != ":6060" {
		t.Errorf("HTTP.Addr = %q, want :6060", cfg.HTTP.Addr)
	}
}

func TestLoad_ConfigFileNotFound(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() error = nil, want unreadable CONFIG_FILE rejected")
	}
}

func TestLoad_ConfigFileStrict_Rejected(t *testing.T) {
	cases := map[string]string{
		"未知字段": "http:\n  addr: \":7070\"\n  request_timout: 5s\n",
		"重复键":  "http:\n  addr: \":7070\"\n  addr: \":7071\"\n",
		"多文档":  "http:\n  addr: \":7070\"\n---\nhttp:\n  addr: \":7071\"\n",
		"空文件":  "",
		"只有注释": "# comment\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			setRequiredEnv(t)
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CONFIG_FILE", path)
			if _, err := config.Load(); err == nil {
				t.Fatal("Load() error = nil, want strict YAML error")
			}
		})
	}
}

func TestSource_FinishRejectsUnclaimedSection(t *testing.T) {
	setRequiredEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("removed:\n  value: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", path)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Source.Finish(); err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("Finish() error = %v, want unclaimed removed section", err)
	}
}

func TestLoadDB_OnlyRequiresDatabase(t *testing.T) {
	t.Setenv("DB_DSN", "postgres://user:pass@localhost:5432/app")
	cfg, err := config.LoadDB()
	if err != nil || cfg.DSN == "" {
		t.Fatalf("LoadDB = %+v, %v", cfg, err)
	}
}

func TestConfig_Redacted_HidesSecrets(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("REDIS_PASSWORD", "secret")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	redacted := cfg.Redacted()
	if redacted["db_dsn"] != "***" || redacted["redis_password"] != "***" {
		t.Errorf("Redacted = %v", redacted)
	}
}

func TestLoad_AdminServer(t *testing.T) {
	t.Run("默认不启动后台 server", func(t *testing.T) {
		setRequiredEnv(t)
		cfg, err := config.Load()
		if err != nil || cfg.HTTP.AdminAddr != "" {
			t.Fatalf("AdminAddr = %q, err = %v", cfg.HTTP.AdminAddr, err)
		}
	})
	t.Run("配置了地址必须配置 origin", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("HTTP_ADMIN_ADDR", "127.0.0.1:8081")
		if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "HTTP_ADMIN_ORIGIN") {
			t.Fatalf("Load() error = %v", err)
		}
	})
	t.Run("origin 必须规范", func(t *testing.T) {
		for _, origin := range []string{"https://admin.example.com/", "https://Admin.example.com", "https://admin.example.com:443"} {
			setRequiredEnv(t)
			t.Setenv("HTTP_ADMIN_ADDR", "127.0.0.1:8081")
			t.Setenv("HTTP_ADMIN_ORIGIN", origin)
			if _, err := config.Load(); err == nil {
				t.Errorf("HTTP_ADMIN_ORIGIN=%q 应被拒绝", origin)
			}
		}
	})
	t.Run("后台地址不能和前台相同", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("HTTP_ADDR", ":8080")
		t.Setenv("HTTP_ADMIN_ADDR", ":8080")
		t.Setenv("HTTP_ADMIN_ORIGIN", "https://admin.example.com")
		if _, err := config.Load(); err == nil {
			t.Fatal("相同地址应被拒绝")
		}
	})
}

func TestLoad_APIDocsEnabled_CanBeDisabledByEnv(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("HTTP_API_DOCS_ENABLED", "false")
	cfg, err := config.Load()
	if err != nil || cfg.HTTP.APIDocsEnabled {
		t.Fatalf("APIDocsEnabled = %v, err = %v", cfg.HTTP.APIDocsEnabled, err)
	}
}

func TestLoad_DBMaxConnLifetimeEnv(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DB_MAX_CONN_LIFETIME", "2h")
	cfg, err := config.Load()
	if err != nil || cfg.DB.MaxConnLifetime != 2*time.Hour {
		t.Fatalf("MaxConnLifetime = %v, err = %v", cfg.DB.MaxConnLifetime, err)
	}
}

// LoadDB 与 Load 共用数据库配置的解析：连接池参数的环境变量覆盖对 migrate 命令同样生效。
func TestLoadDB_HonorsPoolEnv(t *testing.T) {
	t.Setenv("DB_DSN", "postgres://user:pass@localhost:5432/app")
	t.Setenv("DB_MAX_CONNS", "7")
	t.Setenv("DB_MAX_CONN_LIFETIME", "5m")
	cfg, err := config.LoadDB()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != 7 || cfg.MaxConnLifetime != 5*time.Minute {
		t.Fatalf("LoadDB = %+v, want MaxConns=7 MaxConnLifetime=5m", cfg)
	}
}

func TestLoad_RateLimitAndTimeoutMustBePositive(t *testing.T) {
	for _, env := range []string{"HTTP_GLOBAL_RATE_LIMIT_PER_SECOND", "HTTP_REQUEST_TIMEOUT"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("DB_DSN", "postgres://user:pass@localhost:5432/app")
			t.Setenv("CORS_ALLOW_ORIGINS", "https://example.com")
			value := "0"
			if env == "HTTP_REQUEST_TIMEOUT" {
				value = "0s"
			}
			t.Setenv(env, value)
			if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), env) {
				t.Fatalf("err = %v, want 提到 %s", err, env)
			}
		})
	}
}
