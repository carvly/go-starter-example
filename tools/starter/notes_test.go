package main

import "testing"

const changelogFixture = `# 更新日志

## v0.3.0 — 2026-12-01

### 升级说明

#### 合并后迁移前

- 三的迁移前事项

## v0.2.0 — 2026-11-25

### 改动

- 普通改动，不是升级说明

## v0.1.0 — 2026-11-20

### 升级说明

#### 合并前

- 一的合并前事项

#### 迁移后

- 一的迁移后事项
`

func TestUpgradeNotes(t *testing.T) {
	tests := []struct {
		name, base, target string
		want               map[string]string
	}{
		{"只取 base 之后的版本", "v0.1.0", "v0.3.0", map[string]string{"合并后迁移前": "- 三的迁移前事项"}},
		{"目标版本没有升级说明", "v0.1.0", "v0.2.0", map[string]string{}},
		{"跨多个版本合并小节", "v0.0.9", "v0.3.0", map[string]string{"合并前": "- 一的合并前事项", "合并后迁移前": "- 三的迁移前事项", "迁移后": "- 一的迁移后事项"}},
		{"base 不是版本号时取目标及以前的全部版本", "abc1234", "v0.3.0", map[string]string{"合并前": "- 一的合并前事项", "合并后迁移前": "- 三的迁移前事项", "迁移后": "- 一的迁移后事项"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := upgradeNotes(changelogFixture, tt.base, tt.target)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
