"use client";

import { useEffect, useState, useCallback, useRef, useMemo } from "react";
import Link from "next/link";

type Article = {
  id: number;
  source_key: string;
  url: string;
  title: string;
  content: string;
  author: string;
  heat: string;
  heat_value: number;
  prev_heat: string;
  prev_heat_value: number;
  published_at: string;
  fetched_at: string;
};

type ListResp = {
  items: Article[];
  limit: number;
  offset: number;
  total: number;
  has_more: boolean;
  next_offset: number;
};

type Announcement = {
  id: number;
  content: string;
  level: "info" | "warn" | "critical" | "quote";
  priority: number;
  starts_at: string;
  ends_at: string | null;
  is_active: boolean;
};

type TrackerTopic = {
  label: string;
  kind: "entity" | "keyword";
  score: number;
  prev_score: number;
  score_delta: number;
  count: number;
  prev_count: number;
  count_delta: number;
  momentum: "up" | "flat" | "down";
  related_terms: string[];
  sources: { source_key: string; count: number }[];
  sample_article?: {
    id: number;
    title: string;
    source_key: string;
    heat: string;
    heat_value: number;
  };
};

type TrackerResp = {
  window: { hours: number };
  items: TrackerTopic[];
};

// 自动刷新间隔:5 分钟
const POLL_INTERVAL_MS = 5 * 60 * 1000;
// 首页每页条数。首屏保持轻量,需要时再继续展开。
const PAGE_SIZE = 30;
// 公告刷新间隔:10 分钟(变更频率远低于文章)
const ANNOUNCEMENT_POLL_MS = 10 * 60 * 1000;
// 搜索框防抖:300ms
const SEARCH_DEBOUNCE_MS = 300;
// localStorage 键:已被用户关闭、不再展示的公告 id 列表
const DISMISSED_KEY = "dismissed_announcements";
// localStorage 键:已读 / 收藏的文章 id
const READ_KEY = "read_ids";
const STARRED_KEY = "starred_ids";

// Tab 特殊 key:热点模式标识,不与 API source_key 冲突。
const TAB_TRACKER = "__tracker__";

// Tab 配置。热点 Tab 放第一位(最显眼),其余为文章源过滤。
const TABS: { key: string; label: string }[] = [
  { key: TAB_TRACKER, label: "🔥 热点" },
  { key: "", label: "全部" },
  { key: "zhihu_hot", label: "知乎" },
  { key: "bilibili_popular", label: "B站" },
];

