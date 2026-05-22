package api

import (
	"fmt"
	"testing"
	"unicode"
)

func TestBadCaseProbe(t *testing.T) {
	trackerSegOnce.Do(loadTrackerSegmenter)

	// 方案C:查4字词的子词频率,确认阈值
	fmt.Println("=== 方案C 子词频率验证 ===")
	subwords := []string{
		// 发现异常 的拆解
		"发现", "异常",
		// 潜在误伤:量子计算、文心一言
		"量子", "计算", "文心", "一言",
		// 报告显示 / 调查发现 等类似功能性短语
		"报告", "显示", "调查", "发现",
		// 其他高频2字词做对比
		"中国", "美国", "事件", "专家",
	}
	fmt.Printf("%-8s %8s\n", "词", "gseFreq")
	fmt.Println("-------------------")
	for _, w := range subwords {
		freq, _, ok := trackerSeg.Find(w)
		if !ok {
			fmt.Printf("%-8s %8s\n", w, "(不在词典)")
		} else {
			fmt.Printf("%-8s %8.0f\n", w, freq)
		}
	}

	fmt.Println()
	fmt.Println("=== bad case 完整诊断 ===")
	cases := []struct {
		word   string
		remark string
	}{
		{"立马", "2字副词"},
		{"扔掉", "2字动词"},
		{"发现异常", "4字动宾短语"},
		{"到金项链朱自清散文", "9字长碎片"},
		{"率超60%", "含%数字碎片"},
		{"AI率超", "混合ASCII+汉字"},
		{"朱自清散文AI", "混合汉字+ASCII"},
		{"散文AI率超", "混合汉字+ASCII"},
	}
	fmt.Printf("%-24s %8s %4s %6s %6s %8s %8s  %s\n",
		"词", "gseFreq", "字数", "在dict", "已拦截", "minArts", "混合", "说明")
	fmt.Println(string(make([]byte, 95)))
	for _, c := range cases {
		runes := []rune(c.word)
		freq, _, ok := trackerSeg.Find(c.word)
		excluded := isExcludedHeatWord(c.word)
		minArts := heatMinArticles(c.word)

		// 检查是否混合
		hasASCII, hasChinese := false, false
		for _, r := range runes {
			if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
				hasASCII = true
			}
			if unicode.Is(unicode.Han, r) {
				hasChinese = true
			}
		}
		mixed := hasASCII && hasChinese

		fmt.Printf("%-24s %8.0f %4d %6v %6v %8d %8v  %s\n",
			c.word, freq, len(runes), ok, excluded, minArts, mixed, c.remark)
	}
}
