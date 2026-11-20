package migrate_test

import (
	"io"
	"strings"
	"testing"
	"testing/fstest"

	"app.local/internal/platform/migrate"
)

func sources() []migrate.Source {
	return []migrate.Source{{Name: "platform", FS: fstest.MapFS{}}, {Name: "user", FS: fstest.MapFS{}}}
}

func TestRunRejectsGlobalDown(t *testing.T) {
	err := migrate.Run("postgres://unused", sources(), []string{"down"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-module") {
		t.Fatalf("down error = %v, want -module requirement", err)
	}
}

func TestNewRunnerRejectsDuplicateSources(t *testing.T) {
	_, err := migrate.NewRunner("postgres://unused", []migrate.Source{{Name: "user", FS: fstest.MapFS{}}, {Name: "user", FS: fstest.MapFS{}}})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error = %v, want duplicate source", err)
	}
}
