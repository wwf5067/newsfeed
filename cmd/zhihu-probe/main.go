// zhihu-probe 是一个一次性调试工具:用 ZHIHU_COOKIE 环境变量调一次知乎热榜,
// 直接在终端打印解析结果。不写库,不开 DB,不跑 cron。
//
// 用法:
//
//	ZHIHU_COOKIE='xxx' go run ./cmd/zhihu-probe          # 打印解析结果 + heat
//	ZHIHU_COOKIE='xxx' go run ./cmd/zhihu-probe --raw    # 额外 dump 原始 JSON
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/wwf5067/newsfeed/internal/crawler/sources"
)

func main() {
	raw := flag.Bool("raw", false, "dump 知乎接口原始 JSON(pretty-print)")
	flag.Parse()

	cookie := os.Getenv("ZHIHU_COOKIE")
	if cookie == "" {
		fmt.Fprintln(os.Stderr, "ZHIHU_COOKIE is empty. Set it from your browser DevTools and rerun.")
		os.Exit(1)
	}

	s := sources.NewZhihuHot(cookie, "")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if *raw {
		body, err := s.FetchRaw(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fetch raw error:", err)
			os.Exit(1)
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, body, "", "  "); err != nil {
			// 解析失败就直接输出原文
			os.Stdout.Write(body)
		} else {
			os.Stdout.Write(pretty.Bytes())
		}
		fmt.Println()
		return
	}

	articles, err := s.Fetch(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch error:", err)
		os.Exit(1)
	}

	fmt.Printf("got %d items\n\n", len(articles))
	withHeat := 0
	for i, a := range articles {
		heat := a.Heat
		if heat == "" {
			heat = "<空>"
		} else {
			withHeat++
		}
		fmt.Printf("%2d. [%s] %s\n    %s\n", i+1, heat, a.Title, a.URL)
	}
	fmt.Printf("\nheat 非空: %d / %d\n", withHeat, len(articles))
}
