//go:build integration

package migrate_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"app.local/internal/platform/migrate"
)

func TestRunnerAndAPICommand(t *testing.T) {
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("app"),
		tcpostgres.WithPassword("app"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	_ = dsn

	platform := fstest.MapFS{
		"migrations/0001_platform.up.sql":   {Data: []byte(`CREATE TABLE platform_items (id int);`)},
		"migrations/0001_platform.down.sql": {Data: []byte(`DROP TABLE platform_items;`)},
	}
	user := fstest.MapFS{
		"migrations/0001_users.up.sql":    {Data: []byte(`CREATE TABLE auth_users (id int);`)},
		"migrations/0001_users.down.sql":  {Data: []byte(`DROP TABLE auth_users;`)},
		"migrations/0002_events.up.sql":   {Data: []byte(`CREATE TABLE auth_events (id int);`)},
		"migrations/0002_events.down.sql": {Data: []byte(`DROP TABLE auth_events;`)},
	}
	runner, err := migrate.NewRunner(dsn, []migrate.Source{{Name: "test_platform", FS: platform}, {Name: "test_user", FS: user}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Up(); err != nil {
		t.Fatal(err)
	}
	versions, err := runner.Versions()
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Version != 1 || versions[1].Version != 2 {
		t.Fatalf("versions = %+v", versions)
	}
	if err := runner.Down("test_user"); err != nil {
		t.Fatal(err)
	}
	versions, err = runner.Versions()
	if err != nil || versions[0].Version != 1 || versions[1].Version != 1 {
		t.Fatalf("down 后 versions = %+v, %v", versions, err)
	}
	if err := runner.Force("test_user", 1); err != nil {
		t.Fatal(err)
	}
	versions, err = runner.Versions()
	if err != nil || versions[1].Version != 1 || versions[1].Dirty {
		t.Fatalf("force 后 versions = %+v, %v", versions, err)
	}
	if err := runner.Up(); err != nil {
		t.Fatal(err)
	}

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "./cmd/api", "migrate", "up")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"DB_DSN="+dsn,
		"CONFIG_FILE=",
		"JWT_SIGNING_KEY=",
		"JWT_KEY_ID=",
		"USER_PUBLIC_BASE_URL=",
		"CORS_ALLOW_ORIGINS=",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("只配 DB_DSN 的 api migrate up 失败：%v\n%s", err, out)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, table := range []string{"users", "staff_accounts", "river_job"} {
		var found *string
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1)`, table).Scan(&found); err != nil {
			t.Fatal(err)
		}
		if found == nil || !strings.Contains(*found, table) {
			t.Errorf("表 %s 不存在", table)
		}
	}
}
