package extractor

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode"
)

// RuleExtractor 基于正则与小型词典的规则抽取器。
// 不追求高准确率,目标是先把数据管道跑通,产出可观察的结果。
type RuleExtractor struct{}

func NewRuleExtractor() *RuleExtractor { return &RuleExtractor{} }

func (RuleExtractor) Backend() string { return "rule" }

// 《XX》→ work 实体。允许书名号嵌套时只取最外层一对。
var reWork = regexp.MustCompile(`《([^《》]{1,40})》`)

// "X 宣布/表示/发布/回应/确认" → 取动词前 2~6 字汉字作为人名/机构候选。
// 不区分人/机构,统一标 person(规则版精度有限,后续 LLM 再细分)。
// 命中后再过 actorPrefixBlacklist,排除"在上海发布"里"在上海"这类介词短语。
var reActor = regexp.MustCompile(`([\p{Han}]{2,6})(?:宣布|表示|发布|回应|确认|否认|警告|批评|呼吁|强调)`)

// 介词/常用虚词。出现在候选首字时,说明这不是一个独立的人名/机构。
var actorPrefixBlacklist = map[rune]bool{
	'在': true, '对': true, '向': true, '与': true, '和': true, '及': true,
	'到': true, '从': true, '被': true, '把': true, '让': true, '于': true,
	'为': true, '由': true, '给': true, '替': true, '将': true, '使': true,
}

// 常见地名词典。手工维护,不追求全。命中即算 location 实体。
// 命中靠子串匹配,放在最前的优先(如"中国香港"在"香港"前)。
var locations = []string{
	"中国大陆", "中国香港", "中国台湾", "中国澳门",
	"北京", "上海", "广州", "深圳", "杭州", "成都", "重庆", "武汉", "西安", "南京",
	"天津", "苏州", "厦门", "青岛", "长沙", "郑州", "合肥", "济南", "沈阳", "大连",
	"香港", "澳门", "台湾", "台北",
	"美国", "日本", "韩国", "朝鲜", "俄罗斯", "乌克兰", "英国", "法国", "德国",
	"意大利", "西班牙", "加拿大", "澳大利亚", "新西兰", "印度", "巴基斯坦",
	"以色列", "巴勒斯坦", "伊朗", "伊拉克", "叙利亚", "土耳其", "沙特", "阿联酋",
	"新加坡", "马来西亚", "泰国", "越南", "菲律宾", "印尼",
	"华盛顿", "纽约", "洛杉矶", "东京", "首尔", "莫斯科", "伦敦", "巴黎", "柏林",
}

// Extract 主入口。content 当前未使用(标题信息已足够 MVP);
// 保留参数以便后续 LLM 版接同样签名。
func (e RuleExtractor) Extract(_ context.Context, title, _ string) (ExtractResult, error) {
	res := ExtractResult{}
	if strings.TrimSpace(title) == "" {
		return res, nil
	}

	// 用 map 去重,避免同一实体在标题里出现两次时存两份。
	seen := make(map[string]struct{})
	add := func(name, typ string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := typ + ":" + name
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		res.Entities = append(res.Entities, ExtractedEntity{Name: name, Type: typ})
	}

	// 1. 书名号 → work
	for _, m := range reWork.FindAllStringSubmatch(title, -1) {
		add(m[1], EntityWork)
	}

	// 2. 动作模式 → person 候选(过一道介词黑名单)
	for _, m := range reActor.FindAllStringSubmatch(title, -1) {
		cand := m[1]
		first := []rune(cand)[0]
		if actorPrefixBlacklist[first] {
			continue
		}
		add(cand, EntityPerson)
	}

	// 3. 地名词典 → location
	for _, loc := range locations {
		if strings.Contains(title, loc) {
			add(loc, EntityLocation)
		}
	}

	// 事件:每篇文章产生一个事件单元,Fingerprint 用于跨文章归并。
	res.Events = append(res.Events, ExtractedEvent{
		Title:       truncate(title, 80),
		Fingerprint: fingerprint(title),
		Summary:     "",
	})

	return res, nil
}

// fingerprint 把标题归一化(去掉所有标点/空白/特殊字符,只留字母数字汉字)
// 后取 sha1 前 16 hex 字符。同主题不同标点的标题会映射到同一指纹。
func fingerprint(title string) string {
	var b strings.Builder
	b.Grow(len(title))
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	sum := sha1.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:16]
}

// truncate 按 rune 安全截断,避免切到中文半个字。
func truncate(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n])
}
