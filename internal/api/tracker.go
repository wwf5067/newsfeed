package api

import (
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/wwf5067/newsfeed/internal/model"
)

type trackerWindow struct {
	Hours int `json:"hours"`
}

type trackerSourceStat struct {
	SourceKey string `json:"source_key"`
	Count     int    `json:"count"`
}

type trackerArticleRef struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	SourceKey   string    `json:"source_key"`
	Heat        string    `json:"heat"`
	HeatValue   int64     `json:"heat_value"`
	PublishedAt time.Time `json:"published_at"` // 前端按时间分组用
}

type trackerTopic struct {
	Label            string              `json:"label"`
	Kind             string              `json:"kind"`
	Score            int64               `json:"score"`
	PrevScore        int64               `json:"prev_score"`
	ScoreDelta       int64               `json:"score_delta"`
	Count            int                 `json:"count"`
	PrevCount        int                 `json:"prev_count"`
	CountDelta       int                 `json:"count_delta"`
	Momentum         string              `json:"momentum"`
	Sources          []trackerSourceStat `json:"sources"`
	RelatedTerms     []string            `json:"related_terms"`
	Articles         []trackerArticleRef `json:"articles"`
	IsHeatDiscovered bool                `json:"is_heat_discovered,omitempty"`
	SampleArticle    *trackerArticleRef  `json:"sample_article,omitempty"`
}

type trackerResp struct {
	Window trackerWindow       `json:"window"`
	Items  []trackerTopic      `json:"items"`
	Events []trackerEventGroup `json:"events,omitempty"`
}

// trackerEventGroup 事件聚类结果:多篇相关文章(共享≥2个实体)合并为一个"事件"。
type trackerEventGroup struct {
	Title                  string              `json:"title"`
	Entities               []string            `json:"entities"`
	Keywords               []string            `json:"keywords"`
	Score                  int64               `json:"score"`
	Count                  int                 `json:"count"`
	Momentum               string              `json:"momentum"`
	Sources                []trackerSourceStat `json:"sources"`
	Articles               []trackerArticleRef `json:"articles"`
	HeatDiscoveredEntities []string            `json:"heat_discovered_entities,omitempty"`
	HeatDiscoveredKeywords []string            `json:"heat_discovered_keywords,omitempty"`
}

type trackerStorylineResp struct {
	Term          string              `json:"term"`
	Window        trackerWindow       `json:"window"`
	Summary       []string            `json:"summary"`
	Sources       []trackerSourceStat `json:"sources"`
	Items         []trackerArticleRef `json:"items"`
	Momentum      string              `json:"momentum"`
	ScoreDelta    int64               `json:"score_delta"` // 窗口内真实热度净增(基于 snapshot,跟 window 对齐)
	NewCount      int                 `json:"new_count"`   // 窗口内新出现的文章数(baseline snapshot 不存在)
	TotalArticles int                 `json:"total_articles"`
}

type trackerAccumulator struct {
	Label            string
	Kind             string
	Score            int64 // 绝对热度累加(降级排序信号,snapshot 不可用时用)
	WindowDelta      int64 // 窗口内真实热度增量累加(基于 snapshot,主排序/momentum 信号)
	NewCount         int   // 窗口内新出现的关联文章数
	Count            int
	Sources          map[string]int
	RelatedTerms     map[string]struct{}
	SampleArticle    *trackerArticleRef
	Articles         []trackerArticleRef // 所有关联文章引用
	IsHeatDiscovered bool                // 该词是否来自热词发现(非静态词典)
}

type trackerCandidate struct {
	Label        string
	Kind         string
	RelatedTerms []string
}

type trackerLexiconEntry struct {
	Label    string
	Aliases  []string
	Category string // 元数据,前端可据此分类筛选(company/person/ip/event/place);空字符串表示未归类
}

type trackerLexiconAlias struct {
	Label            string
	Needle           string
	Lower            string
	RequiresBoundary bool
}

type trackerEventArticleMeta struct {
	id          int64
	title       string
	sourceKey   string
	heatValue   int64
	publishedAt time.Time
	entities    []string
	keywords    []string
}

// heatBlacklistSet 热词黑名单(内存缓存,启动时从 DB 加载,删除时实时更新)。
// 黑名单中的词不参与热词发现、不参与实体抽取、不出 topic 卡片。
var (
	heatBlacklistSet = map[string]struct{}{}
	heatBlacklistMu  sync.RWMutex
)

// LoadHeatBlacklist 启动时从 DB 加载黑名单到内存。
func LoadHeatBlacklist(words []string) {
	heatBlacklistMu.Lock()
	defer heatBlacklistMu.Unlock()
	heatBlacklistSet = make(map[string]struct{}, len(words))
	for _, w := range words {
		heatBlacklistSet[w] = struct{}{}
	}
}

// AddToHeatBlacklist 运行时添加词到黑名单。
func AddToHeatBlacklist(word string) {
	heatBlacklistMu.Lock()
	defer heatBlacklistMu.Unlock()
	heatBlacklistSet[word] = struct{}{}
}

// isBlacklisted 检查词是否在黑名单中。
func isBlacklisted(word string) bool {
	heatBlacklistMu.RLock()
	defer heatBlacklistMu.RUnlock()
	_, ok := heatBlacklistSet[word]
	return ok
}

