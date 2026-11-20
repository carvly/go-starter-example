package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	versionHeadingRe = regexp.MustCompile(`^## (v\d+\.\d+\.\d+)\b`)
	noteHeadingRe    = regexp.MustCompile(`^#### (合并前|合并后迁移前|迁移后|部署后)\s*$`)
	semverRe         = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)
)

// noteOrder 是升级说明小节的固定顺序，也是机器读取的标题（见 docs/agents/upgrade.md）。
var noteOrder = []string{"合并前", "合并后迁移前", "迁移后", "部署后"}

// upgradeNotes 汇总 starter-changelog.md 里 base < 版本 <= target 的升级说明，按小节合并。base 不是版本号（基线只有
// merge-base 提交）时取 target 及以前的全部版本：宁可多给人看，不漏掉手动事项。
func upgradeNotes(changelog, base, target string) map[string]string {
	out := map[string][]string{}
	inRange, section := false, ""
	for _, line := range strings.Split(changelog, "\n") {
		if m := versionHeadingRe.FindStringSubmatch(line); m != nil {
			inRange = versionInRange(m[1], base, target)
			section = ""
			continue
		}
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			section = ""
			continue
		}
		if m := noteHeadingRe.FindStringSubmatch(line); m != nil {
			section = m[1]
			continue
		}
		if strings.HasPrefix(line, "#### ") {
			section = ""
			continue
		}
		if inRange && section != "" && strings.TrimSpace(line) != "" {
			out[section] = append(out[section], line)
		}
	}
	notes := map[string]string{}
	for k, lines := range out {
		notes[k] = strings.Join(lines, "\n")
	}
	return notes
}

func versionInRange(v, base, target string) bool {
	vv, ok := parseVersion(v)
	tv, tok := parseVersion(target)
	if !ok || !tok || compareVersion(vv, tv) > 0 {
		return false
	}
	bv, bok := parseVersion(base)
	if !bok {
		return true
	}
	return compareVersion(vv, bv) > 0
}

func parseVersion(v string) ([3]int, bool) {
	m := semverRe.FindStringSubmatch(v)
	if m == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i := range out {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out, true
}

func compareVersion(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			return a[i] - b[i]
		}
	}
	return 0
}

func printNotes(notes map[string]string) {
	if len(notes) == 0 {
		fmt.Println("本次升级没有需要人工处理的事项。")
		return
	}
	for _, k := range noteOrder {
		if n := notes[k]; n != "" {
			fmt.Printf("升级说明 · %s：\n%s\n", k, n)
		}
	}
}
