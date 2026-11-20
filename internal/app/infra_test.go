package app

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"app.local/internal/config"
)

// pgxpool.NewWithConfig 只解析配置、不立即连接，所以这些测试不需要真实 Postgres。

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func baseTestConfig() *config.Config {
	return &config.Config{
		DB: config.DBConfig{DSN: "postgres://app:app@127.0.0.1:5432/app?sslmode=disable"},
		Argon2: config.Argon2Params{
			MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		},
		HTTP: config.HTTPConfig{
			BodyLimitBytes:           2 * 1024 * 1024,
			RequestTimeout:           5 * time.Second,
			GlobalRateLimitPerSecond: 1000,
		},
		CORS: config.CORSConfig{AllowOrigins: []string{"https://example.com"}},
	}
}

func testModuleConfigs(t *testing.T) moduleConfigs {
	t.Helper()
	t.Setenv("JWT_SIGNING_KEY", "test-signing-key")
	t.Setenv("JWT_KEY_ID", "test-kid")
	t.Setenv("USER_PUBLIC_BASE_URL", "https://api.example.com")
	configs, err := loadModuleConfigs(config.NewSource())
	if err != nil {
		t.Fatal(err)
	}
	return configs
}

func TestNewInfra_WithoutRedis(t *testing.T) {
	in, err := NewInfra(t.Context(), baseTestConfig(), discardLogger())
	if err != nil {
		t.Fatalf("NewInfra() error = %v", err)
	}
	defer in.Close()

	if in.Redis != nil {
		t.Error("没有配置 REDIS_ADDR 时不应该构造 Redis 客户端")
	}
	if len(in.Pingers) != 1 {
		t.Errorf("len(Pingers) = %d, want 1（只有数据库）", len(in.Pingers))
	}
}

func TestNewInfra_WithRedis_RegistersPinger(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := baseTestConfig()
	cfg.Redis.Addr = mr.Addr()

	in, err := NewInfra(t.Context(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("NewInfra() error = %v", err)
	}
	defer in.Close()

	if in.Redis == nil {
		t.Fatal("配置了 REDIS_ADDR 就应该构造 Redis 客户端")
	}
	if len(in.Pingers) != 2 {
		t.Fatalf("len(Pingers) = %d, want 2（数据库 + Redis）", len(in.Pingers))
	}
	if err := in.Pingers[1].Ping(t.Context()); err != nil {
		t.Errorf("Redis Pinger 应该能 Ping 通 miniredis: %v", err)
	}
}
