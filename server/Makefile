VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILT   := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X kept/server/internal/version.Version=$(VERSION) \
	-X kept/server/internal/version.Commit=$(COMMIT) \
	-X kept/server/internal/version.BuildTime=$(BUILT)
TOOL    := go tool -modfile=tools/go.mod
# v1.13: the official build has the paid modules (-tags ee); TAGS= builds the open-source one
TAGS    ?=

.PHONY: help generate build run test vet check oss-check tidy clean web release

help: ## 列出可用命令
	@grep -E '^[a-z]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

generate: ## 由 SQL 和 OpenAPI 生成代码（sqlc、oapi-codegen）
	$(TOOL) sqlc generate
	$(TOOL) oapi-codegen -config internal/api/oapi-codegen.yaml ../api/openapi.yaml
	go run ./internal/api/genmodules ../api/openapi.yaml internal/api/oss_modules_gen.go

build: ## 编译到 bin/kept
	CGO_ENABLED=0 go build -tags "$(TAGS)" -trimpath -ldflags "$(LDFLAGS)" -o bin/kept ./cmd/kept

web: ## 构建网页端并复制到 internal/web/dist（随后 make build 即可嵌入）
	cd ../app && pnpm install --frozen-lockfile && if [ "$(TAGS)" = ee ]; then pnpm build; else pnpm build:oss; fi
	find internal/web/dist -mindepth 1 ! -name placeholder.html -exec rm -rf {} +
	cp -r ../app/dist/. internal/web/dist/

release: web ## 编译二进制发布包到 ../dist（Linux amd64/arm64、macOS arm64/amd64）
	../deploy/package.sh $(VERSION) "$(LDFLAGS)"

run: ## 本地启动（数据放在 ./data，日志为 debug）
	KEPT_LOG_LEVEL=debug go run -tags "$(TAGS)" ./cmd/kept serve

test: ## 运行测试
	go test -tags "$(TAGS)" -race -timeout 20m ./...

vet: ## 静态检查（含 gofmt 格式）
	@test -z "$$(gofmt -l .)" || (echo "以下文件需要 gofmt：" && gofmt -l . && exit 1)
	go vet -tags "$(TAGS)" ./...

check: generate vet test ## 提交前检查：生成代码、静态检查、测试
	@git diff --quiet -- internal/store/sqlc internal/api/gen 2>/dev/null || \
		(echo "生成的代码有变化，请一起提交" && exit 1)

oss-check: ## 开源的免费版（不带 ee）也要能编译、静态检查、测试（v1.13）
	go build ./...
	go vet ./...
	go test -timeout 20m ./...
	cd ../app && pnpm typecheck:oss && pnpm test:oss

tidy: ## 整理依赖
	go mod tidy
	cd tools && go mod tidy

clean: ## 删除编译产物和本地数据
	rm -rf bin data
