// zhihu-probe 是一个一次性调试工具:用 ZHIHU_COOKIE 环境变量调一次知乎热榜,
// 直接在终端打印解析结果。不写库,不开 DB,不跑 cron。
// 用法:ZHIHU_COOKIE='xxx' go run ./cmd/zhihu-probe
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/wwf5067/newsfeed/internal/crawler/sources"
)

func main() {
	cookie := os.Getenv("ZHIHU_COOKIE")
	if cookie == "" {
		fmt.Fprintln(os.Stderr, "ZHIHU_COOKIE is empty. Set it from your browser DevTools and rerun.")
		os.Exit(1)
	}

	s := sources.NewZhihuHot(cookie, "")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	articles, err := s.Fetch(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch error:", err)
		os.Exit(1)
	}

	fmt.Printf("got %d items\n\n", len(articles))
	for i, a := range articles {
		fmt.Printf("%2d. %s\n    %s\n", i+1, a.Title, a.URL)
	}
}
