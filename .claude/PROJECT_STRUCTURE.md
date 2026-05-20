# 项目整体结构探索总结

## 📁 项目概览
- **项目名**: Newsfeed (个人聚合新闻源)
- **技术栈**: 
  - 后端: Go + PostgreSQL
  - 前端: Next.js 16 + React 19 + TypeScript + Tailwind CSS
  - 部署: Docker

## 📂 目录结构

### 项目根目录
```
newsfeed/
├── web/                    # Next.js 前端应用
├── internal/               # Go 后端核心逻辑
├── cmd/                    # 命令行入口
├── migrations/             # 数据库迁移文件
├── deploy/                 # 部署配置
├── go.mod / go.sum         # Go 依赖
├── Makefile                # 构建脚本
├── .env / .env.example     # 环境配置
└── README.md               # 项目说明
```

---

## 🎨 前端结构 (`web/src/`)

### 核心页面

#### 1. **首页** (`web/src/app/page.tsx`) - 主新闻聚合页面
**文件大小**: ~1114 行

**核心类型定义**:
```typescript
type Article {
  id: number
  source_key: string        // 来源标识: zhihu_hot, bilibili_popular, baidu_hot, weibo_hot
  url: string
  title: string
  content: string
  heat: string              // 热度文本(如"1234万")
  heat_value: number        // 热度数值
  prev_heat_value: number   // 上次热度(用于飙升判定)
  published_at: string
  fetched_at: string
}

type TrackerTopic {           // 实体/关键词话题
  label: string
  kind: "entity" | "keyword"
  score: number
  count: number
  momentum: "up" | "flat" | "down"
  related_terms: string[]
  sources: { source_key: string; count: number }[]
  sample_article?: ArticleRef
}

type TrackerEventGroup {      // 事件聚类(多篇文章共享≥2个实体)
  title: string
  entities: string[]
  keywords: string[]
  score: number
  count: number
  momentum: string
  sources: { source_key: string; count: number }[]
  articles: ArticleRef[]
}

type HotlistItem {           // 热榜条目(知乎/百度/微博)
  id: number
  rank: number
  rank_change: number
  is_new: boolean
}
```

**功能模块**:
1. **AnnouncementBar** - 公告栏组件
2. **TopicGroup** - 话题卡片(实体聚合视图)
   - 展示话题标题+热度
   - 关联词 chips
   - 相关文章列表
   - 来源分布

3. **EventGroupCard** - 事件卡片(事件聚类结果)
   - 事件标题+热度
   - 涉及实体/关键词
   - 相关文章(默认3条,可展开)
   - 来源分布

4. **ArticleRow** - 紧凑型文章行(知乎/百度/微博 Tab)
   - 排序号 + 标题 + 时间 + 热度

5. **CompactRow** - 热榜条目
   - 排序号 + 标题 + 排名变化 + 热度

6. **HotBadge** - 热度徽章组件
   - 源标志 + 热度数值 + 趋势箭头

**页面布局**:
- 全部 Tab(无搜索) → **话题聚合视图** (TopicGroup + EventGroupCard)
- 知乎/百度/微博 Tab 或 搜索 → **时间流视图** (ArticleRow 卡片)
- 侧栏: HotPanel (大家在聊什么)
- 邮件订阅折叠区

**API 调用**:
- `GET /api/v1/articles?source=<>&q=<>&limit=<>&offset=<>` - 文章列表
- `GET /api/v1/trackers?window=<>&limit=<>` - 话题+事件
- `GET /api/v1/hotlist` - 热榜
- `GET /api/v1/announcements` - 公告
- `GET /api/v1/subscriptions` - 邮件订阅列表
- `POST /api/v1/subscriptions` - 添加订阅
- `DELETE /api/v1/subscriptions/<id>` - 删除订阅

---

#### 2. **实体详情页** (`web/src/app/tracker/page.tsx`) - 时间线详情
**文件大小**: ~530 行

