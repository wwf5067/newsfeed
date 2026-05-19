package api

import (
	"strings"

	"github.com/cloudflare/ahocorasick"
)

// trackerEntityLexicon 是人工维护的专用实体词典。
// 设计原则:
//  1. 优先覆盖产品里高频、用户感知强的实体(AI / 互联网公司 / 内容平台 / 游戏 IP /
//     政治新闻人物 / 影视作品 / 大型事件)。
//  2. 别名只列"在标题里真会出现的形式":中文+英文+常见缩写,不追求穷举。
//  3. Category 仅作元数据,前端按类筛选用;不进任何索引,不影响匹配语义。
//
// 增删条目时:只动这个变量即可,所有 alias 索引会在包初始化时自动重建。
var trackerEntityLexicon = []trackerLexiconEntry{
	// === AI / 大模型公司 ===
	{Label: "OpenAI", Category: "company", Aliases: []string{"OpenAI", "openai"}},
	{Label: "ChatGPT", Category: "company", Aliases: []string{"ChatGPT", "chatgpt", "GPT-4", "GPT4", "GPT-4o", "GPT4o", "GPT-5", "GPT5", "o1", "o3"}},
	{Label: "DeepSeek", Category: "company", Aliases: []string{"DeepSeek", "deepseek", "深度求索"}},
	{Label: "Claude", Category: "company", Aliases: []string{"Claude", "claude", "Anthropic", "anthropic"}},
	{Label: "Gemini", Category: "company", Aliases: []string{"Gemini", "gemini", "Bard", "bard"}},
	{Label: "Manus", Category: "company", Aliases: []string{"Manus", "manus"}},
	{Label: "月之暗面", Category: "company", Aliases: []string{"月之暗面", "Moonshot", "moonshot", "Kimi", "kimi"}},
	{Label: "智谱", Category: "company", Aliases: []string{"智谱", "智谱AI", "Zhipu", "zhipu", "ChatGLM", "chatglm"}},
	{Label: "百川智能", Category: "company", Aliases: []string{"百川智能", "Baichuan", "baichuan"}},
	{Label: "MiniMax", Category: "company", Aliases: []string{"MiniMax", "minimax", "海螺AI", "海螺"}},
	{Label: "阶跃星辰", Category: "company", Aliases: []string{"阶跃星辰", "StepFun", "stepfun", "跃问"}},

	// === 互联网公司 ===
	{Label: "字节跳动", Category: "company", Aliases: []string{"字节跳动", "ByteDance", "bytedance", "抖音集团"}},
	{Label: "腾讯", Category: "company", Aliases: []string{"腾讯", "Tencent", "tencent", "腾讯视频", "微信", "WeChat", "wechat", "QQ"}},
	{Label: "阿里巴巴", Category: "company", Aliases: []string{"阿里巴巴", "阿里", "Alibaba", "alibaba", "淘宝", "天猫", "支付宝", "Alipay", "alipay", "阿里云"}},
	{Label: "百度", Category: "company", Aliases: []string{"百度", "Baidu", "baidu", "文心一言", "Ernie", "ERNIE"}},
	{Label: "京东", Category: "company", Aliases: []string{"京东", "JD", "jd", "JD.com", "jd.com"}},
	{Label: "拼多多", Category: "company", Aliases: []string{"拼多多", "PDD", "pdd", "Temu", "temu"}},
	{Label: "美团", Category: "company", Aliases: []string{"美团", "Meituan", "meituan"}},
	{Label: "网易", Category: "company", Aliases: []string{"网易", "NetEase", "netease"}},
	{Label: "新东方", Category: "company", Aliases: []string{"新东方", "东方甄选", "与辉同行"}},
	{Label: "三只羊", Category: "company", Aliases: []string{"三只羊", "三只羊网络"}},

	// === 硬件 / 科技公司 ===
	{Label: "小米", Category: "company", Aliases: []string{"小米", "Xiaomi", "xiaomi", "小米SU7", "SU7", "小米YU7"}},
	{Label: "华为", Category: "company", Aliases: []string{"华为", "Huawei", "huawei", "鸿蒙", "HarmonyOS", "harmonyos", "Mate", "Pura", "海思", "麒麟"}},
	{Label: "苹果", Category: "company", Aliases: []string{"苹果", "Apple", "apple", "iPhone", "iOS", "MacBook", "macOS", "macos", "iPad", "Vision Pro"}},
	{Label: "微软", Category: "company", Aliases: []string{"微软", "Microsoft", "microsoft", "Copilot", "copilot", "Azure", "azure", "Windows", "Xbox"}},
	{Label: "谷歌", Category: "company", Aliases: []string{"谷歌", "Google", "google", "Android", "android", "YouTube", "youtube"}},
	{Label: "Meta", Category: "company", Aliases: []string{"Meta", "meta", "Facebook", "facebook", "Instagram", "instagram", "WhatsApp", "whatsapp"}},
	{Label: "英伟达", Category: "company", Aliases: []string{"英伟达", "NVIDIA", "nvidia", "CUDA", "cuda", "H100", "B100", "GB200"}},
	{Label: "AMD", Category: "company", Aliases: []string{"AMD", "amd"}},
	{Label: "Intel", Category: "company", Aliases: []string{"Intel", "intel", "英特尔"}},
	{Label: "台积电", Category: "company", Aliases: []string{"台积电", "TSMC", "tsmc"}},
	{Label: "三星", Category: "company", Aliases: []string{"三星", "Samsung", "samsung"}},
	{Label: "OPPO", Category: "company", Aliases: []string{"OPPO", "oppo"}},
	{Label: "vivo", Category: "company", Aliases: []string{"vivo", "VIVO"}},
	{Label: "荣耀", Category: "company", Aliases: []string{"荣耀", "Honor", "honor"}},

	// === 新能源汽车 ===
	{Label: "特斯拉", Category: "company", Aliases: []string{"特斯拉", "Tesla", "tesla"}},
	{Label: "比亚迪", Category: "company", Aliases: []string{"比亚迪", "BYD", "byd"}},
	{Label: "理想汽车", Category: "company", Aliases: []string{"理想汽车", "理想", "Li Auto", "li auto", "Lixiang", "lixiang", "理想MEGA", "MEGA", "L9", "L8"}},
	{Label: "蔚来", Category: "company", Aliases: []string{"蔚来", "NIO", "nio", "ET5", "ET7", "ET9"}},
	{Label: "小鹏", Category: "company", Aliases: []string{"小鹏", "小鹏汽车", "XPeng", "xpeng", "小鹏P7", "小鹏G6"}},
	{Label: "宁德时代", Category: "company", Aliases: []string{"宁德时代", "CATL", "catl"}},
	{Label: "宇树科技", Category: "company", Aliases: []string{"宇树科技", "宇树", "Unitree", "unitree"}},

	// === 内容平台 ===
	{Label: "B站", Category: "company", Aliases: []string{"B站", "哔哩哔哩", "Bilibili", "bilibili"}},
	{Label: "知乎", Category: "company", Aliases: []string{"知乎", "Zhihu", "zhihu"}},
	{Label: "微博", Category: "company", Aliases: []string{"微博", "Weibo", "weibo"}},
	{Label: "抖音", Category: "company", Aliases: []string{"抖音", "Douyin", "douyin", "TikTok", "tiktok"}},
	{Label: "快手", Category: "company", Aliases: []string{"快手", "Kuaishou", "kuaishou"}},
	{Label: "小红书", Category: "company", Aliases: []string{"小红书", "RED", "red", "rednote", "RedNote"}},

	// === 游戏 IP ===
	{Label: "王者荣耀", Category: "ip", Aliases: []string{"王者荣耀", "Honor of Kings", "honor of kings"}},
	{Label: "英雄联盟", Category: "ip", Aliases: []string{"英雄联盟", "LOL", "lol", "League of Legends", "league of legends"}},
	{Label: "原神", Category: "ip", Aliases: []string{"原神", "Genshin", "genshin", "Genshin Impact", "genshin impact"}},
	{Label: "崩坏：星穹铁道", Category: "ip", Aliases: []string{"崩坏：星穹铁道", "星穹铁道", "Honkai Star Rail", "honkai star rail"}},
	{Label: "黑神话：悟空", Category: "ip", Aliases: []string{"黑神话：悟空", "黑神话悟空", "黑神话", "Black Myth Wukong"}},
	{Label: "DOTA2", Category: "ip", Aliases: []string{"DOTA2", "dota2", "Dota2", "Dota 2", "DOTA 2"}},
	{Label: "CS2", Category: "ip", Aliases: []string{"CS2", "cs2", "CSGO", "csgo", "Counter-Strike", "counter-strike"}},
	{Label: "和平精英", Category: "ip", Aliases: []string{"和平精英", "PUBG Mobile"}},
	{Label: "三角洲行动", Category: "ip", Aliases: []string{"三角洲行动", "Delta Force", "delta force"}},
	{Label: "GTA6", Category: "ip", Aliases: []string{"GTA6", "gta6", "GTA 6", "gta 6", "侠盗猎车手6"}},

	// === 影视作品 / 综艺 ===
	{Label: "流浪地球", Category: "ip", Aliases: []string{"流浪地球", "流浪地球2", "流浪地球3"}},
	{Label: "哪吒", Category: "ip", Aliases: []string{"哪吒", "哪吒2", "哪吒之魔童降世", "哪吒之魔童闹海"}},
	{Label: "繁花", Category: "ip", Aliases: []string{"繁花"}},
	{Label: "狂飙", Category: "ip", Aliases: []string{"狂飙"}},
	{Label: "庆余年", Category: "ip", Aliases: []string{"庆余年", "庆余年2"}},
	{Label: "唐探", Category: "ip", Aliases: []string{"唐探", "唐人街探案", "唐探1900"}},
	{Label: "泰勒斯威夫特", Category: "person", Aliases: []string{"泰勒斯威夫特", "Taylor Swift", "taylor swift", "霉霉"}},
	{Label: "周杰伦", Category: "person", Aliases: []string{"周杰伦"}},
	{Label: "王家卫", Category: "person", Aliases: []string{"王家卫"}},
	{Label: "张艺谋", Category: "person", Aliases: []string{"张艺谋"}},
	{Label: "陈思诚", Category: "person", Aliases: []string{"陈思诚"}},
	{Label: "贾玲", Category: "person", Aliases: []string{"贾玲"}},

	// === 国内人物(企业家/网红/学者) ===
	{Label: "雷军", Category: "person", Aliases: []string{"雷军"}},
	{Label: "马斯克", Category: "person", Aliases: []string{"马斯克", "Elon Musk", "elon musk", "Musk", "musk"}},
	{Label: "黄仁勋", Category: "person", Aliases: []string{"黄仁勋", "Jensen Huang", "jensen huang"}},
	{Label: "张一鸣", Category: "person", Aliases: []string{"张一鸣"}},
	{Label: "马化腾", Category: "person", Aliases: []string{"马化腾", "Pony Ma"}},
	{Label: "马云", Category: "person", Aliases: []string{"马云", "Jack Ma"}},
	{Label: "刘强东", Category: "person", Aliases: []string{"刘强东"}},
	{Label: "丁磊", Category: "person", Aliases: []string{"丁磊"}},
	{Label: "王传福", Category: "person", Aliases: []string{"王传福"}},
	{Label: "李书福", Category: "person", Aliases: []string{"李书福"}},
	{Label: "曾毓群", Category: "person", Aliases: []string{"曾毓群"}},
	{Label: "任正非", Category: "person", Aliases: []string{"任正非"}},
	{Label: "余承东", Category: "person", Aliases: []string{"余承东"}},
	{Label: "李想", Category: "person", Aliases: []string{"李想"}},
	{Label: "李斌", Category: "person", Aliases: []string{"李斌"}},
	{Label: "何小鹏", Category: "person", Aliases: []string{"何小鹏"}},
	{Label: "周鸿祎", Category: "person", Aliases: []string{"周鸿祎"}},
	{Label: "罗永浩", Category: "person", Aliases: []string{"罗永浩", "老罗"}},
	{Label: "俞敏洪", Category: "person", Aliases: []string{"俞敏洪"}},
	{Label: "董宇辉", Category: "person", Aliases: []string{"董宇辉"}},
	{Label: "张雪峰", Category: "person", Aliases: []string{"张雪峰"}},
	{Label: "李子柒", Category: "person", Aliases: []string{"李子柒"}},
	{Label: "辛巴", Category: "person", Aliases: []string{"辛巴"}},
	{Label: "李佳琦", Category: "person", Aliases: []string{"李佳琦"}},
	{Label: "薇娅", Category: "person", Aliases: []string{"薇娅"}},

	// === 国际人物(政治/科技) ===
	{Label: "特朗普", Category: "person", Aliases: []string{"特朗普", "Trump", "trump", "川普"}},
	{Label: "拜登", Category: "person", Aliases: []string{"拜登", "Biden", "biden"}},
	{Label: "哈里斯", Category: "person", Aliases: []string{"哈里斯", "Harris", "harris"}},
	{Label: "普京", Category: "person", Aliases: []string{"普京", "Putin", "putin"}},
	{Label: "泽连斯基", Category: "person", Aliases: []string{"泽连斯基", "Zelensky", "zelensky"}},
	{Label: "内塔尼亚胡", Category: "person", Aliases: []string{"内塔尼亚胡", "Netanyahu", "netanyahu"}},
	{Label: "金正恩", Category: "person", Aliases: []string{"金正恩"}},
	{Label: "Sam Altman", Category: "person", Aliases: []string{"Sam Altman", "sam altman", "山姆奥特曼", "奥特曼"}},
	{Label: "扎克伯格", Category: "person", Aliases: []string{"扎克伯格", "Zuckerberg", "zuckerberg"}},
	{Label: "贝佐斯", Category: "person", Aliases: []string{"贝佐斯", "Bezos", "bezos"}},
	{Label: "库克", Category: "person", Aliases: []string{"库克", "Tim Cook", "tim cook"}},

	// === 体育明星 ===
	{Label: "孙颖莎", Category: "person", Aliases: []string{"孙颖莎"}},
	{Label: "樊振东", Category: "person", Aliases: []string{"樊振东"}},
	{Label: "王楚钦", Category: "person", Aliases: []string{"王楚钦"}},
	{Label: "陈梦", Category: "person", Aliases: []string{"陈梦"}},
	{Label: "全红婵", Category: "person", Aliases: []string{"全红婵"}},
	{Label: "梅西", Category: "person", Aliases: []string{"梅西", "Messi", "messi"}},
	{Label: "C罗", Category: "person", Aliases: []string{"C罗", "Ronaldo", "ronaldo", "克里斯蒂亚诺·罗纳尔多"}},

	// === 赛事 / 大事件 ===
	{Label: "NBA", Category: "event", Aliases: []string{"NBA", "nba"}},
	{Label: "CBA", Category: "event", Aliases: []string{"CBA", "cba"}},
	{Label: "世界杯", Category: "event", Aliases: []string{"世界杯", "World Cup", "world cup"}},
	{Label: "欧冠", Category: "event", Aliases: []string{"欧冠", "Champions League", "champions league"}},
	{Label: "奥运会", Category: "event", Aliases: []string{"奥运会", "奥运", "Olympics", "olympics", "巴黎奥运", "洛杉矶奥运"}},
	{Label: "亚运会", Category: "event", Aliases: []string{"亚运会", "亚运"}},
	{Label: "冬奥会", Category: "event", Aliases: []string{"冬奥会", "冬奥"}},
	{Label: "315晚会", Category: "event", Aliases: []string{"315晚会", "315", "3·15", "三一五"}},
	{Label: "双11", Category: "event", Aliases: []string{"双11", "双十一", "双 11"}},
	{Label: "618大促", Category: "event", Aliases: []string{"618大促", "618"}},
	{Label: "高考", Category: "event", Aliases: []string{"高考"}},
	{Label: "考研", Category: "event", Aliases: []string{"考研"}},
	{Label: "春节", Category: "event", Aliases: []string{"春节", "过年"}},
	{Label: "春晚", Category: "event", Aliases: []string{"春晚", "央视春晚"}},
	{Label: "国庆", Category: "event", Aliases: []string{"国庆", "国庆节"}},
	{Label: "WAIC", Category: "event", Aliases: []string{"WAIC", "waic", "世界人工智能大会"}},

	// === 政治 / 机构 ===
	{Label: "联合国", Category: "place", Aliases: []string{"联合国", "UN", "United Nations"}},
	{Label: "白宫", Category: "place", Aliases: []string{"白宫", "White House"}},
	{Label: "欧盟", Category: "place", Aliases: []string{"欧盟", "EU"}},
	{Label: "北约", Category: "place", Aliases: []string{"北约", "NATO", "nato"}},
	{Label: "美联储", Category: "place", Aliases: []string{"美联储", "Federal Reserve", "Fed"}},
	{Label: "央行", Category: "place", Aliases: []string{"央行", "中国人民银行", "人民银行"}},
}

