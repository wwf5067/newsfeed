# newsfeed

定时抓取 + 查询服务,部署在腾讯云 Lighthouse。

## 架构

- **crawler** (`cmd/crawler`): 定时抓取入库,仅监听 `127.0.0.1`,不对外暴露
- **api** (`cmd/api`): 对外只读查询接口
- **web** (`web/`): Next.js 前端,静态导出后由 Nginx 提供
- **PostgreSQL**: 共享存储,通过账号权限实现读写分离

两个服务共享 `internal/model` 和 `internal/storage`,通过 DB 解耦,互不直接通信。

## 目录

```
cmd/
  crawler/        crawler 服务入口
  api/            api 服务入口
internal/
  config/         环境变量加载
  logger/         slog 封装
  storage/        pgx 连接池
  model/          共享数据模型
  crawler/        抓取逻辑(Source 接口 + Runner)
  api/            HTTP handler + 只读 repo
migrations/       SQL 迁移文件
web/              Next.js 前端
deploy/           docker-compose / nginx / Dockerfile
```

## 本地开发

```bash
# 1. 准备环境变量
cp .env.example .env

# 2. 启动 PostgreSQL (后续会提供 docker-compose)
# 临时方法: docker run -d --name newsfeed-pg -e POSTGRES_USER=newsfeed -e POSTGRES_PASSWORD=newsfeed -e POSTGRES_DB=newsfeed -p 5432:5432 postgres:16

# 3. 安装迁移工具
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# 4. 执行迁移
make migrate-up

# 5. 整理依赖
make tidy

# 6. 分别启动两个服务
make run-api
make run-crawler
```

## 添加新爬虫源

在 `internal/crawler/sources/` 下新增一个文件,实现 `crawler.Source` 接口并暴露一个构造函数,然后在 `cmd/crawler/main.go` 里显式注册。

```go
// internal/crawler/sources/my_source.go
type MySource struct { /* ... */ }

func NewMySource(/* deps */) *MySource { return &MySource{} }
func (s *MySource) Key() string      { return "my_source" }
func (s *MySource) Schedule() string { return "0 */30 * * * *" } // 每30分钟
func (s *MySource) Fetch(ctx context.Context) ([]model.Article, error) { ... }
```

然后在 `cmd/crawler/main.go` 里加一行注册:
```go
runner.Register(sources.NewMySource(/* ... */))
```
