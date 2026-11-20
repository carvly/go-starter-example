package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func testCommands(got *[]string) []Command {
	return []Command{
		{Name: "staff create", Summary: "创建员工", Run: func(_ context.Context, args []string, out io.Writer) error {
			*got = args
			_, err := io.WriteString(out, "created\n")
			return err
		}},
		{Name: "staff reset-password", Summary: "重置密码", Run: func(context.Context, []string, io.Writer) error {
			return errors.New("boom")
		}},
		{Name: "user purge", Summary: "清理", Run: func(context.Context, []string, io.Writer) error { return nil }},
	}
}

func run(args ...string) (code int, gotArgs []string, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = Run(context.Background(), "api", testCommands(&gotArgs), args, &out, &errOut)
	return code, gotArgs, out.String(), errOut.String()
}

func TestRun_FindsCommandAndPassesRemainingArgs(t *testing.T) {
	code, args, stdout, stderr := run("staff", "create", "-email", "a@b.c")
	if code != ExitOK {
		t.Fatalf("code = %d, want 0; stderr=%s", code, stderr)
	}
	if !slices.Equal(args, []string{"-email", "a@b.c"}) {
		t.Errorf("args = %q, want 命令名之后的参数", args)
	}
	if stdout != "created\n" || stderr != "" {
		t.Errorf("stdout = %q, stderr = %q：命令结果只写 stdout", stdout, stderr)
	}
}

func TestRun_CommandErrorReturns1(t *testing.T) {
	code, _, stdout, stderr := run("staff", "reset-password")
	if code != ExitError {
		t.Fatalf("code = %d, want 1", code)
	}
	if stdout != "" || !strings.Contains(stderr, "boom") {
		t.Errorf("stdout = %q, stderr = %q：错误写 stderr", stdout, stderr)
	}
}

func TestRun_ModuleOnlyListsThatModule(t *testing.T) {
	code, _, _, stderr := run("staff")
	if code != ExitUsage {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "staff create") || !strings.Contains(stderr, "staff reset-password") || strings.Contains(stderr, "user purge") {
		t.Errorf("只给模块名时应只列该模块的命令，got:\n%s", stderr)
	}
}

func TestRun_UnknownListsAllCommands(t *testing.T) {
	code, _, stdout, stderr := run("nosuch")
	if code != ExitUsage {
		t.Fatalf("code = %d, want 2", code)
	}
	if stdout != "" || !strings.Contains(stderr, "staff create") || !strings.Contains(stderr, "user purge") {
		t.Errorf("找不到命令时应在 stderr 列出全部命令，stdout=%q stderr=\n%s", stdout, stderr)
	}
}

func TestRun_HelpGoesToStdout(t *testing.T) {
	code, _, stdout, _ := run("help")
	if code != ExitOK || !strings.Contains(stdout, "user purge") {
		t.Errorf("code = %d, stdout =\n%s", code, stdout)
	}
}
