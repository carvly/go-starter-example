.PHONY: setup run-api run-worker build test test-integration vet fmt lint lint-new tidy verify \
	migrate-up migrate-down govulncheck generate check-docs check-versions check-modules remove-modules ci \
	outdated upgrade doc-impact starter-upgrade actionlint gitleaks

# tidy 会原地改写 go.mod/go.sum，与同一次 make 里读它们的 target 并发会读到
# 半截文件，所以禁止并行。
.NOTPARALLEL:

# 命令行工具固定版本，不用 @latest：版本漂移会让 CI 因为上游发版而变色，与本仓库
# 的改动无关。scripts/check-versions.sh 校验这里和 CI、compose 里的版本一致。
GOLANGCI_LINT_VERSION := v2.15.0
# 第一次运行要编译约 1 分钟，之后走构建缓存。
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOVULNCHECK := go run golang.org/x/vuln/cmd/govulncheck@v1.10.0
ACTIONLINT := go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
GITLEAKS := go run github.com/zricethezav/gitleaks/v8@v8.30.1

# 安装提交钩子。lefthook 版本由 go.mod 的 tool 指令固定。
setup:
	go tool lefthook install

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

build:
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker

# 单元测试 + 架构测试（internal/archtest）+ 注册完整性测试，秒级，不需要 Docker。
test:
	go test ./...

# 真实 Postgres（testcontainers-go）上的 Repository 契约测试、dbtx、队列入队，需要 Docker
# 或 Podman（脚本自动准备 Podman 需要的 DOCKER_HOST 等环境变量）。
test-integration:
	scripts/test-integration.sh

# 带上 build tag，集成测试和生成器夹具的文件也要 vet。
vet:
	go vet -tags=integration,genfixture ./...

fmt:
	$(GOLANGCI_LINT) fmt ./...

lint:
	$(GOLANGCI_LINT) run ./...

# 只检查相对 HEAD 新增的问题，供 pre-commit 钩子使用。
lint-new:
	$(GOLANGCI_LINT) run --new-from-rev=HEAD ./...

tidy:
	go mod tidy

verify:
	go mod verify

govulncheck:
	$(GOVULNCHECK) ./...

# 扫描整个 git 历史里的密钥泄露（只输出脱敏后的位置）。
gitleaks:
	$(GITLEAKS) git --redact --no-banner .

# 检查 .github/workflows/*.yml：YAML 语法、表达式、run 里的脚本（装了 shellcheck 时）。
actionlint:
	$(ACTIONLINT)

# 列出可以升级的版本（只读）：Go 依赖、工具链、固定版本的工具、后台前端依赖、镜像。
outdated:
	scripts/outdated.sh

# 把 Go 直接依赖和后台前端依赖升到兼容范围内的最新版，重新生成代码并跑检查，最后列出
# 每项的 changelog。看 changelog、评估新特性是升级的一部分，见 docs/agents/deploy-ci.md。
upgrade:
	scripts/upgrade.sh

# 契约固定入口：只改 VERSION / CONTINUE 的传参，不改变获取目标版本并自举的行为。
starter-upgrade:
	@test -n "$(VERSION)" || (echo "用法：make starter-upgrade VERSION=v0.1.0 [CONTINUE=1]" && exit 1)
	scripts/starter-upgrade-entry.sh "$(VERSION)" "$(CONTINUE)"

# 重新生成由 spec 派生的代码：各模块 httpapi/oapi、adminapi/oapi 的 api.gen.go，
# 以及 web/admin/src/api/meta.gen.ts。
generate:
	go generate ./...

check-docs:
	scripts/check-docs.sh

# 列出可能受这次改动影响的文档（和 main 的分叉点比较），逐篇核对；只提示，不失败。
doc-impact:
	scripts/doc-impact.sh

check-versions:
	scripts/check-versions.sh

# 对每个模块演练一次删除，约 1 分钟；CI 里只在相关路径变化时运行。
check-modules:
	scripts/check-module-removal.sh

remove-modules:
	@test -n "$(M)" || (echo "用法：make remove-modules M=user（多个模块用逗号分隔）" && exit 1)
	scripts/remove-modules.sh "$(M)"

migrate-up:
	go run ./cmd/api migrate up

migrate-down:
	@test -n "$(M)" || (echo "用法：make migrate-down M=user" && exit 1)
	go run ./cmd/api migrate down -module "$(M)"

# 本地复现 CI 的主流程。用 git status --porcelain 而不是 git diff：后者看不到未跟踪的新文件。
ci: vet lint govulncheck actionlint verify test build check-docs check-versions tidy generate
	@if [ -n "$$(git status --porcelain -- go.mod go.sum)" ]; then \
		echo "go.mod/go.sum 与实际依赖不一致（或是尚未提交的新文件），跑 'go mod tidy' 后提交" && exit 1; \
	fi
	@if [ -n "$$(git status --porcelain -- 'internal/modules/*/httpapi/oapi/api.gen.go')" ]; then \
		echo "internal/modules/*/httpapi/oapi/api.gen.go 与 api/openapi.yaml 不一致（或尚未提交），跑 'make generate' 后提交" && exit 1; \
	fi
	@if [ -n "$$(git status --porcelain -- 'internal/modules/*/adminapi/oapi/api.gen.go' web/admin/src/api/meta.gen.ts)" ]; then \
		echo "internal/modules/*/adminapi/oapi/api.gen.go 或 web/admin/src/api/meta.gen.ts 与 api/admin.yaml 不一致（或尚未提交），跑 'make generate' 后提交" && exit 1; \
	fi