var (
	trackerTokenRegex = regexp.MustCompile(
		`[#A-Za-z0-9][#A-Za-z0-9+._-]{1,31}` +
			`|[\p{Han}A-Za-z0-9][\p{Han}A-Za-z0-9·.]{1,15}` +
			`|[\p{Han}]{2,12}`,
	)
	trackerTitleSplitRegex = regexp.MustCompile(`[|｜/:：,，。！？!?()（）\[\]【】<>《》"“”‘’·]+`)
	// bookTitleRegex 提取《...》 内的作品名(电影/综艺/书/歌曲),
	// 内容长度 1-20 字,无嵌套(子组只匹配非闭合括号字符)。
	// 只有《》包围的内容才 force entity,「」/『』仅进入 pool 走正常过滤。
	bookTitleRegex = regexp.MustCompile(`[《]([^》]{1,20})[》]`)
	// quotedPhraseRegex 提取「...」/『...』 内的引用短语。
	// 这些在知乎标题中常用于引用普通短语(如"肉夹馍""受害者思维"),不是作品名。
	// 进入 pool 但不 force entity,走正常的 shouldKeepTrackerToken 过滤。
	quotedPhraseRegex = regexp.MustCompile(`[「『]([^」』]{1,20})[」』]`)
	// honorificRegex 提取"X+院士/教授/总裁/董事长/市长/省长/部长"形式的人名+头衔。
	// gse 切词常把"方岱宁院士"切成 ["方岱宁", "院士"] 两段,前者 3 字汉字不命中
	// 任何 entity 启发式,后者单字头衔被弱过滤丢。这里在切词前用正则把整段
	// 拽出来作为 entity 强候选,让用户能看到"X院士"这种事件主体。
	// 限制人名 2-4 字汉字以减少误命中(如"成功院士"中的"成功"不是人名)。
	honorificRegex = regexp.MustCompile(`[\p{Han}]{2,4}(院士|教授|总裁|董事长|市长|省长|部长)`)
	stopTokens     = map[string]struct{}{
		"这个": {}, "那个": {}, "一个": {}, "一次": {}, "一些": {}, "一种": {},
		"我们": {}, "你们": {}, "他们": {}, "大家": {}, "自己": {}, "别人": {},
		"什么": {}, "哪些": {}, "怎么": {}, "如何": {}, "为什么": {}, "为何": {}, "为啥": {},
		"是否": {}, "能否": {}, "可以": {}, "可能": {}, "应该": {}, "需要": {},
		"已经": {}, "正在": {}, "一直": {}, "终于": {}, "到底": {}, "竟然": {},
		"居然": {}, "其实": {}, "原来": {}, "只是": {}, "而已": {}, "之后": {},
		"之前": {}, "目前": {}, "当前": {}, "现在": {}, "以后": {}, "以前": {},
		"表示": {}, "认为": {}, "发现": {}, "出现": {}, "进行": {}, "开始": {},
		"继续": {}, "成为": {}, "属于": {}, "引发": {}, "导致": {}, "造成": {},
		"相关": {}, "有关": {}, "关于": {}, "对于": {}, "通过": {}, "根据": {},
		"知乎": {}, "热榜": {}, "热门": {}, "视频": {}, "话题": {}, "网友": {},
		"官方": {}, "回应": {}, "发布": {}, "新闻": {}, "记者": {}, "记者称": {},
		"万热度": {}, "万播放": {}, "播放": {}, "热度": {}, "全文": {}, "全文如下": {},
		"直播": {}, "网友称": {}, "详情": {}, "原文": {}, "账号": {}, "博主": {},
		"评论区": {}, "最新消息": {}, "哔哩哔哩": {}, "bilibili": {},
		"关注": {}, "收藏": {}, "转发": {}, "评论": {}, "回答": {}, "问题": {},
		"曝光": {}, "爆料": {}, "独家": {}, "突发": {}, "重磅": {}, "实锤": {},
		"今天": {}, "今日": {}, "昨天": {}, "昨日": {}, "明天": {}, "明日": {},
		"最新": {}, "最近": {}, "最大": {}, "最小": {}, "最高": {}, "最低": {},
		"非常": {}, "十分": {}, "极其": {}, "特别": {}, "真的": {}, "确实": {},
		"内容": {}, "方面": {}, "情况": {}, "事情": {}, "东西": {}, "地方": {},
		"眼睛": {}, "专业": {}, "球队": {}, "巴西队": {},
		"村民": {}, "村庄": {}, "工厂": {}, "公司": {}, "企业": {}, "政府": {},
		"警方": {}, "医院": {}, "学校": {}, "大学": {}, "法院": {}, "检方": {},
		"相关方": {}, "负责": {}, "值得关注": {},
		"警察": {}, "平民": {}, "中国人": {}, "中国游客": {}, "游客": {}, "多人": {},
		// 赛事/比赛连接词:大写英文但没语义价值
		"VS": {}, "vs": {}, "Vs": {},
		// 赛事通用词:单独成词无信息量("总决赛"/"NBA决赛"等组合词不受影响,此处为精确匹配)
		"决赛": {},
		// 套话/问句独立成段时直接命中(trim 路径之外的兜底)
		"如何评价": {}, "怎么评价": {}, "怎么看": {}, "如何看待": {},
		"哪些信息": {}, "哪些相关方": {}, "该负责": {},
		"姐妹关系": {}, "私生子": {},
		"重警告处分": {}, "警告处分": {},
		// 第三波补:讨论性套话单成段 case
		"对此你怎么看": {}, "这是什么": {}, "这意味着什么": {},
		"这会带来哪些影响": {}, "怎样解读": {}, "亟待解决": {},
		"会怎么写": {}, "是基于哪些考虑": {}, "背后原因是什么": {},
		"是什么原因导致的": {}, "有哪些影响": {}, "有哪些考虑": {},
		"应如何理解": {}, "如何解读": {}, "应如何": {}, "如何应对": {},
		"全面推广": {}, "韩国公布": {},
		// 第四波补:知乎热榜标题常见噪声碎片
		"工作人员": {}, "什么原因": {}, "什么情况": {},
		"普通人": {}, "过来人": {}, "年轻人": {},
		"年度": {}, "季度": {}, "月度": {}, "全年": {},
		// 长串残骸(切完是奇怪片段)
		"四具降落伞弹出": {}, "二三线降幅收窄": {},
		// 比分赛事残骸
		"0 AL": {}, "AL": {}, "项目六人大名单": {},
	}
	entitySuffixes = []string{
		"公司", "集团", "大学", "医院", "银行", "汽车", "平台", "手机", "芯片", "模型",
		"赛事", "政府", "委员会", "研究院", "实验室", "科技", "电子", "网络", "基金",
		"联盟", "协会", "机构", "中心", "工厂", "品牌", "航空", "铁路", "地铁",
		// 行业展会/活动:让"珠海航展""上海车展""科博会""服贸会"自动当 entity
		"航展", "车展", "展会", "博览会", "交易会",
		// 注:人物头衔(院士/教授/总裁/...)不放这里,改由 honorificRegex 在
		// extractTrackerCandidates 阶段做精准的"2-4 字汉字 + 头衔"预提取。
		// 之前放进 entitySuffixes 会让 gse 切出的"位部长""副总裁"这种 1 字
		// 前缀片段也被判 entity,污染候选(实测普京/俄罗斯 related_terms
		// 里出现过"位部长")。
	}
	entityPrefixes = []string{
		"中国", "美国", "日本", "韩国", "俄罗斯", "北京", "上海", "深圳", "广州",
		"国家", "中央", "全国",
	}
	// strongGeoNames 高频地名:2字但信息量极高,不应被 weak 过滤。
	// 只收"经常出现在热搜标题里且构成话题核心"的地名,不追求穷举。
	strongGeoNames = map[string]struct{}{
		// 国内省份/自治区(热搜标题常以省名起头,如"河南一幼儿园""山东化工厂")
		"河南": {}, "湖南": {}, "湖北": {}, "广东": {}, "山东": {}, "江苏": {},
		"浙江": {}, "四川": {}, "河北": {}, "安徽": {}, "福建": {}, "江西": {},
		"山西": {}, "陕西": {}, "云南": {}, "贵州": {}, "甘肃": {}, "吉林": {},
		"辽宁": {}, "黑龙江": {}, "海南": {}, "广西": {}, "内蒙古": {},
		"西藏": {}, "新疆": {}, "青海": {}, "宁夏": {},
		// 国内主要城市(直辖市/省会/副省级/热搜高频地级市)
		"武汉": {}, "成都": {}, "重庆": {}, "杭州": {}, "南京": {}, "西安": {},
		"长沙": {}, "天津": {}, "青岛": {}, "厦门": {}, "郑州": {}, "合肥": {},
		"苏州": {}, "东莞": {}, "佛山": {}, "昆明": {}, "贵阳": {}, "福州": {},
		"济南": {}, "沈阳": {}, "大连": {}, "哈尔滨": {},
		"宁波": {}, "无锡": {}, "温州": {}, "珠海": {}, "常州": {}, "徐州": {},
		"南昌": {}, "南宁": {}, "太原": {}, "石家庄": {}, "兰州": {}, "海口": {},
		"拉萨": {}, "银川": {}, "西宁": {}, "呼和浩特": {},
		"三亚": {}, "桂林": {}, "洛阳": {}, "烟台": {}, "开封": {},
		// 港澳台
		"台湾": {}, "香港": {}, "澳门": {}, "台北": {},
		// 亚洲国家
		"泰国": {}, "印度": {}, "越南": {}, "菲律宾": {}, "缅甸": {}, "朝鲜": {},
		// 欧美/中东/其他
		"英国": {}, "法国": {}, "德国": {}, "意大利": {}, "西班牙": {}, "葡萄牙": {},
		"荷兰": {}, "比利时": {}, "瑞士": {}, "瑞典": {}, "挪威": {},
		"乌克兰": {}, "以色列": {}, "伊朗": {},
		"巴西": {}, "澳洲": {}, "澳大利亚": {}, "新西兰": {}, "加拿大": {},
		"阿根廷": {}, "墨西哥": {},
		// 五常/高频(也在 entityPrefixes 中,但这里是 strongGeo 保底)
		"美国": {}, "中国": {}, "日本": {}, "韩国": {}, "俄罗斯": {},
		"欧盟": {}, "巴勒斯坦": {}, "巴基斯坦": {},
	}
	// strongVerbs 高信息量动词:2字但对新闻话题识别至关重要。
	// 这些词出现在标题里时几乎一定是核心事件描述,不应被 weak 过滤。
	strongVerbs = map[string]struct{}{
		"绑架": {}, "勒索": {}, "杀害": {}, "枪杀": {}, "失踪": {}, "逮捕": {},
		"判刑": {}, "起诉": {}, "举报": {}, "维权": {}, "罢工": {}, "暴动": {},
		"患癌": {}, "确诊": {}, "感染": {}, "死亡": {}, "中毒": {}, "猝死": {},
		"裁员": {}, "破产": {}, "暴雷": {}, "跑路": {}, "爆炸": {}, "坍塌": {},
		"坠毁": {}, "翻车": {}, "泄漏": {}, "污染": {}, "造假": {}, "贪腐": {},
		"霸凌": {}, "性侵": {}, "家暴": {}, "虐待": {}, "诈骗": {}, "洗钱": {},
		"降价": {}, "涨价": {}, "崩盘": {}, "熔断": {}, "退市": {}, "暴跌": {},
		"持枪": {},
		"辟谣": {}, "道歉": {}, "塌房": {}, "翻供": {}, "自首": {},
		"造谣": {}, "处罚": {}, "爆单": {},
	}
	// strongTopicNouns 高信息量话题名词:2-3字但构成热搜核心话题,不应被 weak 过滤。
	// 和 strongVerbs 的区别:这些是名词/名词性短语,不是动作,但同样有聚合价值。
	// 选词原则:频繁出现在热搜标题 + 有独立话题追踪意义 + 不会和通用词混淆。
	strongTopicNouns = map[string]struct{}{
		// 经济/房产
		"房价": {}, "楼市": {}, "股市": {}, "基金": {}, "期货": {},
		"通胀": {}, "通缩": {}, "加息": {}, "降息": {}, "汇率": {},
		"GDP": {}, "CPI": {}, "PMI": {},
		// 民生
		"高考": {}, "考研": {}, "考公": {}, "就业": {}, "失业": {},
		"社保": {}, "医保": {}, "养老": {}, "生育": {}, "房贷": {},
		"彩礼": {}, "离婚": {}, "少子化": {},
		// 科技/互联网
		"芯片": {}, "光刻机": {}, "算力": {},
		"内卷": {}, "躺平": {},
		// 社会/法治
		"醉驾": {}, "酒驾": {}, "碰瓷": {}, "传销": {},
		"网暴": {}, "AI换脸": {},
		"烈性犬": {}, "净网": {}, "公安": {},
		// 气候/环境
		"雾霾": {}, "沙尘暴": {},
		// 国际
		"关税": {}, "制裁": {}, "脱钩": {},
		// 体育赛事结果:独立成词时有强聚合价值,不应被 weak 过滤
		"夺冠": {}, "晋级": {}, "出线": {}, "夺金": {}, "夺银": {},
		"冲冠": {}, "夺标": {}, "战胜": {}, "战平": {}, "获胜": {}, "败给": {},
	}
	topicSuffixes = []string{
		"事件", "计划", "政策", "比赛", "决赛", "演唱会", "电影", "电视剧", "综艺", "事故",
		"台风", "地震", "暴雨", "洪水", "发布会", "裁员", "融资", "停运", "停播", "罢工",
		"选举", "高考", "考研", "春晚", "奥运", "世界杯", "季后赛",
		// 补充:社会/教育/气候/金融话题关键词尾缀
		"欺凌", "打假", "通胀", "高温", "降温",
		"信用卡", "涨价", "降价", "暴跌", "暴涨",
		// 事件公告/政策调整类高频尾缀
		"免签", "名单", "阵容", "排名",
		"校园暴力", "虚假宣传", "偷税漏税",
		// 医疗/民生:单独出现时是高价值话题词,不能被 weak 过滤
		"医保",
	}
	trackerTrimPrefixes = []string{"关于", "有关", "对于", "因为", "因", "就", "将", "把", "被", "让", "请问", "如何看待", "如何评价", "为什么", "怎么评价", "怎么看", "最新", "突发", "热议", "围观"}
	trackerTrimSuffixes = []string{
		"怎么回事", "是真的吗", "意味着什么", "说了什么", "最新回应", "回应",
		"发布", "表示", "来了", "出炉", "曝光", "完整版", "完整版视频", "视频",
		"全文", "详情", "后续", "什么情况", "哪些信息值得关注", "当前局势如何",
		"值得关注", "如何防范此类事情",
		// 体育动词后缀:trim 后留下"内马尔回归" → "内马尔",运动员变干净 entity
		// 注:夺冠/晋级/出线/获胜/战胜/战平/败给 已移至 strongTopicNouns,
		// 独立成词时保留语义价值;此处仅保留纯"人员动向"后缀。
		"回归", "落选", "复出", "加盟", "转会",
		// 套话/标题党/讨论性后缀
		"如何评价", "哪些信息", "哪些相关方该负责", "该负责",
		"开开眼", "大秘密", "暖心安慰", "怎么看待",
		// 新增:疑问 / 评论性套话(prod 高频残留)
		"对此你怎么看", "这是什么", "是什么原因导致的", "这会带来哪些影响",
		"这意味着什么", "怎样解读", "亟待解决", "会怎么写", "是基于哪些考虑",
		"背后原因是什么", "有哪些影响", "有哪些考虑", "应如何理解",
		"原因是什么", "如何解读", "该如何", "应如何", "如何应对",
		// "X 真的吗 / 真的假的 / 真假" 类
		"真的假的", "是真是假",
		// 知乎热榜疑问尾缀补充
		"什么原因", "什么情况", "合理吗", "你认同吗",
		"能走多远", "出路在哪", "在哪里",
	}

	// compoundGeoAbbrevs 2字合称→两个实体的拆解。
	// 热搜标题常用"美以""中美""俄乌"等缩写指代两个国家/地区。
	compoundGeoAbbrevs = map[string][2]string{
		"美以": {"美国", "以色列"},
		"中美": {"中国", "美国"},
		"中日": {"中国", "日本"},
		"中韩": {"中国", "韩国"},
		"中俄": {"中国", "俄罗斯"},
		"俄乌": {"俄罗斯", "乌克兰"},
		"巴以": {"巴勒斯坦", "以色列"},
		"美俄": {"美国", "俄罗斯"},
		"美欧": {"美国", "欧盟"},
		"中欧": {"中国", "欧盟"},
		"朝韩": {"朝鲜", "韩国"},
		"印巴": {"印度", "巴基斯坦"},
		"两岸": {"大陆", "台湾"},
	}

	// visitSuffixToEntity 动词+地名缩写→目的地实体的映射。
	// 热搜标题常以"访华""赴美""访俄"等简短动宾短语指代访问目的地,
	// 但目的地本身不出现在标题中(如"普京启程访华"→中国)。
	// 以 strings.Contains 扫描整段标题,匹配即注入对应实体。
	visitSuffixToEntity = map[string]string{
		"访华": "中国", "赴华": "中国", "来华": "中国", "入华": "中国", "赴中": "中国",
		"访美": "美国", "赴美": "美国", "访俄": "俄罗斯",
		"访欧": "欧盟", "赴欧": "欧盟",
		"访日": "日本", "赴日": "日本",
		"访韩": "韩国", "赴韩": "韩国",
		"访英": "英国", "赴英": "英国",
		"访法": "法国", "赴法": "法国",
		"访德": "德国", "赴德": "德国",
	}

	// compoundTopicKeywords 复合话题关键词:GSE 分词时容易被切散的高价值复合词。
	// 直接对标题全文扫描,确保整体形式被提取出来;在 dedup Pass 2 中也保护其不被短子串吸收。
	compoundTopicKeywords = map[string]struct{}{
		// 学术诚信
		"论文打假": {}, "论文造假": {}, "学术造假": {},
		// 校园
		"校园欺凌": {}, "校园霸凌": {}, "校园暴力": {},
		// 经济/金融事件
		"贸易谈判": {}, "贸易战": {}, "贸易摩擦": {},
		"关税战": {}, "芯片禁令": {}, "芯片战争": {},
		"债务危机": {}, "银行暴雷": {}, "股市熔断": {},
		"量化宽松": {}, "货币战争": {}, "汇率战": {},
		"财政悬崖": {}, "经济衰退": {}, "经济危机": {},
		"房地产危机": {}, "楼市崩盘": {},
		// 气候/环境
		"全球升温": {}, "气候变化": {}, "全球变暖": {},
		"极端天气": {}, "碳排放": {},
		// 体育赛事复合词
		"晋级决赛": {}, "晋级半决赛": {}, "夺得冠军": {},
		"打破纪录": {}, "创历史新高": {}, "时隔多年": {},
		"世界纪录": {}, "亚洲纪录": {},
		// 科技/数据安全
		"数据泄露": {}, "隐私保护": {}, "信息安全": {},
		"网络攻击": {}, "勒索软件": {}, "供应链攻击": {},
		"人工智能监管": {},
		// 政治/地缘事件
		"弹劾案": {}, "核谈判": {}, "朝核问题": {},
		"俄乌战争": {}, "俄乌冲突": {}, "巴以冲突": {},
		"台海局势": {}, "南海争端": {}, "领土争端": {},
		"制裁名单": {}, "出口管制": {},
		// 社会事件
		"罢工潮": {}, "大罢工": {},
		"断崖式降温": {},
	}

	// geoAdjectiveSuffixes 地名仅以"地名+职业/角色"形式出现时(如"中国车手"),
	// 该地名是形容词修饰语而非话题实体,应在最终候选中过滤。
	geoAdjectiveSuffixes = []string{
		"车手", "球员", "选手", "运动员", "赛手", "棋手",
	}

	// aiModifierSuffixes 判定 "AI" 仅作定语修饰时的后缀。
	// 例: "AI翻译" / "AI专业" / "AI课程"。
	// 这类场景里 AI 常是技术属性,不是当前事件主语实体,应避免把 "AI" 强行当 entity 输出。
	aiModifierSuffixes = []string{
		"翻译", "工具", "助手", "应用", "专业", "课程", "功能",
		"生成", "模型", "软件", "服务", "系统", "平台",
	}

	// personEntityHints 人物→关联实体推断。
	// 当标题中出现这些人物时,自动将其关联实体(国家/企业)注入候选池。
	//
	// 解决问题:
	//  - "普京启程访华" → "俄罗斯"不出现在标题里但仍是核心实体
	//  - "俄罗斯远东地区"等复合词导致独立地名无法提取
	//  - "马斯克谈特斯拉裁员" → "特斯拉"若词典未覆盖可通过人名推断补全
	//
	// 注意:应只收录"一提到此人就能确定关联实体"的强绑定关系。泛化度高的政治
	// 人物(如某国总统发言时其国家几乎必然关联)和有专属企业的企业家都适合放入。
	personEntityHints = map[string]string{
		// 政治人物 → 所属国家
		"普京":    "俄罗斯",
		"泽连斯基":  "乌克兰",
		"特朗普":   "美国",
		"拜登":    "美国",
		"哈里斯":   "美国",
		"内塔尼亚胡": "以色列",
		"马克龙":   "法国",
		"朔尔茨":   "德国",
		"梅茨":    "德国",
		"苏纳克":   "英国",
		"斯塔默":   "英国",
		"石破茂":   "日本",
		"岸田":    "日本",
		"尹锡悦":   "韩国",
		"李在明":   "韩国",
		"莫迪":    "印度",
		"哈梅内伊":  "伊朗",
		"哈马斯":   "巴勒斯坦",
		// 企业家 → 旗下核心企业
		"马斯克":  "特斯拉",
		"雷军":   "小米",
		"余承东":  "华为",
		"任正非":  "华为",
		"黄仁勋":  "英伟达",
		"贝佐斯":  "亚马逊",
		"扎克伯格": "Meta",
		"库克":   "苹果",
		"皮查伊":  "谷歌",
		"何小鹏":  "小鹏",
		"李想":   "理想汽车",
		"李斌":   "蔚来",
		"王传福":  "比亚迪",
		"曾毓群":  "宁德时代",
		"张一鸣":  "字节跳动",
		"马化腾":  "腾讯",
		"丁磊":   "网易",
		"俞敏洪":  "新东方",
		"周鸿祎":  "360",
		"刘强东":  "京东",
	}

	// broadGeoEntities 是超泛地名集合:这些实体在大量文章中作为背景出现,
	// 单独作为 topic 需要有更多文章支撑,否则会稀释真正的热点。
	// 门槛在 buildTrackerTopics 中用 broadGeoMinCount 控制。
	broadGeoEntities = map[string]struct{}{
		"中国": {}, "美国": {}, "俄罗斯": {}, "日本": {}, "英国": {},
		"德国": {}, "法国": {}, "韩国": {}, "印度": {}, "澳大利亚": {},
		"加拿大": {}, "意大利": {}, "西班牙": {}, "巴西": {}, "墨西哥": {},
		"沙特": {}, "土耳其": {}, "伊朗": {}, "以色列": {}, "巴勒斯坦": {},
		"乌克兰": {}, "欧洲": {}, "亚洲": {}, "中东": {}, "全球": {}, "世界": {},
	}

	// institutionCityHints 机构/大学→所在城市推断。
	// 当标题中出现这些机构/大学简称或全称时,若城市名未显式出现,注入城市实体。
	// 只收"城市名不含在机构名里"的情形,避免"武汉大学"这类已含城市名的重复注入。
	institutionCityHints = map[string]string{
		"清华大学":     "北京",
		"北京大学":     "北京",
		"清华":       "北京",
		"北大":       "北京",
		"复旦大学":     "上海",
		"同济大学":     "上海",
		"上海交通大学":   "上海",
		"交大":       "上海", // 仅作兜底,词典里更具体的 alias 优先
		"浙江大学":     "杭州",
		"南开大学":     "天津",
		"中山大学":     "广州",
		"中国人民大学":   "北京",
		"人民大学":     "北京",
		"中国科学技术大学": "合肥",
		"中科大":      "合肥",
		"南京大学":     "南京",
		"上海财经大学":   "上海",
		"中央财经大学":   "北京",
		"对外经济贸易大学": "北京",
	}
)

