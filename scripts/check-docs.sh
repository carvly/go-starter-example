#!/usr/bin/env bash
# 文档里用反引号引用的仓库路径和 Markdown 链接的目标必须真实存在：过期的文档会把 agent 引向不存在的
# 文件，比没有文档更糟。docs/proposals/（规划）和 docs/archive/（历史）只在本地保留、不入库，已提交的文档
# 不能引用其中的具体文件（只提目录名可以），否则 clone 下来的人看到的是失效链接。
#
# 只检查以 internal/ cmd/ scripts/ api/ migrations/ deploy/ configs/ docs/ 开头的
# 路径；含 <...>、*、{...} 的是模式而不是具体路径，跳过。模块 migrations/*.sql 里的引用
# （`--` 注释、COMMENT ON 字符串）不用反引号，逐行按路径字符匹配。

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

files=(AGENTS.md AGENTS.project.md README.md docs/ARCHITECTURE.md docs/PROJECT.md)
while IFS= read -r f; do files+=("$f"); done < <(find docs/agents internal/modules -path '*/docs/*.md' -o -path 'docs/agents/*.md' 2>/dev/null | sort)

missing=0
removed_modules=""
if [ -f starter-removed.yaml ]; then
	removed_modules="$(sed -nE 's/^removed_modules:[[:space:]]*\[([^]]*)\].*/\1/p' starter-removed.yaml | tr ',' ' ')"