var trackerEntityAliasIndex = buildTrackerEntityAliasIndex(trackerEntityLexicon)

var trackerEntityLabelSet = buildTrackerEntityLabelSet(trackerEntityLexicon)

var trackerEntityAliasToLabel = buildTrackerEntityAliasToLabel(trackerEntityLexicon)

// trackerEntityAliasToLabelLower 是 alias→label 的不区分大小写索引。
// 给 gse 分词时的小写化结果(openai / gpt-5)走 fallback 还原回规范 Label(OpenAI / GPT-5)。
var trackerEntityAliasToLabelLower = buildTrackerEntityAliasToLabelLower(trackerEntityLexicon)

var trackerEntityTermsByLabel = buildTrackerEntityTermsByLabel(trackerEntityLexicon)

func buildTrackerEntityLabelSet(entries []trackerLexiconEntry) map[string]struct{} {
	set := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		// 不使用 normalizeTrackerToken(会引发初始化循环依赖),直接用 TrimSpace 后的 Label。
		// Lexicon 的 Label 都是人工维护的规范形式,不需要额外标准化。
		label := strings.TrimSpace(entry.Label)
		if label == "" {
			continue
		}
		set[label] = struct{}{}
	}
	return set
}

func buildTrackerEntityAliasToLabel(entries []trackerLexiconEntry) map[string]string {
	out := make(map[string]string, len(entries)*4)
	for _, entry := range entries {
		label := normalizeLexiconAlias(entry.Label)
		if label == "" {
			continue
		}
		out[label] = label
		for _, alias := range entry.Aliases {
			needle := normalizeLexiconAlias(alias)
			if needle == "" {
				continue
			}
			out[needle] = label
		}
	}
	return out
}