// 静态导出时不能用 SSR,所以走 client fetch。
// 开发环境由 next.config rewrites 代理到 localhost:8080,
// 生产环境由 Nginx 反代,前端永远访问相对路径 /api/v1/*。
async function fetchArticles(source: string, q: string, limit: number, offset = 0): Promise<ListResp> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  if (source) params.set("source", source);
  if (q) params.set("q", q);
  const res = await fetch(`/api/v1/articles?${params.toString()}`, { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: ListResp = await res.json();
  return {
    ...data,
    items: data.items ?? [],
    total: data.total ?? 0,
    has_more: Boolean(data.has_more),
    next_offset: data.next_offset ?? offset + (data.items?.length ?? 0),
  };
}

async function fetchAnnouncements(): Promise<Announcement[]> {
  const res = await fetch("/api/v1/announcements", { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: { items: Announcement[] } = await res.json();
  return data.items ?? [];
}

async function fetchTrackers(windowHours: number): Promise<TrackerResp> {
  const params = new URLSearchParams({ window: String(windowHours), limit: "8" });
  const res = await fetch(`/api/v1/trackers?${params.toString()}`, { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: TrackerResp = await res.json();
  return { window: data.window, items: data.items ?? [] };
}

function formatTime(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleString("zh-CN", { hour12: false });
  } catch {
    return iso;
  }
}

function formatRelativeTime(date: Date): string {
  const now = new Date();
  const diffSec = Math.floor((now.getTime() - date.getTime()) / 1000);
  if (diffSec < 60) return "刚刚";
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)} 分钟前`;
  return formatTime(date.toISOString());
}

// 把数值热度格式化为中文短文本:1.2 亿 / 1234 万 / 500
// 用于趋势差值显示;主热度优先用源原文(article.heat),源原文为空时回退到 formatHeat(value)。
function formatHeat(v: number): string {
  if (!Number.isFinite(v) || v <= 0) return "";
  if (v >= 1e8) {
    const n = v / 1e8;
    return `${n >= 10 ? n.toFixed(0) : n.toFixed(1)} 亿`;
  }
  if (v >= 1e4) {
    return `${Math.round(v / 1e4)} 万`;
  }
  return String(Math.round(v));
}

function formatSignedHeat(v: number): string {
  if (!Number.isFinite(v) || v === 0) return "0";
  return `${v > 0 ? "+" : "-"}${formatHeat(Math.abs(v))}`;
}

function trackerDetailHref(term: string, windowHours: number): string {
  return `/tracker?term=${encodeURIComponent(term)}&window=${windowHours}`;
}

// HeatBadge 统一热度展示样式:🔥 主热度 + 可选趋势(↑/↓ 差值)。
// 趋势仅在 prev_value > 0 且与当前不同时显示,首次抓取的条目不展示趋势,
// 改为渲染一个脉动的 NEW 徽章(prev_value === 0 视为首次上榜)。
// 不同源的热度语义不一样:知乎是"讨论度",B 站是"播放量",量纲也不在同一级。
// 用不同图标 + tooltip 让用户一眼看出指标类型,避免拿"100 万播放"和"100 万热度"
// 做心理换算。新增源时在此追加一项即可,默认走 fire 配置。
const HEAT_ICONS: Record<string, { icon: string; label: string }> = {
  zhihu_hot: { icon: "🔥", label: "热度" },
  bilibili_popular: { icon: "▶", label: "播放量" },
};

const SOURCE_LABELS: Record<string, string> = {
  zhihu_hot: "知乎",
  bilibili_popular: "B站",
};

function HeatBadge({
  sourceKey,
  heat,
  value,
  prevValue,
}: {
  sourceKey: string;
  heat: string;
  value: number;
  prevValue: number;
}) {
  const main = heat || formatHeat(value);
  if (!main) return null;

  const meta = HEAT_ICONS[sourceKey] ?? { icon: "🔥", label: "热度" };
  const isNew = prevValue === 0 && value > 0;
  // 首次上榜不显示趋势(没有比较基准),改用 NEW 徽章表达。
  const hasTrend = !isNew && prevValue > 0 && value > 0 && value !== prevValue;
  const diff = value - prevValue;
  const up = diff > 0;
  const trendText = hasTrend ? formatHeat(Math.abs(diff)) : "";

  return (
    <>
      {isNew && (
        <span
          className="inline-flex shrink-0 animate-pulse items-center rounded-full bg-red-600 px-1.5 py-0.5 text-[10px] font-bold leading-none text-white shadow-sm"
          title="首次上榜"
        >
          NEW
        </span>
      )}
      <span
        className="inline-flex shrink-0 items-center gap-1 rounded-full bg-red-50 px-2 py-0.5 text-xs font-medium tabular-nums text-red-600 dark:bg-red-950 dark:text-red-400"
        title={meta.label}
      >
        <span aria-hidden="true">{meta.icon}</span>
        <span>{main}</span>
        {hasTrend && (
          <span
            className={
              up
                ? "text-emerald-600 dark:text-emerald-400"
                : "text-zinc-500 dark:text-zinc-400"
            }
            title={`${meta.label}相比上次${up ? "上升" : "下降"} ${trendText}`}
          >
            {up ? "↑" : "↓"}
            {trendText}
          </span>
        )}
      </span>
    </>
  );
}

// useIdSet 把 localStorage 里的 number[] 暴露成 Set,提供 has/add/toggle。
// 已读、收藏共用同一份逻辑,key 不同即可。
function useIdSet(storageKey: string) {
  const [ids, setIds] = useState<Set<number>>(new Set());

  useEffect(() => {
    try {
      const raw = localStorage.getItem(storageKey);
      if (!raw) return;
      const parsed = JSON.parse(raw);
      if (Array.isArray(parsed)) {
        setIds(new Set(parsed.filter((x) => typeof x === "number")));
      }
    } catch {
      // 隐私模式 / 非法数据 → 静默忽略
    }
  }, [storageKey]);

  const persist = useCallback(
    (next: Set<number>) => {
      try {
        localStorage.setItem(storageKey, JSON.stringify([...next]));
      } catch {
        // localStorage 写失败也无视,内存态仍可用
      }
    },
    [storageKey]
  );

  const add = useCallback(
    (id: number) => {
      setIds((prev) => {
        if (prev.has(id)) return prev;
        const next = new Set(prev);
        next.add(id);
        persist(next);
        return next;
      });
    },
    [persist]
  );

  const toggle = useCallback(
    (id: number) => {
      setIds((prev) => {
        const next = new Set(prev);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        persist(next);
        return next;
      });
    },
    [persist]
  );

  return { ids, add, toggle };
}

// useSubscriptions 走后端 /api/v1/subscriptions 管理关键词订阅。
// 替代了原先 localStorage + Web Notification 的本地方案——
// 后端命中后会发邮件,所以手机/关闭浏览器也能收到。
//
// 状态:
//   items     当前订阅的关键词列表(后端返回)
//   notifyTo  邮件发往的邮箱(已模糊化展示)
//   loading   true 表示首次加载未完成,避免界面闪烁"还没订阅"
//   error     接口错误时展示给用户
type Subscription = { id: number; keyword: string; created_at: string };

function useSubscriptions() {
  const [items, setItems] = useState<Subscription[]>([]);
  const [notifyTo, setNotifyTo] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/subscriptions", { cache: "no-store" });
      if (res.status === 503) {
        setError("订阅功能未启用");
        setLoading(false);
        return;
      }
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data: { items: Subscription[]; notify_to: string } = await res.json();
      setItems(data.items ?? []);
      setNotifyTo(data.notify_to ?? "");
      setError(null);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const add = useCallback(
    async (raw: string) => {
      const v = raw.trim();
      if (!v) return;
      try {
        const res = await fetch("/api/v1/subscriptions", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ keyword: v }),
        });
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        await refresh();
      } catch (e) {
        setError(String(e));
      }
    },
    [refresh]
  );

  const remove = useCallback(
    async (id: number) => {
      try {
        const res = await fetch(`/api/v1/subscriptions/${id}`, { method: "DELETE" });
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        await refresh();
      } catch (e) {
        setError(String(e));
      }
    },
    [refresh]
  );

  return { items, notifyTo, loading, error, add, remove };
}

// level → Tailwind class 映射。深色模式自动适配。
// quote 是 crawler 每日轮询发布的名言,用中性偏文艺的紫灰区分于通知类。
const LEVEL_CLASSES: Record<Announcement["level"], string> = {
  info: "border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-300",
  warn: "border-yellow-200 bg-yellow-50 text-yellow-800 dark:border-yellow-800 dark:bg-yellow-950 dark:text-yellow-300",
  critical: "border-red-200 bg-red-50 text-red-800 dark:border-red-800 dark:bg-red-950 dark:text-red-300",
  quote: "border-zinc-200 bg-zinc-50 text-zinc-700 italic dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300",
};

function readDismissed(): number[] {
  // 仅在浏览器调用,SSR 环境下 typeof window 检查由调用方保证(useEffect 内)
  try {
    const raw = localStorage.getItem(DISMISSED_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.filter((x) => typeof x === "number") : [];
  } catch {
    return [];
  }
}

function writeDismissed(ids: number[]): void {
  try {
    localStorage.setItem(DISMISSED_KEY, JSON.stringify(ids));
  } catch {
    // localStorage 可能因隐私模式不可用,静默忽略
  }
}

function AnnouncementBar() {
  const [items, setItems] = useState<Announcement[]>([]);
  const [dismissedIds, setDismissedIds] = useState<number[]>([]);

  // 初次挂载时从 localStorage 同步已读 id(必须在 useEffect 内,避免 SSR/SSG 阶段访问 window)
  useEffect(() => {
    setDismissedIds(readDismissed());
  }, []);

  // 拉取 + 10 分钟轮询
  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const list = await fetchAnnouncements();
        if (!cancelled) setItems(list);
      } catch {
        // 公告失败完全静默,不影响主页面
      }
    };
    load();
    const timer = setInterval(load, ANNOUNCEMENT_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, []);

  const visible = items.filter((a) => !dismissedIds.includes(a.id));
  if (visible.length === 0) return null;

  const dismiss = (id: number) => {
    const next = [...dismissedIds, id];
    setDismissedIds(next);
    writeDismissed(next);
  };

  return (
    <div className="mb-4 space-y-2">
      {visible.map((a) => (
        <div
          key={a.id}
          className={`flex items-start gap-3 rounded-md border px-4 py-3 text-sm ${LEVEL_CLASSES[a.level]}`}
          role="status"
        >
          <div className="min-w-0 flex-1 whitespace-pre-wrap break-words">{a.content}</div>
          <button
            type="button"
            onClick={() => dismiss(a.id)}
            aria-label="关闭公告"
            className="shrink-0 rounded p-0.5 text-current opacity-60 transition hover:opacity-100"
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 20 20"
              className="h-4 w-4"
              fill="currentColor"
              aria-hidden="true"
            >
              <path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z" />
            </svg>
          </button>
        </div>
      ))}
    </div>
  );
}

// TrackerTopicCard 单个热点话题卡片:与文章卡片共享视觉风格。
function TrackerTopicCard({
  topic,
  index,
  windowHours,
  onPickTerm,
}: {
  topic: TrackerTopic;
  index: number;
  windowHours: number;
  onPickTerm: (term: string) => void;
}) {
  const momentumConfig: Record<string, { text: string; icon: string; cls: string }> = {
    up: { text: "升温", icon: "↗", cls: "bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300" },
    down: { text: "回落", icon: "↘", cls: "bg-zinc-100 text-zinc-600 dark:bg-zinc-800 dark:text-zinc-400" },
    flat: { text: "持平", icon: "→", cls: "bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300" },
  };
  const m = momentumConfig[topic.momentum] ?? momentumConfig.flat;
  const sourceText = topic.sources.map((s) => `${SOURCE_LABELS[s.source_key] ?? s.source_key} ${s.count}`).join(" · ");

  return (
    <li className="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm transition hover:shadow-md dark:border-zinc-800 dark:bg-zinc-900">
      <div className="flex gap-3">
        <span className="shrink-0 select-none pt-0.5 font-mono text-sm text-zinc-400 tabular-nums">
          {String(index + 1).padStart(2, "0")}
        </span>
        <div className="min-w-0 flex-1">
          {/* 主行:话题名 + 趋势 + kind + 热度 */}
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <Link
                  href={trackerDetailHref(topic.label, windowHours)}
                  className="text-base font-semibold text-zinc-900 transition hover:text-amber-700 hover:underline dark:text-zinc-100 dark:hover:text-amber-300"
                >
                  {topic.label}
                </Link>
                <span className={`inline-flex items-center gap-0.5 rounded-full px-2 py-0.5 text-[11px] font-medium ${m.cls}`}>
                  {m.icon} {m.text}
                </span>
                <span className="rounded-full bg-zinc-100 px-2 py-0.5 text-[11px] text-zinc-600 dark:bg-zinc-800 dark:text-zinc-400">
                  {topic.kind === "entity" ? "实体" : "话题"}
                </span>
              </div>
              {/* 副标题行 */}
              <p className="mt-1 text-xs text-zinc-500">
                {topic.count} 条相关
                {topic.count_delta !== 0 && (
                  <span className="ml-1">
                    ({topic.count_delta > 0 ? `+${topic.count_delta}` : topic.count_delta})
                  </span>
                )}
                {sourceText && <span className="ml-2">{sourceText}</span>}
              </p>
            </div>
            {/* 右侧聚合热度 */}
            <div className="shrink-0 text-right">
              <div className="text-sm font-semibold tabular-nums text-emerald-600 dark:text-emerald-400">
                {formatHeat(topic.score)}
              </div>
              {topic.score_delta !== 0 && (
                <div className="text-[11px] text-zinc-500">{formatSignedHeat(topic.score_delta)}</div>
              )}
            </div>
          </div>

          {/* Sample article 内嵌预览 */}
          {topic.sample_article && (
            <Link
              href={`/article?id=${topic.sample_article.id}`}
              className="mt-3 block rounded-md border border-zinc-100 bg-zinc-50 px-3 py-2 text-xs transition hover:border-zinc-300 hover:bg-white dark:border-zinc-800 dark:bg-zinc-950 dark:hover:border-zinc-700 dark:hover:bg-zinc-900"
            >
              <div className="line-clamp-1 font-medium text-zinc-700 dark:text-zinc-300">{topic.sample_article.title}</div>
              <div className="mt-1 flex items-center gap-2 text-[11px] text-zinc-500">
                <span>{SOURCE_LABELS[topic.sample_article.source_key] ?? topic.sample_article.source_key}</span>
                {(topic.sample_article.heat || topic.sample_article.heat_value > 0) && (
                  <span>{topic.sample_article.heat || formatHeat(topic.sample_article.heat_value)}</span>
                )}
              </div>
            </Link>
          )}

          {/* 底部:关联词 + 时间线链接 */}
          {(topic.related_terms.length > 0 || true) && (
            <div className="mt-3 flex flex-wrap items-center gap-1.5 text-[11px]">
              {topic.related_terms.map((term) => (
                <button
                  key={term}
                  type="button"
                  onClick={() => onPickTerm(term)}
                  className="rounded-full bg-amber-50 px-2 py-0.5 text-amber-700 transition hover:bg-amber-100 dark:bg-amber-950 dark:text-amber-300 dark:hover:bg-amber-900"
                >
                  {term}
                </button>
              ))}
              <Link
                href={trackerDetailHref(topic.label, windowHours)}
                className="ml-auto rounded-full bg-zinc-100 px-2 py-0.5 text-zinc-600 transition hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-300 dark:hover:bg-zinc-700"
              >
                查看时间线 →
              </Link>
            </div>
          )}
        </div>
      </div>
    </li>
  );
}

export default function Home() {
  const [articles, setArticles] = useState<Article[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [, setTick] = useState(0); // 用于触发 formatRelativeTime 重渲染
  const initialLoad = useRef(true);

  // 当前选中的 Tab(source key 或 TAB_TRACKER)+ 搜索词。
  const [source, setSource] = useState("");
  const [query, setQuery] = useState("");
  const [debouncedQ, setDebouncedQ] = useState("");

  // 热点追踪模式 state
  const isTrackerMode = source === TAB_TRACKER;
  const [trackerItems, setTrackerItems] = useState<TrackerTopic[]>([]);
  const [trackerWindow, setTrackerWindow] = useState(24);
  const [trackerActualWindow, setTrackerActualWindow] = useState(24);
  const [trackerLoading, setTrackerLoading] = useState(false);

  // 已读 / 收藏 id(localStorage 持久化)
  const read = useIdSet(READ_KEY);
  const starred = useIdSet(STARRED_KEY);

  // 关键词订阅:走后端 API,命中后服务端发邮件(替代了原先的桌面通知)。
  const subs = useSubscriptions();
  const [keywordInput, setKeywordInput] = useState("");
  const [subOpen, setSubOpen] = useState(false);

  // 全局 toast(短暂提示,如"分享链接已复制")
  const [toast, setToast] = useState<string | null>(null);
  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(null), 2000);
    return () => clearTimeout(timer);
  }, [toast]);

  const resetListing = useCallback(() => {
    initialLoad.current = true;
    setLoading(true);
    setLoadingMore(false);
    setError(null);
    setArticles([]);
    setTotal(0);
    setHasMore(false);
    setLastUpdated(null);
    setPage(1);
  }, []);

  const copyShareLink = useCallback(async (id: number) => {
    const url = `${window.location.origin}/share/${id}`;
    try {
      await navigator.clipboard.writeText(url);
      setToast("分享链接已复制");
    } catch {
      // 降级:某些环境(非 HTTPS)没有 clipboard API
      setToast(url);
    }
  }, []);

  const handleTrackerPick = useCallback(
    (term: string) => {
      resetListing();
      setSource("");           // 切回"全部"Tab
      setQuery(term);
      setDebouncedQ(term);
    },
    [resetListing]
  );

  // 搜索框防抖
  useEffect(() => {
    const timer = setTimeout(() => {
      const next = query.trim();
      if (next === debouncedQ) return;
      resetListing();
      setDebouncedQ(next);
    }, SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [query, debouncedQ, resetListing]);

  const refresh = useCallback(async () => {
    try {
      const data = await fetchArticles(source, debouncedQ, page * PAGE_SIZE);
      setArticles(data.items);
      setTotal(data.total);
      setHasMore(data.has_more);
      setLastUpdated(new Date());
      setError(null);
    } catch (e) {
      // 仅在首次加载时展示错误,后续静默失败保留旧数据
      if (initialLoad.current) {
        setError(String(e));
      }
    } finally {
      if (initialLoad.current) {
        setLoading(false);
        initialLoad.current = false;
      }
      setLoadingMore(false);
    }
  }, [source, debouncedQ, page]);

  useEffect(() => {
    if (isTrackerMode) return; // 热点模式下不拉文章
    refresh();
    const timer = setInterval(refresh, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [refresh, isTrackerMode]);

  // 热点追踪数据:仅在热点 Tab 激活时 fetch + 轮询
  useEffect(() => {
    if (!isTrackerMode) return;
    let cancelled = false;
    const load = async () => {
      setTrackerLoading(true);
      try {
        const data = await fetchTrackers(trackerWindow);
        if (cancelled) return;
        setTrackerItems(data.items);
        setTrackerActualWindow(data.window.hours);
      } catch {
        // 静默
      } finally {
        if (!cancelled) setTrackerLoading(false);
      }
    };
    load();
    const timer = setInterval(load, POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [isTrackerMode, trackerWindow]);

  // 每 30 秒更新一次"最后更新"的相对时间显示
  useEffect(() => {
    const timer = setInterval(() => setTick((t) => t + 1), 30_000);
    return () => clearInterval(timer);
  }, []);

  // 同源 Top 10 排名映射:每个 source_key 内部按 heat_value 降序前 10 名。
  // 用 Map<id, rank> 让卡片渲染时 O(1) 判断并获取排名序号。每次 articles 变才重算。
  const topIdsBySource = useMemo(() => {
    const bySource = new Map<string, Article[]>();
    for (const a of articles) {
      const list = bySource.get(a.source_key) ?? [];
      list.push(a);
      bySource.set(a.source_key, list);
    }
    const top = new Map<number, number>();
    for (const list of bySource.values()) {
      list
        .filter((a) => a.heat_value > 0)
        .sort((x, y) => y.heat_value - x.heat_value)
        .slice(0, 10)
        .forEach((a, idx) => top.set(a.id, idx + 1));
    }
    return top;
  }, [articles]);

  // 热点 Tab 下的本地搜索过滤(按话题名 + 关联词)
  const filteredTrackerItems = useMemo(() => {
    if (!debouncedQ) return trackerItems;
    const q = debouncedQ.toLowerCase();
    return trackerItems.filter(
      (t) =>
        t.label.toLowerCase().includes(q) ||
        t.related_terms.some((r) => r.toLowerCase().includes(q))
    );
  }, [trackerItems, debouncedQ]);

  // 飙升判定:命中以下任一即认为飙升。
  //  1) 相比上一次抓取(30 分钟前) heat 增幅 >= 10%
  //  2) 首次上榜(prev_heat_value === 0 且当前 heat_value > 0)
  //     **且**当前热度进入该源 top10 —— 否则只是冷门内容刚被抓到,不是真"飙升"
  // NEW 徽章和 🚀 图标会重叠显示(语义独立:NEW 强调"刚出现",🚀 强调"涨势"),
  // 视觉上能让用户一眼分清"老朋友突然涨"与"新面孔进 top10"。
  function isSurging(a: Article, topIds: Map<number, number>): boolean {
    if (a.heat_value <= 0) return false;
    if (a.prev_heat_value <= 0) return topIds.has(a.id); // 首次上榜:必须在 top10
    return (a.heat_value - a.prev_heat_value) / a.prev_heat_value >= 0.1;
  }

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-8">
      <AnnouncementBar />
      <header className="mb-6 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Newsfeed</h1>
        <div className="flex items-baseline gap-3 text-sm text-zinc-500">
          {!isTrackerMode && lastUpdated && (
            <span title={lastUpdated.toLocaleString("zh-CN", { hour12: false })}>
              {formatRelativeTime(lastUpdated)}更新
            </span>
          )}
          <span>
            {isTrackerMode
              ? trackerLoading
                ? "加载中…"
                : `${filteredTrackerItems.length} 个热点话题`
              : loading
                ? "加载中…"
                : total > articles.length
                  ? `已显示 ${articles.length} / ${total} 条`
                  : `共 ${total} 条`}
          </span>
        </div>
      </header>

      {/* Tab + 搜索框 */}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="flex gap-1 rounded-md border border-zinc-200 bg-white p-0.5 dark:border-zinc-800 dark:bg-zinc-900">
          {TABS.map((tab) => {
            const active = source === tab.key;
            return (
              <button
                key={tab.key || "all"}
                type="button"
                onClick={() => {
                  if (source === tab.key) return;
                  resetListing();
                  setSource(tab.key);
                }}
                className={
                  "rounded px-3 py-1 text-sm transition " +
                  (active
                    ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900"
                    : "text-zinc-600 hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800")
                }
              >
                {tab.label}
              </button>
            );
          })}
        </div>

        <input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={isTrackerMode ? "搜索热点话题…" : "搜索标题或内容…"}
          aria-label="搜索"
          className="ml-auto min-w-0 flex-1 rounded-md border border-zinc-200 bg-white px-3 py-1 text-sm outline-none placeholder:text-zinc-400 focus:border-zinc-400 dark:border-zinc-800 dark:bg-zinc-900 dark:focus:border-zinc-600"
        />
      </div>

      {isTrackerMode ? (
        /* ========== 热点追踪内容 ========== */
        <div>
          {/* 时间窗口切换 */}
          <div className="mb-4 flex items-center justify-between">
            <p className="text-xs text-zinc-500">
              基于最近 {trackerActualWindow} 小时内容自动聚合
            </p>
            <div className="flex items-center gap-1">
              {[6, 24, 72].map((w) => (
                <button
                  key={w}
                  type="button"
                  onClick={() => setTrackerWindow(w)}
                  className={
                    "rounded-full px-2.5 py-0.5 text-[11px] font-medium transition " +
                    (trackerWindow === w
                      ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900"
                      : "bg-zinc-100 text-zinc-500 hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-400 dark:hover:bg-zinc-700")
                  }
                >
                  {w}h
                </button>
              ))}
            </div>
          </div>

          {trackerLoading && filteredTrackerItems.length === 0 && (
            <div className="py-12 text-center text-sm text-zinc-500">加载热点话题…</div>
          )}

          {!trackerLoading && filteredTrackerItems.length === 0 && (
            <div className="rounded-md border border-dashed border-zinc-300 p-8 text-center text-sm text-zinc-500 dark:border-zinc-700">
              {debouncedQ ? "没有匹配的热点话题" : "暂无热点话题数据"}
            </div>
          )}

          <ul className="space-y-3">
            {filteredTrackerItems.map((topic, i) => (
              <TrackerTopicCard
                key={`${topic.kind}:${topic.label}`}
                topic={topic}
                index={i}
                windowHours={trackerWindow}
                onPickTerm={handleTrackerPick}
              />
            ))}
          </ul>
        </div>
      ) : (
        /* ========== 文章列表内容 ========== */
        <>
          {/* 邮件订阅区(默认折叠) */}
          <div className="mb-4 text-sm">
            <button
              type="button"
              onClick={() => setSubOpen((v) => !v)}
              className="text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
            >
              📧 邮件订阅
              {subs.items.length > 0 && (
                <span className="ml-1 text-xs text-zinc-400">({subs.items.length})</span>
              )}
              <span className="ml-1 text-xs">{subOpen ? "▴" : "▾"}</span>
            </button>
            {subOpen && (
              <div className="mt-2 rounded-md border border-zinc-200 bg-zinc-50 p-3 dark:border-zinc-800 dark:bg-zinc-900">
                {subs.error && (
                  <div className="mb-2 rounded bg-red-50 px-2 py-1 text-xs text-red-700 dark:bg-red-950 dark:text-red-300">
                    {subs.error}
                  </div>
                )}
                <div className="mb-2 flex flex-wrap gap-2">
                  {subs.loading ? (
                    <span className="text-xs text-zinc-500">加载中…</span>
                  ) : subs.items.length === 0 ? (
                    <span className="text-xs text-zinc-500">还没有订阅关键词</span>
                  ) : (
                    subs.items.map((s) => (
                      <span
                        key={s.id}
                        className="inline-flex items-center gap-1 rounded-full bg-zinc-200 px-2 py-0.5 text-xs text-zinc-700 dark:bg-zinc-700 dark:text-zinc-200"
                      >
                        {s.keyword}
                        <button
                          type="button"
                          onClick={() => subs.remove(s.id)}
                          aria-label={`移除 ${s.keyword}`}
                          className="text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
                        >
                          ×
                        </button>
                      </span>
                    ))
                  )}
                </div>
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    if (!keywordInput.trim()) return;
                    subs.add(keywordInput);
                    setKeywordInput("");
                  }}
                  className="flex gap-2"
                >
                  <input
                    type="text"
                    value={keywordInput}
                    onChange={(e) => setKeywordInput(e.target.value)}
                    placeholder="输入关键词后回车,如 AI / 裁员"
                    className="min-w-0 flex-1 rounded-md border border-zinc-300 bg-white px-2 py-1 text-xs outline-none focus:border-zinc-500 dark:border-zinc-700 dark:bg-zinc-800"
                  />
                  <button
                    type="submit"
                    className="rounded-md bg-zinc-900 px-3 py-1 text-xs text-white hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900"
                  >
                    添加
                  </button>
                </form>
                <div className="mt-3 text-xs text-zinc-500">
                  {subs.notifyTo
                    ? <>命中后会发邮件到 <span className="font-mono">{subs.notifyTo}</span>,延迟约等于抓取间隔(30 分钟内)</>
                    : "未配置收件邮箱(SMTP_HOST / DIGEST_TO)。后端命中仍会登记去重,配上之后从下次开始发送。"}
                </div>
              </div>
            )}
          </div>

          {error && (
            <div className="mb-6 rounded-md border border-red-300 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-300">
              加载失败: {error}
              <div className="mt-1 text-xs opacity-70">
                请确认 Go API 服务正在 :8080 运行(make run-api)
              </div>
            </div>
          )}

          {!loading && articles.length === 0 && !error && (
            <div className="rounded-md border border-dashed border-zinc-300 p-8 text-center text-sm text-zinc-500 dark:border-zinc-700">
              没有匹配的内容
            </div>
          )}

          <ul className="space-y-3">
            {articles.map((a, i) => {
              const isRead = read.ids.has(a.id);
              const isStarred = starred.ids.has(a.id);
              return (
                <li
                  key={a.id}
                  className={
                    "relative rounded-lg border bg-white p-4 shadow-sm transition hover:shadow-md dark:bg-zinc-900 " +
                    (isStarred
                      ? "border-zinc-200 border-l-4 border-l-amber-400 dark:border-zinc-800 dark:border-l-amber-500"
                      : "border-zinc-200 dark:border-zinc-800") +
                    (isRead ? " opacity-60" : "")
                  }
                >
                  {/* 收藏按钮 */}
                  <button
                    type="button"
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      starred.toggle(a.id);
                    }}
                    aria-label={isStarred ? "取消收藏" : "收藏"}
                    aria-pressed={isStarred}
                    className={
                      "absolute right-3 top-3 rounded p-1 text-base transition " +
                      (isStarred
                        ? "text-amber-500"
                        : "text-zinc-300 hover:text-amber-500 dark:text-zinc-600")
                    }
                  >
                    {isStarred ? "★" : "☆"}
                  </button>
                  {/* 分享按钮 */}
                  <button
                    type="button"
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      copyShareLink(a.id);
                    }}
                    aria-label="复制分享链接"
                    className="absolute right-9 top-3 rounded p-1 text-sm text-zinc-300 transition hover:text-zinc-700 dark:text-zinc-600 dark:hover:text-zinc-200"
                  >
                    ↗
                  </button>
                  <Link
                    href={`/article?id=${a.id}`}
                    onClick={() => read.add(a.id)}
                    className="flex gap-3"
                  >
                    <span className="shrink-0 select-none font-mono text-sm text-zinc-400 tabular-nums">
                      {String(i + 1).padStart(2, "0")}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 pr-16">
                        <h2
                          className={
                            "text-base font-medium leading-snug hover:underline " +
                            (isRead
                              ? "text-zinc-500 dark:text-zinc-500"
                              : "text-zinc-900 dark:text-zinc-100")
                          }
                        >
                          {a.title}
                        </h2>
                        {(a.heat || a.heat_value > 0) && (
                          <HeatBadge
                            sourceKey={a.source_key}
                            heat={a.heat}
                            value={a.heat_value}
                            prevValue={a.prev_heat_value}
                          />
                        )}
                        {topIdsBySource.has(a.id) && (
                          <span
                            className="inline-flex shrink-0 items-center gap-0.5 rounded-full bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-400"
                            title={`同源热度 / 播放量 Top ${topIdsBySource.get(a.id)}`}
                          >
                            🏆<span className="font-semibold tabular-nums">{topIdsBySource.get(a.id)}</span>
                          </span>
                        )}
                        {isSurging(a, topIdsBySource) && (
                          <span
                            className="inline-flex shrink-0 items-center rounded-full bg-orange-100 px-1.5 py-0.5 text-xs font-medium text-orange-700 dark:bg-orange-950 dark:text-orange-400"
                            title="近 30 分钟热度飙升"
                          >
                            🚀
                          </span>
                        )}
                      </div>
                      {a.content && (
                        <p className="mt-1.5 line-clamp-4 text-[13px] leading-relaxed text-zinc-600 dark:text-zinc-400">
                          {a.content}
                        </p>
                      )}
                      <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-500">
                        <span>{a.source_key}</span>
                        <span>{formatTime(a.published_at)}</span>
                      </div>
                    </div>
                  </Link>
                </li>
              );
            })}
          </ul>

          {hasMore && !error && (
            <div className="mt-6 flex justify-center">
              <button
                type="button"
                onClick={() => {
                  setLoadingMore(true);
                  setPage((prev) => prev + 1);
                }}
                disabled={loadingMore || loading}
                className="rounded-md border border-zinc-300 bg-white px-4 py-2 text-sm text-zinc-700 transition hover:border-zinc-400 hover:text-zinc-900 disabled:cursor-not-allowed disabled:opacity-60 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200 dark:hover:border-zinc-500 dark:hover:text-zinc-100"
              >
                {loadingMore ? "加载中…" : `加载更多 (${articles.length}/${total})`}
              </button>
            </div>
          )}
        </>
      )}

      {toast && (
        <div
          role="status"
          className="fixed bottom-6 left-1/2 z-50 -translate-x-1/2 rounded-md bg-zinc-900 px-4 py-2 text-sm text-white shadow-lg dark:bg-zinc-100 dark:text-zinc-900"
        >
          {toast}
        </div>
      )}
    </main>
  );
}
