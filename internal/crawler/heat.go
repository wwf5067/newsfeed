package crawler

import (
	"strconv"
	"strings"
	"unicode"
)

// ParseHeat 把源返回的热度文本解析为整数,失败返回 0。
// 当前只覆盖知乎风格:
//
//	"1234 万热度" -> 12340000
//	"12 亿热度"   -> 1200000000
//	"1.5 万热度"  -> 15000
//	"500 热度"    -> 500
//	""           -> 0
//	"乱码"        -> 0
//
// 接其它源时按需扩展(可改为 per-source 实现);保留默认 0 让前端"无趋势可显示"。
func ParseHeat(text string) int64 {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}

	// 提取第一段连续的数字 + 小数点(扫到第一个非数字字符为止)。
	var numEnd int
	for numEnd < len(text) {
		r := rune(text[numEnd])
		if unicode.IsDigit(r) || r == '.' {
			numEnd++
			continue
		}
		break
	}
	if numEnd == 0 {
		return 0
	}
	num, err := strconv.ParseFloat(text[:numEnd], 64)
	if err != nil || num < 0 {
		return 0
	}

	// 单位:扫数字之后的字符串里是否含 "万"/"亿"。
	rest := text[numEnd:]
	switch {
	case strings.Contains(rest, "亿"):
		return int64(num * 1e8)
	case strings.Contains(rest, "万"):
		return int64(num * 1e4)
	default:
		return int64(num)
	}
}
