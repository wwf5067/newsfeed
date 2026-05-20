# 项目快速参考卡片

## 🎯 重点关注的文件

### 前端核心 (web/src/)
```
page.tsx (1114 行)           ← 首页,实体卡片核心代码在这里
├─ TopicGroup (310-422)       ← 话题卡片组件
├─ EventGroupCard (424-497)   ← 事件卡片组件
├─ ArticleRow (501-523)       ← 文章行组件
├─ CompactRow (527-554)       ← 热榜条目组件
├─ HeatBadge (218-237)        ← 热度徽章组件
└─ 数据聚合逻辑 (774-812)     ← 话题分组算法

tracker/page.tsx (530 行)     ← 实体详情页+时间线

lib/useIdSet.ts (71 行)       ← 已读/收藏跨页共享

layout.tsx (34 行)            ← 全局布局
```

### 后端核心 (internal/)
```
api/tracker.go                ← 实体抽取+事件聚类+热度计算
api/handler.go                ← HTTP 路由总入口
model/article.go              ← Article 数据模型
crawler/source.go             ← 爬虫源配置(知乎/百度/微博)
```

---

## 📊 核心数据结构速查

### Article (文章)
```typescript
{
  id: number
  source_key: "zhihu_hot" | "baidu_hot" | "weibo_hot" | "bilibili_popular"
  url: string
  title: string
  content: string           // 摘要
  heat: string              // "1234万热度" (源原文)
  heat_value: number        // 数值 (1234000)
  prev_heat_value: number   // 上次热度 (用于飙升判定)
  published_at: ISO string
  fetched_at: ISO string
}
```

### TrackerTopic (实体/关键词)
```typescript
{
  label: string             // "普京" / "AI"
  kind: "entity" | "keyword"
  score: number             // 热度数值
  momentum: "up" | "flat" | "down"
  count: number             // 相关文章数
  related_terms: string[]   // 关联词 (max 5)
  sources: { source_key: string; count: number }[]
  sample_article?: ArticleRef
}
```

### TrackerEventGroup (事件)
```typescript
{
  title: string             // 代表标题 (最高热度文章)
  entities: string[]        // 涉及实体列表
  keywords: string[]        // 涉及关键词列表
  score: number             // 组内总热度
  count: number             // 相关文章数
  momentum: "up" | "flat" | "down"
  sources: { source_key: string; count: number }[]
  articles: ArticleRef[]    // top 3
}
```

---

## 🎨 样式速查

### TopicGroup 卡片结构
```
┌─ 头部 (px-3 py-2.5) ──────────────────────┐
│ 标题      ↗ momentum    热度(emerald)      │
├─ 关联词区 (px-3 py-1.5) ───────────────────┤
│ 关联词chip... 关联词chip...  时间线 →     │
├─ 文章列表 (border-t) ────────────────────┤
│ 文章1 (px-3 py-2)                        │
│ 文章2 (border-b, hover:bg-zinc-50)       │
│ 文章3                                    │
├─ 底部 (px-3 py-1.5) ─────────────────────┤
│ 统计  [展开+5更多] [知乎 12] [百度 8]     │
└────────────────────────────────────────┘
```

### 色彩标注
```
🟨 amber-50/amber-100     ← chips 背景 (实体/关键词)
🟩 emerald-600            ← 热度数值 (增长趋势)
🔴 red-600                ← 热度徽章背景
⚪ zinc-100/zinc-200      ← 来源 chips / 副文本
🟦 blue-50                ← 信息提示
🟧 orange-100             ← 飙升标签
```

---

## 🔌 API 快速调用

### 获取首页数据
```
GET /api/v1/articles?limit=300&offset=0
    → { items: Article[], total, has_more, next_offset }

GET /api/v1/trackers?window=3&limit=12
    → { window: { hours }, items: TrackerTopic[], events: EventGroupCard[] }

GET /api/v1/hotlist
    → { zhihu: HotlistItem[], baidu: [], weibo: [] }
```

### 获取实体详情
```
GET /api/v1/trackers/storyline?term=普京&window=24
    → { term, window, summary: [], items: ArticleRef[], momentum, score_delta, new_count }

GET /api/v1/trackers/related?term=普京&window=24
    → { term, items: { label, kind, score, count, momentum }[] }
```

