#!/usr/bin/env bash
# 列出可以升级的版本，只读。make upgrade 只处理其中兼容范围内的 Go 直接依赖和后台前端
# 依赖；其余（工具链、Makefile 固定的工具、镜像、跨大版本）写在多处或可能不兼容，要人改，
# 做法见 docs/agents/deploy-ci.md 的"依赖升级"。

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
. scripts/lib-deps.sh

echo "== Go 直接依赖（兼容范围内；发布不满 ${COOLDOWN_DAYS} 天的标为冷却中，make upgrade 会跳过）"
updates="$(go_direct_updates)"
if [ -z "$updates" ]; then
	echo "  全部是最新"
else
	echo "$updates" | while read -r path cur next released; do
		tag=""
		if [[ "$released" > "$(cooldown_cutoff)" ]]; then tag="  [冷却中]"; fi
		printf '  %-58s %s -> %s  (%s)%s\n' "$path" "$cur" "$next" "${released%%T*}" "$tag"
		printf '    %s\n' "$(changelog_url "$path")"
	done
fi
echo "  跨大版本（模块路径换成 /vN）不在上面，按需到各项目的发布页确认。"

echo
echo "== Go 工具链（go.mod 的 go 行，同步改 CI、Dockerfile，见 scripts/check-versions.sh）"
cur_go="go$(sed -n 's/^go \([0-9.]*\)$/\1/p' go.mod)"
latest_go="$(curl -fsS -m 10 'https://go.dev/VERSION?m=text' 2>/dev/null | head -1 || true)"
printf '  %s -> %s\n' "$cur_go" "${latest_go:-（查询失败）}"
echo "    https://go.dev/doc/devel/release"

echo
echo "== Makefile 固定版本的工具（同步改 CI，见 scripts/check-versions.sh）"
pinned_tool "golangci-lint" "github.com/golangci/golangci-lint/v2" "$(sed -n 's/^GOLANGCI_LINT_VERSION := //p' Makefile)"
pinned_tool "govulncheck" "golang.org/x/vuln" "$(sed -n 's/^GOVULNCHECK := .*govulncheck@\(v[0-9.]*\).*/\1/p' Makefile)"
pinned_tool "actionlint" "github.com/rhysd/actionlint" "$(sed -n 's/^ACTIONLINT := .*actionlint@\(v[0-9.]*\).*/\1/p' Makefile)"
pinned_tool "gitleaks" "github.com/zricethezav/gitleaks/v8" "$(sed -n 's/^GITLEAKS := .*gitleaks\/v8@\(v[0-9.]*\).*/\1/p' Makefile)"

echo
echo "== 后台前端依赖（web/admin；Latest 和 Current 同一个大版本的，make upgrade 会升，跨大版本的要人改）"
(cd web/admin && pnpm outdated || true)

echo
echo "== 镜像（Dockerfile 的由 dependabot 跟进；compose 的 postgres 要人改）"
grep -hoE '^FROM [^ ]+:[^ ]+' deploy/Dockerfile | sort -u | sed 's/^FROM /  /'
grep -hoE 'image: [^ ]+' deploy/docker-compose.yml | sort -u | sed 's/^image: /  /'