**核心类型定义**:
```typescript
type TrackerStorylineResp {
  term: string
  window: { hours: number }
  summary: string[]              // 事件摘要(多行)
  sources: { source_key: string; count: number }[]
  items: ArticleRef[]
  momentum: "up" | "flat" | "down"
  score_delta: number            // 窗口内热度净增
  new_count: number              // 新增文章数
  total_articles: number
}

type RelatedTrackerResp {
  term: string
  items: {
    label: string
    kind: "entity" | "keyword"
    score: number
    count: number
    momentum: string
  }[]
}
```

**功能**:
1. **时间分组** (groupByTime)
   - 今天 / 昨天 / 本周 / 更早
   - 组内按热度倒序

2. **订阅管理**
   - 一键订阅/取消当前实体
   - 关键词邮件通知

3. **窗口切换** (3h / 6h / 24h / 3d / 7d / 30d)
   - 动态切换时间范围

4. **关联话题** (RelatedTrackerResp)
   - 相关实体/关键词跳转

**API 调用**:
- `GET /api/v1/trackers/storyline?term=<>&window=<>` - 时间线
- `GET /api/v1/trackers/related?term=<>&window=<>` - 关联话题
- `GET/POST/DELETE /api/v1/subscriptions` - 订阅管理

---

#### 3. **文章详情页** (`web/src/app/article/page.tsx`)
- 尚未详细查看,但通过路由 `/article?id=<>` 存在

---

### 共享 Hooks & 工具

#### **useIdSet** (`web/src/lib/useIdSet.ts`)
```typescript
useIdSet(storageKey: string): {
  ids: Set<number>
  add(id: number): void
  toggle(id: number): void
}
```
- 跨页面共享已读/收藏状态 (localStorage)
- 键: `READ_KEY = "read_ids"` / `STARRED_KEY = "starred_ids"`
- 异常隐性处理(隐私模式、配额满等)

#### **格式化工具**:
- `formatHeat(v)` - 数字转文本(1e8 → 亿, 1e4 → 万)
- `formatRelativeTime(iso)` - 相对时间(刚刚/5分钟前)
- `formatTime(iso)` - 本地化时间
- `formatTimeShort(iso)` - 时间简写(HH:MM)
- `formatDateShort(iso)` - 日期简写(MM-DD)
- `formatSignedHeat(v)` - 带符号的热度变化(±100)

#### **CJK 文本处理**:
- `hanRuneCount(s)` - 汉字统计
- `isShortCJKTerm(s)` - 短词判定(≤2汉字+无英文 → titleOnly 匹配)

#### **常量配置**:
```typescript
SOURCE_LABELS = {
  zhihu_hot: "知乎",
  bilibili_popular: "B站",
  baidu_hot: "百度",
  weibo_hot: "微博",
}

SOURCE_FILTERS = [
  { key: "", label: "全部" },
  { key: "zhihu_hot", label: "知乎" },
  { key: "baidu_hot", label: "百度" },
  { key: "weibo_hot", label: "微博" },
]

POLL_INTERVAL_MS = 5 * 60 * 1000    // 5 分钟刷新
PAGE_SIZE = 50                      // 分页大小
WINDOW_OPTIONS = [3, 6, 24, 72, 168, 720] // 时间窗口(小时)
```

### 样式配置

#### **Tailwind CSS** (`web/tailwind.config.js`)
- 深色模式支持 (`dark:`)
- 调色板: 主要用 zinc(灰) / amber(琥珀) / emerald(绿) / red(红) / blue(蓝)
- 响应式: 移动优先

#### **全局样式** (`web/src/app/globals.css`)
- 使用 Tailwind PostCSS 4

#### **字体** (Google Fonts)
- Geist Sans(正文)
- Geist Mono(代码)

---

## 🔧 后端结构 (`internal/`)

### API 模块 (`internal/api/`)

