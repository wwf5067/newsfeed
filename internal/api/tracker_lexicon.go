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
	{Label: "AI", Category: "event", Aliases: []string{"AI", "人工智能", "Artificial Intelligence"}},
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

	// === AI 工具 / 视频生成 / 开发工具 ===
	// 新兴 AI 产品:单独成段,与"大模型公司"区分(部分是工具而非公司)。
	{Label: "Cursor", Category: "company", Aliases: []string{"Cursor", "cursor"}},
	{Label: "Windsurf", Category: "company", Aliases: []string{"Windsurf", "windsurf"}},
	{Label: "Sora", Category: "company", Aliases: []string{"Sora", "sora"}},
	{Label: "Runway", Category: "company", Aliases: []string{"Runway", "runway", "Runway ML", "runway ml"}},
	{Label: "可灵", Category: "company", Aliases: []string{"可灵", "Kling", "kling", "可灵AI"}},
	{Label: "Veo", Category: "company", Aliases: []string{"Veo", "veo", "Veo2", "Veo3", "veo2", "veo3"}},
	{Label: "Luma", Category: "company", Aliases: []string{"Luma", "luma", "Luma AI", "luma ai", "Dream Machine"}},
	{Label: "即梦", Category: "company", Aliases: []string{"即梦", "即梦AI"}},
	{Label: "MidJourney", Category: "company", Aliases: []string{"MidJourney", "midjourney", "Midjourney"}},
	{Label: "Stable Diffusion", Category: "company", Aliases: []string{"Stable Diffusion", "stable diffusion", "ComfyUI", "comfyui", "Flux", "flux"}},
	{Label: "Grok", Category: "company", Aliases: []string{"Grok", "grok", "xAI", "xai"}},
	{Label: "Perplexity", Category: "company", Aliases: []string{"Perplexity", "perplexity", "Perplexity AI"}},
	{Label: "MCP协议", Category: "event", Aliases: []string{"MCP协议", "MCP", "Model Context Protocol"}},
	{Label: "RAG", Category: "event", Aliases: []string{"RAG", "rag", "检索增强生成"}},
	{Label: "AI Agent", Category: "event", Aliases: []string{"AI Agent", "ai agent", "智能体"}},
	{Label: "Vibe Coding", Category: "event", Aliases: []string{"Vibe Coding", "vibe coding", "氛围编程"}},
	{Label: "LangChain", Category: "company", Aliases: []string{"LangChain", "langchain"}},

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
	{Label: "小米", Category: "company", Aliases: []string{"小米", "Xiaomi", "xiaomi", "小米SU7", "SU7", "小米YU7", "YU7", "小米澎湃"}},
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
	{Label: "联想", Category: "company", Aliases: []string{"联想", "Lenovo", "lenovo", "ThinkPad", "thinkpad"}},
	{Label: "拯救者", Category: "company", Aliases: []string{"拯救者", "Legion", "legion"}}, // 联想旗下游戏品牌
	{Label: "戴尔", Category: "company", Aliases: []string{"戴尔", "Dell", "dell"}},
	{Label: "惠普", Category: "company", Aliases: []string{"惠普", "HP", "hp"}},

	// === 消费品牌(B 站/小红书/抖音高频)===
	// 选词原则:用户群高频提到 + 不会跟通用词冲突。
	// 注:小米/华为/苹果/三星 已在硬件科技段;特斯拉/理想/蔚来/小鹏在新能源段。
	{Label: "戴森", Category: "company", Aliases: []string{"戴森", "Dyson", "dyson"}},
	{Label: "徕芬", Category: "company", Aliases: []string{"徕芬", "Laifen", "laifen"}},
	{Label: "追觅", Category: "company", Aliases: []string{"追觅", "Dreame", "dreame"}},
	{Label: "石头科技", Category: "company", Aliases: []string{"石头科技", "石头扫地机", "Roborock", "roborock"}},
	{Label: "科沃斯", Category: "company", Aliases: []string{"科沃斯", "Ecovacs", "ecovacs"}},
	{Label: "海尔", Category: "company", Aliases: []string{"海尔", "Haier", "haier"}},
	{Label: "美的", Category: "company", Aliases: []string{"美的", "Midea", "midea"}},
	{Label: "格力", Category: "company", Aliases: []string{"格力", "Gree"}},
	{Label: "九阳", Category: "company", Aliases: []string{"九阳", "Joyoung"}},
	{Label: "苏泊尔", Category: "company", Aliases: []string{"苏泊尔", "Supor"}},
	{Label: "飞利浦", Category: "company", Aliases: []string{"飞利浦", "Philips", "philips"}},
	{Label: "松下", Category: "company", Aliases: []string{"松下", "Panasonic", "panasonic"}},
	{Label: "索尼", Category: "company", Aliases: []string{"索尼", "Sony", "SONY"}},
	{Label: "SK-II", Category: "company", Aliases: []string{"SK-II", "SK2", "sk-ii", "sk2"}},
	{Label: "雅诗兰黛", Category: "company", Aliases: []string{"雅诗兰黛", "Estee Lauder"}},
	{Label: "兰蔻", Category: "company", Aliases: []string{"兰蔻", "Lancome", "lancome"}},
	{Label: "乐高", Category: "company", Aliases: []string{"乐高", "Lego", "LEGO"}},
	{Label: "宜家", Category: "company", Aliases: []string{"宜家", "IKEA", "ikea"}},
	{Label: "无印良品", Category: "company", Aliases: []string{"无印良品", "MUJI", "muji"}},
	{Label: "优衣库", Category: "company", Aliases: []string{"优衣库", "Uniqlo", "UNIQLO", "uniqlo"}},
	{Label: "ZARA", Category: "company", Aliases: []string{"ZARA", "zara"}},
	{Label: "耐克", Category: "company", Aliases: []string{"耐克", "Nike", "NIKE"}},
	{Label: "阿迪达斯", Category: "company", Aliases: []string{"阿迪达斯", "Adidas", "ADIDAS", "adidas"}},
	{Label: "新百伦", Category: "company", Aliases: []string{"新百伦", "New Balance", "new balance"}},

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
	{Label: "崩坏：星穹铁道", Category: "ip", Aliases: []string{"崩坏：星穹铁道", "崩坏星穹铁道", "星穹铁道", "Honkai Star Rail", "honkai star rail"}},
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
	// 近期热门电影
	{Label: "消失的她", Category: "ip", Aliases: []string{"消失的她"}},
	{Label: "孤注一掷", Category: "ip", Aliases: []string{"孤注一掷"}},
	{Label: "封神", Category: "ip", Aliases: []string{"封神", "封神第一部", "封神第二部", "封神三部曲", "封神传奇"}},
	{Label: "周处除三害", Category: "ip", Aliases: []string{"周处除三害"}},
	{Label: "抓娃娃", Category: "ip", Aliases: []string{"抓娃娃"}},
	{Label: "默杀", Category: "ip", Aliases: []string{"默杀"}},
	{Label: "热辣滚烫", Category: "ip", Aliases: []string{"热辣滚烫"}},
	{Label: "年会不能停", Category: "ip", Aliases: []string{"年会不能停"}},
	{Label: "飞驰人生", Category: "ip", Aliases: []string{"飞驰人生", "飞驰人生2"}},
	{Label: "满江红", Category: "ip", Aliases: []string{"满江红"}},
	// 近期热门综艺
	{Label: "歌手2024", Category: "ip", Aliases: []string{"歌手2024", "歌手 2024", "歌手2025", "歌手 2025"}},
	{Label: "声生不息", Category: "ip", Aliases: []string{"声生不息", "声生不息·港乐季", "声生不息·宝岛季"}},
	{Label: "浪姐", Category: "ip", Aliases: []string{"浪姐", "乘风破浪", "乘风2024", "乘风2023", "乘风破浪的姐姐"}},
	{Label: "披哥", Category: "ip", Aliases: []string{"披哥", "披荆斩棘", "披荆斩棘的哥哥"}},
	{Label: "中国有嘻哈", Category: "ip", Aliases: []string{"中国有嘻哈", "说唱新世代", "中国说唱"}},
	{Label: "脱口秀大会", Category: "ip", Aliases: []string{"脱口秀大会", "喜剧之王单口季"}},
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
	// 围棋
	{Label: "柯洁", Category: "person", Aliases: []string{"柯洁"}},
	{Label: "古力", Category: "person", Aliases: []string{"古力"}},
	{Label: "聂卫平", Category: "person", Aliases: []string{"聂卫平"}},
	{Label: "井山裕太", Category: "person", Aliases: []string{"井山裕太"}},
	{Label: "申真谞", Category: "person", Aliases: []string{"申真谞"}},
	{Label: "全红婵", Category: "person", Aliases: []string{"全红婵"}},
	{Label: "梅西", Category: "person", Aliases: []string{"梅西", "Messi", "messi"}},
	{Label: "C罗", Category: "person", Aliases: []string{"C罗", "Ronaldo", "ronaldo", "克里斯蒂亚诺·罗纳尔多"}},
	{Label: "内马尔", Category: "person", Aliases: []string{"内马尔", "Neymar", "neymar"}},
	{Label: "姆巴佩", Category: "person", Aliases: []string{"姆巴佩", "Mbappe", "mbappe"}},
	{Label: "哈兰德", Category: "person", Aliases: []string{"哈兰德", "Haaland", "haaland"}},
	{Label: "文班亚马", Category: "person", Aliases: []string{"文班亚马", "Wembanyama", "wembanyama", "文班"}},
	{Label: "詹姆斯", Category: "person", Aliases: []string{"勒布朗·詹姆斯", "LeBron", "lebron", "詹姆斯"}},
	{Label: "库里", Category: "person", Aliases: []string{"库里", "Curry", "curry", "斯蒂芬·库里"}},
	// 葡萄牙国家队骨干球员(常出现在大赛名单标题)
	{Label: "B费", Category: "person", Aliases: []string{"B费", "B 费", "布鲁诺·费尔南德斯", "Bruno Fernandes"}},
	{Label: "B席", Category: "person", Aliases: []string{"B席", "B 席", "贝尔纳多·席尔瓦", "Bernardo Silva"}},
	{Label: "维蒂尼亚", Category: "person", Aliases: []string{"维蒂尼亚", "Vitinha"}},
	{Label: "莱奥", Category: "person", Aliases: []string{"莱奥", "Rafael Leao", "拉法埃尔·莱奥"}},
	{Label: "若塔", Category: "person", Aliases: []string{"若塔", "Diogo Jota"}},
	{Label: "鲁本·迪亚斯", Category: "person", Aliases: []string{"鲁本·迪亚斯", "Ruben Dias"}},
	// 阿根廷
	{Label: "迪马利亚", Category: "person", Aliases: []string{"迪马利亚", "Di Maria"}},
	// 法国
	{Label: "格列兹曼", Category: "person", Aliases: []string{"格列兹曼", "Griezmann"}},
	{Label: "登贝莱", Category: "person", Aliases: []string{"登贝莱", "Dembele"}},
	// 英格兰
	{Label: "哈里·凯恩", Category: "person", Aliases: []string{"哈里·凯恩", "Harry Kane", "凯恩"}},
	{Label: "贝林厄姆", Category: "person", Aliases: []string{"贝林厄姆", "Bellingham"}},

	// === 体育队伍(英超 / 西甲 / 意甲 / 德甲 / 法甲 / NBA / 国家队)===
	// 这些队伍名经常出现在赛事标题里(阿森纳 1-0 战胜伯恩利、马刺 vs 雷霆),
	// 没有词典命中就会落到 dataLikePatterns 整段过滤逻辑,导致整条标题没 entity。
	// 中英 Aliases 让国际化标题(zhihu 偶尔出现 Manchester United)也能匹配。
	{Label: "阿森纳", Category: "team", Aliases: []string{"阿森纳", "Arsenal", "arsenal"}},
	{Label: "曼联", Category: "team", Aliases: []string{"曼联", "曼彻斯特联", "Manchester United", "Man Utd"}},
	{Label: "曼城", Category: "team", Aliases: []string{"曼城", "Manchester City", "Man City"}},
	{Label: "利物浦", Category: "team", Aliases: []string{"利物浦", "Liverpool", "liverpool"}},
	{Label: "切尔西", Category: "team", Aliases: []string{"切尔西", "Chelsea", "chelsea"}},
	{Label: "热刺", Category: "team", Aliases: []string{"热刺", "Tottenham", "tottenham", "Spurs"}},
	{Label: "纽卡斯尔", Category: "team", Aliases: []string{"纽卡斯尔", "Newcastle", "newcastle"}},
	{Label: "伯恩利", Category: "team", Aliases: []string{"伯恩利", "Burnley", "burnley"}},
	{Label: "皇马", Category: "team", Aliases: []string{"皇马", "皇家马德里", "Real Madrid"}},
	{Label: "巴萨", Category: "team", Aliases: []string{"巴萨", "巴塞罗那", "Barcelona", "barcelona"}},
	{Label: "马竞", Category: "team", Aliases: []string{"马竞", "马德里竞技", "Atletico Madrid"}},
	{Label: "国际米兰", Category: "team", Aliases: []string{"国际米兰", "Inter Milan", "Inter"}},
	{Label: "AC米兰", Category: "team", Aliases: []string{"AC米兰", "AC Milan", "ac milan"}},
	{Label: "尤文图斯", Category: "team", Aliases: []string{"尤文图斯", "尤文", "Juventus", "juventus"}},
	{Label: "拜仁", Category: "team", Aliases: []string{"拜仁", "拜仁慕尼黑", "Bayern", "Bayern Munich"}},
	{Label: "多特蒙德", Category: "team", Aliases: []string{"多特蒙德", "多特", "Dortmund", "BVB"}},
	{Label: "巴黎圣日耳曼", Category: "team", Aliases: []string{"巴黎圣日耳曼", "大巴黎", "PSG", "psg", "Paris Saint-Germain"}},
	{Label: "马刺", Category: "team", Aliases: []string{"马刺", "圣安东尼奥马刺", "Spurs"}},
	{Label: "雷霆", Category: "team", Aliases: []string{"雷霆", "俄克拉荷马雷霆", "Thunder"}},
	{Label: "湖人", Category: "team", Aliases: []string{"湖人", "洛杉矶湖人", "Lakers"}},
	{Label: "勇士", Category: "team", Aliases: []string{"勇士", "金州勇士", "Warriors"}},
	{Label: "凯尔特人", Category: "team", Aliases: []string{"凯尔特人", "波士顿凯尔特人", "Celtics"}},
	{Label: "热火", Category: "team", Aliases: []string{"热火", "迈阿密热火", "Heat"}},
	{Label: "公牛", Category: "team", Aliases: []string{"公牛", "芝加哥公牛", "Bulls"}},
	{Label: "骑士", Category: "team", Aliases: []string{"骑士", "克利夫兰骑士", "Cavaliers", "Cavs"}},
	{Label: "独行侠", Category: "team", Aliases: []string{"独行侠", "达拉斯独行侠", "小牛", "Mavericks"}},
	{Label: "太阳", Category: "team", Aliases: []string{"凤凰城太阳", "Phoenix Suns", "Suns"}},
	{Label: "阿根廷队", Category: "team", Aliases: []string{"阿根廷队", "阿根廷国家队"}},
	{Label: "葡萄牙队", Category: "team", Aliases: []string{"葡萄牙队", "葡萄牙国家队"}},

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

	// === 体育赛事(青年/U系列) ===
	{Label: "亚洲杯", Category: "event", Aliases: []string{"亚洲杯", "亚足联亚洲杯"}},
	{Label: "U17亚洲杯", Category: "event", Aliases: []string{"U17亚洲杯", "U17 亚洲杯"}},
	{Label: "U19亚洲杯", Category: "event", Aliases: []string{"U19亚洲杯", "U19 亚洲杯"}},
	{Label: "U21亚洲杯", Category: "event", Aliases: []string{"U21亚洲杯", "U21 亚洲杯"}},
	{Label: "U17世界杯", Category: "event", Aliases: []string{"U17世界杯", "U17 世界杯"}},
	{Label: "U19世界杯", Category: "event", Aliases: []string{"U19世界杯", "U19 世界杯"}},
	{Label: "U21世界杯", Category: "event", Aliases: []string{"U21世界杯", "U21 世界杯"}},
	{Label: "亚冠", Category: "event", Aliases: []string{"亚冠", "亚冠联赛"}},
	{Label: "中超", Category: "event", Aliases: []string{"中超", "中超联赛"}},
	{Label: "英超", Category: "event", Aliases: []string{"英超", "英超联赛", "Premier League"}},
	{Label: "西甲", Category: "event", Aliases: []string{"西甲"}},
	{Label: "意甲", Category: "event", Aliases: []string{"意甲"}},
	{Label: "德甲", Category: "event", Aliases: []string{"德甲"}},
	{Label: "法甲", Category: "event", Aliases: []string{"法甲"}},
	// 围棋赛事
	{Label: "围棋", Category: "event", Aliases: []string{"围棋"}},
	{Label: "围甲", Category: "event", Aliases: []string{"围甲", "围甲联赛", "中国围棋甲级联赛"}},
	{Label: "LG杯", Category: "event", Aliases: []string{"LG杯"}},
	{Label: "三星杯", Category: "event", Aliases: []string{"三星杯"}},
	{Label: "应氏杯", Category: "event", Aliases: []string{"应氏杯"}},
	// 赛车 / 赛道
	{Label: "F1", Category: "event", Aliases: []string{"F1", "f1", "一级方程式"}},
	{Label: "WSBK", Category: "event", Aliases: []string{"WSBK", "世界超级摩托车锦标赛"}},
	{Label: "MotoGP", Category: "event", Aliases: []string{"MotoGP", "motogp"}},
	{Label: "纽博格林", Category: "place", Aliases: []string{"纽博格林", "纽北", "Nurburgring", "Nordschleife"}},
	{Label: "勒芒", Category: "event", Aliases: []string{"勒芒", "勒芒24小时", "Le Mans"}},

	// === 中国国家队(简称在体育新闻高频)===
	{Label: "国足", Category: "team", Aliases: []string{"国足", "中国男足", "中国国家队"}},
	{Label: "国奥", Category: "team", Aliases: []string{"国奥", "中国国奥队"}},
	{Label: "国少", Category: "team", Aliases: []string{"国少", "中国国少队", "U17国少"}},
	{Label: "国青", Category: "team", Aliases: []string{"国青", "中国国青队", "U19国青"}},
	{Label: "女足", Category: "team", Aliases: []string{"女足", "中国女足"}},

	// === 政治 / 机构 ===
	{Label: "联合国", Category: "place", Aliases: []string{"联合国", "UN", "United Nations"}},
	{Label: "白宫", Category: "place", Aliases: []string{"白宫", "White House"}},
	{Label: "欧盟", Category: "place", Aliases: []string{"欧盟", "EU"}},
	{Label: "北约", Category: "place", Aliases: []string{"北约", "NATO", "nato"}},
	{Label: "美联储", Category: "place", Aliases: []string{"美联储", "Federal Reserve", "Fed"}},
	{Label: "央行", Category: "place", Aliases: []string{"央行", "中国人民银行", "人民银行"}},

	// === 国内政府部门(高频出现在政策类新闻)===
	{Label: "国务院", Category: "place", Aliases: []string{"国务院"}},
	{Label: "教育部", Category: "place", Aliases: []string{"教育部"}},
	{Label: "财政部", Category: "place", Aliases: []string{"财政部"}},
	{Label: "外交部", Category: "place", Aliases: []string{"外交部"}},
	{Label: "公安部", Category: "place", Aliases: []string{"公安部"}},
	{Label: "国家发改委", Category: "place", Aliases: []string{"国家发改委", "发改委"}},
	{Label: "国家卫健委", Category: "place", Aliases: []string{"国家卫健委", "卫健委"}},
	{Label: "证监会", Category: "place", Aliases: []string{"证监会"}},
	{Label: "银保监会", Category: "place", Aliases: []string{"银保监会"}},
	{Label: "网信办", Category: "place", Aliases: []string{"网信办", "中央网信办", "国家网信办"}},
	{Label: "工信部", Category: "place", Aliases: []string{"工信部", "工业和信息化部"}},

	// === 知名大学(zhihu/微博热议高频)===
	{Label: "清华大学", Category: "place", Aliases: []string{"清华大学", "清华"}},
	{Label: "北京大学", Category: "place", Aliases: []string{"北京大学", "北大"}},
	{Label: "复旦大学", Category: "place", Aliases: []string{"复旦大学", "复旦"}},
	{Label: "上海交通大学", Category: "place", Aliases: []string{"上海交通大学", "上海交大", "上交大", "上交"}},
	{Label: "浙江大学", Category: "place", Aliases: []string{"浙江大学", "浙大"}},
	{Label: "武汉大学", Category: "place", Aliases: []string{"武汉大学", "武大"}},
	{Label: "南京大学", Category: "place", Aliases: []string{"南京大学", "南大"}},
	{Label: "中山大学", Category: "place", Aliases: []string{"中山大学", "中大"}},
	{Label: "中国科学技术大学", Category: "place", Aliases: []string{"中国科学技术大学", "中科大", "科大"}},
	{Label: "西安交通大学", Category: "place", Aliases: []string{"西安交通大学", "西安交大", "西交"}},
	{Label: "同济大学", Category: "place", Aliases: []string{"同济大学", "同济"}},
	{Label: "北京师范大学", Category: "place", Aliases: []string{"北京师范大学", "北师大"}},
	{Label: "华东师范大学", Category: "place", Aliases: []string{"华东师范大学", "华师大"}},
	{Label: "北航", Category: "place", Aliases: []string{"北京航空航天大学", "北航"}},
	{Label: "人民大学", Category: "place", Aliases: []string{"中国人民大学", "人大", "人民大学"}},

	// === 运营商 / 通信品牌 ===
	{Label: "中国电信", Category: "company", Aliases: []string{"中国电信", "电信"}},
	{Label: "中国移动", Category: "company", Aliases: []string{"中国移动"}}, // "移动"太通用不放别名
	{Label: "中国联通", Category: "company", Aliases: []string{"中国联通", "联通"}},
	{Label: "中国广电", Category: "company", Aliases: []string{"中国广电", "广电"}},
	{Label: "天翼", Category: "company", Aliases: []string{"天翼", "天翼云"}},

	// === 大银行(国有/股份制 头部)===
	{Label: "工商银行", Category: "company", Aliases: []string{"工商银行", "工行", "ICBC"}},
	{Label: "建设银行", Category: "company", Aliases: []string{"建设银行", "建行", "CCB"}},
	{Label: "农业银行", Category: "company", Aliases: []string{"农业银行", "农行", "ABC"}},
	{Label: "中国银行", Category: "company", Aliases: []string{"中国银行", "BOC"}}, // "中行"易混不放
	{Label: "交通银行", Category: "company", Aliases: []string{"交通银行", "交行", "BCM"}},
	{Label: "招商银行", Category: "company", Aliases: []string{"招商银行", "招行", "CMB"}},
	{Label: "兴业银行", Category: "company", Aliases: []string{"兴业银行", "兴业"}},
	{Label: "浦发银行", Category: "company", Aliases: []string{"浦发银行", "浦发"}},

	// === 军事方称呼(冲突 / 国防新闻高频)===
	{Label: "美军", Category: "place", Aliases: []string{"美军", "美国军方", "US Military"}},
	{Label: "解放军", Category: "place", Aliases: []string{"解放军", "中国人民解放军", "PLA"}},
	{Label: "俄军", Category: "place", Aliases: []string{"俄军", "俄罗斯军方"}},
	{Label: "以军", Category: "place", Aliases: []string{"以军", "以色列国防军", "IDF"}},
	{Label: "伊军", Category: "place", Aliases: []string{"伊军", "伊朗革命卫队", "IRGC"}},
	{Label: "乌军", Category: "place", Aliases: []string{"乌军", "乌克兰军方"}},
	{Label: "驻日美军", Category: "place", Aliases: []string{"驻日美军", "驻日美军基地"}},
	{Label: "驻韩美军", Category: "place", Aliases: []string{"驻韩美军"}},

	// === 行业展会 / 国际组织(辅助 entitySuffixes 把"航展/车展"自动 entity 化)===
	{Label: "国际足联", Category: "event", Aliases: []string{"国际足联", "FIFA", "fifa"}},
	{Label: "亚足联", Category: "event", Aliases: []string{"亚足联", "AFC"}},
	{Label: "国际奥委会", Category: "event", Aliases: []string{"国际奥委会", "IOC"}},
	{Label: "珠海航展", Category: "event", Aliases: []string{"珠海航展", "中国国际航空航天博览会"}},
	{Label: "上海车展", Category: "event", Aliases: []string{"上海车展", "上海国际汽车工业展览会"}},
	{Label: "北京车展", Category: "event", Aliases: []string{"北京车展", "北京国际车展"}},
	{Label: "广交会", Category: "event", Aliases: []string{"广交会", "中国进出口商品交易会"}},
	{Label: "进博会", Category: "event", Aliases: []string{"进博会", "中国国际进口博览会"}},
	{Label: "服贸会", Category: "event", Aliases: []string{"服贸会", "中国国际服务贸易交易会"}},
	{Label: "数博会", Category: "event", Aliases: []string{"数博会", "中国国际大数据产业博览会"}},

	// === 补充高频热搜实体(评估 2025-05 迭代)===
	// 人物:知乎/微博热搜高频出现的明星/运动员
	{Label: "孙杨", Category: "person", Aliases: []string{"孙杨"}},
	{Label: "谷爱凌", Category: "person", Aliases: []string{"谷爱凌", "Eileen Gu", "eileen gu"}},
	{Label: "易烊千玺", Category: "person", Aliases: []string{"易烊千玺"}},
	{Label: "王一博", Category: "person", Aliases: []string{"王一博"}},
	{Label: "肖战", Category: "person", Aliases: []string{"肖战"}},
	{Label: "杨幂", Category: "person", Aliases: []string{"杨幂"}},
	{Label: "迪丽热巴", Category: "person", Aliases: []string{"迪丽热巴", "热巴"}},
	{Label: "沈奕斐", Category: "person", Aliases: []string{"沈奕斐"}},

	// 品牌/金融资产
	{Label: "茅台", Category: "company", Aliases: []string{"茅台", "贵州茅台", "Moutai", "moutai"}},
	{Label: "比特币", Category: "company", Aliases: []string{"比特币", "Bitcoin", "bitcoin", "BTC", "btc"}},
	{Label: "以太坊", Category: "company", Aliases: []string{"以太坊", "Ethereum", "ethereum", "ETH"}},
	{Label: "洁丽雅", Category: "company", Aliases: []string{"洁丽雅"}},

	// === 补充消费/国民品牌(热搜高频) ===
	// 食品饮料
	{Label: "瑞幸", Category: "company", Aliases: []string{"瑞幸", "瑞幸咖啡", "Luckin", "luckin"}},
	{Label: "蜜雪冰城", Category: "company", Aliases: []string{"蜜雪冰城", "蜜雪"}},
	{Label: "喜茶", Category: "company", Aliases: []string{"喜茶", "HEYTEA", "heytea"}},
	{Label: "奈雪", Category: "company", Aliases: []string{"奈雪", "奈雪的茶"}},
	{Label: "星巴克", Category: "company", Aliases: []string{"星巴克", "Starbucks", "starbucks"}},
	{Label: "麦当劳", Category: "company", Aliases: []string{"麦当劳", "McDonald", "mcdonald", "金拱门"}},
	{Label: "肯德基", Category: "company", Aliases: []string{"肯德基", "KFC", "kfc"}},
	{Label: "海底捞", Category: "company", Aliases: []string{"海底捞", "Haidilao"}},
	{Label: "农夫山泉", Category: "company", Aliases: []string{"农夫山泉"}},
	{Label: "伊利", Category: "company", Aliases: []string{"伊利"}},
	{Label: "蒙牛", Category: "company", Aliases: []string{"蒙牛"}},
	{Label: "娃哈哈", Category: "company", Aliases: []string{"娃哈哈", "Wahaha"}},
	{Label: "可口可乐", Category: "company", Aliases: []string{"可口可乐", "Coca-Cola", "coca-cola"}},
	{Label: "百事可乐", Category: "company", Aliases: []string{"百事可乐", "百事", "Pepsi", "pepsi"}},
	{Label: "元气森林", Category: "company", Aliases: []string{"元气森林"}},
	{Label: "依云", Category: "company", Aliases: []string{"依云", "Evian", "evian"}},
	// 日化/个护
	{Label: "花西子", Category: "company", Aliases: []string{"花西子"}},
	{Label: "完美日记", Category: "company", Aliases: []string{"完美日记"}},
	{Label: "欧莱雅", Category: "company", Aliases: []string{"欧莱雅", "L'Oreal", "loreal"}},
	{Label: "资生堂", Category: "company", Aliases: []string{"资生堂", "Shiseido", "shiseido"}},
	// 汽车(补充非新能源)
	{Label: "奔驰", Category: "company", Aliases: []string{"奔驰", "Mercedes", "mercedes", "Benz"}},
	{Label: "宝马", Category: "company", Aliases: []string{"宝马", "BMW", "bmw"}},
	{Label: "奥迪", Category: "company", Aliases: []string{"奥迪", "Audi", "audi"}},
	{Label: "丰田", Category: "company", Aliases: []string{"丰田", "Toyota", "toyota"}},
	{Label: "本田", Category: "company", Aliases: []string{"本田", "Honda", "honda"}},
	{Label: "大众", Category: "company", Aliases: []string{"大众汽车", "Volkswagen", "volkswagen", "VW"}},
	{Label: "保时捷", Category: "company", Aliases: []string{"保时捷", "Porsche", "porsche"}},
	// 地产/房企
	{Label: "恒大", Category: "company", Aliases: []string{"恒大", "恒大集团", "Evergrande"}},
	{Label: "碧桂园", Category: "company", Aliases: []string{"碧桂园", "Country Garden"}},
	{Label: "万科", Category: "company", Aliases: []string{"万科", "Vanke"}},
	{Label: "融创", Category: "company", Aliases: []string{"融创", "融创中国"}},
	// 快递/物流
	{Label: "顺丰", Category: "company", Aliases: []string{"顺丰", "顺丰快递", "SF Express"}},
	{Label: "中通", Category: "company", Aliases: []string{"中通", "中通快递"}},
	{Label: "圆通", Category: "company", Aliases: []string{"圆通", "圆通快递"}},
	// 航空
	{Label: "国航", Category: "company", Aliases: []string{"国航", "中国国航", "Air China"}},
	{Label: "南航", Category: "company", Aliases: []string{"南航", "中国南方航空"}},
	{Label: "东航", Category: "company", Aliases: []string{"东航", "中国东方航空", "东方航空"}},
	// 互联网/科技补充
	{Label: "滴滴", Category: "company", Aliases: []string{"滴滴", "滴滴出行", "DiDi", "didi"}},
	{Label: "饿了么", Category: "company", Aliases: []string{"饿了么"}},
	{Label: "携程", Category: "company", Aliases: []string{"携程", "Ctrip", "Trip.com"}},

	// 地名/国家(补充 strongGeoNames 中没有但词典命中更可靠的)
	{Label: "新加坡", Category: "place", Aliases: []string{"新加坡", "Singapore", "singapore"}},
	{Label: "伊朗", Category: "place", Aliases: []string{"伊朗", "Iran", "iran"}},
	{Label: "巴西", Category: "place", Aliases: []string{"巴西", "Brazil", "brazil"}},
	{Label: "深圳", Category: "place", Aliases: []string{"深圳", "Shenzhen", "shenzhen"}},
	{Label: "广州", Category: "place", Aliases: []string{"广州", "Guangzhou", "guangzhou"}},

	// 高校补充
	{Label: "上海财经大学", Category: "place", Aliases: []string{"上海财经大学", "上财"}},
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
	// acPatternPatterns 索引→命中的原始 alias(lower-case)
	acPatternPatterns []string
	// acPatternNeedBoundary 索引→是否需要 boundary check
	acPatternNeedBoundary []bool
)

func init() {
	acMatcher, acPatternLabels, acPatternPatterns, acPatternNeedBoundary = buildACMatcher(trackerEntityLexicon)
}

func buildACMatcher(entries []trackerLexiconEntry) (*ahocorasick.Matcher, []string, []string, []bool) {
	var patterns []string
	var labels []string
	var rawPatterns []string
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
			rawPatterns = append(rawPatterns, lower)
			labels = append(labels, label)
			// 短纯英文 alias(如 "o1","cs2")需要 boundary check 防止误匹配
			needBoundary = append(needBoundary, len(lower) <= 3 && isASCII(lower))
		}
	}

	matcher := ahocorasick.NewStringMatcher(patterns)
	return matcher, labels, rawPatterns, needBoundary
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}
