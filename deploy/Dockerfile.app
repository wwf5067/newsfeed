# 通用 Go 服务镜像。SERVICE 决定构建 cmd/api 还是 cmd/crawler。
# 用法:docker build --build-arg SERVICE=api -f deploy/Dockerfile.go -t newsfeed/api .

# ---- build stage ----
FROM golang:1.25-alpine AS builder

ARG SERVICE
RUN test -n "$SERVICE" || (echo "SERVICE build-arg required (api|crawler)" && exit 1)

WORKDIR /src

# 先单独拷依赖文件,利用 layer cache:只要 go.mod/go.sum 没变就不重下依赖
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# 再拷源码并编译
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

# ---- runtime stage ----
FROM alpine:3.20

# tzdata: 让容器内时间用上海时区;ca-certificates: 给 crawler 调 https 用
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app app

ENV TZ=Asia/Shanghai

COPY --from=builder /out/app /usr/local/bin/app
USER app

ENTRYPOINT ["/usr/local/bin/app"]