### 订阅管理
```
GET /api/v1/subscriptions
    → { items: Subscription[], notify_to: "user@example.com" }

POST /api/v1/subscriptions { keyword: "AI" }
    → { created: true }

DELETE /api/v1/subscriptions/123
    → success
```

---

## 🧮 算法要点

### 短词判定 (isShortCJKTerm)
```typescript
// ≤2 汉字 + 无英文字母 → titleOnly 匹配
// 例: "中国"(短词) vs "SpaceX"(非短词)
// 目的: 减少 content 误匹配
```

### 话题分组 (多重归属)
```typescript
// 一篇文章可属于多个话题
// grouped.push({ topic, articles: matched })
// matchedIds.add(a.id)  // 不占用
```

### 事件聚类 (≥2 entity)
```go
// 多篇文章共享≥2个实体 → 合并为事件
// 代表标题: max(heat_value).title
// 顶部优先: events 区块 > topics 区块
```

### 热度 Snapshot
```
score_delta = current_sum - baseline_sum

baseline    = N小时前的热度总和
current     = 现在的热度总和
momentum    = score_delta > 0 ? "up" : (score_delta < 0 ? "down" : "flat")
```

---

## 🚀 常见修改点

### 1. 调整卡片样式
- TopicGroup/EventGroupCard 头部: `px-3 py-2.5`
- 文章行: `px-3 py-2`
- Chips: `rounded-full bg-amber-50 px-1.5 py-0.5 text-[11px]`
- Hover 效果: `hover:bg-zinc-50 dark:hover:bg-zinc-800/50`

### 2. 改变数据来源
- 首页轮询间隔: `POLL_INTERVAL_MS = 5 * 60 * 1000`
- 分页大小: `PAGE_SIZE = 50`
- 时间窗口: `WINDOW_OPTIONS = [3, 6, 24, 72, 168, 720]`

### 3. 修改话题分组
- 多重归属 vs 唯一归属: 第 774-812 行
- 关联词数量: `related_terms.slice(0, 5)` 改为 `.slice(0, N)`
- 文章显示数: `articles.slice(0, 3)` 改为 `.slice(0, N)`

### 4. 调整热度计算
- 格式化: `formatHeat()` 第 184-192 行
- 飙升判定: `isSurging()` 第 824-828 行
- Top 排名: `topIdsBySource` 第 815-821 行

---

## 🔍 调试技巧

### 1. 查看实体分组结果
```typescript
console.log("grouped:", grouped);
console.log("ungrouped:", ungrouped);
console.log("matchedIds:", matchedIds);
```

### 2. 检查 API 响应
```typescript
const data = await fetchTrackers(3);
console.log("topics:", data.items);
console.log("events:", data.events);
```

### 3. 验证短词判定
```typescript
isShortCJKTerm("中国")          // true
isShortCJKTerm("SpaceX")        // false
isShortCJKTerm("人工智能")       // false (3字)
```

### 4. 查看热度趋势
```typescript
const diff = a.heat_value - a.prev_heat_value;
const surging = isSurging(a);
console.log("heat:", a.heat_value, "prev:", a.prev_heat_value, "surging:", surging);
```

---

## 📱 响应式设计

### 断点
- 手机: 无特殊约束 (移动优先)
- 平板: `max-w-3xl` (首页最大宽度)
- 桌面: `max-w-4xl` (详情页最大宽度)

### 关键类
```
mx-auto w-full max-w-3xl px-4 py-8    ← 首页/详情页通用容器
flex flex-wrap items-center gap-x-3   ← 响应式行
line-clamp-2 text-sm leading-snug     ← 标题截断
```

---

## 🌙 深色模式

### 常用类对
```
dark:bg-zinc-900        (白色 → 深灰)
dark:text-zinc-100      (黑色 → 浅白)
dark:border-zinc-800    (浅灰线 → 深灰线)
dark:hover:bg-zinc-800/50
dark:hover:text-zinc-200
```

---

## 📦 依赖版本

```json
{
  "next": "16.2.6",
  "react": "19.2.4",
  "tailwindcss": "^4"
}
```

---

## 💡 设计原则

1. **多维度视角**: 一篇文章可属于多个话题
2. **短词保护**: ≤2汉字+titleOnly 防止噪声
3. **事件优先**: 先展示聚合事件,再展示单实体
4. **热度快照**: 真实计算窗口内的增量(不是累加)
5. **深色友好**: 所有卡片都有 `dark:` 对应样式

---

**最后更新**: 2026-05-20
**维护者**: Claude Code