// buildTrackerEntityAliasToLabelLower 跟 AliasToLabel 同源,但 key 全部 lower-case,
// 用于 gse 分词后小写 token 的反查(如 "openai" → "OpenAI")。
// 冲突时后写覆盖前写(实际 lexicon 里别名不会真冲突)。
func buildTrackerEntityAliasToLabelLower(entries []trackerLexiconEntry) map[string]string {
	out := make(map[string]string, len(entries)*4)
	for _, entry := range entries {
		label := normalizeLexiconAlias(entry.Label)
		if label == "" {
			continue
		}
		out[strings.ToLower(label)] = label
		for _, alias := range entry.Aliases {
			needle := normalizeLexiconAlias(alias)
			if needle == "" {
				continue
			}
			out[strings.ToLower(needle)] = label
		}
	}
	return out
}

func buildTrackerEntityTermsByLabel(entries []trackerLexiconEntry) map[string][]string {
	out := make(map[string][]string, len(entries))
	for _, entry := range entries {
		label := normalizeLexiconAlias(entry.Label)
		if label == "" {
			continue
		}
		seen := map[string]struct{}{label: {}}
		terms := []string{label}
		for _, alias := range entry.Aliases {
			needle := normalizeLexiconAlias(alias)
			if needle == "" {
				continue
			}
			if _, ok := seen[needle]; ok {
				continue
			}
			seen[needle] = struct{}{}
			terms = append(terms, needle)
		}
		out[label] = terms
	}
	return out
}