// buildTrackerTopics 把 articles 按 entity 聚合成首页热点。
//
// articles 是窗口期内的文章(handler 已按 window 过滤),deltas 是这些文章
// 在该窗口内的真实热度增量(基于 article_heat_snapshots 的首尾差);
// deltas 跟 articles 同 id 集合,nil 时降级 — 每个 entity 的 WindowDelta=0、
// NewCount=0,momentum 退化到 flat,排序仍可走 acc.Score 兜底。
//
// 历史:旧实现把 articles 按 fetched_at 切两段(recent / prev),用 prev 段的
// acc.Score 跟 recent 段比。但 acc.Score 是 scoreArticle 的累加(永远≥10000),
// 跨段比较时新文章进入会让 recent.Score > prev.Score 几乎稳定成立 → 全 up
// 偏置。改用 snapshot 真实增量后,跟 storyline 端语义一致(都是 detectMomentum
// 的 scoreDelta + newCount 模型),首页和实体页同一 term 不再矛盾。
// collectHeatDiscoveredWords 跨文章词频统计,发现热点短词。
//
// 原理:如果一个 2-4 汉字的词出现在 ≥3 篇不同文章的标题中,说明它是当前热点话题词,
// 即使不在词典/strongVerbs/strongTopicNouns 中也应该被保留。
//
// 额外做 bigram 合并:gse 可能把人名/复合词切碎(如"段永平"→"段"+"永平",
// "智商税"→"智商"+"税"),统计相邻 token 拼接的 bigram,如果 bigram 的文章频次
// ≥ 其任一组成部分,则用 bigram 替代碎片。
//
// 使用轻量 normalize(不做 weak 过滤),否则短词在统计阶段就被消灭。
// 安全防线:stopTokens 排除 + 词典已覆盖的排除 + 长度范围限制。
//
// 性能:O(N*M) N=文章数 M=平均标题词数,对 500 篇文章 < 50ms,无压力。
func collectHeatDiscoveredWords(articles []model.Article) map[string]struct{} {
	const (
		minArticles = 2 // 至少出现在 2 篇不同文章中
		// 热搜数据源已经是高度筛选的舆论热点,2 篇共现即为有效信号。
		// 原值 3 在实际生产中难以达到:同一热搜词经 url 去重后全天仅 1 行,
		// 除非同一词在多个来源(baidu/weibo/zhihu)同时上榜才能超过 1。
		minHanLen = 2 // 最少 2 个汉字
		maxHanLen = 4 // 最多 4 个汉字(超过的已能被 looksLikeTopicPhrase 保留)
		// bigram 合并允许的最大汉字长度(放宽到 5,覆盖三字姓名+修饰)
		maxBigramHanLen = 5
	)

	// word → 出现在哪些文章(按 article ID 去重)
	wordArticles := make(map[string]map[int64]struct{})
	// bigram → 出现在哪些文章
	bigramArticles := make(map[string]map[int64]struct{})

	for _, article := range articles {
		title := strings.TrimSpace(article.Title)
		if title == "" {
			continue
		}
		// 用 gse 分词提取所有词
		tokens := segmentTitle(title)
		if len(tokens) == 0 {
			continue
		}

		// 预处理: 标准化所有 token
		normalized := make([]string, len(tokens))
		for i, tok := range tokens {
			cleaned := normalizeLexiconAlias(tok)
			if cleaned == "" {
				normalized[i] = ""
				continue
			}
			normalized[i] = canonicalizeTrackerToken(cleaned)
		}

		seen := make(map[string]struct{})       // 同一篇文章内去重(unigram)
		seenBigram := make(map[string]struct{}) // 同一篇文章内去重(bigram)

		for i, cleaned := range normalized {
			// --- unigram 统计 ---
			if cleaned != "" && utf8.RuneCountInString(cleaned) >= 2 {
				hanLen := hanRuneCount(cleaned)
				if hanLen >= minHanLen && hanLen <= maxHanLen {
					if !isExcludedHeatWord(cleaned) {
						if _, ok := seen[cleaned]; !ok {
							seen[cleaned] = struct{}{}
							if wordArticles[cleaned] == nil {
								wordArticles[cleaned] = make(map[int64]struct{})
							}
							wordArticles[cleaned][article.ID] = struct{}{}
						}
					}
				}
			}

			// --- bigram 统计: 当前 token + 下一个 token ---
			if i+1 < len(normalized) {
				left := normalized[i]
				right := normalized[i+1]
				if left == "" || right == "" {
					continue
				}
				bigram := left + right
				bigramHanLen := hanRuneCount(bigram)
				// bigram 汉字长度在 [minHanLen, maxBigramHanLen] 范围内
				if bigramHanLen < minHanLen || bigramHanLen > maxBigramHanLen {
					continue
				}
				// 排除已在词典中的(不需要发现)
				if _, ok := trackerEntityLabelSet[bigram]; ok {
					continue
				}
				if isExcludedHeatWord(bigram) {
					continue
				}
				if _, ok := seenBigram[bigram]; !ok {
					seenBigram[bigram] = struct{}{}
					if bigramArticles[bigram] == nil {
						bigramArticles[bigram] = make(map[int64]struct{})
					}
					bigramArticles[bigram][article.ID] = struct{}{}
				}
			}
		}
	}

	// 筛选满足阈值的 unigram
	result := make(map[string]struct{})
	for word, arts := range wordArticles {
		if len(arts) >= minArticles {
			result[word] = struct{}{}
		}
	}

	// 筛选满足阈值的 bigram,并替代其碎片
	// 规则:如果 bigram 频次 ≥ minArticles 且 ≥ 其任一组成部分的频次,
	// 则优先保留 bigram,移除对应碎片。
	for bigram, arts := range bigramArticles {
		if len(arts) < minArticles {
			continue
		}
		// bigram 达标,加入结果
		result[bigram] = struct{}{}
		// 检查 bigram 的组成部分是否在 result 中,如果是则移除碎片
		// (避免同时出现"永平"和"段永平")
		// 方法:遍历 unigram 结果,看是否是 bigram 的子串
		for word := range result {
			if word == bigram {
				continue
			}
			if strings.Contains(bigram, word) {
				// 碎片是 bigram 的一部分,检查频次:
				// 如果碎片的频次不高于 bigram,移除碎片
				if len(wordArticles[word]) <= len(arts) {
					delete(result, word)
				}
			}
		}
	}

	return result
}

// isExcludedHeatWord 判断一个词是否应被热词发现排除。
//
// 自动泛化词过滤:利用 gse 词典词频作为"通用度"指标。
// 词频 > heatFreqThreshold 的词属于日常高频用词(如"朋友""合作""数据"),
// 在热搜标题中虽然跨文章高频,但本身没有话题追踪价值。
// 词频 ≤ 阈值或不在词典中的词(如"武契奇""智商税""段永平")则是有信息量的。
const heatFreqThreshold = 2000

func isExcludedHeatWord(word string) bool {
	if isBlacklisted(word) {
		return true
	}
	if _, ok := trackerEntityLabelSet[word]; ok {
		return true
	}
	if _, ok := strongGeoNames[word]; ok {
		return true
	}
	if _, ok := strongVerbs[word]; ok {
		return true
	}
	if _, ok := strongTopicNouns[word]; ok {
		return true
	}
	if _, ok := stopTokens[word]; ok {
		return true
	}
	if isAllDigits(word) || looksLikeNumericMeasure(word) {
		return true
	}
	// 自动泛化词过滤:gse 词典词频 > 阈值 → 太通用,不值得作为热词
	trackerSegOnce.Do(loadTrackerSegmenter)
	if trackerSegErr == nil {
		if freq, _, ok := trackerSeg.Find(word); ok && freq > heatFreqThreshold {
			return true
		}
	}
	return false
}

// topicFreqThreshold 话题级泛化词阈值。
// 比 heatFreqThreshold(2000) 更严格:独立成为话题卡片的门槛更高。
// 分界线:保留词最高 freq=510(裁员),过滤词最低 freq=635(元首)。
const topicFreqThreshold = 600

// isGenericTopicByFreq 判断一个 topic label 是否太泛化不适合独立成卡片。
// 规则:词典词频 > topicFreqThreshold 且不在任何白名单中 → 过滤。
// 白名单包括:trackerEntityLabelSet / strongGeoNames / strongVerbs / strongTopicNouns。
func isGenericTopicByFreq(label string) bool {
	// 黑名单直接拦截
	if isBlacklisted(label) {
		return true
	}
	// 白名单豁免
	if _, ok := trackerEntityLabelSet[label]; ok {
		return false
	}
	if _, ok := strongGeoNames[label]; ok {
		return false
	}
	if _, ok := strongVerbs[label]; ok {
		return false
	}
	if _, ok := strongTopicNouns[label]; ok {
		return false
	}
	// 词频检查
	trackerSegOnce.Do(loadTrackerSegmenter)
	if trackerSegErr == nil {
		if freq, _, ok := trackerSeg.Find(label); ok && freq > topicFreqThreshold {
			return true
		}
	}
	return false
}