#### **主要文件**:
1. **handler.go** - HTTP 路由总入口
2. **tracker.go** - 话题追踪逻辑
   - `trackerTopic` - 话题结构
   - `trackerEventGroup` - 事件聚类结构
   - `trackerStorylineResp` - 时间线响应
   - 实体/关键词抽取算法
   - CJK 文本处理(token 提取、别名识别)
   - 热度 snapshot 计算
   - 事件聚类(共享≥2实体)

3. **tracker_extract_test.go** - 实体抽取单元测试
4. **tracker_event_cluster_test.go** - 事件聚类单元测试
5. **tracker_heat_discovery_test.go** - 热度发现单元测试
6. **subscriptions.go** - 邮件订阅 API
7. **share.go** - 文章分享 API
8. **rss.go** - RSS 输出 API

**核心数据结构**:
```go
type trackerTopic struct {
  Label         string              // 实体/关键词
  Kind          string              // "entity" / "keyword"
  Score         int64               // 热度(绝对值+窗口增量)
  PrevScore     int64
  ScoreDelta    int64
  Count         int                 // 相关文章数
  PrevCount     int
  CountDelta    int
  Momentum      string              // "up" / "flat" / "down"
  Sources       []trackerSourceStat
  RelatedTerms  []string
  SampleArticle *trackerArticleRef
}

type trackerEventGroup struct {
  Title    string
  Entities []string
  Keywords []string
  Score    int64
  Count    int
  Momentum string
  Sources  []trackerSourceStat
  Articles []trackerArticleRef     // 最多 3 条
}

type trackerLexiconEntry struct {
  Label    string
  Aliases  []string
  Category string               // 元数据: company/person/ip/event/place/""
}
```

**核心算法**:
- 实体抽取: 正则 token 提取 + 启发式过滤 + 词典别名匹配
- CJK 文本处理: 汉字 Unicode 范围检测 (`一-鿿` 基本区 + 扩展 A 区)
- 短词判定: ≤2 汉字 + 无英文 → titleOnly 匹配 (减少噪声)
- 事件聚类: 多篇文章共享≥2 个实体 → 合并为一个事件
- 热度 snapshot: 记录窗口起点/末端热度 → 计算真实增量(不是累加)
- Momentum 判定: 基于热度增量 + 新增文章数

---

### 数据模型 (`internal/model/`)

#### **article.go**
```go
type Article struct {
  ID            int64
  SourceKey     string      // 源: zhihu_hot / bilibili_popular / baidu_hot / weibo_hot
  URL           string      // 唯一,去重用
  Title         string
  Content       string
  Author        string
  Heat          string      // 源原文热度(如"1234万热度")
  HeatValue     int64       // 数值形式
  PrevHeat      string
  PrevHeatValue int64
  SourceRank    int         // 官方榜位(1-based, 0=未知)
  PublishedAt   time.Time
  FetchedAt     time.Time
}
```

#### **announcement.go**
```go
type Announcement struct {
  ID        int64
  Content   string
  Level     string        // "info" / "warn" / "critical" / "quote"
  Priority  int
  StartsAt  time.Time
  EndsAt    *time.Time
  IsActive  bool
}
```

---

### 爬虫模块 (`internal/crawler/`)

#### **主要文件**:
1. **source.go** - 数据源配置(知乎/B站/百度/微博)
2. **runner.go** - 爬虫调度
3. **heat.go** + **heat_test.go** - 热度解析
4. **repository.go** - 数据库操作
5. **announcements_repository.go** - 公告管理

**支持的数据源**:
- 知乎热榜 (`zhihu_hot`)
- B 站热门 (`bilibili_popular`)
- 百度热搜 (`baidu_hot`)
- 微博热搜 (`weibo_hot`)

---

### 邮件 & 订阅模块

#### **mailer/mailer.go**
- 邮件发送接口
- 集成第三方邮件服务(如 SendGrid/Mailgun)

#### **subscribe/matcher.go** & **subscribe/repository.go**
- 关键词匹配逻辑
- 订阅表管理

---

### 存储模块 (`internal/storage/`)