// ======================== Aho-Corasick 多模匹配 ========================
//
// 预构建一个 AC 自动机,patterns 是所有 alias 的 lower 形式。
// Match(lowerTitle) 返回命中的 pattern 索引,通过 acPatternLabels 映射回 Label。
// 复杂度从 O(标题数 × 别名数) 降为 O(标题长度)。

var (
	// acMatcher AC 自动机实例(启动时一次性构建)
	acMatcher *ahocorasick.Matcher
	// acPatternLabels 索引→Label 映射:acMatcher.Match 返回的整数是 patterns 数组下标
	acPatternLabels []string
	// acPatternNeedBoundary 索引→是否需要 boundary check
	acPatternNeedBoundary []bool
)

func init() {
	acMatcher, acPatternLabels, acPatternNeedBoundary = buildACMatcher(trackerEntityLexicon)
}

func buildACMatcher(entries []trackerLexiconEntry) (*ahocorasick.Matcher, []string, []bool) {
	var patterns []string
	var labels []string
	var needBoundary []bool

	seen := map[string]struct{}{}
	for _, entry := range entries {
		label := strings.TrimSpace(entry.Label)
		if label == "" {
			continue
		}
		allAliases := append([]string{label}, entry.Aliases...)
		for _, alias := range allAliases {
			lower := strings.ToLower(strings.TrimSpace(alias))
			if lower == "" || len(lower) < 2 {
				continue
			}
			if _, ok := seen[lower]; ok {
				continue
			}
			seen[lower] = struct{}{}
			patterns = append(patterns, lower)
			labels = append(labels, label)
			// 短纯英文 alias(如 "o1","cs2")需要 boundary check 防止误匹配
			needBoundary = append(needBoundary, len(lower) <= 3 && isASCII(lower))
		}
	}

	matcher := ahocorasick.NewStringMatcher(patterns)
	return matcher, labels, needBoundary
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}
