# tracker.go 快速参考表

## 🎯 核心数据结构

| 类型 | 功能 | 关键字段 |
|------|------|---------|
| `trackerCandidate` | 候选实体/关键词 | Label, Kind(entity/keyword), RelatedTerms |
| `trackerLexiconEntry` | 词典条目 | Label, Aliases[], Category(event/person/company/place/ip) |
| `trackerAccumulator` | 热点聚合器 | Label, Kind, Score, WindowDelta, NewCount, Count |

---

## 🔄 extractTrackerCandidates 多层 Pipeline

```
入口: article.Title

┌─ 阶段 -1: 《》作品名提取 (bookTitleRegex)
├─ 阶段 -0.5: 「」引用短语提取 (quotedPhraseRegex)
├─ 阶段 -0.3: 头衔人名提取 (honorificRegex) → "X+院士"
├─ 阶段 0: 2字合称拆解 (compoundGeoAbbrevs) → "美以"→"美国+以色列"
├─ 阶段 0.5: 动词+地名扫描 (visitSuffixToEntity) → "访华"→"中国"
├─ 阶段 0.55: 人物→实体推断 (personEntityHints) → "普京"→"俄罗斯"
├─ 阶段 0.6: 复合词扫描 (compoundTopicKeywords) → "论文打假"
├─ 阶段 1: 词典AC匹配 (Aho-Corasick)
├─ 阶段 2: 中文分词 (GSE) + 重复检测
├─ 阶段 3: 兜底 - 标点切分 + 正则扫描
│
└─ 后处理:
   ├─ Pass 0: 长entity(>8字)包含短entity → 删长
   ├─ Pass 1: keyword包含entity → 删keyword
   ├─ Pass 2: keyword之间,删长的
   ├─ Pass 2.5: 复合词的子词被吸收
   ├─ 地名形容词过滤 (如"中国车手")
   └─ 最多返回6个
```

---

## 🏷️ 关键判定函数

### looksLikeEntity (判定是否是实体)
```
优先级递减:
1. 词典命中 (trackerEntityLabelSet) ✓
2. 强地名 (strongGeoNames) ✓
3. stopTokens? ✗ 
4. 通用英文词? ✗ (video/live/special)
5. 集数编号? ✗ (G1/S22/E3)
6. 句子? ✗ (looksLikeSentence)
7. 数据描述? ✗ (looksLikeDataLikePhrase)
8. 有大写字母?
   ├─ 短纯大写(≤3字)? ✗ (GT/AI)
   ├─ 长中英混合(汉>4)? ✗
   ├─ 1字汉字+英文? ✗
   ├─ 含动词(汉≥2)? ✗ (如"发布GPT")
   └─ 其他 ✓
9. 含"·"? ✓
10. 后缀匹配(entitySuffixes)? ✓ (如"XX公司")
11. 前缀匹配(entityPrefixes + 1-3字)? ✓ (如"北京XX")
12. 兜底 → ✗
```

### shouldKeepTrackerToken (综合决策)
```
1. 词典精确命中? → ✓ (最高优先)
2. stopTokens? → ✗
3. 通用英文词? → ✗
4. 集数编号? → ✗
5. 栏目标签? → ✗ (【调查】【测评】)
6. looksLikeEntity? → ✓
7. strongTopicNouns? → ✓ (房价/社保等)
8. 数据描述? → ✗
9. 句子? → ✗
10. 话题短语? → ✓ (looksLikeTopicPhrase)
```

### looksLikeSentence (判定是否是句子)
```
任一命中 → 是句子:
- 汉字>10 OR 总rune>14
- sentenceVerbInfixes在中间 (46个动词)
- sentenceTailQuestions结尾 (如何/怎样/吗)
- sentenceColloquialPrefixes开头(汉≥4) (我/为了/试了/这种等)
- sentenceCopulaInfixes在中间 (是第一/其实是等)
- 从...到... (句式)
- sentenceVerbPrefixes开头(汉≥4) (刷新/打破等)
```

---

## 📋 关键集合与字典

### compoundTopicKeywords (10个)
```
论文打假 论文造假 学术造假
校园欺凌 校园霸凌
贸易谈判 贸易战
全球升温 气候变化 全球变暖
```
**作用**: 在阶段0.6扫描,整体注入,不走正常过滤

### stopTokens (~167个)
```
分类:
- 代词/疑问: 这个/什么/为什么等
- 情态词: 是否/可以/应该等
- 平台词: 知乎/视频/网友等
- 套话: 回应/发布/如何评价等
- 时间: 今天/昨天/最新等
- 修饰: 非常/十分/特别等
```
**作用**: isGenericRoleToken 检查,优先级最高的过滤机制

