package main

import (
	"fmt"
	"go/format"
	"os"
	"regexp"
	"strings"
)

// removeModuleLines 按硬规则 3 的约定，从组合根三个文件里删掉提到模块的行：import、<m>M、<m>.、
// *<m>.Module、m.<M>.、configs.<M> / c.<M>。
func removeModuleLines(root, module string) error {
	field := strings.ToUpper(module[:1]) + module[1:]
	q := regexp.QuoteMeta(module)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`/internal/modules/` + q + `"`),
		regexp.MustCompile(`\b` + q + `M\b`),
		regexp.MustCompile(`\b` + q + `\.`),
		regexp.MustCompile(`\bm\.` + field + `\.`),
		regexp.MustCompile(`\b(?:configs|c)\.` + field + `\b`),
	}
	for _, name := range []string{"internal/app/modules.go", "internal/app/module_configs.go", "internal/app/migrate.go"} {
		path := root + "/" + name
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var out []string
	lines:
		for line := range strings.SplitSeq(string(data), "\n") {
			for _, re := range patterns {
				if re.MatchString(line) {
					continue lines
				}
			}
			out = append(out, line)
		}
		formatted, err := format.Source([]byte(strings.Join(out, "\n")))
		if err != nil {
			return fmt.Errorf("format %s after removing %s: %w", name, module, err)
		}
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			return err
		}
	}
	return nil
}