#### **db.go**
- PostgreSQL 连接池
- 数据库初始化

---

### 配置 & 日志 (`internal/config/`, `internal/logger/`)

#### **config/config.go**
- 环境变量解析
- 数据库/邮件/爬虫配置

#### **logger/logger.go**
- 结构化日志 (slog)

---

## 📊 API 端点总览

### 文章相关
```
GET  /api/v1/articles
     ?source=<zhihu_hot|baidu_hot|weibo_hot>
     &q=<搜索词>
     &limit=50&offset=0
  
  返回: { items: Article[], total, has_more, next_offset }
```

### 话题追踪
```
GET  /api/v1/trackers
     ?window=3&limit=12
  
  返回: { window: { hours }, items: TrackerTopic[], events: EventGroupCard[] }

GET  /api/v1/trackers/storyline
     ?term=<>&window=3
  
  返回: TrackerStorylineResp (时间线+摘要)

GET  /api/v1/trackers/related
     ?term=<>&window=3
  
  返回: RelatedTrackerResp (关联话题)
```

### 热榜
```
GET  /api/v1/hotlist

  返回: { zhihu: HotlistItem[], baidu: [], weibo: [] }
```

### 公告
```
GET  /api/v1/announcements

  返回: { items: Announcement[] }
```

### 邮件订阅
```
GET    /api/v1/subscriptions
       返回: { items: Subscription[], notify_to: string }

POST   /api/v1/subscriptions
       body: { keyword: string }
       返回: { created?: boolean }

DELETE /api/v1/subscriptions/:id
```

### 文章分享
```
GET  /share/:id
     返回: 分享页面(og meta 用于 social share preview)
```

### RSS
```
GET  /rss.xml
     返回: RSS feed
```

---

## 🎯 前端实体卡片相关关键代码

### TopicGroup 卡片(话题卡片)
**路径**: `page.tsx` 第 310-422 行

```tsx
function TopicGroup({
  topic: TrackerTopic,
  articles: Article[],
  windowHours: number,
  onSearch: (q: string) => void
}) {
  // 1. 头部: 实体标题 + momentum + 热度
  <div className="flex items-center gap-2 px-3 py-2.5">
    <Link className="text-[14px] font-semibold">{topic.label}</Link>
    <span className="shrink-0">{momentum icon} {momentum text}</span>
    <span className="ml-auto text-emerald-600">{formatHeat(topic.score)}</span>
  </div>

  // 2. 关联词 chips + 时间线链接
  {topic.related_terms.slice(0, 5).map(term => 
    <button className="rounded-full bg-amber-50 px-1.5 py-0.5">{term}</button>
  )}
  <Link className="ml-auto text-[11px]">时间线 →</Link>

  // 3. 文章列表 (默认 3 条,可展开)
  {displayArticles.map(a => 
    <Link className="flex items-center gap-2 px-3 py-2">
      {a.title} | {SOURCE_LABELS[a.source_key]} | {formatHeat(a.heat_value)}
    </Link>
  )}

  // 4. 底部: 文章统计 + 展开/收起 + 来源 chips
  <span>{totalCount} 篇</span>
  {hasMore && <button>+{articles.length - 3} 更多</button>}
  {topic.sources.map(s => 
    <span className="rounded bg-zinc-100 px-1.5 py-0.5">{SOURCE_LABELS[s.source_key]} {s.count}</span>
  )}
}
```

**样式特征**:
- 卡片: `rounded-lg border shadow-sm dark:bg-zinc-900`
- 头部: `px-3 py-2.5` (与事件卡片保持一致)
- Chips: `rounded-full bg-amber-50 px-1.5 py-0.5 text-[11px]` (琥珀色)
- 文章行: `border-b px-3 py-2 hover:bg-zinc-50`
- 底部: `border-t border-zinc-50 px-3 py-1.5`

---

### EventGroupCard 卡片(事件卡片)
**路径**: `page.tsx` 第 424-497 行

