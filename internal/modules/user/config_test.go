package user

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"app.local/internal/config"
)

func setAuthEnv(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_SIGNING_KEY", "test-signing-key")
	t.Setenv("JWT_KEY_ID", "test-kid")
	t.Setenv("USER_PUBLIC_BASE_URL", "https://api.example.com")
	t.Setenv("WECHAT_WEB_APP_ID", "web-appid")
	t.Setenv("WECHAT_WEB_SECRET", "web-secret")
	t.Setenv("WECHAT_MP_APP_ID", "mp-appid")
	t.Setenv("WECHAT_MP_SECRET", "mp-secret")
	t.Setenv("WECHAT_MINI_APP_ID", "mini-appid")
	t.Setenv("WECHAT_MINI_SECRET", "mini-secret")
	t.Setenv("WECHAT_APP_APP_ID", "app-appid")
	t.Setenv("WECHAT_APP_SECRET", "app-secret")
}

func TestLoadConfig_RequiredAndDefaults(t *testing.T) {
	setAuthEnv(t)
	cfg, err := LoadConfig(config.NewSource())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionMaxAge != DefaultConfig().SessionMaxAge || cfg.AccessTokenTTL != DefaultConfig().AccessTokenTTL {
		t.Errorf("session defaults = %+v", cfg)
	}
	if !cfg.WeChat.Web.Enabled() || !cfg.WeChat.MP.Enabled() || !cfg.WeChat.Mini.Enabled() || !cfg.WeChat.App.Enabled() {
		t.Errorf("微信入口没有全部启用：%+v", cfg.WeChat)
	}
}

func TestLoadConfig_MissingRequired(t *testing.T) {
	if _, err := LoadConfig(config.NewSource()); err == nil {
		t.Fatal("LoadConfig() error = nil, want missing required fields")
	}
}

func TestLoadConfig_JWTAdditionalKeys(t *testing.T) {
	setAuthEnv(t)
	t.Setenv("JWT_ADDITIONAL_KEYS", "old-1:secret-1, old-2:secret-2")
	cfg, err := LoadConfig(config.NewSource())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.JWTAdditionalKeys) != 2 || cfg.JWTAdditionalKeys["old-1"] != "secret-1" {
		t.Errorf("JWTAdditionalKeys = %v", cfg.JWTAdditionalKeys)
	}
}

func TestLoadConfig_WeChatEntriesOptionalButMustBeComplete(t *testing.T) {
	setAuthEnv(t)
	for _, key := range []string{"WECHAT_WEB_APP_ID", "WECHAT_WEB_SECRET", "WECHAT_MP_APP_ID", "WECHAT_MP_SECRET", "WECHAT_APP_APP_ID", "WECHAT_APP_SECRET"} {
		t.Setenv(key, "")
	}
	cfg, err := LoadConfig(config.NewSource())
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.WeChat.Mini.Enabled() || cfg.WeChat.Web.Enabled() || cfg.WeChat.MP.Enabled() || cfg.WeChat.App.Enabled() {
		t.Errorf("enabled entries = %+v", cfg.WeChat)
	}
	t.Setenv("WECHAT_MP_APP_ID", "mp-only")
	if _, err := LoadConfig(config.NewSource()); err == nil || !strings.Contains(err.Error(), "WECHAT_MP") {
		t.Fatalf("half-configured error = %v", err)
	}
}

func TestLoadConfig_RejectsInvalidSessionAndOrigin(t *testing.T) {
	setAuthEnv(t)
	t.Setenv("USER_ACCESS_TOKEN_TTL", "0s")
	if _, err := LoadConfig(config.NewSource()); err == nil {
		t.Fatal("USER_ACCESS_TOKEN_TTL=0 should fail")
	}
	setAuthEnv(t)
	t.Setenv("USER_PUBLIC_BASE_URL", "https://api.example.com/")
	if _, err := LoadConfig(config.NewSource()); err == nil {
		t.Fatal("non-canonical public base URL should fail")
	}
}

func TestLoadConfig_SecretInYAMLRejected(t *testing.T) {
	setAuthEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("user:\n  jwt_signing_key: leaked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", path)
	t.Setenv("DB_DSN", "postgres://unused")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://example.com")
	infra, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(infra.Source); err == nil {
		t.Fatal("JWT secret in YAML should fail")
	}
}

func TestConfig_Redacted(t *testing.T) {
	setAuthEnv(t)
	cfg, err := LoadConfig(config.NewSource())
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Redacted()["jwt_signing_key"]; got != "***" {
		t.Errorf("jwt_signing_key = %v", got)
	}
}
