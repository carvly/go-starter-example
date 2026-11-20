package app

import (
	"errors"

	"app.local/internal/config"
	"app.local/internal/modules/staff"
	"app.local/internal/modules/user"
)

// moduleConfigs 汇总各模块的配置。每个模块在下面三处各占一行（字段、loadModuleConfigs、Redacted），
// 删除演练按行删除。
type moduleConfigs struct {
	User  user.Config
	Staff staff.Config
}

// loadModuleConfigs 一次报出所有模块的配置错误，再检查配置文件里有没有不认识的段。
func loadModuleConfigs(src *config.Source) (moduleConfigs, error) {
	var c moduleConfigs
	if err := errors.Join(
		loadInto(&c.User, user.LoadConfig, src),
		loadInto(&c.Staff, staff.LoadConfig, src),
	); err != nil {
		return moduleConfigs{}, err
	}
	if err := src.Finish(); err != nil {
		return moduleConfigs{}, err
	}
	return c, nil
}

func loadInto[T any](dst *T, load func(*config.Source) (T, error), src *config.Source) error {
	v, err := load(src)
	if err != nil {
		return err
	}
	*dst = v
	return nil
}

// Redacted 是启动日志里的配置快照：基础设施加全部模块，敏感字段已脱敏。
func (c moduleConfigs) Redacted(infra *config.Config) map[string]any {
	out := infra.Redacted()
	for _, m := range []map[string]any{
		c.User.Redacted(),
		c.Staff.Redacted(),
	} {
		config.MergeRedacted(out, m)
	}
	return out
}