```tsx
function EventGroupCard({
  event: TrackerEventGroup,
  windowHours: number
}) {
  // 1. 头部: 事件标题 + 热度
  <div className="flex items-center gap-2 px-3 py-2.5">
    <h3 className="text-[14px] font-semibold">{event.title}</h3>
    <span className="shrink-0 text-emerald-600">{formatHeat(event.score)}</span>
  </div>

  // 2. 实体/关键词 chips
  {event.entities.map(e => 
    <Link className="rounded-full bg-amber-50 px-1.5 py-0.5 text-amber-700">{e}</Link>
  )}
  {event.keywords.map(k => 
    <span className="rounded-full bg-zinc-100 px-1.5 py-0.5 text-zinc-500">{k}</span>
  )}

  // 3. 文章列表 (默认 3 条,可展开)
  {displayArticles.map(a => 
    <Link className="flex items-center gap-2 px-3 py-2">
      {a.title} | {SOURCE_LABELS[a.source_key]} | {formatHeat(a.heat_value)}
    </Link>
  )}

  // 4. 底部: 文章统计 + 展开/收起 + 来源 chips
  <span>{event.count} 篇</span>
  {hasMore && <button>+{event.articles.length - 3} 更多</button>}
  {event.sources.map(s => 
    <span className="rounded bg-zinc-100 px-1.5 py-0.5">{SOURCE_LABELS[s.source_key]} {s.count}</span>
  )}
}
```

**与 TopicGroup 的异同**:
- 相同: 整体卡片结构、文章行样式、底部统计
- 不同:
  - TopicGroup 有"关联词"和"时间线"链接
  - EventGroupCard 有"实体"+"关键词"两类 chips
  - momentum 显示位置: TopicGroup 在头部, EventGroupCard 隐含在整体热度

---

### 文章卡片(时间流)
**路径**: `page.tsx` 第 1040-1080 行

```tsx
<li key={a.id} className="rounded-lg border bg-white p-4 shadow-sm dark:bg-zinc-900">
  {/* 收藏 + 分享按钮 (右上) */}
  <button className="absolute right-3 top-3">★/☆</button>
  <button className="absolute right-9 top-3">↗</button>

  {/* 内容 */}
  <Link className="flex gap-3">
    <span className="shrink-0 font-mono text-sm text-zinc-400">{index+1}</span>
    
    <div className="min-w-0 flex-1">
      {/* 标题 + 标签行 */}
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <h2 className="text-base font-medium leading-snug">{a.title}</h2>
        {a.heat_value > 0 && <HeatBadge ... />}
        {topIdsBySource.has(a.id) && <span>🏆 Top{topIdsBySource.get(a.id)}</span>}
        {isSurging(a) && <span>🚀 飙升</span>}
      </div>

      {/* 摘要 */}
      <p className="mt-1.5 line-clamp-2 text-[13px] text-zinc-600">{a.content}</p>

      {/* 元数据 */}
      <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-500">
        <span>{SOURCE_LABELS[a.source_key]}</span>
        <span>{formatTime(a.published_at)}</span>
      </div>
    </div>
  </Link>
</li>
```

**样式特征**:
- 卡片: `rounded-lg border bg-white p-4 shadow-sm dark:bg-zinc-900`
- 已收藏: `border-l-4 border-l-amber-400`
- 已读: `opacity-60`
- 标题: `text-base font-medium leading-snug`
- 标签: `inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-xs font-medium`
- 热度徽章: 红色 + 上升/下降箭头
- Top 标签: 琥珀色 + 🏆
- 飙升标签: 橙色 + 🚀

---

## 🔑 关键设计决策

### 1. **话题聚合分组算法** (page.tsx 第 774-812 行)
- 原则: **多重归属** (一篇文章可属于多个话题)
- 好处: 信息完整,用户可从多个维度切入同一事件
- 代价: 首页可能有冗余文章(用户可接受)
- 实现: `grouped` 数组 + `matchedIds` Set

