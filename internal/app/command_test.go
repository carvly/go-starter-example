package app

import (
	"bytes"
	"log/slog"
	"testing"

	"app.local/internal/platform/cli"
)

// 列出命令不能依赖数据库：部署前排查、写脚本时都可能在连不上库的地方运行。
// NewInfra 将来如果开始在构造时连库，这个测试会失败。
func TestRunCommand_ListsCommandsWithoutDatabase(t *testing.T) {
	defaultLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(defaultLogger) })
	t.Setenv("JWT_SIGNING_KEY", "test-signing-key")
	t.Setenv("JWT_KEY_ID", "test-kid")
	t.Setenv("USER_PUBLIC_BASE_URL", "https://api.example.com")
	cfg := baseTestConfig()
	cfg.DB.DSN = "postgres://app:app@127.0.0.1:1/app?sslmode=disable&connect_timeout=1"

	var stdout, stderr bytes.Buffer
	code := RunCommand(t.Context(), cfg, []string{"nosuch"}, &stdout, &stderr)
	if code != cli.ExitUsage {
		t.Fatalf("RunCommand(nosuch) = %d, want %d; stderr:\n%s", code, cli.ExitUsage, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("用法错误不应写 stdout，got %q", stdout.String())
	}
}
