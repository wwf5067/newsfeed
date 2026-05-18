package api

// trackerEntityLexicon 是第一版人工维护的专用实体词典。
// 目标不是追求“大而全”，而是优先覆盖当前产品里高频、用户感知强的实体，
// 让 tracker 在 AI / 科技公司 / 内容平台 / 游戏 IP 这些场景里先稳定下来。
var trackerEntityLexicon = []trackerLexiconEntry{
	{Label: "OpenAI", Aliases: []string{"OpenAI", "openai"}},
	{Label: "ChatGPT", Aliases: []string{"ChatGPT", "chatgpt", "GPT-4", "GPT4", "GPT-4o", "GPT4o", "o1", "o3"}},
	{Label: "DeepSeek", Aliases: []string{"DeepSeek", "deepseek", "深度求索"}},
	{Label: "Claude", Aliases: []string{"Claude", "claude", "Anthropic", "anthropic"}},
	{Label: "Gemini", Aliases: []string{"Gemini", "gemini", "Bard", "bard"}},
	{Label: "Manus", Aliases: []string{"Manus", "manus"}},
	{Label: "月之暗面", Aliases: []string{"月之暗面", "Moonshot", "moonshot", "Kimi", "kimi"}},
	{Label: "智谱", Aliases: []string{"智谱", "智谱AI", "Zhipu", "zhipu", "ChatGLM", "chatglm"}},
	{Label: "百川智能", Aliases: []string{"百川智能", "Baichuan", "baichuan"}},
	{Label: "MiniMax", Aliases: []string{"MiniMax", "minimax", "海螺AI", "海螺"}},
	{Label: "阶跃星辰", Aliases: []string{"阶跃星辰", "StepFun", "stepfun", "跃问"}},
	{Label: "字节跳动", Aliases: []string{"字节跳动", "ByteDance", "bytedance", "抖音集团"}},
	{Label: "腾讯", Aliases: []string{"腾讯", "Tencent", "tencent", "腾讯视频", "微信", "WeChat", "wechat", "QQ"}},
	{Label: "阿里巴巴", Aliases: []string{"阿里巴巴", "阿里", "Alibaba", "alibaba", "淘宝", "天猫", "支付宝", "Alipay", "alipay", "阿里云"}},
	{Label: "百度", Aliases: []string{"百度", "Baidu", "baidu", "文心一言", "Ernie", "ERNIE"}},
	{Label: "京东", Aliases: []string{"京东", "JD", "jd", "JD.com", "jd.com"}},
	{Label: "拼多多", Aliases: []string{"拼多多", "PDD", "pdd", "Temu", "temu"}},
	{Label: "美团", Aliases: []string{"美团", "Meituan", "meituan"}},
	{Label: "网易", Aliases: []string{"网易", "NetEase", "netease"}},
	{Label: "小米", Aliases: []string{"小米", "Xiaomi", "xiaomi", "雷军"}},
	{Label: "华为", Aliases: []string{"华为", "Huawei", "huawei", "鸿蒙", "HarmonyOS", "harmonyos"}},
	{Label: "苹果", Aliases: []string{"苹果", "Apple", "apple", "iPhone", "iOS", "MacBook", "macOS", "macos"}},
	{Label: "微软", Aliases: []string{"微软", "Microsoft", "microsoft", "Copilot", "copilot", "Azure", "azure"}},
	{Label: "谷歌", Aliases: []string{"谷歌", "Google", "google", "Android", "android", "YouTube", "youtube"}},
	{Label: "Meta", Aliases: []string{"Meta", "meta", "Facebook", "facebook", "Instagram", "instagram", "WhatsApp", "whatsapp"}},
	{Label: "英伟达", Aliases: []string{"英伟达", "NVIDIA", "nvidia", "黄仁勋", "CUDA", "cuda"}},
	{Label: "特斯拉", Aliases: []string{"特斯拉", "Tesla", "tesla", "马斯克", "Elon Musk", "elon musk"}},
	{Label: "比亚迪", Aliases: []string{"比亚迪", "BYD", "byd"}},
	{Label: "理想汽车", Aliases: []string{"理想汽车", "理想", "Li Auto", "li auto", "Lixiang", "lixiang"}},
	{Label: "蔚来", Aliases: []string{"蔚来", "NIO", "nio"}},
	{Label: "小鹏", Aliases: []string{"小鹏", "小鹏汽车", "XPeng", "xpeng"}},
	{Label: "宁德时代", Aliases: []string{"宁德时代", "CATL", "catl"}},
	{Label: "宇树科技", Aliases: []string{"宇树科技", "宇树", "Unitree", "unitree"}},
	{Label: "B站", Aliases: []string{"B站", "哔哩哔哩", "Bilibili", "bilibili"}},
	{Label: "知乎", Aliases: []string{"知乎", "Zhihu", "zhihu"}},
	{Label: "微博", Aliases: []string{"微博", "Weibo", "weibo"}},
	{Label: "抖音", Aliases: []string{"抖音", "Douyin", "douyin", "TikTok", "tiktok"}},
	{Label: "快手", Aliases: []string{"快手", "Kuaishou", "kuaishou"}},
	{Label: "小红书", Aliases: []string{"小红书", "RED", "red", "rednote", "RedNote"}},
	{Label: "王者荣耀", Aliases: []string{"王者荣耀", "Honor of Kings", "honor of kings"}},
	{Label: "英雄联盟", Aliases: []string{"英雄联盟", "LOL", "lol", "League of Legends", "league of legends"}},
	{Label: "原神", Aliases: []string{"原神", "Genshin", "genshin", "Genshin Impact", "genshin impact"}},
	{Label: "崩坏：星穹铁道", Aliases: []string{"崩坏：星穹铁道", "星穹铁道", "Honkai Star Rail", "honkai star rail"}},
}

var trackerEntityAliasIndex = buildTrackerEntityAliasIndex(trackerEntityLexicon)

var trackerEntityLabelSet = buildTrackerEntityLabelSet(trackerEntityLexicon)

var trackerEntityAliasToLabel = buildTrackerEntityAliasToLabel(trackerEntityLexicon)

var trackerEntityTermsByLabel = buildTrackerEntityTermsByLabel(trackerEntityLexicon)

func buildTrackerEntityLabelSet(entries []trackerLexiconEntry) map[string]struct{} {
	set := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		label := normalizeTrackerToken(entry.Label)
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
		label := normalizeTrackerToken(entry.Label)
		if label == "" {
			continue
		}
		out[label] = label
		for _, alias := range entry.Aliases {
			needle := normalizeTrackerToken(alias)
			if needle == "" {
				continue
			}
			out[needle] = label
		}
	}
	return out
}

func buildTrackerEntityTermsByLabel(entries []trackerLexiconEntry) map[string][]string {
	out := make(map[string][]string, len(entries))
	for _, entry := range entries {
		label := normalizeTrackerToken(entry.Label)
		if label == "" {
			continue
		}
		seen := map[string]struct{}{label: {}}
		terms := []string{label}
		for _, alias := range entry.Aliases {
			needle := normalizeTrackerToken(alias)
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