func buildTrackerTopics(
	articles []model.Article,
	deltas []WindowDelta,
	windowHours, limit int,
) []trackerTopic {
	if windowHours <= 0 || windowHours > 720 {
		windowHours = 24
	}

	// deltas → map: articleID → WindowDelta,O(1) 查询
	deltaByID := make(map[int64]WindowDelta, len(deltas))
	for _, d := range deltas {
		deltaByID[d.ArticleID] = d
	}

	// 热度反馈式实体发现:跨文章统计词频,高频出现的短词(不在词典中)自动升级为候选。
	heatDiscovered := collectHeatDiscoveredWords(articles)
	if len(heatDiscovered) > 0 {
		words := make([]string, 0, len(heatDiscovered))
		for w := range heatDiscovered {
			words = append(words, w)
		}
		slog.Default().Debug("heat discovery", "words", words, "count", len(words))
	}

	accs := map[string]*trackerAccumulator{}
	seen := make(map[int64]map[string]struct{}, len(articles))

	for _, article := range articles {
		accumulateTrackerTopics(accs, seen, article, deltaByID[article.ID], heatDiscovered)
	}

	items := make([]trackerTopic, 0, len(accs))
	for _, acc := range accs {
		// 超泛地名(中国/美国/欧洲 等)作为话题背景词出现频率极高,
		// 需要更多文章支撑才能上榜,避免稀释真正的热点。
		const broadGeoMinCount = 5
		if _, isBroad := broadGeoEntities[acc.Label]; isBroad && acc.Count < broadGeoMinCount {
			continue
		}
		// 超泛地名即使过了门槛也不独立展示为卡片(中国/美国/俄罗斯这类),
		// 它们作为背景信息已通过 related_terms 和事件聚类体现。
		if _, isBroad := broadGeoEntities[acc.Label]; isBroad {
			continue
		}
		if acc.Count < 2 {
			continue
		}
		// 以前用 acc.Score == 0 过滤,现在还按这个走,确保 entity 至少有热度数据
		if acc.Score == 0 {
			continue
		}
		// 泛化词自动过滤:gse 词频 > topicFreqThreshold 的词太通用,
		// 不值得作为独立话题(如"男孩""事件""通报""元首")。
		// 但已在白名单(词典/strongGeo/strongVerb/strongTopicNouns)中的词跳过此检查。
		if isGenericTopicByFreq(acc.Label) {
			continue
		}
		// 文章按发布时间降序(最新在前)
		topArticles := acc.Articles
		sort.Slice(topArticles, func(i, j int) bool {
			return topArticles[i].PublishedAt.After(topArticles[j].PublishedAt)
		})

		items = append(items, trackerTopic{
			Label:            acc.Label,
			Kind:             acc.Kind,
			Score:            acc.Score,
			PrevScore:        0,
			ScoreDelta:       acc.WindowDelta,
			Count:            acc.Count,
			PrevCount:        0,
			CountDelta:       acc.NewCount,
			Momentum:         detectMomentum(acc.WindowDelta, acc.NewCount),
			Sources:          flattenTrackerSources(acc.Sources),
			RelatedTerms:     flattenTrackerTerms(acc.RelatedTerms, 4),
			Articles:         topArticles,
			IsHeatDiscovered: acc.IsHeatDiscovered,
			SampleArticle:    acc.SampleArticle,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		// 主排序:当前窗口关联文章数由大到小
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		// 次排序:热度得分
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].Label < items[j].Label
	})

	items = deduplicateTrackerTopics(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func accumulateTrackerTopics(
	accs map[string]*trackerAccumulator,
	articleSeen map[int64]map[string]struct{},
	article model.Article,
	delta WindowDelta,
	heatDiscovered map[string]struct{}, // 热度反馈发现的跨文章高频词
) {
	candidates := extractTrackerCandidates(article, heatDiscovered)
	if len(candidates) == 0 {
		return
	}
	seen := articleSeen[article.ID]
	if seen == nil {
		seen = map[string]struct{}{}
		articleSeen[article.ID] = seen
	}

	for _, c := range candidates {
		key := c.Kind + ":" + c.Label
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		acc := accs[key]
		if acc == nil {
			acc = &trackerAccumulator{
				Label:        c.Label,
				Kind:         c.Kind,
				Sources:      map[string]int{},
				RelatedTerms: map[string]struct{}{},
			}
			// 标记是否来自热词发现:不在静态词典中的词 = 热词发现产出
			if _, inLexicon := trackerEntityLabelSet[c.Label]; !inLexicon {
				if _, inGeo := strongGeoNames[c.Label]; !inGeo {
					if _, inVerb := strongVerbs[c.Label]; !inVerb {
						if _, inTopic := strongTopicNouns[c.Label]; !inTopic {
							acc.IsHeatDiscovered = true
						}
					}
				}
			}
			accs[key] = acc
		}

		acc.Count++
		acc.Score += scoreArticle(article)
		acc.WindowDelta += delta.Delta
		if delta.IsNewInWindow {
			acc.NewCount++
		}
		acc.Sources[article.SourceKey]++
		acc.Articles = append(acc.Articles, trackerArticleRef{
			ID:          article.ID,
			Title:       article.Title,
			SourceKey:   article.SourceKey,
			Heat:        article.Heat,
			HeatValue:   article.HeatValue,
			PublishedAt: article.PublishedAt,
		})
		for _, related := range c.RelatedTerms {
			if related != acc.Label {
				acc.RelatedTerms[related] = struct{}{}
			}
		}
		if acc.SampleArticle == nil || article.HeatValue > acc.SampleArticle.HeatValue {
			acc.SampleArticle = &trackerArticleRef{
				ID:        article.ID,
				Title:     article.Title,
				SourceKey: article.SourceKey,
				Heat:      article.Heat,
				HeatValue: article.HeatValue,
			}
		}
	}
}

func extractTrackerCandidates(article model.Article, heatDiscovered map[string]struct{}) []trackerCandidate {
	title := strings.TrimSpace(article.Title)
	if title == "" {
		return nil
	}

	ordered := make([]string, 0, 8)
	poolSeen := map[string]struct{}{}
	// repeatedTokens 记录在标题中出现 ≥2 次的词(gse 切词后统计)。
	// 重复出现 = 作者刻意强调的核心话题词,即使长度 ≤3 字也应保留,跳过 weak 过滤。
	repeatedTokens := map[string]struct{}{}
	// forcedEntities 由"被《》明确包围的内容"组成,跳过 looksLikeEntity 的
	// 启发式判定,直接标 entity。这样《给阿嬷的情书》《影·迷》这种作品名就算
	// 没在词典里也能正确识别为 entity,而不是落到 keyword/被句子检测丢掉。
	forcedEntities := map[string]struct{}{}

	// -1. 《》包围的内容预提取为高优先级 entity(电影/综艺/书名/作品名)。
	//     trackerTitleSplitRegex 已经把这些括号当切分符,但切完只剩"裸内容",
	//     在 ordered 里跟普通片段没区别 — 长度超过 8 字会被 looksLikeSentence 丢。
	//     这里多走一遍正则把它们标成 forced entity,后面 shouldKeepTrackerToken
	//     看到这个标记就跳过过滤。
	for _, m := range bookTitleRegex.FindAllStringSubmatch(title, -1) {
		if name := strings.TrimSpace(m[1]); name != "" {
			normalized := canonicalizeTrackerToken(name)
			if normalized == "" {
				continue
			}
			// 即使被《》包围,如果是套话(如「私生子」「姐妹关系」)或通用英文词,
			// 也不应强制当 entity — 它们是引号引用,不是作品名。
			if isGenericRoleToken(normalized) || isGenericEnglishWord(normalized) {
				continue
			}
			forcedEntities[normalized] = struct{}{}
			appendTrackerPool(&ordered, poolSeen, normalized)
		}
	}

	// -0.5. 「」/『』包围的引用短语:进入 pool 但不 force entity。
	//       知乎标题大量使用「」来引用普通短语(如"肉夹馍""受害者思维""做好抗通胀准备"),
	//       这些不是作品名,应走正常的 shouldKeepTrackerToken 过滤流程。
	for _, m := range quotedPhraseRegex.FindAllStringSubmatch(title, -1) {
		if name := strings.TrimSpace(m[1]); name != "" {
			normalized := canonicalizeTrackerToken(name)
			if normalized == "" {
				continue
			}
			appendTrackerPool(&ordered, poolSeen, normalized)
		}
	}

	// -0.3. 头衔人名预提取:"X+院士/教授/总裁/董事长/市长/省长/部长"整段当 entity。
	//       gse 切词常把"方岱宁院士"切成两段都识别不出,这里整段拽出来强制 entity。
	for _, m := range honorificRegex.FindAllString(title, -1) {
		normalized := canonicalizeTrackerToken(m)
		if normalized == "" {
			continue
		}
		forcedEntities[normalized] = struct{}{}
		appendTrackerPool(&ordered, poolSeen, normalized)
	}

	// 0. 合称拆解:检测标题中的2字合称(美以/中美/俄乌等),展开为两个独立实体。
	for abbrev, pair := range compoundGeoAbbrevs {
		if strings.Contains(title, abbrev) {
			appendTrackerPool(&ordered, poolSeen, pair[0])
			appendTrackerPool(&ordered, poolSeen, pair[1])
		}
	}

	// 0.5. 动词+地名缩写扫描:检测"访华""赴美""访俄"等动宾短语,注入目的地实体。
	//      目的地本身不一定出现在标题里,但短语暗示了涉及该实体(如"普京启程访华"→中国)。
	for suffix, entity := range visitSuffixToEntity {
		if strings.Contains(title, suffix) {
			appendTrackerPool(&ordered, poolSeen, entity)
		}
	}

	// 0.55. 人物→关联实体推断:当标题中出现已知人物时,自动注入其关联实体(国家/企业)。
	//       解决"普京访华"/"俄罗斯远东地区"等场景中关联国家未显式出现的问题。
	//       institutionCityHints 已定义但不做注入,城市-机构关联通过 related_terms 体现。
	for person, entity := range personEntityHints {
		if strings.Contains(title, person) {
			appendTrackerPool(&ordered, poolSeen, entity)
		}
	}

	// 0.6. 复合话题关键词扫描:直接检测易被分词切散的复合词,整体注入 pool。
	//      例:"论文打假"/"校园欺凌" 被 gse 切成"论文"+"打假",导致短子串"打假"吸收长串。
	//      这里提前把完整复合词放入 pool,dedup Pass 2 中也会保护其不被短子串吸收。
	for kw := range compoundTopicKeywords {
		if strings.Contains(title, kw) {
			appendTrackerPool(&ordered, poolSeen, kw)
		}
	}

	// 1. 词典扫描:整段标题不区分大小写匹配 lexicon 别名,优先级最高。
	collectTrackerLexiconMatches(title, &ordered, poolSeen)

	// 2. 中文分词:用 gse 把标题切成词序列。
	//    先做一遍词频统计,标记出现 ≥2 次的词为 repeatedTokens。
	//    注意:用 normalizeLexiconAlias(轻量 trim)而非 normalizeTrackerToken(会 weak 过滤),
	//    否则 weak 词(如"总冠军"3字)在统计阶段就被消灭,永远无法触发重复检测。
	gseTokens := segmentTitle(title)
	{
		freq := map[string]int{}
		for _, tok := range gseTokens {
			cleaned := normalizeLexiconAlias(tok)
			if cleaned == "" || utf8.RuneCountInString(cleaned) < 2 {
				continue
			}
			cleaned = canonicalizeTrackerToken(cleaned)
			if cleaned == "" {
				continue
			}
			freq[cleaned]++
		}
		for tok, count := range freq {
			if count >= 2 {
				repeatedTokens[tok] = struct{}{}
			}
		}
	}
	for _, tok := range gseTokens {
		normalized := normalizeTrackerToken(tok)
		if normalized == "" {
			// 如果 normalizeTrackerToken 过滤了但该词是 repeated 或 heatDiscovered,
			// 尝试轻量 normalize 后放入 pool
			cleaned := normalizeLexiconAlias(tok)
			if cleaned == "" || utf8.RuneCountInString(cleaned) < 2 {
				continue
			}
			cleaned = canonicalizeTrackerToken(cleaned)
			if cleaned == "" {
				continue
			}
			_, isRepeated := repeatedTokens[cleaned]
			_, isHeatFound := heatDiscovered[cleaned]
			if isRepeated || isHeatFound {
				appendTrackerPool(&ordered, poolSeen, cleaned)
			}
			continue
		}
		appendTrackerPool(&ordered, poolSeen, normalized)
	}

	// 3. 兜底:按标点切分 + 正则扫描
	segments := trackerTitleSplitRegex.Split(title, -1)
	for _, segment := range segments {
		normalized := normalizeTrackerToken(segment)
		if normalized != "" {
			appendTrackerPool(&ordered, poolSeen, normalized)
		}
		for _, token := range trackerTokenRegex.FindAllString(segment, -1) {
			normalized = normalizeTrackerToken(token)
			if normalized == "" {
				continue
			}
			appendTrackerPool(&ordered, poolSeen, normalized)
		}
	}
	if len(ordered) == 0 {
		for _, token := range trackerTokenRegex.FindAllString(title, -1) {
			normalized := normalizeTrackerToken(token)
			if normalized == "" {
				continue
			}
			appendTrackerPool(&ordered, poolSeen, normalized)
		}
	}
	if len(ordered) == 0 {
		return nil
	}

	out := make([]trackerCandidate, 0, 6)
	for _, label := range ordered {
		_, forced := forcedEntities[label]
		_, repeated := repeatedTokens[label]
		_, isCompoundKw := compoundTopicKeywords[label]
		_, heatFound := heatDiscovered[label]
		// repeated: 标题中出现 ≥2 次的词,跳过大部分过滤但仍需排除 stopTokens 和噪声形态。
		// stopTokens 里的词(如"回应""发布")即使重复出现也不应该当关键词。
		// 纯数字("78")和弱碎片("半斤")重复出现同样无价值。
		if repeated && isGenericRoleToken(label) {
			repeated = false
		}
		if repeated && (isAllDigits(label) || isWeakChineseFragment(label) || looksLikeNumericMeasure(label)) {
			repeated = false
		}
		// heatDiscovered: 跨文章高频词。isWeakChineseFragment 不在 isExcludedHeatWord 里检查
		// (避免阻断"红包""油柑"这类有价值的 2-3 字热词被发现),但在消费时过滤。
		// looksLikeNumericMeasure 已在 isExcludedHeatWord 里检查,这里是双重保险。
		if heatFound && isGenericRoleToken(label) {
			heatFound = false
		}
		// heatFound 路径:跨文章高频就是信号,不用 isWeakChineseFragment 过滤
		// (短词如"红包""油柑"一旦多文章命中就有话题价值)。
		// 但仍过滤纯数字和量词短语(这类无论多频繁都无追踪意义)。
		if heatFound && (isAllDigits(label) || looksLikeNumericMeasure(label)) {
			heatFound = false
		}
		// compoundTopicKeywords 是人工精选的高价值复合词(如"贸易谈判""全球升温"),
		// 已在 0.6 步显式扫描注入,不再经过 shouldKeepTrackerToken 过滤。
		if !forced && !repeated && !isCompoundKw && !heatFound && !shouldKeepTrackerToken(label) {
			continue
		}
		kind := "keyword"
		if forced || looksLikeEntity(label) {
			kind = "entity"
		} else if heatFound || repeated {
			// heat-discovered / repeated 词跳过了 shouldKeepTrackerToken,
			// 但 looksLikeEntity 的启发式规则覆盖不到它们 —
			// 用 POS 分词器推断更准确的 entity/keyword 归类。
			// 注意:仅凭 POS 会把"公布/球队/专业"这类名词误升为 entity,
			// 这里额外要求满足 looksLikeEntity 形态规则,避免实体泛化污染聚类。
			if inferWordKind(label) == "entity" && looksLikeEntity(label) {
				kind = "entity"
			}
		}
		related := []string{}
		if kind == "entity" {
			related = collectTrackerRelatedTerms(ordered, label)
		}
		out = append(out, trackerCandidate{Label: label, Kind: kind, RelatedTerms: related})
		if len(out) >= 8 { // 多取一些,dedup 后保留 6 个
			break
		}
	}

	// 子串去重:如果短 candidate 是另一个更长 candidate 的子串,去掉短的。
	// 保留更完整、信息量更大的版本(如保留"美以袭击伊朗进入第 81 天",去掉"美以袭击伊朗进入第")。
	out = deduplicateCandidateSubstrings(out)

	// 过滤"地名作形容词修饰语"的情形:如"首个中国车手"中,"中国"只是修饰"车手",
	// 不是独立话题实体;应从输出中删去。仅当地名在标题中全程只以"地名+职业角色"
	// 形式出现时才过滤(visitSuffixToEntity 等注入的地名本身不在标题里,不受影响)。
	out = filterAdjunctiveGeoEntities(title, out)

	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

// deduplicateCandidateSubstrings 去除候选列表中的子串冗余。
// 如果 A.Label 是 B.Label 的子串且 A 不是 entity,则移除 A。
// entity 类型的短词永远保留(如"伊朗"不会被"美以袭击伊朗..."吸收)。
// deduplicateCandidateSubstrings 去除候选列表中的子串冗余。
//
// 规则(两轮处理):
//
//	Pass 1: 任何 keyword 包含已识别的 entity Label,丢弃这个 keyword。
//	  例:已有 entity "普京"+"俄罗斯",keyword "俄罗斯总统普京发表视频讲话" 应被吸收。
//
//	Pass 2: 两个 keyword 之间,留更短的(信息浓度高)。
//	  例:已有 keyword "绑架",候选 "绑架勒索全家" 应被吸收。
//
// entity 永不被吸收(地名/品牌/人名属于高价值短词,即使被某 keyword 包含也保留)。
//
// 历史:旧实现是反向(短被长吸收),想法是"保留信息丰富的长句"。但实际跑下来
// 发现长句几乎全是噪声(完整问句、冗余描述),反而高价值实体被嫌短而保留长句。
// 改成现行规则后,"普京/俄罗斯" 留下,长句被吸收,前 5 个 keyword 信息密度大幅提升。
func deduplicateCandidateSubstrings(candidates []trackerCandidate) []trackerCandidate {
	if len(candidates) <= 1 {
		return candidates
	}
	absorbed := make([]bool, len(candidates))

	// Pass 0: 长 entity(> 8 汉字)包含其他更短 entity → 丢长的。
	// 启发式 entity 判定(hasUpperASCII / entityPrefixes)会把"2526赛季NBA..."
	// 这类长串误判 entity,而里面真正的实体(NBA/马刺/俄罗斯)已被独立识别。
	// 词典 Label 最长 6 字,8 字阈值不会误伤。
	for i := range candidates {
		if candidates[i].Kind != "entity" {
			continue
		}
		if hanRuneCount(candidates[i].Label) <= 8 {
			continue
		}
		for j := range candidates {
			if i == j || candidates[j].Kind != "entity" || absorbed[j] {
				continue
			}
			if hanRuneCount(candidates[j].Label) > hanRuneCount(candidates[i].Label) {
				continue
			}
			if strings.Contains(candidates[i].Label, candidates[j].Label) {
				absorbed[i] = true
				break
			}
		}
	}
	// Pass 1: keyword 包含 entity → 丢 keyword
	for i := range candidates {
		if candidates[i].Kind == "entity" {
			continue
		}
		for j := range candidates {
			if i == j || candidates[j].Kind != "entity" {
				continue
			}
			if strings.Contains(candidates[i].Label, candidates[j].Label) {
				absorbed[i] = true
				break
			}
		}
	}

	// Pass 2: keyword vs keyword,留短的(更长的被吸收)
	for i := range candidates {
		if absorbed[i] || candidates[i].Kind == "entity" {
			continue
		}
		// 复合话题关键词不被短子串吸收(如"论文打假"不被"打假"替换)
		if _, isCompound := compoundTopicKeywords[candidates[i].Label]; isCompound {
			continue
		}
		for j := range candidates {
			if i == j || absorbed[j] || candidates[j].Kind == "entity" {
				continue
			}
			// i 比 j 长,且 j 是 i 的子串 → 移除 i(长的)
			if len(candidates[i].Label) > len(candidates[j].Label) &&
				strings.Contains(candidates[i].Label, candidates[j].Label) {
				absorbed[i] = true
				break
			}
		}
	}

	// Pass 2.5: 复合话题关键词的子词应被吸收。
	// 如"打假"/"欺凌"是"论文打假"/"校园欺凌"的组成部分,单独出现时信息量更低,
	// 应被吸收(保留更完整的复合形式)。
	for i := range candidates {
		if absorbed[i] || candidates[i].Kind == "entity" {
			continue
		}
		// i 本身是复合关键词则跳过(保留)
		if _, iIsCompound := compoundTopicKeywords[candidates[i].Label]; iIsCompound {
			continue
		}
		for j := range candidates {
			if i == j || absorbed[j] || candidates[j].Kind == "entity" {
				continue
			}
			// j 必须是复合关键词
			if _, jIsCompound := compoundTopicKeywords[candidates[j].Label]; !jIsCompound {
				continue
			}
			// i 是 j 的子串 → i 被更完整的复合形式 j 吸收
			if strings.Contains(candidates[j].Label, candidates[i].Label) {
				absorbed[i] = true
				break
			}
		}
	}

	out := make([]trackerCandidate, 0, len(candidates))
	for i, c := range candidates {
		if !absorbed[i] {
			out = append(out, c)
		}
	}
	return out
}

// filterAdjunctiveGeoEntities 过滤在标题中仅以"地名+职业/角色后缀"形式出现的地名实体。
//
// 例:"如何评价...任周灿成为首个获得纽北官方圈速认证的中国车手？"
// "中国"只修饰"车手",本身不是话题实体 → 应过滤。
//
// 仅当 geoName 在标题中全程只以"geoName+geoAdjectiveSuffix"组合形式出现时才过滤。
// 若 geoName 不在原始标题文本中(如由 visitSuffixToEntity 注入),则保留(返回 false)。
func filterAdjunctiveGeoEntities(title string, candidates []trackerCandidate) []trackerCandidate {
	out := make([]trackerCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.Kind == "entity" && onlyAppearsAsGeoAdjective(title, c.Label) {
			continue
		}
		if c.Kind == "entity" && shouldDropContextualEntity(title, c.Label) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// shouldDropContextualEntity 做少量上下文过滤,清理"看起来像 entity 但语义上是修饰语"
// 的噪声。
//
// 规则:
//  1. "AI" 仅作定语时丢弃(如"AI翻译/AI专业"):避免把技术属性词误当主语实体。
//  2. "纽博格林"在"圈速/纪录"语境下通常是赛道修饰词,不是事件主体。
func shouldDropContextualEntity(title, label string) bool {
	if label == "AI" && onlyAppearsAsAIModifier(title) {
		return true
	}
	return false
}

func onlyAppearsAsAIModifier(title string) bool {
	if !strings.Contains(title, "AI") {
		return false
	}
	allModifier := true
	for start := 0; start < len(title); {
		idx := strings.Index(title[start:], "AI")
		if idx < 0 {
			break
		}
		pos := start + idx
		after := title[pos+len("AI"):]
		isModifier := false
		for _, suf := range aiModifierSuffixes {
			if strings.HasPrefix(after, suf) {
				isModifier = true
				break
			}
		}
		if !isModifier {
			allModifier = false
			break
		}
		start = pos + len("AI")
	}
	return allModifier
}

// onlyAppearsAsGeoAdjective 判定 geoName 是否仅在标题中以"geoName+角色后缀"形式出现。
//
// 返回 true(应过滤)的条件:
//  1. geoName 在标题中至少出现一次(若完全不出现说明是注入来的,应保留)
//  2. geoName 在标题中的所有出现位置,右侧都紧跟 geoAdjectiveSuffixes 中某个后缀
//
// 返回 false(保留)的条件:
//   - geoName 不在原始标题文字中
//   - geoName 有任一出现位置后面不跟角色后缀(说明它有独立信息价值)
func onlyAppearsAsGeoAdjective(title, geoName string) bool {
	if geoName == "" || !strings.Contains(title, geoName) {
		return false // 不在标题里 → 是注入来的 → 保留
	}
	geoBytes := []byte(geoName)
	titleBytes := []byte(title)
	foundAny := false
	allAdjective := true
	for i := 0; i <= len(titleBytes)-len(geoBytes); {
		idx := strings.Index(string(titleBytes[i:]), geoName)
		if idx < 0 {
			break
		}
		foundAny = true
		pos := i + idx
		after := string(titleBytes[pos+len(geoBytes):])
		isAdj := false
		for _, suf := range geoAdjectiveSuffixes {
			if strings.HasPrefix(after, suf) {
				isAdj = true
				break
			}
		}
		if !isAdj {
			allAdjective = false
			break
		}
		i = pos + len(geoBytes)
	}
	return foundAny && allAdjective
}

func appendTrackerPool(pool *[]string, seen map[string]struct{}, label string) {
	label = canonicalizeTrackerToken(label)
	if label == "" {
		return
	}
	if _, ok := seen[label]; ok {
		return
	}
	seen[label] = struct{}{}
	*pool = append(*pool, label)
}

func canonicalizeTrackerToken(token string) string {
	if token == "" {
		return ""
	}
	if label, ok := trackerEntityAliasToLabel[token]; ok {
		return label
	}
	// gse 分词把英文/混合 token 全转小写(openai / gpt-5),走 lower-case 索引兜底
	if label, ok := trackerEntityAliasToLabelLower[strings.ToLower(token)]; ok {
		return label
	}
	return token
}

// normalizeLexiconAlias 给 lexicon 别名索引专用的轻量 normalizer。
// 只 trim 前后空白和包裹符号,不做内容过滤。这样纯数字别名(315/618)、
// 短词(B站)等都能进索引。区别于 normalizeTrackerToken 用于"用户输入或抽出 token
// 的过滤"——那里要过滤纯数字防止"500 万热度"里的"500"被认成实体。
func normalizeLexiconAlias(alias string) string {
	alias = strings.TrimSpace(alias)
	alias = strings.Trim(alias, "#.,!?:;，。！？：；（）()[]【】《》\"'“”‘’·-")
	return strings.TrimSpace(alias)
}

func buildTrackerEntityAliasIndex(entries []trackerLexiconEntry) []trackerLexiconAlias {
	out := make([]trackerLexiconAlias, 0, len(entries)*3)
	seen := map[string]struct{}{}
	for _, entry := range entries {
		// 用轻量 normalizer:lexicon 数据是人工维护的规范形式,
		// 不能走 normalizeTrackerToken 的"纯数字过滤/最小长度"等输入侧规则,
		// 否则像 315/618/B站 这种合法 alias 会被吃掉
		label := normalizeLexiconAlias(entry.Label)
		if label == "" {
			continue
		}
		aliases := append([]string{label}, entry.Aliases...)
		for _, alias := range aliases {
			needle := normalizeLexiconAlias(alias)
			if needle == "" {
				continue
			}
			if strings.ContainsAny(needle, " -") {
				for _, part := range splitTrackerAliasParts(needle) {
					partKey := label + "\x00" + strings.ToLower(part)
					if _, ok := seen[partKey]; ok {
						continue
					}
					seen[partKey] = struct{}{}
					out = append(out, trackerLexiconAlias{
						Label:            label,
						Needle:           part,
						Lower:            strings.ToLower(part),
						RequiresBoundary: trackerAliasRequiresBoundary(part),
					})
				}
			}
			if len(needle) >= 6 && strings.ContainsRune(needle, ' ') {
				compact := strings.ReplaceAll(needle, " ", "")
				compactKey := label + "\x00" + strings.ToLower(compact)
				if compact != "" {
					if _, ok := seen[compactKey]; !ok {
						seen[compactKey] = struct{}{}
						out = append(out, trackerLexiconAlias{
							Label:            label,
							Needle:           compact,
							Lower:            strings.ToLower(compact),
							RequiresBoundary: trackerAliasRequiresBoundary(compact),
						})
					}
				}
			}
			key := label + "\x00" + strings.ToLower(needle)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, trackerLexiconAlias{
				Label:            label,
				Needle:           needle,
				Lower:            strings.ToLower(needle),
				RequiresBoundary: trackerAliasRequiresBoundary(needle),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if utf8.RuneCountInString(out[i].Needle) != utf8.RuneCountInString(out[j].Needle) {
			return utf8.RuneCountInString(out[i].Needle) > utf8.RuneCountInString(out[j].Needle)
		}
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return out[i].Needle < out[j].Needle
	})
	return out
}

func collectTrackerLexiconMatches(title string, pool *[]string, seen map[string]struct{}) {
	lowerTitle := strings.ToLower(title)
	if acMatcher == nil {
		return
	}
	// Aho-Corasick 一次扫描找到所有命中的 alias 索引
	hits := acMatcher.MatchThreadSafe([]byte(lowerTitle))
	for _, idx := range hits {
		if idx < 0 || idx >= len(acPatternLabels) {
			continue
		}
		alias := acPatternPatterns[idx]
		if alias == "" {
			continue
		}
		// 短 alias 需要 boundary check(防止 "o1" 匹配 "go123")
		if acPatternNeedBoundary[idx] {
			if !containsTrackerAliasWithBoundary(lowerTitle, alias) {
				continue
			}
		}
		appendTrackerPool(pool, seen, acPatternLabels[idx])
	}
}

func splitTrackerAliasParts(alias string) []string {
	parts := strings.FieldsFunc(alias, func(r rune) bool {
		return r == ' ' || r == '-'
	})
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = normalizeTrackerToken(part)
		if part == "" || utf8.RuneCountInString(part) < 2 {
			continue
		}
		if trackerAliasRequiresBoundary(part) && utf8.RuneCountInString(part) < 5 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func trackerAliasRequiresBoundary(alias string) bool {
	for _, r := range alias {
		if r > unicode.MaxASCII {
			return false
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func containsTrackerAliasWithBoundary(text, alias string) bool {
	if alias == "" {
		return false
	}
	for start := 0; start < len(text); {
		idx := strings.Index(text[start:], alias)
		if idx < 0 {
			return false
		}
		idx += start
		end := idx + len(alias)
		if hasTrackerAliasBoundary(text, idx, end) {
			return true
		}
		start = idx + len(alias)
	}
	return false
}

func hasTrackerAliasBoundary(text string, start, end int) bool {
	if start > 0 {
		prev, _ := utf8.DecodeLastRuneInString(text[:start])
		if isTrackerASCIIWordRune(prev) {
			return false
		}
	}
	if end < len(text) {
		next, _ := utf8.DecodeRuneInString(text[end:])
		if isTrackerASCIIWordRune(next) {
			return false
		}
	}
	return true
}

func isTrackerASCIIWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func normalizeTrackerToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "#.,!?:;，。！？：；（）()[]【】《》\"'“”‘’·-")
	token = strings.TrimPrefix(token, "#")
	token = strings.TrimSuffix(token, "#")
	token = compactTrackerSpaces(token)
	if token == "" {
		return ""
	}

	for changed := true; changed; {
		changed = false
		for _, prefix := range trackerTrimPrefixes {
			if strings.HasPrefix(token, prefix) && utf8.RuneCountInString(token) > utf8.RuneCountInString(prefix)+1 {
				token = strings.TrimSpace(strings.TrimPrefix(token, prefix))
				changed = true
			}
		}
		for _, suffix := range trackerTrimSuffixes {
			if strings.HasSuffix(token, suffix) && utf8.RuneCountInString(token) > utf8.RuneCountInString(suffix)+1 {
				token = strings.TrimSpace(strings.TrimSuffix(token, suffix))
				changed = true
			}
		}
		token = strings.Trim(token, "#.,!?:;，。！？：；（）()[]【】《》\"'“”‘’·-")
	}

	if token == "" {
		return ""
	}
	if _, blocked := stopTokens[token]; blocked {
		return ""
	}
	runeCount := utf8.RuneCountInString(token)
	if runeCount < 2 || runeCount > 20 {
		return ""
	}
	if strings.HasSuffix(token, "热度") || strings.HasSuffix(token, "播放") {
		return ""
	}
	if strings.Contains(strings.ToLower(token), "http") {
		return ""
	}
	if isAllDigits(token) {
		return ""
	}
	if looksLikeNumericMeasure(token) {
		return ""
	}
	if !containsHanOrLetter(token) {
		return ""
	}
	if isWeakChineseFragment(token) {
		return ""
	}
	return token
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func looksLikeNumericMeasure(token string) bool {
	if token == "" {
		return false
	}
	runes := []rune(token)
	if len(runes) < 2 {
		return false
	}

	// 路径 A:阿拉伯数字主导的量词短语 —— 原有逻辑
	// 要求:至少一半字符是 ASCII 数字,且以计量单位结尾。
	digits := 0
	for _, r := range runes {
		if unicode.IsDigit(r) {
			digits++
		}
	}
	if digits > 0 && digits*2 >= len(runes) {
		for _, suffix := range []string{"人", "名", "例", "岁", "年", "天", "家", "次", "%", "万", "亿", "元", "斤", "公里", "小时"} {
			if strings.HasSuffix(token, suffix) {
				return true
			}
		}
	}

	// 路径 B:中文大数量词 + 金融/计量单位 —— 覆盖"万亿美元""亿元""千亿欧元"等
	// 规则:只要 token 含至少一个大数量级字符(万/亿/千/百/兆),
	// 且以货币或常见计量单位结尾,就认为是数量短语而非可追踪实体/关键词。
	// 这类 token 即使不含阿拉伯数字也属于数量描述(路径 A 无法捕获)。
	const chineseNumMultipliers = "万亿千百兆"
	for _, r := range runes {
		if strings.ContainsRune(chineseNumMultipliers, r) {
			// token 含大数量级字符 → 再检查是否以量词单位结尾
			for _, suffix := range []string{
				"美元", "欧元", "英镑", "日元", "港元", "韩元", "卢布",
				"人民币", "元", "斤", "吨", "克", "升",
			} {
				if strings.HasSuffix(token, suffix) {
					return true
				}
			}
			break
		}
	}

	return false
}

func containsHanOrLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func compactTrackerSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func hasUpperASCII(token string) bool {
	for _, r := range token {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

// isAllUpperASCII 判定 token 是否全部由大写英文字母组成。
// 用于过滤"GT/AI/SUV"这类无独立辨识度的纯大写缩写词——真正的大写缩写实体
// (NBA/FIFA/GPU)已在词典中精确命中,不会走到这条路径。
func isAllUpperASCII(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// mixedTokenVerbs 用于检测中英混合 token 中的动词片段。
// 如果一个含大写英文的 token 同时包含这些动词,说明它是"动词+英文名"的句子片段
// (如"发布GPT""超越Claude""指标超GPT"),而不是真正的 entity。
var mixedTokenVerbs = []string{
	"发布", "超越", "指标", "性能", "接入", "搭载", "采用", "支持",
	"对比", "碾压", "吊打", "战胜", "击败", "淘汰", "加盟",
	"普及", "推出", "升级", "更新", "兼容",
}

func containsMixedTokenVerb(token string) bool {
	for _, v := range mixedTokenVerbs {
		if strings.Contains(token, v) {
			return true
		}
	}
	return false
}

func hanRuneCount(token string) int {
	count := 0
	for _, r := range token {
		if unicode.Is(unicode.Han, r) {
			count++
		}
	}
	return count
}

func isWeakChineseFragment(token string) bool {
	if hasUpperASCII(token) || strings.Contains(token, "·") {
		return false
	}
	if looksLikeEntity(token) || looksLikeTopicPhrase(token) {
		return false
	}
	// 强地名和强动词:2字但信息量极高,不视为弱碎片
	if _, ok := strongGeoNames[token]; ok {
		return false
	}
	if _, ok := strongVerbs[token]; ok {
		return false
	}
	if _, ok := strongTopicNouns[token]; ok {
		return false
	}
	return hanRuneCount(token) <= 3
}

func looksLikeEntity(token string) bool {
	if _, ok := trackerEntityLabelSet[token]; ok {
		return true
	}
	if _, ok := strongGeoNames[token]; ok {
		return true
	}
	if isGenericRoleToken(token) {
		return false
	}
	// 通用英文词(Special/Video/Live)即使有大写字母也不是 entity。
	// 防 hasUpperASCII 分支把它们误判 entity。
	if isGenericEnglishWord(token) || isAllGenericEnglishWords(token) {
		return false
	}
	// 集数/赛事编号代号("G1"/"S22"/"E3"/"Ep5"),纯数字标记无独立辨识度。
	// 只对"1-2 个英文字母 + 1-3 位数字"的孤立 token 生效,词典里的 NBA/F1/G7
	// 等已经在函数开头被精确命中,不会到这一步。
	if isEpisodeCode(token) {
		return false
	}
	if strings.HasPrefix(token, "#") {
		return true
	}
	// 关键防线:如果整个 token 是句子片段或赛事数据描述,即使开头是
	// "俄罗斯""乐高"等 entity 词,也不应整体当 entity 输出。这种长串的真正实体
	// 已经通过 lexicon AC 扫描或 gse 切词独立提取出来了,长串保留只会污染候选列表。
	//
	// 注意:这个 check 只对 prefix/suffix/hasUpperASCII 这些"启发式判定"生效;
	// 词典精确命中的 entity(trackerEntityLabelSet)早在函数开头就 return true 了,
	// 不会到这一步,所以词典里的真实长 Label(如"无印良品")不受影响。
	if looksLikeSentence(token) {
		return false
	}
	if looksLikeDataLikePhrase(token) {
		return false
	}
	if hasUpperASCII(token) {
		// 短纯大写英文(GT/AI/SUV 等)不是实体。
		// 真正的缩写实体(NBA/FIFA/GPU)已在函数开头被词典精确命中,不会到这里。
		// ≤ 3 个字符且全大写 → 通用配置/型号代号,跳过。
		if utf8.RuneCountInString(token) <= 3 && isAllUpperASCII(token) {
			return false
		}
		// hasUpperASCII 之前可能放过"Donk踩火三杀拯救世界"这种"短英文+长中文描述"
		// 的伪 entity(因为有 ASCII 字母触发命中)。真实 entity 通常是:
		// · 纯英文/字母数字(NBA / iPhone / GPT-5)
		// · 短中英混合(R.E.D组合 / 追觅T60 / AC米兰)
		// 长度 > 4 个汉字的中英混合大概率是"短英文起手 + 长中文动词宾语"的描述
		// 短串,丢掉它(里面的真实 entity 已被独立切出)。
		if hanRuneCount(token) > 4 {
			return false
		}
		// 只有 1 个汉字+英文的混合(如"底BOSS""关AI")几乎不可能是品牌名,
		// 都是切分残骸。真实 1 汉字 entity(如"O2")是纯英文不走 hanRuneCount 路径。
		if hanRuneCount(token) == 1 {
			return false
		}
		// 短中英混合(hanRuneCount ≤ 4)中如果包含常见动词,说明是"动词+英文名"
		// 的描述片段(如"发布GPT""超越Claude"),不是真正的 entity。
		// 真实 entity 中不会出现这些动词(如"华为Mate"/"小米SU7"/"AI翻译")。
		if hanRuneCount(token) >= 2 && containsMixedTokenVerb(token) {
			return false
		}
		return true
	}
	if strings.Contains(token, "·") {
		return true
	}
	for _, suffix := range entitySuffixes {
		if strings.HasSuffix(token, suffix) && utf8.RuneCountInString(token) > utf8.RuneCountInString(suffix) {
			return true
		}
	}
	for _, prefix := range entityPrefixes {
		remaining := utf8.RuneCountInString(token) - utf8.RuneCountInString(prefix)
		if strings.HasPrefix(token, prefix) && remaining > 0 && remaining <= 3 {
			return true
		}
	}
	return false
}

func looksLikeTopicPhrase(token string) bool {
	if token == "" || looksLikeEntity(token) {
		return false
	}
	if _, ok := strongVerbs[token]; ok {
		return true
	}
	for _, suffix := range topicSuffixes {
		if strings.HasSuffix(token, suffix) {
			return true
		}
	}
	return false
}

// dataLikePatterns 识别"伪体育/数据描述"片段,语义上等同于通用统计文案,
// 不是事件也不是实体,应过滤掉。例:
//   - "2526赛季英超联赛第37轮"
//   - "2526赛季NBA季后赛马刺 VS 雷霆"
//   - "巴西公布世界杯 26 人名单"(已被 looksLikeSentence 拦,这里兜底)
//
// 这些 pattern 跟 looksLikeSentence 的差异:
//   - looksLikeSentence 看动词/疑问句尾,语义偏"叙述句"
//   - dataLikePatterns 看赛季/轮次/比分等数字描述,语义偏"列表/统计"
//
// 不写得太严:只针对"两个数字片段+连接词"这种明确无信息密度的形态。
var dataLikePatterns = []*regexp.Regexp{
	// "2526赛季英超联赛第37轮" / "2526赛季NBA季后赛..."
	regexp.MustCompile(`^\d{2,4}\s*赛季.+第\s*\d+\s*轮`),
	regexp.MustCompile(`^\d{2,4}\s*赛季`),
	// "赛季英超联赛第37轮"(头部数字被切掉后剩下的残骸,亦即包含 "赛季...第N轮")
	regexp.MustCompile(`赛季.{2,8}第\s*\d+\s*轮`),
	regexp.MustCompile(`第\s*\d+\s*轮`),
	// "赛季NBA季后赛马刺"(头部年份被切掉的残骸,通用 "赛季...季后赛/联赛/总决赛")
	regexp.MustCompile(`^赛季.+(季后赛|联赛|总决赛|常规赛)`),
	// "X 1-0 战胜 Y" 比分赛事描述(单独的 X/Y 已经被 entity 词典/地名识别)
	regexp.MustCompile(`\d+\s*[-:：]\s*\d+\s*战胜`),
	// "战胜 + 队名"(头部比分被切掉的残骸,如"战胜伯恩利")
	regexp.MustCompile(`^战胜.{2,6}$`),
	// "马刺 122-115 雷霆"整段比分串(中间是数字-数字,前后是 2-8 字内容)
	regexp.MustCompile(`.{2,8}\s+\d+\s*[-:：]\s*\d+\s+.{2,8}`),
	// 运动员数据描述:"文班亚马 41 分 24 篮板"
	regexp.MustCompile(`.{2,8}\s*\d+\s*分\s*\d+\s*(篮板|助攻|抢断|盖帽|出场|首发)`),
	// 系列赛代号:"西决G1" / "东决G3" / "总决赛G7"
	regexp.MustCompile(`^.{1,4}决\s*G\d+$`),
	// 票房/冠军数字描述:"累计票房破5亿" / "票房破10亿"
	regexp.MustCompile(`票房破\s*\d+\s*亿`),
	regexp.MustCompile(`累计.{0,4}破\s*\d+`),
	// "第N冠" / "年度总冠军"
	regexp.MustCompile(`^第\s*\d+\s*冠$`),
	regexp.MustCompile(`机车夺.*第\s*\d+\s*冠`),
	// 含"年X月X号"完整日期 + 后缀
	regexp.MustCompile(`\d{4}年\s*\d+月\s*\d+号`),
	// "LPL2026" "S22" 这种"赛事名+年份"组合(2-5 字英文/字母 + 4 位年份)
	regexp.MustCompile(`^[A-Z]{2,5}\d{4}$`),
	// "X 公布 Y 名单 / 阵容 / 排名"  通用赛事公示句式
	regexp.MustCompile(`公布.{2,10}(名单|阵容|排名|榜单)`),
	// "含 N 位 X" 数量描述(代表团/嘉宾名单常见,如"含 5 位副总理 8 位部长")
	regexp.MustCompile(`含\s*\d+\s*位.{2,10}`),
	regexp.MustCompile(`\d+\s*位(副?总理|部长|院士|教授|代表|嘉宾|官员|博士)`),
	regexp.MustCompile(`^位(副?总理|部长|院士|教授|代表|嘉宾|官员|博士)$`),
	// 年龄/数据 + 人名描述:"41岁C罗领衔" "33岁梅西首发" 等
	// (人名本身已被 lexicon 独立识别,这种描述短串保留只是噪声)
	regexp.MustCompile(`^\d{1,3}岁.{1,8}领衔$`),
	regexp.MustCompile(`^\d{1,3}岁.{1,6}(首发|入选|进球|出场)`),
	// "岁X领衔" 头部数字被切掉的残骸
	regexp.MustCompile(`^岁.{1,8}领衔$`),
}

func looksLikeDataLikePhrase(token string) bool {
	for _, re := range dataLikePatterns {
		if re.MatchString(token) {
			return true
		}
	}
	return false
}

// shouldUseKeywordAsClusterConnector 判定 keyword 是否可作为事件聚类连接器。
// 目标:保留有事件语义的关键词,过滤过泛/句式型关键词,避免误连。
func shouldUseKeywordAsClusterConnector(keyword string) bool {
	if keyword == "" {
		return false
	}
	if isGenericRoleToken(keyword) {
		return false
	}
	if looksLikeSentence(keyword) || looksLikeDataLikePhrase(keyword) {
		return false
	}
	runeLen := len([]rune(keyword))
	if runeLen < 2 || runeLen > 12 {
		return false
	}
	// 主路径:复用既有过滤体系,保证连接词质量。
	if shouldKeepTrackerToken(keyword) {
		return true
	}
	// 保底:面向"一类短事件词"做通用兜底,避免 shouldKeep 对短词过严导致断链。
	if _, ok := compoundTopicKeywords[keyword]; ok {
		return true
	}
	if _, ok := strongVerbs[keyword]; ok {
		return true
	}
	if _, ok := strongTopicNouns[keyword]; ok {
		return true
	}
	// 动词+目的地缩写类(访华/赴美/访俄...):按映射表统一判定,不写散落 case。
	if _, ok := visitSuffixToEntity[keyword]; ok {
		return true
	}
	// 统一语义规则兜底:仅保留具备明确事件后缀的话题词。
	// 不再使用纯长度兜底,避免把任意短词放进连接器造成误连。
	if looksLikeTopicPhrase(keyword) {
		return true
	}
	return false
}

// sentenceVerbInfixes 在 token 中间出现这些动词,几乎一定是 S+V+O 完整句子。
// 跟 strongVerbs 的区别:strongVerbs 是"独立成词的事件动词"(裁员/患癌),
// 出现在中间且前后都有内容,意味着这是一个事件描述句子而非短语。
var sentenceVerbInfixes = []string{
	"发生", "发表", "发布", "宣布", "回应", "辟谣", "披露",
	"公布", "证实", "否认", "表态", "回复", "怀疑", "举报",
	"晒", // 网络新闻常见单字动词:洁丽雅"晒"报案回执 / 网友"晒"图
	// 第三波补:更多句子结构动词
	"称", "遭", "爆料", "通报", "抓获", "确认", "收获",
	"哭诉", "选择", "推出", "再爆", "导致", "斩获", "夺",
	// 体育/赛事描述动词(治"时隔22年再次打进决赛"这种残骸)
	// 注:晋级已移至 strongTopicNouns,独立短语(晋级决赛 ≤4字)由 looksLikeSentence 豁免。
	"打进", "闯进", "杀入", "时隔",
}

// sentenceTailQuestions 疑问句尾(trackerTrimSuffixes 已删大部分,这里兜底剩余)。
var sentenceTailQuestions = []string{
	"如何", "怎样", "怎么", "为何", "吗", "呢",
	// 结构助词"的"结尾:几乎一定是不完整的修饰语片段(如"学生时代的""真正意义上的")
	"的",
}

// sentenceColloquialPrefixes 口语化主语前缀。开头是这些词且整体 ≥ 4 个汉字
// 时,几乎一定是 B 站/小红书娱乐标题的口语描述句,无信息价值。
//
// 例:"还好我有一个哥哥" / "我们发现了大秘密" / "为了吃龙虾" / "试了三辆007"
//
// 注意:"试了"和"为了"虽不是主语,但也是开头叙述句的明显起手词,一并放入。
// 严肃新闻偶尔以"我"开头(如"我国")已被 strongGeoNames("中国")等机制
// 独立识别,不依赖这种长串。
var sentenceColloquialPrefixes = []string{
	"我", "我们", "你", "咱", "咱们", "他", "她",
	"为了", "还好", "试了", "今天", "昨天",
	"学校", "司机", "妈妈", "爸爸", "老婆", "老公",
	"小朋友", "外地朋友", "湖南人", "广东人", "北京人",
	"但很", "依旧", "突然", "原来",
	"给外地", "给本地", "给家里", "给朋友",
	// 指示代词起手:几乎一定是评论性描述片段,无独立话题价值
	// 例:"这种活动形式" / "这些做法" / "那个时候" / "那种感觉" / "这支球队"
	"这种", "这些", "这个", "这次", "这位",
	"这支", "这场", "这条", "这辆", "这部", "这首", "这件", "这段", "这款",
	"那种", "那些", "那个", "那次", "那位",
	"那支", "那场", "那条", "那辆", "那部", "那首", "那件", "那段", "那款",
	// 疑问词起手(trim 没切完的残片):
	// 例:"为何学生时代的" / "为啥现在的年轻人"
	"为何", "为啥", "怎么", "如何", "难道",
}

// sentenceCopulaInfixes 标题党"是 X"句式中间词。这些片段出现在 token 内
// 通常意味着评论性/感叹性陈述,不是事件也不是实体。
//
// 例:"真诚是第一要素" / "我才是主角" / "其实是诈骗" / "自开园起便是如此"
var sentenceCopulaInfixes = []string{
	"是第一", "是真的", "是最", "是最大", "竟然是", "其实是", "原来是", "才是真",
	// "便是/就是/即是...如此" 评价性结构
	"便是如此", "就是如此", "即是如此", "并非如此", "本就如此",
	"便是这样", "就是这样", "本来如此",
}

// sentenceFromToRegex 标题党"从 X 到 Y"句式("从黑马到大爆""从青铜到王者")。
// 整体作为事件描述句子,无独立信息价值。
var sentenceFromToRegex = regexp.MustCompile(`^从.{1,5}到.{1,5}`)

// sentenceVerbPrefixes 动词前缀:以这些动词开头且后面有宾语内容的短片段,
// 是"V+O"动作描述而非话题名词短语。
// 例:"刷新纽北" / "战胜日本" / "击败对手" / "淘汰勇士" / "打破纪录"
//
// 和 sentenceVerbInfixes 的区别:
//   - Infixes 检测动词在中间(S+V+O 完整句子)
//   - Prefixes 检测动词在开头(V+O 残片,trim 掉主语后的产物)
//
// 不收 strongVerbs 里的词(如"裁员""暴跌"): 那些本身就是事件关键词,
// 独立成词时("裁员"2字)有聚合价值;这里只收"作为前缀+宾语"才有意义的动词。
var sentenceVerbPrefixes = []string{
	"刷新", "打破", "突破", "创下", "超越", "领跑",
	"战胜", "击败", "淘汰", "碾压", "横扫", "力克",
	"获得", "赢得", "拿下", "斩获", "摘得", "夺得",
	"引发", "带动", "推动", "促进", "加速",
	"入选", "落选", "当选", "连任",
	"采访", "回应", "怒斥", "炮轰", "质疑",
}

// looksLikeSentence 判定 token 是否看起来是完整句子片段而非实体或短语。
// 任一信号命中即认为是句子,应当从 keyword 候选中剔除:
//   - 长度过长(> 10 个汉字,词典命中的 entity 不会到这一步)
//   - rune 总数过长(> 14,覆盖中英文混合赛事描述如 "2526赛季NBA季后赛马刺 VS 雷霆")
//   - 含完整谓语动词模式("X 发表 Y" / "X 公布 Y")
//   - 疑问句尾("...如何""...怎样")
//   - 口语化主语开头("我/我们/你/为了/还好/试了..."且 hanCount ≥ 4)
func looksLikeSentence(token string) bool {
	hanCount := hanRuneCount(token)
	if hanCount > 10 {
		return true
	}
	// rune 总长度兜底:中英文混合 + 长度 > 14 个 rune,基本是赛事/事件描述句
	if utf8.RuneCountInString(token) > 14 {
		return true
	}
	for _, v := range sentenceVerbInfixes {
		idx := strings.Index(token, v)
		if idx > 0 && idx+len(v) < len(token) {
			// 中间出现谓语动词,前后都有内容 → S+V+O
			// 豁免:≤4 个汉字的短语(如"晋级决赛")不是句子,是高价值赛事短语
			if hanCount <= 4 {
				continue
			}
			return true
		}
	}
	for _, q := range sentenceTailQuestions {
		if strings.HasSuffix(token, q) {
			return true
		}
	}
	if hanCount >= 4 {
		for _, p := range sentenceColloquialPrefixes {
			if strings.HasPrefix(token, p) {
				return true
			}
		}
	}
	// 标题党"是 X"陈述句:中间含 sentenceCopulaInfixes 任一片段即丢
	for _, c := range sentenceCopulaInfixes {
		if strings.Contains(token, c) {
			return true
		}
	}
	// "从 X 到 Y" 句式
	if sentenceFromToRegex.MatchString(token) {
		return true
	}
	// "V+O" 动词前缀:动词开头 + 后面有宾语(整体 ≥ 4 汉字)→ 动作描述残片
	if hanCount >= 4 {
		for _, v := range sentenceVerbPrefixes {
			if strings.HasPrefix(token, v) {
				return true
			}
		}
	}
	return false
}

// channelTagSuffixes B 站/视频平台典型的栏目/分类标签后缀。
// 这些 token 通常出现在 【XX调查】【XX测评】【XX锐评】这种栏目分类括号里,
// 是 UP 主的内容分类,不是事件或实体本身。例:旧核调查 / 无聊的开箱 / 短的发布会 /
// 真人实验 / 基因对比 / 毒舌的南瓜 / 泰拉TV合集版。
var channelTagSuffixes = []string{
	"调查", "测评", "锐评", "开箱", "评测", "实验",
	"合集版", "合集", "栏目", "专栏", "深扒",
	"探店",
}

// looksLikeChannelTag B 站栏目分类标签判定:
// 含 channelTagSuffixes 任一后缀 + 长度 ≥ 3 个汉字 → 是栏目标签,不当 entity/keyword
func looksLikeChannelTag(token string) bool {
	if hanRuneCount(token) < 3 {
		return false
	}
	for _, s := range channelTagSuffixes {
		if strings.HasSuffix(token, s) {
			return true
		}
	}
	return false
}

func shouldKeepTrackerToken(token string) bool {
	// 词典中精确命中的 entity 总是保留,优先级高于 stopTokens 等过滤。
	// 这保证了像"B站"这种同时在 lexicon 和可能被当作通用词的 token 不被误过滤。
	if _, ok := trackerEntityLabelSet[token]; ok {
		return true
	}
	if isGenericRoleToken(token) {
		return false
	}
	if isGenericEnglishWord(token) {
		return false
	}
	if isAllGenericEnglishWords(token) {
		return false
	}
	if isEpisodeCode(token) {
		return false
	}
	if looksLikeChannelTag(token) {
		return false
	}
	if looksLikeEntity(token) {
		return true
	}
	// strongTopicNouns: 高信息量话题名词(房价/楼市/社保等),和 entity 同优先级保留
	if _, ok := strongTopicNouns[token]; ok {
		return true
	}
	if looksLikeDataLikePhrase(token) {
		return false
	}
	if looksLikeSentence(token) {
		return false
	}
	return looksLikeTopicPhrase(token)
}

func isGenericRoleToken(token string) bool {
	_, ok := stopTokens[token]
	return ok
}

// genericEnglishWords 通用英文标签/形容词,在视频标题中出现频率高但无辨识度。
// 这类词单独出现时既不是品牌也不是事件,作为 keyword/entity 都是噪声。
// gse 切词遇到 "Special Video" / "Live Show" 经常输出整段或独立词,需要在
// shouldKeepTrackerToken 入口直接拦截。
var genericEnglishWords = map[string]struct{}{
	"video": {}, "videos": {},
	"live": {}, "stream": {}, "streaming": {},
	"trailer": {}, "teaser": {},
	"demo": {}, "beta": {}, "alpha": {},
	"mv": {}, "ep": {}, "pv": {},
	"special": {}, "official": {},
	"feat": {}, "vs": {}, "ft": {},
	"preview": {}, "review": {}, "reaction": {},
	"intro": {}, "outro": {}, "short": {}, "shorts": {},
	"vlog": {}, "podcast": {},
	// 通用技术/产品词:单独出现时是泛指,不构成 entity
	"app": {}, "apps": {},
}

func isGenericEnglishWord(token string) bool {
	_, ok := genericEnglishWords[strings.ToLower(token)]
	return ok
}

// isAllGenericEnglishWords 处理空格分隔的英文复合词,如"Special Video""Live Stream"。
// trackerTitleSplitRegex 不切空格,这种 token 整体进 pool,需要按空格拆开判断。
// 全部都是 generic 时整段丢;有任一非 generic 部分(如人名)则保留(可能是品牌名)。
func isAllGenericEnglishWords(token string) bool {
	parts := strings.Fields(token)
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if !isGenericEnglishWord(p) {
			return false
		}
	}
	return true
}

// episodeCodeRegex 匹配孤立的赛事/集数编号(G1/S22/E3/Ep5/EP10)。
// 1-2 个英文字母 + 1-3 位数字,字母前后无其他内容。词典里的真实 entity
// (NBA/F1/G7 国家集团 等)在 looksLikeEntity 顶部已精确命中,不会到这一步。
var episodeCodeRegex = regexp.MustCompile(`^[A-Za-z]{1,2}\d{1,3}$`)

func isEpisodeCode(token string) bool {
	return episodeCodeRegex.MatchString(token)
}

func collectTrackerRelatedTerms(tokens []string, label string) []string {
	terms := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for _, token := range tokens {
		token = canonicalizeTrackerToken(token)
		if token == "" || token == label {
			continue
		}
		if !shouldKeepTrackerToken(token) {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		terms = append(terms, token)
		if len(terms) == 4 {
			break
		}
	}
	if len(terms) > 0 {
		return terms
	}
	for _, alias := range trackerEntityTermsByLabel[label] {
		normalized := normalizeTrackerToken(alias)
		if normalized == "" || strings.EqualFold(normalized, label) {
			continue
		}
		if !shouldKeepTrackerToken(normalized) && !trackerAliasRequiresBoundary(strings.ToLower(normalized)) {
			continue
		}
		if canonical := canonicalizeTrackerToken(normalized); canonical != label && canonical != "" {
			continue
		}
		terms = append(terms, normalized)
		if len(terms) == 2 {
			break
		}
	}
	return terms
}

func trackerTermMatchesArticle(term string, article model.Article) bool {
	term = normalizeTrackerToken(term)
	if term == "" {
		return false
	}
	canonical := canonicalizeTrackerToken(term)
	if canonical != "" && canonical != term {
		for _, alias := range trackerEntityTermsByLabel[canonical] {
			if trackerTermMatchesArticle(alias, article) {
				return true
			}
		}
		return false
	}
	// 匹配 title 或 content:title 是强信号,content 是弱信号(提升召回率)。
	// 具体权重区分在 scoreTrackerTermMatch 中体现。
	title := strings.ToLower(article.Title)
	content := strings.ToLower(article.Content)
	needle := strings.ToLower(term)
	if trackerAliasRequiresBoundary(needle) {
		return containsTrackerAliasWithBoundary(title, needle) || containsTrackerAliasWithBoundary(content, needle)
	}
	return strings.Contains(title, needle) || strings.Contains(content, needle)
}

func scoreTrackerTermMatch(term string, article model.Article) int {
	term = normalizeTrackerToken(term)
	if term == "" {
		return 0
	}
	// title 匹配权重 3(强信号),content 匹配权重 1(弱信号,提升召回)。
	title := strings.ToLower(article.Title)
	content := strings.ToLower(article.Content)
	needle := strings.ToLower(term)
	weight := 0
	if trackerAliasRequiresBoundary(needle) {
		if containsTrackerAliasWithBoundary(title, needle) {
			weight += 3
		}
		if containsTrackerAliasWithBoundary(content, needle) {
			weight += 1
		}
	} else {
		if strings.Contains(title, needle) {
			weight += 3
		}
		if strings.Contains(content, needle) {
			weight += 1
		}
	}
	return weight
}

func scoreArticle(article model.Article) int64 {
	if article.HeatValue > 0 {
		return article.HeatValue + maxInt64(article.HeatValue-article.PrevHeatValue, 0)
	}
	return 10_000
}

// applySourceDiversityBoost 对事件分组总分做"来源多样性"加权。
// 来源越多样,分数越高,让跨平台共振事件优先级更高。
//
// 系数设计(离散阶梯,便于调参):
//   - 1 个来源: 1.00x
//   - 2 个来源: 2.00x
//   - >=3 个来源: 4.00x
func applySourceDiversityBoost(base int64, distinctSources int) int64 {
	if base <= 0 {
		return base
	}
	if distinctSources <= 1 {
		return base
	}
	multiplier := int64(400)
	switch distinctSources {
	case 2:
		multiplier = 200
	}
	return base * multiplier / 100
}

// detectMomentum 严格判断:
// - up:热度有净增 AND 窗口内有新增文章(两条都满足才能判"升温")
// - down:热度净降(不要求 newCount,跌就是跌)
// - flat:其余(纯新增没热度上升,或纯热度上升没新增)
//
// 历史:旧实现用 OR — score_delta>0 || count_delta>0 都判 up,导致几乎不可能 down。
// 配合"acc.Score 是绝对值累加"的 bug,长窗口下永远升温。改 AND 后语义严格,
// 配合 GetWindowDeltas 的真实增量才有意义。
func detectMomentum(scoreDelta int64, newCount int) string {
	if scoreDelta > 0 && newCount > 0 {
		return "up"
	}
	if scoreDelta < 0 {
		return "down"
	}
	return "flat"
}

func trackerMomentumRank(momentum string) int {
	switch momentum {
	case "up":
		return 0
	case "flat":
		return 1
	default:
		return 2
	}
}

// truncateTitleAtWordBoundary 在 maxRunes 处截断标题,但尽量在词边界处切断,
// 避免把一个中文词劈开显示(如"暗杀" → "暗" 末尾)。
//
// 策略:
//  1. 若标题长度 ≤ maxRunes,直接返回原串。
//  2. 用 GSE 分词把标题切成词序列,从头累积到不超过 maxRunes 为止,
//     取最后一个完整词的结尾作为切断点。
//  3. GSE 不可用时(fallback)直接按 maxRunes 截断。
//  4. 截断后追加 "…" 省略号。
func truncateTitleAtWordBoundary(title string, maxRunes int) string {
	runes := []rune(title)
	if len(runes) <= maxRunes {
		return title
	}

	tokens := segmentTitle(title)
	if len(tokens) > 0 {
		var pos int   // 当前光标(rune 偏移)
		var cutAt int // 最近一次完整词结尾
		for _, tok := range tokens {
			tokLen := utf8.RuneCountInString(tok)
			if pos+tokLen > maxRunes {
				break
			}
			pos += tokLen
			cutAt = pos
		}
		if cutAt > 0 {
			return string(runes[:cutAt]) + "…"
		}
	}

	// fallback: 硬截断
	return string(runes[:maxRunes]) + "…"
}

// clusterTrackerEvents 将文章按共享实体聚类为事件组。
//
// 算法:
//  1. 为每篇文章提取实体/关键词集合(复用 extractTrackerCandidates)
//  2. 先按 entity 倒排索引找候选文章对,仅保留"共享非 hub 实体"的 pair
//  3. 对候选 pair 计算标题向量余弦相似度,达到阈值才 Union-Find 合并
//  4. 每组选最高热度文章标题为代表,聚合实体/关键词/来源
//  5. 只保留 count≥2 的组(单篇文章不算事件),按 score 降序
//
// deltaByID 用于计算每个事件的真实 momentum(升温/回落/持平)。
// 传 nil 时退化为 "flat"。
//
// 性能: 对 500 篇文章 < 20ms(倒排索引避免 O(N²))。
func clusterTrackerEvents(articles []model.Article, heatDiscovered map[string]struct{}, deltaByID map[int64]WindowDelta, limit int, windowHours int) []trackerEventGroup {
	if len(articles) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}

	metas := make([]trackerEventArticleMeta, 0, len(articles))
	idxByID := make(map[int64]int) // articleID → metas 索引

	for _, a := range articles {
		candidates := extractTrackerCandidates(a, heatDiscovered)
		if len(candidates) == 0 {
			continue
		}
		meta := trackerEventArticleMeta{
			id:          a.ID,
			title:       a.Title,
			sourceKey:   a.SourceKey,
			heatValue:   a.HeatValue,
			publishedAt: a.PublishedAt,
		}
		for _, c := range candidates {
			if c.Kind == "entity" {
				meta.entities = append(meta.entities, c.Label)
			} else {
				meta.keywords = append(meta.keywords, c.Label)
			}
		}
		idxByID[a.ID] = len(metas)
		metas = append(metas, meta)
	}

	if len(metas) < 2 {
		return nil
	}

	// Step 2: 倒排索引召回候选 pair。
	// 仅在"共享非 hub 实体"或"共享高置信关键词"下生成候选,避免全量 O(N²)。
	entityToArticles := make(map[string][]int)
	keywordToArticles := make(map[string][]int)
	for i, meta := range metas {
		for _, e := range meta.entities {
			entityToArticles[e] = append(entityToArticles[e], i)
		}
		for _, k := range meta.keywords {
			if shouldUseKeywordAsClusterConnector(k) {
				keywordToArticles[k] = append(keywordToArticles[k], i)
			}
		}
	}

	// Step 3: Union-Find
	parent := make([]int, len(metas))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	// hubEntities 是聚类中的"枢纽 entity" — 高频出现在多个不相关事件里。
	// 它们不参与共现计数,避免"中俄关系" + "中美关税" + "巴西世界杯"
	// 通过共享"中国/美国"等大国名串成一个超级事件团。
	// 注意:只收"出现频率极高且单独无话题区分度"的实体;
	// 俄罗斯/日本等不在此列 — 它们足够具体,与其他实体共现时是有效聚类信号。
	hubEntities := map[string]struct{}{
		"中国": {}, "美国": {},
		"北京": {}, "上海": {},
		"AI": {},
	}

	const titleCosineThreshold = 0.16
	const titleCosineSparseResidualThreshold = 0.06
	const titleCosineRawFallbackThreshold = 0.20
	const pairLinkThreshold = 0.42
	const wEntity = 0.35
	const wTitle = 0.35
	const wKeyword = 0.15
	const wTime = 0.10
	const wSourceDiversity = 0.05

	titleVectors := make([]map[string]float64, len(metas))
	for i, m := range metas {
		titleVectors[i] = buildTitleTermVector(m.title)
	}

	type pairSignal struct {
		entities map[string]struct{}
		keywords map[string]struct{}
	}
	pairSignals := make(map[[2]int]*pairSignal)
	getPairSignal := func(a, b int) *pairSignal {
		if a > b {
			a, b = b, a
		}
		key := [2]int{a, b}
		s, ok := pairSignals[key]
		if !ok {
			s = &pairSignal{
				entities: make(map[string]struct{}),
				keywords: make(map[string]struct{}),
			}
			pairSignals[key] = s
		}
		return s
	}

	for label, idxList := range entityToArticles {
		if _, isHub := hubEntities[label]; isHub {
			continue // hub entity 不参与共现计数
		}
		if len(idxList) < 2 {
			continue
		}
		for x := 0; x < len(idxList); x++ {
			for y := x + 1; y < len(idxList); y++ {
				a, b := idxList[x], idxList[y]
				s := getPairSignal(a, b)
				s.entities[label] = struct{}{}
			}
		}
	}

	for keyword, idxList := range keywordToArticles {
		if len(idxList) < 2 {
			continue
		}
		for x := 0; x < len(idxList); x++ {
			for y := x + 1; y < len(idxList); y++ {
				a, b := idxList[x], idxList[y]
				s := getPairSignal(a, b)
				s.keywords[keyword] = struct{}{}
			}
		}
	}

	for key, signal := range pairSignals {
		a, b := key[0], key[1]
		scoreRaw := cosineSimilarity(titleVectors[a], titleVectors[b])
		titleSemantic := scoreRaw
		if hasSparseResidualSignal(titleVectors[a], titleVectors[b], signal.entities) {
			// 去除共享实体与停用词后,若剩余有效词过少,
			// 优先使用 raw 相似度,避免排除词后向量几乎为空导致漏合并。
			titleSemantic = scoreRaw
		} else {
			scoreExcluded := cosineSimilarityExcludeTerms(titleVectors[a], titleVectors[b], signal.entities)
			titleSemantic = scoreExcluded
			if scoreRaw >= titleCosineRawFallbackThreshold {
				titleSemantic = scoreRaw
			}
		}

		entityOverlap := overlapRatio(signal.entities, 2)
		keywordOverlap := overlapRatio(signal.keywords, 2)
		timeProximity := timeProximityScore(metas[a].publishedAt, metas[b].publishedAt)
		sourceDiversity := 0.0
		if metas[a].sourceKey != metas[b].sourceKey {
			sourceDiversity = 1.0
		}

		pairScore := wEntity*entityOverlap +
			wTitle*titleSemantic +
			wKeyword*keywordOverlap +
			wTime*timeProximity +
			wSourceDiversity*sourceDiversity

		// keyword 不是硬门槛,但在无实体时仍需足够标题语义支撑,避免泛词误连。
		if len(signal.entities) == 0 && titleSemantic < titleCosineThreshold {
			continue
		}

		if pairScore >= pairLinkThreshold {
			// 低信息残量场景也保持一个最小语义底线。
			if titleSemantic < titleCosineSparseResidualThreshold {
				continue
			}
			union(a, b)
		}
	}

	// Step 4: 按组聚合
	groups := make(map[int][]int) // root → 组内文章索引列表
	for i := range metas {
		root := find(i)
		groups[root] = append(groups[root], i)
	}

	// Step 5: 构建事件组
	type eventSortKey struct {
		group             trackerEventGroup
		sourceDiversity   int
		latestPublishedAt time.Time
	}
	sortKeys := make([]eventSortKey, 0, len(groups))
	for _, members := range groups {
		minEventCount := 2
		if len(members) < minEventCount {
			continue // 单篇文章不算事件
		}

		// 同一来源的近重复标题去重:
		// 例如百度热榜同一事件会出现两条极相似标题,这里折叠成 1 条。
		// 去重后再参与 count/score/source/articles 统计,保证"计数和展示都按一条"。
		members = deduplicateClusterMembersBySource(members, metas)
		if len(members) < minEventCount {
			continue
		}

		// 找最高热度文章作为代表;同时累积 momentum 数据
		var bestIdx int
		var bestHeat int64
		latestIdx := members[0] // 初始化为组内第一篇,避免零值指向全局 metas[0]
		var latestPublishedAt time.Time
		entitySet := make(map[string]struct{})
		keywordSet := make(map[string]struct{})
		sourceCount := make(map[string]int)
		var baseScore int64
		var windowDelta int64
		var newCount int

		for _, idx := range members {
			meta := metas[idx]
			baseScore += scoreArticle(model.Article{HeatValue: meta.heatValue})
			sourceCount[meta.sourceKey]++
			if meta.heatValue > bestHeat {
				bestHeat = meta.heatValue
				bestIdx = idx
			}
			if meta.publishedAt.After(latestPublishedAt) {
				latestPublishedAt = meta.publishedAt
				latestIdx = idx
			}
			for _, e := range meta.entities {
				entitySet[e] = struct{}{}
			}
			for _, k := range meta.keywords {
				keywordSet[k] = struct{}{}
			}
			if deltaByID != nil {
				if d, ok := deltaByID[meta.id]; ok {
					windowDelta += d.Delta
					if d.IsNewInWindow {
						newCount++
					}
				}
			}
		}

		// 代表标题:3h 窗口用最新文章标题,其余用最高热度文章标题
		titleIdx := bestIdx
		if windowHours <= 3 {
			titleIdx = latestIdx
		}
		title := truncateTitleAtWordBoundary(metas[titleIdx].title, 25)

		// 实体/关键词列表 — 排序保证输出稳定
		entities := make([]string, 0, len(entitySet))
		for e := range entitySet {
			entities = append(entities, e)
		}
		sort.Strings(entities)
		keywords := make([]string, 0, len(keywordSet))
		for k := range keywordSet {
			keywords = append(keywords, k)
		}
		sort.Strings(keywords)

		// 组内文章:3h 窗口按发布时间降序,其余按热度降序。
		// 前端默认展示前 3 条,支持展开全部。
		eventArticles := make([]trackerArticleRef, 0, len(members))
		type sortItem struct {
			idx         int
			heat        int64
			publishedAt time.Time
		}
		sorted := make([]sortItem, 0, len(members))
		for _, idx := range members {
			sorted = append(sorted, sortItem{idx, metas[idx].heatValue, metas[idx].publishedAt})
		}
		if windowHours <= 3 {
			sort.Slice(sorted, func(i, j int) bool {
				return sorted[i].publishedAt.After(sorted[j].publishedAt)
			})
		} else {
			sort.Slice(sorted, func(i, j int) bool {
				return sorted[i].heat > sorted[j].heat
			})
		}
		for _, s := range sorted {
			m := metas[s.idx]
			eventArticles = append(eventArticles, trackerArticleRef{
				ID:          m.id,
				Title:       m.title,
				SourceKey:   m.sourceKey,
				HeatValue:   m.heatValue,
				PublishedAt: m.publishedAt,
			})
		}

		totalScore := applySourceDiversityBoost(baseScore, len(sourceCount))

		// 标记哪些实体/关键词是热词发现来的(不在静态词典中)
		var hdEntities, hdKeywords []string
		for _, e := range entities {
			if _, inLexicon := trackerEntityLabelSet[e]; !inLexicon {
				if _, inGeo := strongGeoNames[e]; !inGeo {
					hdEntities = append(hdEntities, e)
				}
			}
		}
		for _, k := range keywords {
			if _, inVerb := strongVerbs[k]; !inVerb {
				if _, inTopic := strongTopicNouns[k]; !inTopic {
					hdKeywords = append(hdKeywords, k)
				}
			}
		}

		sortKeys = append(sortKeys, eventSortKey{
			group: trackerEventGroup{
				Title:                  title,
				Entities:               entities,
				Keywords:               keywords,
				Score:                  totalScore,
				Count:                  len(members),
				Momentum:               detectMomentum(windowDelta, newCount),
				Sources:                flattenTrackerSources(sourceCount),
				Articles:               eventArticles,
				HeatDiscoveredEntities: hdEntities,
				HeatDiscoveredKeywords: hdKeywords,
			},
			sourceDiversity:   len(sourceCount),
			latestPublishedAt: latestPublishedAt,
		})
	}

	// 排序:3h 窗口按来源多样性降序、再按最新发布时间降序;其余按 score 降序。
	if windowHours <= 3 {
		sort.Slice(sortKeys, func(i, j int) bool {
			if sortKeys[i].sourceDiversity != sortKeys[j].sourceDiversity {
				return sortKeys[i].sourceDiversity > sortKeys[j].sourceDiversity
			}
			return sortKeys[i].latestPublishedAt.After(sortKeys[j].latestPublishedAt)
		})
	} else {
		sort.Slice(sortKeys, func(i, j int) bool {
			if sortKeys[i].group.Score != sortKeys[j].group.Score {
				return sortKeys[i].group.Score > sortKeys[j].group.Score
			}
			return sortKeys[i].group.Count > sortKeys[j].group.Count
		})
	}

	events := make([]trackerEventGroup, 0, len(sortKeys))
	for _, k := range sortKeys {
		events = append(events, k.group)
	}

	if len(events) > limit {
		events = events[:limit]
	}
	return events
}

// deduplicateClusterMembersBySource 在同一 source 内按标题近似度去重。
// 选择保留热度更高的一条,其余近重复标题折叠掉。
func deduplicateClusterMembersBySource(members []int, metas []trackerEventArticleMeta) []int {
	if len(members) <= 1 {
		return members
	}
	ordered := make([]int, len(members))
	copy(ordered, members)
	sort.Slice(ordered, func(i, j int) bool {
		return metas[ordered[i]].heatValue > metas[ordered[j]].heatValue
	})

	kept := make([]int, 0, len(ordered))
	keptBySource := make(map[string][]int)
	for _, idx := range ordered {
		m := metas[idx]
		dup := false
		for _, k := range keptBySource[m.sourceKey] {
			if isNearDuplicateTitle(m.title, metas[k].title) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		kept = append(kept, idx)
		keptBySource[m.sourceKey] = append(keptBySource[m.sourceKey], idx)
	}
	return kept
}

// buildTitleTermVector 构建标题的稀疏 term 向量(TF,未做 IDF)。
// token 来源优先 segmentTitle,失败回退正则切词。
func buildTitleTermVector(title string) map[string]float64 {
	vec := make(map[string]float64)
	tokens := segmentTitle(title)
	if len(tokens) == 0 {
		tokens = trackerTokenRegex.FindAllString(title, -1)
	}
	total := 0.0
	for _, tok := range tokens {
		n := normalizeTrackerToken(tok)
		if n == "" {
			n = normalizeLexiconAlias(tok)
		}
		n = canonicalizeTrackerToken(n)
		if n == "" || utf8.RuneCountInString(n) < 2 {
			continue
		}
		if isClusterSimilarityStopword(n) {
			continue
		}
		if isGenericRoleToken(n) {
			continue
		}
		vec[n] += 1
		total += 1
	}
	// 标题长度归一化:使用 TF(词频/标题有效 token 数),降低长标题天然高重叠带来的偏置。
	if total > 0 {
		for k, v := range vec {
			vec[k] = v / total
		}
	}
	return vec
}

func isClusterSimilarityStopword(token string) bool {
	_, ok := clusterSimilarityStopwords[token]
	return ok
}

var clusterSimilarityStopwords = map[string]struct{}{
	"消息": {},
	"回应": {},
	"表示": {},
	"公布": {},
	"发布": {},
	"宣布": {},
	"称":  {},
	"进行": {},
	"相关": {},
	"最新": {},
	"今天": {},
	"今日": {},
}

func hasSparseResidualSignal(a, b map[string]float64, excluded map[string]struct{}) bool {
	const minResidualTerms = 1
	return residualTermCount(a, excluded) <= minResidualTerms || residualTermCount(b, excluded) <= minResidualTerms
}

func residualTermCount(vec map[string]float64, excluded map[string]struct{}) int {
	count := 0
	for k := range vec {
		if _, skip := excluded[k]; skip {
			continue
		}
		count++
	}
	return count
}

func overlapRatio(shared map[string]struct{}, saturateAt int) float64 {
	if len(shared) == 0 {
		return 0
	}
	if saturateAt <= 1 {
		return 1
	}
	if len(shared) >= saturateAt {
		return 1
	}
	return float64(len(shared)) / float64(saturateAt)
}

func timeProximityScore(a, b time.Time) float64 {
	if a.IsZero() || b.IsZero() {
		return 0.5
	}
	delta := a.Sub(b)
	if delta < 0 {
		delta = -delta
	}
	h := delta.Hours()
	switch {
	case h <= 1:
		return 1.0
	case h <= 3:
		return 0.8
	case h <= 6:
		return 0.6
	case h <= 12:
		return 0.4
	case h <= 24:
		return 0.2
	default:
		return 0.05
	}
}

// cosineSimilarity 计算两个稀疏向量的余弦相似度。
func cosineSimilarity(a, b map[string]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	dot := 0.0
	normA := 0.0
	normB := 0.0
	for _, v := range a {
		normA += v * v
	}
	for _, v := range b {
		normB += v * v
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	// 遍历更小 map 降低开销
	small, large := a, b
	if len(small) > len(large) {
		small, large = large, small
	}
	for k, v := range small {
		if ov, ok := large[k]; ok {
			dot += v * ov
		}
	}
	return dot / (sqrt(normA) * sqrt(normB))
}

// cosineSimilarityExcludeTerms 计算余弦相似度,但忽略指定 term(如两篇共享实体)。
func cosineSimilarityExcludeTerms(a, b map[string]float64, excluded map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	dot := 0.0
	normA := 0.0
	normB := 0.0
	for k, v := range a {
		if _, skip := excluded[k]; skip {
			continue
		}
		normA += v * v
	}
	for k, v := range b {
		if _, skip := excluded[k]; skip {
			continue
		}
		normB += v * v
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	small, large := a, b
	if len(small) > len(large) {
		small, large = large, small
	}
	for k, v := range small {
		if _, skip := excluded[k]; skip {
			continue
		}
		if ov, ok := large[k]; ok {
			if _, skip2 := excluded[k]; skip2 {
				continue
			}
			dot += v * ov
		}
	}
	return dot / (sqrt(normA) * sqrt(normB))
}

func sqrt(v float64) float64 {
	if v <= 0 {
		return 0
	}
	// 牛顿迭代,避免引入 math 包外依赖变化
	x := v
	for i := 0; i < 8; i++ {
		x = 0.5 * (x + v/x)
	}
	return x
}

// isNearDuplicateTitle 判定两个标题是否属于"近重复"。
// 规则:
//  1. 标点/空白归一化后完全一致
//  2. token 集合重叠比例高(overlap/min(lenA,lenB) >= 0.75 且 overlap>=2)
func isNearDuplicateTitle(a, b string) bool {
	aNorm := normalizeTitleForClusterDedup(a)
	bNorm := normalizeTitleForClusterDedup(b)
	if aNorm != "" && aNorm == bNorm {
		return true
	}
	aSet := titleTokenSetForCluster(a)
	bSet := titleTokenSetForCluster(b)
	if len(aSet) == 0 || len(bSet) == 0 {
		return false
	}
	overlap := 0
	for t := range aSet {
		if _, ok := bSet[t]; ok {
			overlap++
		}
	}
	minSize := len(aSet)
	if len(bSet) < minSize {
		minSize = len(bSet)
	}
	if minSize == 0 {
		return false
	}
	return overlap >= 2 && float64(overlap)/float64(minSize) >= 0.75
}

func normalizeTitleForClusterDedup(title string) string {
	title = strings.ToLower(title)
	var b strings.Builder
	b.Grow(len(title))
	for _, r := range title {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func titleTokenSetForCluster(title string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, tok := range segmentTitle(title) {
		n := normalizeTrackerToken(tok)
		if n == "" {
			n = normalizeLexiconAlias(tok)
		}
		n = canonicalizeTrackerToken(n)
		if n == "" || utf8.RuneCountInString(n) < 2 {
			continue
		}
		if isGenericRoleToken(n) {
			continue
		}
		out[n] = struct{}{}
	}
	return out
}

func flattenTrackerSources(in map[string]int) []trackerSourceStat {
	out := make([]trackerSourceStat, 0, len(in))
	for source, count := range in {
		out = append(out, trackerSourceStat{SourceKey: source, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].SourceKey < out[j].SourceKey
	})
	return out
}

func flattenTrackerTerms(in map[string]struct{}, limit int) []string {
	out := make([]string, 0, len(in))
	for term := range in {
		out = append(out, term)
	}
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// buildTrackerStoryline 组装实体页响应。
//
// articles 由调用方过滤为"已包含 term"(SQL 直查)。
// deltas 是窗口内真实热度增量(每篇文章 captured_at 之前最近 snapshot 与 current 的差),
// 用于精确算 score_delta / momentum / new_count;调用方负责调 GetWindowDeltas 提供。
//
// deltas == nil 时降级:scoreDelta=0、newCount=0、momentum=flat,前端 chip 不显示。
// 这样 snapshot 表查询失败不会让整个 storyline 接口报错。
func buildTrackerStoryline(
	term string,
	articles []model.Article,
	deltas []WindowDelta,
	windowHours int,
) trackerStorylineResp {
	items := make([]trackerArticleRef, 0, len(articles))
	sources := map[string]int{}
	for _, article := range articles {
		items = append(items, trackerArticleRef{
			ID:          article.ID,
			Title:       article.Title,
			SourceKey:   article.SourceKey,
			Heat:        article.Heat,
			HeatValue:   article.HeatValue,
			PublishedAt: article.PublishedAt,
		})
		sources[article.SourceKey]++
	}

	// 累加窗口内真实热度增量 + 数窗口内新文章数。
	// deltas 跟 articles 是同一个 id 集合(handler 一次性传入),用 map 也行,
	// 这里直接遍历两次累加,N <= 200 性能完全无所谓。
	var scoreDelta int64
	newCount := 0
	for _, d := range deltas {
		scoreDelta += d.Delta
		if d.IsNewInWindow {
			newCount++
		}
	}

	summary := buildTrackerSummary(term, articles, windowHours)
	momentum := detectMomentum(scoreDelta, newCount)

	return trackerStorylineResp{
		Term:          term,
		Window:        trackerWindow{Hours: windowHours},
		Summary:       summary,
		Sources:       flattenTrackerSources(sources),
		Items:         items,
		Momentum:      momentum,
		ScoreDelta:    scoreDelta,
		NewCount:      newCount,
		TotalArticles: len(items),
	}
}

func buildRelatedTrackers(term string, articles []model.Article, limit int) []trackerTopic {
	filtered := filterArticlesByTerm(term, articles)
	if len(filtered) == 0 {
		return nil
	}

	// 这里只用 buildTrackerTopics 提取"相关 entity",不关心 momentum/排序方向 —
	// 传 deltas=nil,所有 entity 走 acc.Score 兜底排序,够用。如果未来要给"相关
	// 话题"也加 momentum 标记,再补 GetWindowDeltas 调用。
	related := buildTrackerTopics(filtered, nil, 24, limit+1)
	out := make([]trackerTopic, 0, len(related))
	needle := strings.ToLower(strings.TrimSpace(term))
	for _, item := range related {
		if strings.ToLower(item.Label) == needle {
			continue
		}
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func filterArticlesByTerm(term string, articles []model.Article) []model.Article {
	term = normalizeTrackerToken(term)
	if term == "" {
		return nil
	}

	canonical := canonicalizeTrackerToken(term)
	terms := []string{term}
	if canonical != "" {
		if aliases := trackerEntityTermsByLabel[canonical]; len(aliases) > 0 {
			terms = aliases
		}
	}

	type scored struct {
		article model.Article
		weight  int
	}

	matches := make([]scored, 0, len(articles))
	for _, article := range articles {
		weight := 0
		for _, candidate := range terms {
			weight += scoreTrackerTermMatch(candidate, article)
		}
		if weight > 0 {
			matches = append(matches, scored{article: article, weight: weight})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].weight != matches[j].weight {
			return matches[i].weight > matches[j].weight
		}
		if !matches[i].article.PublishedAt.Equal(matches[j].article.PublishedAt) {
			return matches[i].article.PublishedAt.After(matches[j].article.PublishedAt)
		}
		return matches[i].article.HeatValue > matches[j].article.HeatValue
	})

	if len(matches) > 20 {
		matches = matches[:20]
	}
	out := make([]model.Article, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.article)
	}
	return out
}

func buildTrackerSummary(term string, articles []model.Article, windowHours int) []string {
	if len(articles) == 0 {
		return []string{"当前窗口内还没有足够的关于「" + term + "」的相关文章。"}
	}

	bullets := []string{}
	latest := articles[0]
	bullets = append(bullets,
		"最近 "+strconv.Itoa(windowHours)+" 小时内出现 "+strconv.Itoa(len(articles))+" 条关于「"+term+"」的相关文章，最新进展是《"+latest.Title+"》。")

	sourceCounts := map[string]int{}
	for _, article := range articles {
		sourceCounts[article.SourceKey]++
	}
	sourceStats := flattenTrackerSources(sourceCounts)
	if len(sourceStats) > 1 {
		bullets = append(bullets,
			"讨论已扩散到 "+strconv.Itoa(len(sourceStats))+" 个内容源，主来源是 "+sourceStats[0].SourceKey+"。")
	}

	hottest := latest
	for _, article := range articles[1:] {
		if article.HeatValue > hottest.HeatValue {
			hottest = article
		}
	}
	if hottest.HeatValue > 0 {
		bullets = append(bullets,
			"当前热度最高的相关内容是《"+hottest.Title+"》，热度约 "+hottest.Heat+"。")
	}

	if len(bullets) > 3 {
		bullets = bullets[:3]
	}
	return bullets
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// deduplicateTrackerTopics 去除子串关系的 topic 冗余。
//
// 规则:长 label 包含短 label 时,**丢长的留短的**(短的信息密度高、热度信号集中)。
//
// 历史:旧规则是反过来的(长的吃短的,但要 short.Score <= long.Score*2),
// 这跟 candidates 阶段的 deduplicateCandidateSubstrings Pass 0(长 entity > 8 字
// 被短 entity 吸收)的设计**互相矛盾**:
//
//	· candidates 阶段:`俄罗斯总统普京发表视频讲话` 被 `普京` `俄罗斯` 吸收(短赢)
//	· 旧 topics 阶段:同一组数据,`普京` 又被某个长 label 吸收(长赢)
//
// 两个 pass 反向后,实际效果是"短"两次都不能稳定保留,prod 实测 `普京` 出现
// 在 storyline 接口但完全不出现在首页 topics(被该函数吸收)。
//
// 改回"短赢"统一两阶段:任何长 label 完全包含短 label 时,长 label 被吸收。
// 不再做 score 加权门槛 — 短 label 既然包含在长 label 里,语义上是同一事件,
// 谁的 score 都不影响"哪个更代表事件主体"的判断,主体永远是短词。
func deduplicateTrackerTopics(items []trackerTopic) []trackerTopic {
	if len(items) <= 1 {
		return items
	}

	absorbed := make([]bool, len(items))
	for i := range items {
		if absorbed[i] {
			continue
		}
		for j := range items {
			if i == j || absorbed[j] {
				continue
			}
			li := []rune(items[i].Label)
			lj := []rune(items[j].Label)
			if len(li) == len(lj) {
				continue
			}
			// 找出短和长
			short, long := i, j
			if len(li) > len(lj) {
				short, long = j, i
			}
			// 长 label 必须真的包含短 label 才有"子串"关系
			if !strings.Contains(items[long].Label, items[short].Label) {
				continue
			}
			// 长被吸收(跟 candidates Pass 0 一致)
			absorbed[long] = true
		}
	}

	out := make([]trackerTopic, 0, len(items))
	for i, item := range items {
		if !absorbed[i] {
			out = append(out, item)
		}
	}
	return out
}