### 2. **短词特殊处理** (isShortCJKTerm)
- ≤2 汉字 + 无英文字母 → titleOnly 匹配
- 防止 content 偶然提及导致误分组(首页不一致)

### 3. **时间窗口** (WINDOW_OPTIONS)
- 3h: 刚出热点
- 6h: 实时热度
- 24h: 一日热度
- 72h: 3 日热度
- 168h: 周热度
- 720h: 月热度
- 默认首页: 3h(优先新事件)
- 默认详情页: 30d(查看完整历史)

### 4. **热度 Snapshot 机制**
- 记录窗口起点和末端热度
- 计算真实增量(不是累加)
- Momentum 判定: `(end - start) / start >= 0.1` 为 up

### 5. **事件聚类** (trackerEventGroup)
- 条件: 多篇文章共享≥2 个实体 → 合并为一个"事件"
- 代表标题: 最高热度文章的标题
- 顶部优先级: 事件 > 话题(首页先显示 events 区块)

---

## 🎨 设计系统

### 色彩体系
```
主色:
- 琥珀 (amber): 实体/关键词 chips, Top 标签, 收藏状态
- 绿 (emerald): 热度数值, 上升趋势
- 红 (red): 热度徽章, 热度值显示
- 灰 (zinc): 中性文本, 分割线

状态色:
- 蓝 (blue): 信息提示
- 橙 (orange): 飙升标签
- 绿 (emerald): 正向趋势
```

### 尺寸体系
```
字体:
- H1/H2: text-2xl / text-base
- 正文: text-sm / text-[13px]
- 标签/副文本: text-xs / text-[11px] / text-[10px]

间距:
- 卡片内部: px-3 py-2.5 / px-4 py-3
- 分组间: mb-6 / space-y-2
- Chips: px-1.5 py-0.5 / rounded-full

边界/阴影:
- 卡片: border shadow-sm / rounded-lg
- Hover: hover:shadow-md / hover:bg-zinc-50
```

---

## 📡 数据流

### 首页加载流程
```
1. 用户打开 / (首页)
   ↓
2. useEffect: 拉取 articles + topics + events + hotlist
   ├─ fetchArticles() → setArticles()
   ├─ fetchTrackers() → setTopics(), setEvents()
   ├─ fetchHotlist() → setHotlist()
   └─ 每 5 分钟轮询刷新
   ↓
3. 渲染:
   ├─ isTopicView=true → TopicGroup + EventGroupCard + HotPanel
   └─ isTopicView=false → ArticleRow 列表 + 加载更多

4. 用户交互:
   ├─ 点击实体 → 跳转 /tracker?term=<>&window=<>
   ├─ 搜索词 → setQuery() → 防抖后 setDebouncedQ() → 重新拉取
   ├─ 切换 Tab → setSource() → 重新拉取
   ├─ 点击文章 → read.add(id) → 跳转 /article?id=<>
   ├─ 收藏文章 → starred.toggle(id)
   └─ 分享文章 → copyShareLink(id)
```

### 实体详情页流程
```
1. 用户点击实体 (TopicGroup.label)
   ↓
2. 跳转到 /tracker?term=<>&window=3
   ↓
3. useEffect: 拉取 storyline + related + subscriptions
   ├─ fetchTrackerStoryline() → setData()
   ├─ fetchRelatedTrackers() → setRelated()
   └─ listSubscriptions() → setSubscriptionId()
   ↓
4. 渲染:
   ├─ 头部: 实体名 + 热度变化 + 订阅按钮
   ├─ 时间窗口 Tab
   ├─ 摘要 + 来源分布
   ├─ 时间分组文章列表 (今天/昨天/本周/更早)
   └─ 关联话题 chips
   ↓
5. 用户交互:
   ├─ 切换时间窗口 → router.push() → 重新拉取
   ├─ 订阅/取消订阅 → handleSubscribe()
   └─ 点击文章/关联话题 → 跳转
```

---