### strongTopicNouns (~40个)
```
经济: 房价/楼市/股市/基金等
民生: 高考/考研/社保/医保等
科技: 芯片/算力/内卷等
社会: 醉驾/网暴/AI换脸等
气候: 雾霾/沙尘暴
国际: 关税/制裁/脱钩等
```
**作用**: 跳过weak过滤,同entity优先级保留

### strongGeoNames (~65个)
```
省份: 河南/湖南/广东等 (24个)
城市: 武汉/成都/杭州等 (28个)
港澳台: 台湾/香港/澳门等 (4个)
国家: 泰国/印度/英国/美国等 (14个)
```
**作用**: 2字但信息量高,跳过weak过滤

### entitySuffixes (34个)
```
机构: 公司/集团/政府/银行等
教育: 大学/医院/研究院等
展会: 航展/车展/博览会等
```

### entityPrefixes (12个)
```
地名: 中国/美国/北京/上海等
权力: 国家/中央/全国
```

### compoundGeoAbbrevs (11个)
```
美以 → (美国, 以色列)
中美 → (中国, 美国)
俄乌 → (俄罗斯, 乌克兰)
等...
```

### visitSuffixToEntity (18个)
```
访华 赴华 → 中国
访美 赴美 → 美国
访俄 → 俄罗斯
等...
```

### personEntityHints (~36个)
```
政治: 普京→俄罗斯, 特朗普→美国等
企业: 马斯克→特斯拉, 雷军→小米等
```

---

## 正则表达式速览

| 名称 | 模式 | 例 | 用途 |
|------|------|-----|------|
| `bookTitleRegex` | `《([^》]{1,20})》` | 《影·迷》 | 作品名 |
| `quotedPhraseRegex` | `「([^」『]{1,20})」` | 「肉夹馍」 | 引用短语 |
| `honorificRegex` | `\p{Han}{2,4}(院士\|教授...)` | 方岱宁院士 | 头衔人名 |
| `trackerTokenRegex` | 三分支混合 | 英文/汉字混合 | 通用分词 |
| `trackerTitleSplitRegex` | `[{}/:：,，。！?...]` | 标点切分 | 句子切分 |
| `sentenceFromToRegex` | `^从.{1,5}到.{1,5}` | 从黑马到大爆 | 标题党 |
| `episodeCodeRegex` | `^[A-Za-z]{1,2}\d{1,3}$` | G1/S22 | 集数编号 |

---

## dataLikePatterns (23个正则)

过滤"伪体育/数据描述":
```
赛季相关: 2526赛季...第37轮, 赛季NBA季后赛...
比分相关: 1-0战胜, 马刺122-115雷霆
运动员: 文班亚马41分24篮板
系列赛: 西决G1, 东决G3
票房: 票房破5亿
人物: 41岁C罗领衔, 33岁梅西首发
```

---

## Event 分类

### trackerLexiconEntry.Category
```
company  - 公司
person   - 人物
ip       - 知识产权
event    - 事件 ← 你关心的
place    - 地点
""       - 未分类
```

### 与Event的映射关系
```
strongVerbs (26个) → 事件动词
裁员/患癌/确诊/暴跌/性侵/诈骗等
本质是事件触发词

compoundTopicKeywords中
- 贸易战/贸易谈判 → event
- 校园欺凌/校园霸凌 → event
- 气候变化/全球升温 → event

topicSuffixes (20+个)
- 事件/政策/比赛/事故 → 直接包含event语义
- 台风/地震/暴雨/洪水 → 自然事件
- 罢工/融资/停运 → 社会事件
```

**注意**: 代码中 **没有专门的 event 提取路径**,
event分类是词典中的元数据,前端用来筛选。

---

## 性能提示

```
分词开销: GSE分词 (阶段2) 最耗时
字典查询: AC自动机 O(n) 高效

优化建议:
- 词典精确命中优先级最高,词典是第一道防线
- repeatedTokens检测确保重要词不被weak过滤
- 复合词保护防止长词被短词吸收
- strongGeoNames/strongTopicNouns特殊保护高频词
```

---

## 调试技巧

```go
// 追踪候选生成
if label == "你要调试的词" {
    fmt.Printf("forced=%v repeated=%v compound=%v\n", forced, repeated, isCompoundKw)
}

// 检查过滤原因
if !shouldKeepTrackerToken(token) {
    // 按优先级逐一检查:
    // 1. isGenericRoleToken?
    // 2. isGenericEnglishWord?
    // 3. isEpisodeCode?
    // 4. looksLikeEntity -> false?
    // 5. looksLikeDataLikePhrase?
    // 6. looksLikeSentence?
}

// 检查为什么被判为sentence
if looksLikeSentence(token) {
    // 8条规则中哪一条命中了
    // 比如含sentenceVerbInfixes?
    // 或者汉字数>10?
}
```

