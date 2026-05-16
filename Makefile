.PHONY: help tidy build run-api run-crawler run-crawler-once run-web web-build probe-zhihu test fmt vet migrate-up migrate-down migrate-create db-up db-down db-logs db-psql

# 默认从 .env 加载环境变量
ifneq (,$(wildcard .env))
include .env
export
endif

help: ## 显示所有命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

tidy: ## 整理依赖
	go mod tidy

build: ## 编译两个二进制到 bin/
	mkdir -p bin
	go build -o bin/api ./cmd/api
	go build -o bin/crawler ./cmd/crawler

run-api: ## 本地启动 api 服务
	go run ./cmd/api

run-crawler: ## 本地启动 crawler 服务
	go run ./cmd/crawler

run-crawler-once: ## 启动 crawler 并立即跑一次所有源(调试用,然后保持运行)
	RUN_ON_START=true go run ./cmd/crawler

probe-zhihu: ## 调试:用 .env 中的 ZHIHU_COOKIE 抓一次知乎热榜并打印
	go run ./cmd/zhihu-probe

run-web: ## 启动前端开发服务器 (http://localhost:3000)
	cd web && npm run dev

web-build: ## 构建前端静态产物到 web/out/
	cd web && npm run build

test: ## 运行测试
	go test ./...

fmt: ## 格式化
	gofmt -w .

vet: ## go vet
	go vet ./...

# 自动找到 migrate 二进制(优先 PATH,其次 $GOPATH/bin)
MIGRATE ?= $(shell command -v migrate 2>/dev/null || echo $$(go env GOPATH)/bin/migrate)

migrate-up: ## 执行迁移 (需要先安装: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest)
	$(MIGRATE) -path migrations -database "$(DATABASE_URL)" up

migrate-down: ## 回滚一步迁移
	$(MIGRATE) -path migrations -database "$(DATABASE_URL)" down 1

migrate-create: ## 新建迁移文件,用法: make migrate-create name=add_xxx
	$(MIGRATE) create -ext sql -dir migrations -seq $(name)

db-up: ## 启动本地 PostgreSQL (docker)
	docker compose -f deploy/docker-compose.dev.yml up -d
	@echo "等待 PG 就绪..."
	@until docker exec newsfeed-pg pg_isready -U newsfeed -d newsfeed >/dev/null 2>&1; do sleep 1; done
	@echo "PG ready at 127.0.0.1:5432"

db-down: ## 停止本地 PostgreSQL (保留数据)
	docker compose -f deploy/docker-compose.dev.yml down

db-logs: ## 查看 PG 日志
	docker compose -f deploy/docker-compose.dev.yml logs -f postgres

db-psql: ## 进入 psql shell
	docker exec -it newsfeed-pg psql -U newsfeed -d newsfeed
