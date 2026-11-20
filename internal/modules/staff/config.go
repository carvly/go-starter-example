package staff

import (
	"fmt"
	"time"

	"app.local/internal/config"
)

// Config 是 staff 模块的会话与日志配置。
type Config struct {
	SessionIdleTimeout time.Duration `yaml:"session_idle_timeout"`
	SessionMaxAge      time.Duration `yaml:"session_max_age"`
	TempPasswordTTL    time.Duration `yaml:"temp_password_ttl"`
	AuditRetention     time.Duration `yaml:"audit_retention"`
}

func DefaultConfig() Config {
	return Config{
		SessionIdleTimeout: 30 * time.Minute,
		SessionMaxAge:      12 * time.Hour,
		TempPasswordTTL:    24 * time.Hour,
		AuditRetention:     180 * 24 * time.Hour,
	}
}

func LoadConfig(src *config.Source) (Config, error) {
	cfg := DefaultConfig()
	if err := src.Section("staff", &cfg); err != nil {
		return Config{}, fmt.Errorf("staff config: %w", err)
	}
	for _, item := range []struct {
		name string
		out  *time.Duration
	}{
		{"STAFF_SESSION_IDLE_TIMEOUT", &cfg.SessionIdleTimeout},
		{"STAFF_SESSION_MAX_AGE", &cfg.SessionMaxAge},
		{"STAFF_TEMP_PASSWORD_TTL", &cfg.TempPasswordTTL},
		{"STAFF_AUDIT_RETENTION", &cfg.AuditRetention},
	} {
		if err := src.Env(item.name, item.out); err != nil {
			return Config{}, fmt.Errorf("staff config: %w", err)
		}
	}
	for name, d := range map[string]time.Duration{
		"STAFF_SESSION_IDLE_TIMEOUT": cfg.SessionIdleTimeout,
		"STAFF_SESSION_MAX_AGE":      cfg.SessionMaxAge,
		"STAFF_TEMP_PASSWORD_TTL":    cfg.TempPasswordTTL,
		"STAFF_AUDIT_RETENTION":      cfg.AuditRetention,
	} {
		if d <= 0 {
			return Config{}, fmt.Errorf("staff config: %s must be > 0, got %s", name, d)
		}
	}
	return cfg, nil
}

// Redacted 是启动日志里的配置快照（staff 没有敏感配置）。
func (c Config) Redacted() map[string]any {
	return map[string]any{
		"staff_session_idle_timeout": c.SessionIdleTimeout.String(),
		"staff_session_max_age":      c.SessionMaxAge.String(),
		"staff_temp_password_ttl":    c.TempPasswordTTL.String(),
		"staff_audit_retention":      c.AuditRetention.String(),
	}
}
