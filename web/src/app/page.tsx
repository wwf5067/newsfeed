"use client";

import { useEffect, useState, useCallback, useRef } from "react";

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

// 自动刷新间隔:5 分钟
const POLL_INTERVAL_MS = 5 * 60 * 1000;
// 公告刷新间隔:10 分钟(变更频率远低于文章)
const ANNOUNCEMENT_POLL_MS = 10 * 60 * 1000;
// 搜索框防抖:300ms
const SEARCH_DEBOUNCE_MS = 300;
// localStorage 键:已被用户关闭、不再展示的公告 id 列表
const DISMISSED_KEY = "dismissed_announcements";
// localStorage 键:已读 / 收藏的文章 id
const READ_KEY = "read_ids";
const STARRED_KEY = "starred_ids";

// 源 tab 配置。新增源时在此追加一项。
const SOURCES: { key: string; label: string }[] = [
  { key: "", label: "全部" },
  { key: "zhihu_hot", label: "知乎" },
  { key: "bilibili_popular", label: "B站" },
];

// 静态导出时不能用 SSR,所以走 client fetch。
// 开发环境由 next.config rewrites 代理到 localhost:8080,
// 生产环境由 Nginx 反代,前端永远访问相对路径 /api/v1/*。
async function fetchArticles(source: string, q: string): Promise<Article[]> {
  const params = new URLSearchParams({ limit: "50" });
  if (source) params.set("source", source);
  if (q) params.set("q", q);
  const res = await fetch(`/api/v1/articles?${params.toString()}`, { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: ListResp = await res.json();
  return data.items ?? [];
}

async function fetchAnnouncements(): Promise<Announcement[]> {
  const res = await fetch("/api/v1/announcements", { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: { items: Announcement[] } = await res.json();
  return data.items ?? [];
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

// HeatBadge 统一热度展示样式:🔥 主热度 + 可选趋势(↑/↓ 差值)。
// 趋势仅在 prev_value > 0 且与当前不同时显示,首次抓取的条目不展示趋势。
function HeatBadge({
  heat,
  value,
  prevValue,
}: {
  heat: string;
  value: number;
  prevValue: number;
}) {
  const main = heat || formatHeat(value);
  if (!main) return null;

  const hasTrend = prevValue > 0 && value > 0 && value !== prevValue;
  const diff = value - prevValue;
  const up = diff > 0;
  const trendText = hasTrend ? formatHeat(Math.abs(diff)) : "";

  return (
    <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-red-50 px-2 py-0.5 text-xs font-medium tabular-nums text-red-600 dark:bg-red-950 dark:text-red-400">
      <span aria-hidden="true">🔥</span>
      <span>{main}</span>
      {hasTrend && (
        <span
          className={
            up
              ? "text-emerald-600 dark:text-emerald-400"
              : "text-zinc-500 dark:text-zinc-400"
          }
          title={`相比上次 ${up ? "上升" : "下降"} ${trendText}`}
        >
          {up ? "↑" : "↓"}
          {trendText}
        </span>
      )}
    </span>
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

export default function Home() {
  const [articles, setArticles] = useState<Article[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [, setTick] = useState(0); // 用于触发 formatRelativeTime 重渲染
  const initialLoad = useRef(true);

  // 当前选中的源 + 搜索词。debouncedQ 用于发请求,query 用于输入框受控。
  const [source, setSource] = useState("");
  const [query, setQuery] = useState("");
  const [debouncedQ, setDebouncedQ] = useState("");

  // 已读 / 收藏 id(localStorage 持久化)
  const read = useIdSet(READ_KEY);
  const starred = useIdSet(STARRED_KEY);

  // 搜索框防抖
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedQ(query.trim()), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [query]);

  const refresh = useCallback(async () => {
    try {
      const items = await fetchArticles(source, debouncedQ);
      setArticles(items);
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
    }
  }, [source, debouncedQ]);

  useEffect(() => {
    refresh();
    const timer = setInterval(refresh, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [refresh]);

  // 每 30 秒更新一次"最后更新"的相对时间显示
  useEffect(() => {
    const timer = setInterval(() => setTick((t) => t + 1), 30_000);
    return () => clearInterval(timer);
  }, []);

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-8">
      <AnnouncementBar />
      <header className="mb-6 flex items-baseline justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">Newsfeed</h1>
        <div className="flex items-baseline gap-3 text-sm text-zinc-500">
          {lastUpdated && (
            <span title={lastUpdated.toLocaleString("zh-CN", { hour12: false })}>
              {formatRelativeTime(lastUpdated)}更新
            </span>
          )}
          <span>{loading ? "加载中…" : `共 ${articles.length} 条`}</span>
        </div>
      </header>

      {/* 源 tab + 搜索框 */}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="flex gap-1 rounded-md border border-zinc-200 bg-white p-0.5 dark:border-zinc-800 dark:bg-zinc-900">
          {SOURCES.map((s) => {
            const active = source === s.key;
            return (
              <button
                key={s.key || "all"}
                type="button"
                onClick={() => setSource(s.key)}
                className={
                  "rounded px-3 py-1 text-sm transition " +
                  (active
                    ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900"
                    : "text-zinc-600 hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800")
                }
              >
                {s.label}
              </button>
            );
          })}
        </div>
        <input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="搜索标题或内容…"
          aria-label="搜索"
          className="ml-auto min-w-0 flex-1 rounded-md border border-zinc-200 bg-white px-3 py-1 text-sm outline-none placeholder:text-zinc-400 focus:border-zinc-400 dark:border-zinc-800 dark:bg-zinc-900 dark:focus:border-zinc-600"
        />
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
              {/* 收藏按钮:绝对定位右上角,不影响主链接命中 */}
              <button
                type="button"
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  starred.toggle(a.id);
                }}
                aria-label={isStarred ? "取消收藏" : "收藏"}
                className={
                  "absolute right-3 top-3 rounded p-1 text-base transition " +
                  (isStarred
                    ? "text-amber-500"
                    : "text-zinc-300 hover:text-amber-500 dark:text-zinc-600")
                }
              >
                {isStarred ? "★" : "☆"}
              </button>
              <a
                href={a.url}
                target="_blank"
                rel="noreferrer"
                onClick={() => read.add(a.id)}
                className="flex gap-3 pr-8"
              >
                <span className="shrink-0 select-none font-mono text-sm text-zinc-400 tabular-nums">
                  {String(i + 1).padStart(2, "0")}
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <h2
                      className={
                        "text-base font-medium leading-snug group-hover:underline " +
                        (isRead
                          ? "text-zinc-500 dark:text-zinc-500"
                          : "text-zinc-900 dark:text-zinc-100")
                      }
                    >
                      {a.title}
                    </h2>
                    {(a.heat || a.heat_value > 0) && (
                      <HeatBadge
                        heat={a.heat}
                        value={a.heat_value}
                        prevValue={a.prev_heat_value}
                      />
                    )}
                  </div>
                  {a.content && (
                    <p className="mt-1 line-clamp-2 text-sm text-zinc-600 dark:text-zinc-400">
                      {a.content}
                    </p>
                  )}
                  <div className="mt-2 flex gap-3 text-xs text-zinc-500">
                    <span>{a.source_key}</span>
                    <span>{formatTime(a.published_at)}</span>
                  </div>
                </div>
              </a>
            </li>
          );
        })}
      </ul>
    </main>
  );
}
