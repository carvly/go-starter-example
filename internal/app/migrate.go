package app

import (
	"io"

	"app.local/internal/config"
	"app.local/internal/modules/order"
	"app.local/internal/modules/product"
	"app.local/internal/modules/staff"
	"app.local/internal/modules/user"
	"app.local/internal/platform/migrate"
	"app.local/internal/platform/queue"
)

func migrationSources() []migrate.Source {
	return []migrate.Source{
		queue.Migrations,
		product.Migrations,
		order.Migrations,
		staff.Migrations,
		user.Migrations,
	}
}

// RunMigrateCommand 只使用数据库配置，不构造业务模块。
func RunMigrateCommand(args []string, stdout io.Writer) error {
	cfg, err := config.LoadDB()
	if err != nil {
		return err
	}
	return migrate.Run(cfg.DSN, migrationSources(), args[1:], stdout)
}
