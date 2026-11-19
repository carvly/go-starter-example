package order

import (
	"embed"

	"app.local/internal/platform/migrate"
)

//go:embed migrations
var migrationFiles embed.FS

var Migrations = migrate.Source{Name: "order", FS: migrationFiles}
