"use client";

import { useEffect, useState, useCallback, useRef, useMemo } from "react";
import Link from "next/link";

// ======================== Types ========================

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

// ======================== Constants ========================

const POLL_INTERVAL_MS = 5 * 60 * 1000;
const PAGE_SIZE = 50;
const ANNOUNCEMENT_POLL_MS = 10 * 60 * 1000;
const SEARCH_DEBOUNCE_MS = 300;
const DISMISSED_KEY = "dismissed_announcements";

const SOURCE_LABELS: Record<string, string> = {
  zhihu_hot: "知乎",
  bilibili_popular: "B站",
};

const SOURCE_FILTERS: { key: string; label: string }[] = [
  { key: "", label: "全部" },
  { key: "zhihu_hot", label: "知乎" },
  { key: "bilibili_popular", label: "B站" },
];

// ======================== API ========================

async function fetchArticles(source: string, q: string, limit: number, offset = 0): Promise<ListResp> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  if (source) params.set("source", source);
  if (q) params.set("q", q);
  const res = await fetch(`/api/v1/articles?${params.toString()}`, { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: ListResp = await res.json();
  return { ...data, items: data.items ?? [], total: data.total ?? 0, has_more: Boolean(data.has_more), next_offset: data.next_offset ?? offset + (data.items?.length ?? 0) };
}

async function fetchAnnouncements(): Promise<Announcement[]> {
  const res = await fetch("/api/v1/announcements", { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: { items: Announcement[] } = await res.json();
  return data.items ?? [];
}

async function fetchTrackers(windowHours: number): Promise<TrackerResp> {
  const params = new URLSearchParams({ window: String(windowHours), limit: "12" });
  const res = await fetch(`/api/v1/trackers?${params.toString()}`, { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: TrackerResp = await res.json();
  return { window: data.window, items: data.items ?? [] };
}

// ======================== Formatters ========================

function formatHeat(v: number): string {
  if (!Number.isFinite(v) || v <= 0) return "";
  if (v >= 1e8) {
    const n = v / 1e8;
    return `${n >= 10 ? n.toFixed(0) : n.toFixed(1)}亿`;
  }
  if (v >= 1e4) return `${Math.round(v / 1e4)}万`;
  return String(Math.round(v));
}

function formatRelativeTime(iso: string): string {
  try {
    const d = new Date(iso);
    const now = new Date();
    const diffSec = Math.floor((now.getTime() - d.getTime()) / 1000);
    if (diffSec < 60) return "刚刚";
    if (diffSec < 3600) return `${Math.floor(diffSec / 60)}分钟前`;
    if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}小时前`;
    return `${Math.floor(diffSec / 86400)}天前`;
  } catch {
    return "";
  }
}

// ======================== Announcement Bar ========================

const LEVEL_CLASSES: Record<Announcement["level"], string> = {
  info: "border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-300",
  warn: "border-yellow-200 bg-yellow-50 text-yellow-800 dark:border-yellow-800 dark:bg-yellow-950 dark:text-yellow-300",
  critical: "border-red-200 bg-red-50 text-red-800 dark:border-red-800 dark:bg-red-950 dark:text-red-300",
  quote: "border-zinc-200 bg-zinc-50 text-zinc-700 italic dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300",
};

function AnnouncementBar() {
  const [items, setItems] = useState<Announcement[]>([]);
  const [dismissedIds, setDismissedIds] = useState<number[]>([]);

  useEffect(() => {
    try {
      const raw = localStorage.getItem(DISMISSED_KEY);
      if (raw) {
        const parsed = JSON.parse(raw);
        if (Array.isArray(parsed)) setDismissedIds(parsed.filter((x) => typeof x === "number"));
      }
    } catch { /* ignore */ }
  }, []);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const list = await fetchAnnouncements();
        if (!cancelled) setItems(list);
      } catch { /* silent */ }
    };
    load();
    const timer = setInterval(load, ANNOUNCEMENT_POLL_MS);
    return () => { cancelled = true; clearInterval(timer); };
  }, []);

  const visible = items.filter((a) => !dismissedIds.includes(a.id));
  if (visible.length === 0) return null;

  const dismiss = (id: number) => {
    const next = [...dismissedIds, id];
    setDismissedIds(next);
    try { localStorage.setItem(DISMISSED_KEY, JSON.stringify(next)); } catch { /* ignore */ }
  };

  return (
    <div className="mb-4 space-y-2">
      {visible.map((a) => (
        <div key={a.id} className={`flex items-start gap-3 rounded-md border px-4 py-3 text-sm ${LEVEL_CLASSES[a.level]}`}>
          <div className="min-w-0 flex-1 whitespace-pre-wrap break-words">{a.content}</div>
          <button type="button" onClick={() => dismiss(a.id)} aria-label="关闭" className="shrink-0 opacity-60 hover:opacity-100">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" className="h-4 w-4" fill="currentColor"><path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z" /></svg>
          </button>
        </div>
      ))}
    </div>
  );
}

// ======================== Topic Group ========================

// 每个话题组:话题头 + 折叠的相关文章列表
function TopicGroup({
  topic,
  articles,
  windowHours,
  onSearch,
}: {
  topic: TrackerTopic;
  articles: Article[];
  windowHours: number;
  onSearch: (q: string) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const displayArticles = expanded ? articles : articles.slice(0, 3);
  const hasMore = articles.length > 3;

  const momentumCfg: Record<string, { text: string; icon: string; cls: string }> = {
    up: { text: "升温", icon: "↗", cls: "text-emerald-600 dark:text-emerald-400" },
    down: { text: "回落", icon: "↘", cls: "text-zinc-500 dark:text-zinc-400" },
    flat: { text: "持平", icon: "→", cls: "text-blue-600 dark:text-blue-400" },
  };
  const m = momentumCfg[topic.momentum] ?? momentumCfg.flat;

  return (
    <section className="rounded-lg border border-zinc-200 bg-white shadow-sm dark:border-zinc-800 dark:bg-zinc-900">
      {/* 话题头 */}
      <div className="flex items-center justify-between gap-3 px-4 py-3">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Link
            href={`/tracker?term=${encodeURIComponent(topic.label)}&window=${windowHours}`}
            className="text-[15px] font-semibold text-zinc-900 hover:text-amber-700 dark:text-zinc-100 dark:hover:text-amber-300"
          >
            {topic.label}
          </Link>
          <span className={`text-xs font-medium ${m.cls}`}>{m.icon} {m.text}</span>
          {topic.count > articles.length && (
            <span className="text-xs text-zinc-400">{topic.count}条相关</span>
          )}
        </div>
        <span className="shrink-0 text-sm font-medium tabular-nums text-emerald-600 dark:text-emerald-400">
          {formatHeat(topic.score)}
        </span>
      </div>

      {/* 文章列表(紧凑) */}
      <div className="border-t border-zinc-100 dark:border-zinc-800">
        {displayArticles.map((a) => (
          <Link
            key={a.id}
            href={`/article?id=${a.id}`}
            className="flex items-center gap-3 border-b border-zinc-50 px-4 py-2.5 transition last:border-b-0 hover:bg-zinc-50 dark:border-zinc-800/50 dark:hover:bg-zinc-800/50"
          >
            <span className="min-w-0 flex-1 truncate text-sm text-zinc-800 dark:text-zinc-200">
              {a.title}
            </span>
            <span className="shrink-0 text-[11px] text-zinc-400">
              {SOURCE_LABELS[a.source_key] ?? a.source_key}
            </span>
            {(a.heat || a.heat_value > 0) && (
              <span className="shrink-0 text-[11px] font-medium tabular-nums text-red-500 dark:text-red-400">
                {a.heat || formatHeat(a.heat_value)}
              </span>
            )}
          </Link>
        ))}
      </div>

      {/* 展开/收起 + 关联词 */}
      <div className="flex flex-wrap items-center gap-2 border-t border-zinc-100 px-4 py-2 dark:border-zinc-800">
        {hasMore && (
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="text-xs text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
          >
            {expanded ? "收起" : `展开更多(+${articles.length - 3}条)`}
          </button>
        )}
        {topic.related_terms.slice(0, 3).map((term) => (
          <button
            key={term}
            type="button"
            onClick={() => onSearch(term)}
            className="rounded-full bg-zinc-100 px-2 py-0.5 text-[11px] text-zinc-600 transition hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-400 dark:hover:bg-zinc-700"
          >
            {term}
          </button>
        ))}
        <Link
          href={`/tracker?term=${encodeURIComponent(topic.label)}&window=${windowHours}`}
          className="ml-auto text-[11px] text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200"
        >
          时间线 →
        </Link>
      </div>
    </section>
  );
}

// ======================== Compact Article Row ========================

function ArticleRow({ article, index }: { article: Article; index: number }) {
  return (
    <Link
      href={`/article?id=${article.id}`}
      className="flex items-center gap-3 rounded-md px-3 py-2 transition hover:bg-zinc-50 dark:hover:bg-zinc-800/50"
    >
      <span className="shrink-0 font-mono text-[11px] text-zinc-300 tabular-nums dark:text-zinc-600">
        {String(index + 1).padStart(2, "0")}
      </span>
      <span className="min-w-0 flex-1 truncate text-sm text-zinc-800 dark:text-zinc-200">
        {article.title}
      </span>
      <span className="shrink-0 text-[11px] text-zinc-400">
        {SOURCE_LABELS[article.source_key] ?? article.source_key}
      </span>
      <span className="shrink-0 text-[11px] text-zinc-400">
        {formatRelativeTime(article.published_at)}
      </span>
      {(article.heat || article.heat_value > 0) && (
        <span className="shrink-0 text-[11px] font-medium tabular-nums text-red-500 dark:text-red-400">
          {article.heat || formatHeat(article.heat_value)}
        </span>
      )}
    </Link>
  );
}

// ======================== Main Page ========================

export default function Home() {
  const [articles, setArticles] = useState<Article[]>([]);
  const [total, setTotal] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const initialLoad = useRef(true);

  const [source, setSource] = useState("");
  const [query, setQuery] = useState("");
  const [debouncedQ, setDebouncedQ] = useState("");

  // 话题追踪
  const [topics, setTopics] = useState<TrackerTopic[]>([]);
  const [trackerWindow, setTrackerWindow] = useState(24);

  // 搜索防抖
  useEffect(() => {
    const timer = setTimeout(() => {
      const next = query.trim();
      if (next !== debouncedQ) {
        initialLoad.current = true;
        setLoading(true);
        setArticles([]);
        setTotal(0);
        setHasMore(false);
        setPage(1);
        setDebouncedQ(next);
      }
    }, SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [query, debouncedQ]);

  // 拉取文章
  const refresh = useCallback(async () => {
    try {
      const data = await fetchArticles(source, debouncedQ, page * PAGE_SIZE);
      setArticles(data.items);
      setTotal(data.total);
      setHasMore(data.has_more);
      setError(null);
    } catch (e) {
      if (initialLoad.current) setError(String(e));
    } finally {
      if (initialLoad.current) { setLoading(false); initialLoad.current = false; }
      setLoadingMore(false);
    }
  }, [source, debouncedQ, page]);

  useEffect(() => {
    refresh();
    const timer = setInterval(refresh, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [refresh]);

  // 拉取话题(仅非搜索模式)
  useEffect(() => {
    if (debouncedQ) { setTopics([]); return; }
    let cancelled = false;
    const load = async () => {
      try {
        const data = await fetchTrackers(trackerWindow);
        if (!cancelled) setTopics(data.items);
      } catch { /* silent */ }
    };
    load();
    const timer = setInterval(load, POLL_INTERVAL_MS);
    return () => { cancelled = true; clearInterval(timer); };
  }, [trackerWindow, debouncedQ]);

  // 把文章按话题分组
  const { grouped, ungrouped } = useMemo(() => {
    if (topics.length === 0 || debouncedQ) {
      return { grouped: [] as { topic: TrackerTopic; articles: Article[] }[], ungrouped: articles };
    }

    const usedIds = new Set<number>();
    const grouped: { topic: TrackerTopic; articles: Article[] }[] = [];

    for (const topic of topics) {
      const label = topic.label.toLowerCase();
      const matched = articles.filter((a) => {
        if (usedIds.has(a.id)) return false;
        const text = (a.title + " " + a.content).toLowerCase();
        return text.includes(label);
      });
      if (matched.length === 0) continue;
      matched.forEach((a) => usedIds.add(a.id));
      grouped.push({ topic, articles: matched });
    }

    const ungrouped = articles.filter((a) => !usedIds.has(a.id));
    return { grouped, ungrouped };
  }, [articles, topics, debouncedQ]);

  const handleSourceChange = (key: string) => {
    if (source === key) return;
    initialLoad.current = true;
    setLoading(true);
    setArticles([]);
    setTotal(0);
    setHasMore(false);
    setPage(1);
    setSource(key);
  };

  const handleSearch = (term: string) => {
    setQuery(term);
  };

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-8">
      <AnnouncementBar />

      {/* Header */}
      <header className="mb-5 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Newsfeed</h1>
        <span className="text-sm text-zinc-500">
          {loading ? "加载中…" : `共 ${total} 条`}
        </span>
      </header>

      {/* 筛选栏:来源 + 搜索 */}
      <div className="mb-5 flex flex-wrap items-center gap-2">
        <div className="flex gap-1 rounded-md border border-zinc-200 bg-white p-0.5 dark:border-zinc-800 dark:bg-zinc-900">
          {SOURCE_FILTERS.map((s) => (
            <button
              key={s.key || "all"}
              type="button"
              onClick={() => handleSourceChange(s.key)}
              className={
                "rounded px-3 py-1 text-sm transition " +
                (source === s.key
                  ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900"
                  : "text-zinc-600 hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800")
              }
            >
              {s.label}
            </button>
          ))}
        </div>
        <input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="搜索…"
          className="ml-auto min-w-0 flex-1 rounded-md border border-zinc-200 bg-white px-3 py-1 text-sm outline-none placeholder:text-zinc-400 focus:border-zinc-400 dark:border-zinc-800 dark:bg-zinc-900 dark:focus:border-zinc-600"
        />
        {/* 时间窗口(话题聚合维度) */}
        {!debouncedQ && (
          <div className="flex gap-0.5">
            {[6, 24, 72].map((w) => (
              <button
                key={w}
                type="button"
                onClick={() => setTrackerWindow(w)}
                className={
                  "rounded-full px-2 py-0.5 text-[11px] transition " +
                  (trackerWindow === w
                    ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900"
                    : "text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200")
                }
              >
                {w}h
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Error */}
      {error && (
        <div className="mb-5 rounded-md border border-red-300 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-300">
          加载失败: {error}
        </div>
      )}

      {/* 主内容区 */}
      {!loading && articles.length === 0 && !error && (
        <div className="rounded-md border border-dashed border-zinc-300 p-8 text-center text-sm text-zinc-500 dark:border-zinc-700">
          没有匹配的内容
        </div>
      )}

      {/* 话题聚合区 */}
      {grouped.length > 0 && (
        <div className="mb-6 space-y-4">
          {grouped.map(({ topic, articles: topicArticles }) => (
            <TopicGroup
              key={`${topic.kind}:${topic.label}`}
              topic={topic}
              articles={topicArticles}
              windowHours={trackerWindow}
              onSearch={handleSearch}
            />
          ))}
        </div>
      )}

      {/* 未归入话题的散装文章 */}
      {ungrouped.length > 0 && (
        <div>
          {grouped.length > 0 && (
            <div className="mb-3 flex items-center gap-3">
              <div className="h-px flex-1 bg-zinc-200 dark:bg-zinc-800" />
              <span className="text-xs text-zinc-400">其他热门</span>
              <div className="h-px flex-1 bg-zinc-200 dark:bg-zinc-800" />
            </div>
          )}
          <div className="rounded-lg border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
            {ungrouped.map((a, i) => (
              <ArticleRow key={a.id} article={a} index={i} />
            ))}
          </div>
        </div>
      )}

      {/* 加载更多 */}
      {hasMore && !error && (
        <div className="mt-6 flex justify-center">
          <button
            type="button"
            onClick={() => { setLoadingMore(true); setPage((p) => p + 1); }}
            disabled={loadingMore || loading}
            className="rounded-md border border-zinc-300 bg-white px-4 py-2 text-sm text-zinc-700 transition hover:border-zinc-400 disabled:opacity-60 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200"
          >
            {loadingMore ? "加载中…" : `加载更多 (${articles.length}/${total})`}
          </button>
        </div>
      )}
    </main>
  );
}