## 💡 核心算法详解

### CJK 文本处理 (tracker.go)
```go
// 1. Token 提取正则 (trackerTokenRegex)
//    - 英文 tag: #ABC123 (2-32 char)
//    - 中文短语: 汉字/数字/中文标点组合 (2-16 char)
//    - 纯汉字: 2-12 个

// 2. 别名匹配 (trackerLexiconAlias)
//    case-insensitive + boundary 检测
//    例: "SpaceX" / "space x" / "space_x" 都能匹配

// 3. 启发式过滤
//    - Stop tokens: 人称代词/助词/状态动词/问句 → 过滤
//    - Entity suffixes: 公司/大学/医院/... → 升权
//    - Honorific: 院士/教授/... → 强制 entity
//    - 短词特殊处理: ≤2 汉字 + titleOnly 匹配

// 4. 信息值排序
//    - 首选: 词典 entity (完全精准)
//    - 其次: 启发式 entity (含 suffix 或高频)
//    - 最后: keyword (其他有效 token)
```

### 事件聚类 (EventCluster)
```go
// 条件: 多篇文章共享≥2 个实体 + 时间窗口内

// 算法:
// 1. 为每篇文章提取实体/关键词集合
// 2. 构建 "实体 → 文章列表" 的倒排索引
// 3. 遍历实体,用 Union-Find / 图遍历找连通分量
//    (文章通过共享实体连接)
// 4. 过滤: 分量内 entity count >= 2 才成立
// 5. 聚类: 生成 EventGroupCard
//    ├─ Title: 热度最高文章的标题
//    ├─ Entities: 分量内所有 entity
//    ├─ Keywords: 分量内所有 keyword
//    ├─ Score: 组内总热度
//    ├─ Articles: 热度 top 3
//    └─ Sources: 来源分布

// 时间复杂度: O(n * m) 其中 n=文章数, m=entity/keyword 数/篇
// 空间复杂度: O(n * m)
```

### 热度 Snapshot 计算
```go
// 窗口 N 小时内的真实热度增量:

// 1. baseline snapshot (N 小时前):
//    记录所有相关文章在该时刻的热度总和

// 2. current snapshot (现在):
//    记录所有相关文章的当前热度总和

// 3. score_delta = current - baseline
//    ├─ > 0: 热度上升 (momentum = "up")
//    ├─ = 0: 热度持平 (momentum = "flat")
//    └─ < 0: 热度下降 (momentum = "down")

// 好处:
// - 消除旧文章 off-list 的虚假下降
// - 真实反映窗口内的趋势
// - 与用户选择的窗口对齐
```

---

## 📋 总结

| 层级 | 模块 | 关键文件 | 职责 |
|------|------|--------|------|
| **UI** | 首页 | page.tsx (1114 L) | 话题聚合 + 时间流 |
| | 详情 | tracker/page.tsx (530 L) | 实体时间线 + 订阅 |
| | 工具 | lib/useIdSet.ts | 已读/收藏跨页共享 |
| | 样式 | globals.css + tailwind | Tailwind CSS 4 |
| **API** | 路由 | api/handler.go | HTTP 端点总入口 |
| | 追踪 | api/tracker.go | 话题/事件/时间线 |
| | 订阅 | api/subscriptions.go | 邮件通知 |
| **爬虫** | 源配置 | crawler/source.go | 知乎/百度/微博/B站 |
| | 解析 | crawler/heat.go | 热度数值提取 |
| | 调度 | crawler/runner.go | 定时爬取 |
| **DB** | 存储 | storage/db.go | PostgreSQL 连接 |
| | 模型 | model/article.go | Article / Announcement |
| **配置** | 环境 | config/config.go | 参数读取 |
| | 日志 | logger/logger.go | slog 输出 |

---

**文档生成时间**: 2026-05-20
**扫描广度**: Medium (前端关键业务代码 + 后端核心接口)
**重点关注**: 实体卡片组件设计 + 数据聚合算法