fi
check_refs() {
	local f="$1" line ref path
	while IFS=: read -r line ref; do
		path="${ref%%:*}"   # 去掉 :行号
		path="${path%/}"
		path="${path%%#*}"
		case "$path" in
		*'<'* | *'*'* | *'{'* | *'...'* | *' '* | docs/proposals | docs/archive) continue ;;
		docs/proposals/* | docs/archive/*)
			echo "$f:$line: 引用了只在本地保留的文档：${path}（不入库，别人打不开；把要点写进正文）" >&2
			missing=1
			continue
			;;
		esac
		skip=0
		for module in $removed_modules; do
			case "$path" in "internal/modules/$module" | "internal/modules/$module/"*) skip=1 ;; esac
		done
		[ "$skip" = 0 ] || continue
		if [ ! -e "$path" ]; then
			echo "$f:$line: 引用的路径不存在：$path" >&2
			missing=1
		fi
	done
}

for f in "${files[@]}"; do
	[ -f "$f" ] || continue
	# shellcheck disable=SC2016 # 单引号里的反引号是要匹配的字符，不是命令替换
	check_refs "$f" < <(grep -noE '`(internal|cmd|scripts|api|migrations|deploy|configs|docs)/[^`]*`' "$f" | sed -E 's/^([0-9]+):`([^`]*)`$/\1:\2/')
done

# Markdown 链接 [文字](相对路径)：目标相对文档所在目录解析，必须存在；外部链接、页内锚点、行内代码和代码块
# 里的内容跳过。
check_links() {
	local f="$1" dir line target path
	dir="$(dirname "$f")"
	while IFS=: read -r line target; do
		case "$target" in
		http://* | https://* | mailto:* | '#'*) continue ;;
		esac
		path="${target%%#*}"
		path="${path%% *}"
		[ -n "$path" ] || continue
		case "$dir/$path" in
		*docs/proposals/?* | *docs/archive/?*)
			echo "$f:$line: 链接到只在本地保留的文档：${target}（不入库，别人打不开；把要点写进正文）" >&2
			missing=1
			continue
			;;
		esac
		if [ ! -e "$dir/$path" ]; then
			echo "$f:$line: 链接的目标不存在：$target" >&2
			missing=1
		fi
	done < <(awk '
		/^[[:space:]]*```/ { fence = !fence; next }
		fence { next }
		{
			s = $0
			gsub(/`[^`]*`/, "", s)
			while (match(s, /\]\([^)]+\)/)) {
				print FNR ":" substr(s, RSTART + 2, RLENGTH - 3)
				s = substr(s, RSTART + RLENGTH)
			}
		}' "$f")
}

for f in "${files[@]}"; do
	[ -f "$f" ] || continue
	check_links "$f"
done

# river 导出的 0006 不手改，里面的 URL 也不是仓库路径。左边界排除 URL 和单词中间的子串
# （https://example.com/docs/x、my_api/x）。
for f in internal/platform/queue/migrations/*.sql internal/modules/*/migrations/*.sql; do
	[ -f "$f" ] || continue
	case "$f" in *_river_queue.*) continue ;; esac
	check_refs "$f" < <(awk '{
		s = $0
		while (match(s, /(^|[^A-Za-z0-9_\/.:-])(internal|cmd|scripts|api|migrations|deploy|configs|docs)\/[A-Za-z0-9_.\/-]*[A-Za-z0-9_]/)) {
			m = substr(s, RSTART, RLENGTH)
			sub(/^[^a-z]/, "", m)
			print FNR ":" m
			s = substr(s, RSTART + RLENGTH)
		}
	}' "$f")
done

# 反引号里的标识符也必须存在：Go 标识符（pkg.Name、Type.Method）、测试名、配置项（全大写带
# 下划线）、make 目标。包名按仓库里的目录名和 import 别名解析，解析不到的（第三方包、TS 代码）
# 跳过；同名的包（各模块的 domain、postgres……）有一个声明了就算存在。
MODULE="$(sed -n 's/^module //p' go.mod)"
pkgmap="$(mktemp)"
trap 'rm -f "$pkgmap"' EXIT
{
	git ls-files --cached --others --exclude-standard -- '*.go' | grep -v '^web/' |
		while IFS= read -r g; do dirname "$g"; done | sort -u | awk -F/ '{print $NF " " $0}'
	git grep -hE "^[[:space:]]*[a-z][a-zA-Z0-9]* \"$MODULE/[^\"]+\"" -- '*.go' |
		sed -E "s|^[[:space:]]*([a-zA-Z0-9]+) \"$MODULE/([^\"]+)\"|\1 \2|"
} | sort -u >"$pkgmap"

declared_in() {
	local id="$1" d
	shift
	for d in "$@"; do
		git grep --untracked -qE "^(func (\([^)]*\) )?|type |var |const |[[:space:]]+)$id([^A-Za-z0-9_]|\$)" -- ":(glob)$d/*.go" && return 0
		if [ -n "$removed_modules" ]; then
			git grep -qE "^(func (\([^)]*\) )?|type |var |const |[[:space:]]+)$id([^A-Za-z0-9_]|\$)" "${STARTER_VERSION:-HEAD}" -- ":(glob)$d/*.go" && return 0
		fi
	done
	return 1
}

check_symbol() {
	local tok="$1" t pkg dirs part rest
	case "$tok" in
	make\ *)
		t="${tok#make }"
		t="${t%% *}"
		grep -qE "^$t:" Makefile
		return
		;;
	esac
	t="${tok%%(*}"
	if [[ "$t" =~ ^[a-z][a-z0-9]*(\.[A-Z][A-Za-z0-9_]*)+$ ]]; then
		if [ -n "$removed_modules" ]; then
			git grep -q "${t##*.}" "${STARTER_VERSION:-HEAD}" -- internal/modules && return 0
		fi
		pkg="${t%%.*}"
		dirs="$(awk -v p="$pkg" '$1 == p {print $2}' "$pkgmap")"
		[ -n "$dirs" ] || return 0
		rest="${t#*.}"
		while [ -n "$rest" ]; do
			part="${rest%%.*}"
			# shellcheck disable=SC2086 # dirs 按换行拆成多个目录
			declared_in "$part" $dirs || return 1
			[ "$rest" = "$part" ] && break
			rest="${rest#*.}"
		done
		return 0
	fi
	if [[ "$t" =~ ^([A-Z][A-Za-z0-9]*)\.([A-Z][A-Za-z0-9_]*)$ ]]; then
		if [ -n "$removed_modules" ]; then
			git grep -q "${t##*.}" "${STARTER_VERSION:-HEAD}" -- internal/modules && return 0
		fi
		dirs="$(git grep --untracked -lE "^type ${BASH_REMATCH[1]}([^A-Za-z0-9_]|\$)|^[[:space:]]+${BASH_REMATCH[1]} (struct|interface)" -- '*.go' |
			while IFS= read -r g; do dirname "$g"; done | sort -u)"
		[ -n "$dirs" ] || return 0
		# shellcheck disable=SC2086
		declared_in "${BASH_REMATCH[2]}" $dirs
		return
	fi
	if [[ "$t" =~ ^Test[A-Z][A-Za-z0-9_]*$ ]]; then
		git grep --untracked -qE "^func $t\(" -- '*.go'
		return
	fi
	if [[ "$t" =~ ^[A-Z][A-Z0-9]*(_[A-Z0-9]+)+(=.*)?$ ]]; then
		git grep --untracked -qw -- "${t%%=*}" -- ':!*.md' ':!docs/'
		return
	fi
	return 0
}

for f in "${files[@]}"; do
	[ -f "$f" ] || continue
	# shellcheck disable=SC2016 # 单引号里的反引号是要匹配的字符，不是命令替换
	while IFS=: read -r line tok; do
		raw="$(sed -n "${line}p" "$f")"
		skip=0
		for module in $removed_modules; do
			case "$raw" in *"$module"*) skip=1 ;; esac
		done
		[ "$skip" = 0 ] || continue
		if ! check_symbol "$tok"; then
			echo "$f:$line: 引用的标识符不存在：$tok" >&2
			missing=1
		fi
	done < <(grep -noE '`[^`]+`' "$f" | sed -E 's/^([0-9]+):`([^`]*)`$/\1:\2/')
done

if [ "$missing" = 1 ]; then
	echo "文档引用了不存在的路径或标识符：更新文档使其描述当前代码；规划中的内容写进 docs/proposals/。" >&2
	exit 1
fi
