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
// 趋势仅在 prev_value > 0 且与当前不同时显示,首次抓取的条目不展示趋势,
// 改为渲染一个脉动的 NEW 徽章(prev_value === 0 视为首次上榜)。
// 不同源的热度语义不一样:知乎是"讨论度",B 站是"播放量",量纲也不在同一级。
// 用不同图标 + tooltip 让用户一眼看出指标类型,避免拿"100 万播放"和"100 万热度"
// 做心理换算。新增源时在此追加一项即可,默认走 fire 配置。
const HEAT_ICONS: Record<string, { icon: string; label: string }> = {
  zhihu_hot: { icon: "🔥", label: "热度" },
  bilibili_popular: { icon: "▶", label: "播放量" },
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

      {/* 邮件订阅区(默认折叠)。
          命中后由后端发邮件,跨设备/关浏览器都能收到——不再依赖本机的 Notification API。*/}
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
              {/* 收藏按钮:绝对定位右上角,不影响主链接命中 */}
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
              {/* 分享按钮:复制 /share/{id} 到剪贴板 */}
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
                  {/* 标题行右边留 pr-16 给绝对定位的 ★/↗ 按钮腾位置;
                      摘要和元信息行不需要避让,可以贴到卡片右边沿 */}
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
                    {/* 同源 Top 10:在当前列表里、按 heat_value 降序排前 10 名,显示排名序号 */}
                    {topIdsBySource.has(a.id) && (
                      <span
                        className="inline-flex shrink-0 items-center gap-0.5 rounded-full bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-400"
                        title={`同源热度 / 播放量 Top ${topIdsBySource.get(a.id)}`}
                        aria-label={`同源 Top ${topIdsBySource.get(a.id)}`}
                      >
                        🏆<span className="font-semibold tabular-nums">{topIdsBySource.get(a.id)}</span>
                      </span>
                    )}
                    {/* 飙升:相比 30 分钟前热度增幅 ≥ 10%,或首次上榜且进 top10 */}
                    {isSurging(a, topIdsBySource) && (
                      <span
                        className="inline-flex shrink-0 items-center rounded-full bg-orange-100 px-1.5 py-0.5 text-xs font-medium text-orange-700 dark:bg-orange-950 dark:text-orange-400"
                        title="近 30 分钟热度飙升(增幅 ≥ 10%,或首次上榜进 Top10)"
                        aria-label="飙升"
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
