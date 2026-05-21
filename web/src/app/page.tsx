"use client";

import { useEffect, useState, useCallback, useRef, useMemo } from "react";
import Link from "next/link";
import { useIdSet, READ_KEY, STARRED_KEY } from "@/lib/useIdSet";

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
  heat_discovered_terms?: string[];
  sources: { source_key: string; count: number }[];
  sample_article?: {
    id: number;
    title: string;
    source_key: string;
    heat: string;
    heat_value: number;
  };
  articles: {
    id: number;
    title: string;
    source_key: string;
    heat: string;
    heat_value: number;
    published_at: string;
  }[];
  is_heat_discovered?: boolean;
  is_promoted?: boolean;
};

type TrackerResp = {
  window: { hours: number };
  items: TrackerTopic[];
  events?: TrackerEventGroup[];
};

type TrackerEventGroup = {
  title: string;
  entities: string[];
  keywords: string[];
  score: number;
  count: number;
  momentum: "up" | "flat" | "down";
  sources: { source_key: string; count: number }[];
  articles: { id: number; title: string; source_key: string; heat_value: number }[];
  heat_discovered_entities?: string[];
  heat_discovered_keywords?: string[];
};

type HotlistItem = {
  id: number;
  source_key: string;
  url: string;
  title: string;
  heat: string;
  heat_value: number;
  prev_heat_value: number;
  rank: number;
  rank_change: number;
  is_new: boolean;
};

type HotlistResp = {
  zhihu: HotlistItem[];
  baidu: HotlistItem[];
  weibo: HotlistItem[];
  sogou: HotlistItem[];
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
  baidu_hot: "百度",
  weibo_hot: "微博",
  sogou_hot: "搜狗",
};

const HEAT_ICONS: Record<string, { icon: string; label: string }> = {
  zhihu_hot: { icon: "🔥", label: "热度" },
  bilibili_popular: { icon: "▶", label: "播放量" },
  baidu_hot: { icon: "🔍", label: "热搜" },
  weibo_hot: { icon: "📢", label: "热搜" },
  sogou_hot: { icon: "🔎", label: "热搜" },
};

const SOURCE_FILTERS: { key: string; label: string }[] = [
  { key: "", label: "全部" },
  { key: "zhihu_hot", label: "知乎" },
  // { key: "bilibili_popular", label: "B站" }, // 暂时屏蔽:内容以娱乐视频为主,与新闻聚合相关度低
  { key: "baidu_hot", label: "百度" },
  { key: "weibo_hot", label: "微博" },
  { key: "sogou_hot", label: "搜狗" },
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
  return { window: data.window, items: data.items ?? [], events: data.events ?? [] };
}

async function fetchHotlist(): Promise<HotlistResp> {
  const res = await fetch("/api/v1/hotlist", { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: HotlistResp = await res.json();
  return { zhihu: data.zhihu ?? [], baidu: data.baidu ?? [], weibo: data.weibo ?? [], sogou: data.sogou ?? [] };
}

// ======================== Formatters ========================

// hanRuneCount 统计字符串中汉字个数(CJK 基本区 + 扩展A区)。
// 对应后端 tracker.go 里的 hanRuneCount,用于判断短词。
function hanRuneCount(s: string): number {
  let count = 0;
  for (const ch of s) {
    const cp = ch.codePointAt(0) ?? 0;
    if ((cp >= 0x4e00 && cp <= 0x9fff) || (cp >= 0x3400 && cp <= 0x4dbf)) count++;
  }
  return count;
}

// isShortCJKTerm 对应后端 filterWeakContentMatches 的判断条件:
// 全是 ≤2 汉字、且不含 ASCII 字母 → 属于泛短词,仅 title 命中才算相关。
function isShortCJKTerm(s: string): boolean {
  if (/[a-zA-Z]/.test(s)) return false;
  return hanRuneCount(s) <= 2;
}

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

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString("zh-CN", { hour12: false });
  } catch {
    return iso;
  }
}

// ======================== HeatBadge ========================

function HeatBadge({ sourceKey, heat, value, prevValue }: { sourceKey: string; heat: string; value: number; prevValue: number }) {
  const main = heat || formatHeat(value);
  if (!main) return null;
  const meta = HEAT_ICONS[sourceKey] ?? { icon: "🔥", label: "热度" };
  const hasTrend = prevValue > 0 && value > 0 && value !== prevValue;
  const diff = value - prevValue;
  const up = diff > 0;
  const trendText = hasTrend ? formatHeat(Math.abs(diff)) : "";
  return (
    <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-red-50 px-2 py-0.5 text-xs font-medium tabular-nums text-red-600 dark:bg-red-950 dark:text-red-400" title={meta.label}>
      <span aria-hidden="true">{meta.icon}</span>
      <span>{main}</span>
      {hasTrend && (
        <span className={up ? "text-emerald-600 dark:text-emerald-400" : "text-zinc-500 dark:text-zinc-400"} title={`${meta.label}相比上次${up ? "上升" : "下降"} ${trendText}`}>
          {up ? "↑" : "↓"}{trendText}
        </span>
      )}
    </span>
  );
}

// ======================== Announcement Bar ========================

const LEVEL_CLASSES: Record<Announcement["level"], string> = {
  info: "border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-800/70 dark:bg-blue-950/40 dark:text-blue-300",
  warn: "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-800/70 dark:bg-amber-950/40 dark:text-amber-300",
  critical: "border-red-200 bg-red-50 text-red-800 dark:border-red-800/70 dark:bg-red-950/40 dark:text-red-300",
  quote: "border-zinc-200 bg-zinc-50 text-zinc-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300",
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
    <div className="mb-6">
      <div className="mb-3 flex items-center gap-2">
        <span className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">公告栏</span>
        <span className="text-base leading-none">📢</span>
      </div>
      <div className="space-y-2">
        {visible.map((a) => (
          <section key={a.id} className={`rounded-lg border shadow-sm ${LEVEL_CLASSES[a.level]}`}>
            <div className="flex items-start gap-2 px-3 py-2.5">
              <div className="min-w-0 flex-1 text-[13px] leading-relaxed whitespace-pre-wrap break-words">
                {a.content}
              </div>
              <button type="button" onClick={() => dismiss(a.id)} aria-label="关闭" className="shrink-0 rounded p-1 opacity-60 transition hover:bg-black/5 hover:opacity-100 dark:hover:bg-white/10">
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" className="h-4 w-4" fill="currentColor"><path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z" /></svg>
              </button>
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}

// ======================== Topic Group (全部Tab 话题聚合视图) ========================

function TopicGroup({
  topic,
  windowHours,
  onSearch,
  onDeleteHeatWord,
}: {
  topic: TrackerTopic;
  windowHours: number;
  onSearch: (q: string) => void;
  onDeleteHeatWord?: (word: string) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const articles = topic.articles ?? [];
  const displayArticles = expanded ? articles : articles.slice(0, 3);
  const hasMore = articles.length > 3;
  const displayCount = topic.count;

  const momentumCfg: Record<string, { text: string; icon: string; cls: string }> = {
    up: { text: "升温", icon: "↗", cls: "text-emerald-600 dark:text-emerald-400" },
    down: { text: "回落", icon: "↘", cls: "text-zinc-500 dark:text-zinc-400" },
    flat: { text: "持平", icon: "→", cls: "text-blue-600 dark:text-blue-400" },
  };
  const m = momentumCfg[topic.momentum] ?? momentumCfg.flat;

  return (
    <section className="rounded-lg border border-zinc-200 bg-white shadow-sm dark:border-zinc-800 dark:bg-zinc-900">
      {/* 话题头: px-3 py-2.5 与事件卡片保持一致 */}
      <div className="flex items-center gap-2 px-3 py-2.5">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <Link
            href={`/tracker?term=${encodeURIComponent(topic.label)}&window=${windowHours}`}
            className="text-[14px] font-semibold leading-snug text-zinc-900 hover:text-amber-700 dark:text-zinc-100 dark:hover:text-amber-300"
          >
            {topic.label}
          </Link>
          {topic.is_heat_discovered && (
            topic.is_promoted
              ? <span className="rounded bg-blue-100 px-1 py-0.5 text-[9px] font-bold uppercase text-blue-700 dark:bg-blue-900 dark:text-blue-300">new</span>
              : <span className="rounded bg-emerald-100 px-1 py-0.5 text-[9px] font-bold uppercase text-emerald-700 dark:bg-emerald-900 dark:text-emerald-300">new</span>
          )}
          <span className={`shrink-0 text-[11px] font-medium ${m.cls}`}>{m.icon} {m.text}</span>
        </div>
        <span className="shrink-0 text-xs font-medium tabular-nums text-emerald-600 dark:text-emerald-400">
          {formatHeat(topic.score)}
        </span>
        {topic.is_heat_discovered && onDeleteHeatWord && (
          <button
            type="button"
            onClick={() => onDeleteHeatWord(topic.label)}
            className="shrink-0 text-[11px] text-zinc-300 hover:text-red-500 dark:text-zinc-600 dark:hover:text-red-400"
            title="移除此热词"
          >
            ✕
          </button>
        )}
      </div>

      {/* 关联词 chips + 时间线链接: 与事件卡片实体行保持一致 */}
      {topic.related_terms.length > 0 && (
        <div className="flex flex-wrap items-center gap-1 border-t border-zinc-50 px-3 py-1.5 dark:border-zinc-800/50">
          {topic.related_terms.slice(0, 5).map((term) => {
            const isHd = topic.heat_discovered_terms?.includes(term);
            return (
              <span key={term} className="inline-flex items-center gap-0.5">
                <button
                  type="button"
                  onClick={() => onSearch(term)}
                  className="rounded-full bg-amber-50 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 hover:bg-amber-100 dark:bg-amber-950 dark:text-amber-300 dark:hover:bg-amber-900"
                >
                  {term}
                </button>
                {isHd && (
                  <span className="rounded bg-emerald-100 px-0.5 text-[8px] font-bold text-emerald-700 dark:bg-emerald-900 dark:text-emerald-300">N</span>
                )}
                {isHd && onDeleteHeatWord && (
                  <button type="button" onClick={() => onDeleteHeatWord(term)} className="text-[10px] text-zinc-300 hover:text-red-500">✕</button>
                )}
              </span>
            );
          })}
          <Link
            href={`/tracker?term=${encodeURIComponent(topic.label)}&window=${windowHours}`}
            className="ml-auto shrink-0 text-[11px] text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200"
          >
            时间线 →
          </Link>
        </div>
      )}

      {/* 文章列表: px-3 py-2 text-[13px] 与事件卡片文章行保持一致 */}
      <div className="border-t border-zinc-50 dark:border-zinc-800/50">
        {displayArticles.map((a) => (
          <Link
            key={a.id}
            href={`/article?id=${a.id}`}
            className="flex items-center gap-2 border-b border-zinc-50 px-3 py-2 transition last:border-b-0 hover:bg-zinc-50 dark:border-zinc-800/50 dark:hover:bg-zinc-800/50"
          >
            <span className="min-w-0 flex-1 truncate text-[13px] text-zinc-700 dark:text-zinc-300">
              {a.title}
            </span>
            <span className="shrink-0 text-[10px] text-zinc-400">
              {SOURCE_LABELS[a.source_key] ?? a.source_key}
            </span>
            {(a.heat || a.heat_value > 0) && (
              <span className="shrink-0 text-[10px] font-medium tabular-nums text-red-500 dark:text-red-400">
                {a.heat || formatHeat(a.heat_value)}
              </span>
            )}
          </Link>
        ))}
      </div>

      {/* 底部: 文章数 + 展开 + 查看全部 + 来源 chips */}
      <div className="flex flex-wrap items-center gap-1.5 border-t border-zinc-50 px-3 py-1.5 dark:border-zinc-800/50">
        <span className="text-[11px] text-zinc-400">
          {displayCount} 篇
        </span>
        {hasMore && (
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="text-[11px] text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
          >
            {expanded ? "收起" : `+${articles.length - 3} 更多`}
          </button>
        )}
        {hasMore && (
          <Link
            href={`/tracker?term=${encodeURIComponent(topic.label)}&window=${windowHours}`}
            className="text-[11px] text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200"
          >
            查看全部 →
          </Link>
        )}
        {topic.sources?.map((s) => (
          <span
            key={s.source_key}
            className="rounded bg-zinc-100 px-1.5 py-0.5 text-[10px] text-zinc-500 dark:bg-zinc-800 dark:text-zinc-400"
          >
            {SOURCE_LABELS[s.source_key] ?? s.source_key}&nbsp;{s.count}
          </span>
        ))}
      </div>
    </section>
  );
}

function EventGroupCard({ event, windowHours, onDeleteHeatWord }: { event: TrackerEventGroup; windowHours: number; onDeleteHeatWord?: (word: string) => void }) {
  const [expanded, setExpanded] = useState(false);
  const displayArticles = expanded ? event.articles : event.articles.slice(0, 3);
  const hasMore = event.articles.length > 3;

  return (
    <div className="rounded-lg border border-zinc-200 bg-white shadow-sm dark:border-zinc-800 dark:bg-zinc-900">
      {/* 事件标题 + 热度 */}
      <div className="flex items-center gap-2 px-3 py-2.5">
        <h3 className="min-w-0 flex-1 text-[14px] font-semibold leading-snug text-zinc-900 dark:text-zinc-100">{event.title}</h3>
        <span className="shrink-0 text-xs font-medium tabular-nums text-emerald-600 dark:text-emerald-400">
          {formatHeat(event.score)}
        </span>
      </div>
      {/* 实体 / 关键词 chips */}
      {(event.entities.length > 0 || event.keywords.length > 0) && (
        <div className="flex flex-wrap gap-1 border-t border-zinc-50 px-3 py-1.5 dark:border-zinc-800/50">
          {event.entities.map((e) => (
            <span key={e} className="inline-flex items-center gap-0.5">
              <Link
                href={`/tracker?term=${encodeURIComponent(e)}&window=${windowHours}`}
                className="rounded-full bg-amber-50 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 hover:bg-amber-100 dark:bg-amber-950 dark:text-amber-300 dark:hover:bg-amber-900"
              >
                {e}
              </Link>
              {event.heat_discovered_entities?.includes(e) && (
                <>
                  <span className="rounded bg-emerald-100 px-0.5 text-[8px] font-bold text-emerald-700 dark:bg-emerald-900 dark:text-emerald-300">N</span>
                  {onDeleteHeatWord && (
                    <button type="button" onClick={() => onDeleteHeatWord(e)} className="text-[10px] text-zinc-300 hover:text-red-500">✕</button>
                  )}
                </>
              )}
            </span>
          ))}
          {event.keywords.map((k) => (
            <span key={k} className="inline-flex items-center gap-0.5">
              <Link
                href={`/tracker?term=${encodeURIComponent(k)}&window=${windowHours}`}
                className="rounded-full bg-zinc-100 px-1.5 py-0.5 text-[11px] text-zinc-500 hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-400 dark:hover:bg-zinc-700"
              >
                {k}
              </Link>
              {event.heat_discovered_keywords?.includes(k) && (
                <>
                  <span className="rounded bg-emerald-100 px-0.5 text-[8px] font-bold text-emerald-700 dark:bg-emerald-900 dark:text-emerald-300">N</span>
                  {onDeleteHeatWord && (
                    <button type="button" onClick={() => onDeleteHeatWord(k)} className="text-[10px] text-zinc-300 hover:text-red-500">✕</button>
                  )}
                </>
              )}
            </span>
          ))}
        </div>
      )}
      {/* 相关文章列表(默认3条,可展开/收起) */}
      {event.articles.length > 0 && (
        <div className="border-t border-zinc-50 dark:border-zinc-800/50">
          {displayArticles.map((a) => (
            <Link
              key={a.id}
              href={`/article?id=${a.id}`}
              className="flex items-center gap-2 border-b border-zinc-50 px-3 py-2 transition last:border-b-0 hover:bg-zinc-50 dark:border-zinc-800/50 dark:hover:bg-zinc-800/50"
            >
              <span className="min-w-0 flex-1 truncate text-[13px] text-zinc-700 dark:text-zinc-300">{a.title}</span>
              <span className="shrink-0 text-[10px] text-zinc-400">{SOURCE_LABELS[a.source_key] ?? a.source_key}</span>
              {a.heat_value > 0 && (
                <span className="shrink-0 text-[10px] font-medium tabular-nums text-red-500 dark:text-red-400">{formatHeat(a.heat_value)}</span>
              )}
            </Link>
          ))}
        </div>
      )}
      {/* 底部:文章数 + 展开 + 来源 chips */}
      <div className="flex flex-wrap items-center gap-1.5 border-t border-zinc-50 px-3 py-1.5 dark:border-zinc-800/50">
        <span className="text-[11px] text-zinc-400">
          {event.count > event.articles.length ? `${event.count} 篇（展示 ${event.articles.length}）` : `${event.count} 篇`}
        </span>
        {hasMore && (
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="text-[11px] text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
          >
            {expanded ? "收起" : `+${event.articles.length - 3} 更多`}
          </button>
        )}
        {event.sources.map((s) => (
          <span key={s.source_key} className="rounded bg-zinc-100 px-1.5 py-0.5 text-[10px] text-zinc-500 dark:bg-zinc-800 dark:text-zinc-400">
            {SOURCE_LABELS[s.source_key] ?? s.source_key}&nbsp;{s.count}
          </span>
        ))}
      </div>
    </div>
  );
}

// ======================== Compact Article Row (知乎/B站Tab 时间流) ========================

function ArticleRow({ article, index }: { article: Article; index: number }) {
  return (
    <Link
      href={`/article?id=${article.id}`}
      className="flex items-start gap-3 border-b border-zinc-50 px-3 py-2.5 transition last:border-b-0 hover:bg-zinc-50 dark:border-zinc-800/50 dark:hover:bg-zinc-800/50"
    >
      <span className="mt-0.5 shrink-0 font-mono text-[11px] text-zinc-300 tabular-nums dark:text-zinc-600">
        {String(index + 1).padStart(2, "0")}
      </span>
      <span className="min-w-0 flex-1 line-clamp-2 text-sm leading-snug text-zinc-800 dark:text-zinc-200">
        {article.title}
      </span>
      <span className="mt-0.5 shrink-0 text-[11px] text-zinc-400">
        {formatRelativeTime(article.published_at)}
      </span>
      {(article.heat || article.heat_value > 0) && (
        <span className="mt-0.5 shrink-0 text-[11px] font-medium tabular-nums text-red-500 dark:text-red-400">
          {article.heat || formatHeat(article.heat_value)}
        </span>
      )}
    </Link>
  );
}

// ======================== CompactRow (Hot 榜条目) ========================

function CompactRow({ item, rank }: { item: HotlistItem; rank: number }) {
  const rc = item.rank_change;
  return (
    <Link
      href={`/article?id=${item.id}`}
      className="flex items-center gap-2 border-b border-zinc-50 px-3 py-2 transition last:border-b-0 hover:bg-zinc-50 dark:border-zinc-800/50 dark:hover:bg-zinc-800/50"
    >
      <span className="w-5 shrink-0 text-right font-mono text-[11px] tabular-nums text-zinc-300 dark:text-zinc-600">
        {rank}
      </span>
      <span className="min-w-0 flex-1 line-clamp-2 text-[13px] leading-snug text-zinc-800 dark:text-zinc-200">
        {item.title}
      </span>
      {rc !== 0 ? (
        <span className={`shrink-0 text-[10px] font-medium tabular-nums ${rc > 0 ? "text-emerald-500 dark:text-emerald-400" : "text-zinc-400"}`}>
          {rc > 0 ? `↑${rc}` : `↓${Math.abs(rc)}`}
        </span>
      ) : (
        <span className="shrink-0 text-[10px] text-zinc-300 dark:text-zinc-600">→</span>
      )}
      {(item.heat || item.heat_value > 0) && (
        <span className="shrink-0 text-[11px] font-medium tabular-nums text-red-500 dark:text-red-400">
          {item.heat || formatHeat(item.heat_value)}
        </span>
      )}
    </Link>
  );
}

// ======================== HotPanel ========================

function HotPanel({ zhihu, baidu, weibo, sogou }: { zhihu: HotlistItem[]; baidu: HotlistItem[]; weibo: HotlistItem[]; sogou: HotlistItem[] }) {
  if (zhihu.length === 0 && baidu.length === 0 && weibo.length === 0 && sogou.length === 0) return null;
  return (
    <div className="rounded-lg border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
      <div className="flex items-center gap-2 border-b border-zinc-100 px-4 py-2.5 dark:border-zinc-800">
        <span className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">大家在聊什么</span>
        <span className="text-base leading-none">💬</span>
      </div>
      {zhihu.length > 0 && (
        <>
          <div className="border-b border-zinc-50 px-4 py-1 dark:border-zinc-800/60">
            <span className="text-[11px] font-medium text-zinc-400">知乎</span>
          </div>
          {zhihu.map((item, i) => (
            <CompactRow key={item.id} item={item} rank={i + 1} />
          ))}
        </>
      )}
      {baidu.length > 0 && (
        <>
          <div className="border-b border-zinc-50 border-t border-t-zinc-200 px-4 py-1 dark:border-zinc-800/60 dark:border-t-zinc-700">
            <span className="text-[11px] font-medium text-zinc-400">百度</span>
          </div>
          {baidu.map((item, i) => (
            <CompactRow key={item.id} item={item} rank={i + 1} />
          ))}
        </>
      )}
      {weibo.length > 0 && (
        <>
          <div className="border-b border-zinc-50 border-t border-t-zinc-200 px-4 py-1 dark:border-zinc-800/60 dark:border-t-zinc-700">
            <span className="text-[11px] font-medium text-zinc-400">微博</span>
          </div>
          {weibo.map((item, i) => (
            <CompactRow key={item.id} item={item} rank={i + 1} />
          ))}
        </>
      )}
      {sogou.length > 0 && (
        <>
          <div className="border-b border-zinc-50 border-t border-t-zinc-200 px-4 py-1 dark:border-zinc-800/60 dark:border-t-zinc-700">
            <span className="text-[11px] font-medium text-zinc-400">搜狗</span>
          </div>
          {sogou.map((item, i) => (
            <CompactRow key={item.id} item={item} rank={i + 1} />
          ))}
        </>
      )}
    </div>
  );
}

// ======================== Subscriptions Hook ========================

type Subscription = { id: number; keyword: string; created_at: string };

function useSubscriptions() {
  const [items, setItems] = useState<Subscription[]>([]);
  const [notifyTo, setNotifyTo] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/subscriptions", { cache: "no-store" });
      if (res.status === 503) { setError("订阅功能未启用"); setLoading(false); return; }
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

  useEffect(() => { refresh(); }, [refresh]);

  const add = useCallback(async (raw: string) => {
    const v = raw.trim();
    if (!v) return;
    try {
      const res = await fetch("/api/v1/subscriptions", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ keyword: v }) });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      await refresh();
    } catch (e) { setError(String(e)); }
  }, [refresh]);

  const remove = useCallback(async (id: number) => {
    try {
      const res = await fetch(`/api/v1/subscriptions/${id}`, { method: "DELETE" });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      await refresh();
    } catch (e) { setError(String(e)); }
  }, [refresh]);

  return { items, notifyTo, loading, error, add, remove };
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
  const [events, setEvents] = useState<TrackerEventGroup[]>([]);
  const [trackerWindow, setTrackerWindow] = useState(3);

  // 热榜(专用接口,直接按 heat_value 排序,不受 published_at 分页影响)
  const [hotlist, setHotlist] = useState<HotlistResp>({ zhihu: [], baidu: [], weibo: [], sogou: [] });

  // "全部"Tab + 非搜索 = 话题聚合视图; "知乎"/"B站"Tab 或搜索 = 时间流
  const isTopicView = source === "" && !debouncedQ;

  // 邮件订阅
  const subs = useSubscriptions();
  const [keywordInput, setKeywordInput] = useState("");
  const [subOpen, setSubOpen] = useState(false);

  // 已读/收藏
  const read = useIdSet(READ_KEY);
  const starred = useIdSet(STARRED_KEY);

  // 分享
  const [toast, setToast] = useState<string | null>(null);
  useEffect(() => { if (!toast) return; const t = setTimeout(() => setToast(null), 2000); return () => clearTimeout(t); }, [toast]);
  const copyShareLink = useCallback(async (id: number) => {
    const url = `${window.location.origin}/share/${id}`;
    try { await navigator.clipboard.writeText(url); setToast("分享链接已复制"); } catch { setToast(url); }
  }, []);

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
      const topicViewLimit =
        trackerWindow >= 72 ? 800 :
        trackerWindow >= 24 ? 500 : 300;
      const requestLimit = isTopicView ? topicViewLimit : page * PAGE_SIZE;
      const data = await fetchArticles(source, debouncedQ, requestLimit);
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
  }, [source, debouncedQ, page, isTopicView, trackerWindow]);

  useEffect(() => {
    refresh();
    const timer = setInterval(refresh, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [refresh]);

  // 拉取话题 + 事件聚类(仅话题视图模式,跟随 trackerWindow 切换)
  useEffect(() => {
    if (!isTopicView) { setTopics([]); setEvents([]); return; }
    let cancelled = false;
    const load = async () => {
      try {
        const data = await fetchTrackers(trackerWindow);
        if (!cancelled) {
          setTopics(data.items);
          setEvents(data.events ?? []);
        }
      } catch { /* silent */ }
    };
    load();
    const timer = setInterval(load, POLL_INTERVAL_MS);
    return () => { cancelled = true; clearInterval(timer); };
  }, [trackerWindow, isTopicView]);

  // 拉取热榜(独立接口,按 heat_value 直接排序,与 articles 分页无关)
  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const data = await fetchHotlist();
        if (!cancelled) setHotlist(data);
      } catch { /* silent */ }
    };
    load();
    const timer = setInterval(load, POLL_INTERVAL_MS);
    return () => { cancelled = true; clearInterval(timer); };
  }, []);

  // 话题聚合分组
  //
  // 设计:一篇文章可以同时归到多个话题下(如"普京访华"既归"普京"也归"俄罗斯"
  // 直接使用后端返回的 topic.articles,不再前端匹配。
  // 后端 buildTrackerTopics 通过 gse 分词+多层过滤精确匹配,结果可信。
  const { grouped, ungrouped } = useMemo(() => {
    if (!isTopicView || topics.length === 0) {
      return { grouped: [] as { topic: TrackerTopic }[], ungrouped: articles };
    }

    const grouped: { topic: TrackerTopic }[] = [];
    const matchedIds = new Set<number>();

    for (const topic of topics) {
      if ((topic.articles ?? []).length === 0) continue;
      for (const a of topic.articles) {
        matchedIds.add(a.id);
      }
      grouped.push({ topic });
    }

    // ungrouped: 未被任何话题匹配的文章(兜底)
    const latestPublishedMs = windowedLatestPublishedMs(articles);
    const cutoff = latestPublishedMs - trackerWindow * 3600 * 1000;
    const windowed = articles.filter((a) => {
      try { return new Date(a.published_at).getTime() >= cutoff && a.source_key !== "bilibili_popular"; } catch { return true; }
    });
    const ungrouped = windowed.filter((a) => !matchedIds.has(a.id));
    return { grouped, ungrouped };
  }, [articles, topics, isTopicView, trackerWindow]);

  // 同源 Top 10 排名(按 heat_value 排,所有源统一处理)
  const topIdsBySource = useMemo(() => {
    const bySource = new Map<string, Article[]>();
    for (const a of articles) { const list = bySource.get(a.source_key) ?? []; list.push(a); bySource.set(a.source_key, list); }
    const top = new Map<number, number>();
    for (const list of bySource.values()) { list.filter((a) => a.heat_value > 0).sort((x, y) => y.heat_value - x.heat_value).slice(0, 10).forEach((a, idx) => top.set(a.id, idx + 1)); }
    return top;
  }, [articles]);

  // 飙升判定
  function isSurging(a: Article): boolean {
    if (a.heat_value <= 0) return false;
    if (a.prev_heat_value <= 0) return topIdsBySource.has(a.id);
    return (a.heat_value - a.prev_heat_value) / a.prev_heat_value >= 0.1;
  }

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

  const handleDeleteHeatWord = async (word: string) => {
    try {
      await fetch(`/api/v1/trackers/heat-words/${encodeURIComponent(word)}`, { method: "DELETE" });
      // 乐观更新:移除该 topic + 从事件中移除
      setTopics((prev) => prev.filter((t) => t.label !== word));
      setEvents((prev) =>
        prev.map((ev) => ({
          ...ev,
          entities: ev.entities.filter((e) => e !== word),
          keywords: ev.keywords.filter((k) => k !== word),
          heat_discovered_entities: ev.heat_discovered_entities?.filter((e) => e !== word),
          heat_discovered_keywords: ev.heat_discovered_keywords?.filter((k) => k !== word),
        }))
      );
    } catch {
      // 静默失败,下次刷新会重算
    }
  };

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-8">
      <AnnouncementBar />

      {/* Header */}
      <header className="mb-5 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Newsfeed</h1>
        <span className="text-sm text-zinc-500">
          {loading ? "加载中…" : (
            <>
              共 {total} 条
              {source && articles.length > 0 && (() => {
                // 选定单一源时,展示"最近抓取时间"。articles 默认按 published_at DESC,
                // 但 fetched_at 在同批次几乎一致,取数组里最大的最稳妥。
                let maxFetched = "";
                for (const a of articles) {
                  if (a.fetched_at > maxFetched) maxFetched = a.fetched_at;
                }
                return maxFetched ? (
                  <span className="ml-2 text-zinc-400">· 最近抓取 {formatRelativeTime(maxFetched)}</span>
                ) : null;
              })()}
            </>
          )}
        </span>
      </header>

      {/* 筛选栏: 来源Tab + 搜索 + 时间窗口 */}
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
      </div>

      {/* 邮件订阅(折叠) */}
      <div className="mb-4 text-sm">
        <button type="button" onClick={() => setSubOpen((v) => !v)} className="text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100">
          📧 邮件订阅
          {subs.items.length > 0 && <span className="ml-1 text-xs text-zinc-400">({subs.items.length})</span>}
          <span className="ml-1 text-xs">{subOpen ? "▴" : "▾"}</span>
        </button>
        {subOpen && (
          <div className="mt-2 rounded-md border border-zinc-200 bg-zinc-50 p-3 dark:border-zinc-800 dark:bg-zinc-900">
            {subs.error && (
              <div className="mb-2 rounded bg-red-50 px-2 py-1 text-xs text-red-700 dark:bg-red-950 dark:text-red-300">{subs.error}</div>
            )}
            <div className="mb-2 flex flex-wrap gap-2">
              {subs.loading ? (
                <span className="text-xs text-zinc-500">加载中…</span>
              ) : subs.items.length === 0 ? (
                <span className="text-xs text-zinc-500">还没有订阅关键词</span>
              ) : (
                subs.items.map((s) => (
                  <span key={s.id} className="inline-flex items-center gap-1 rounded-full bg-zinc-200 px-2 py-0.5 text-xs text-zinc-700 dark:bg-zinc-700 dark:text-zinc-200">
                    {s.keyword}
                    <button type="button" onClick={() => subs.remove(s.id)} aria-label={`移除 ${s.keyword}`} className="text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100">×</button>
                  </span>
                ))
              )}
            </div>
            <form onSubmit={(e) => { e.preventDefault(); if (!keywordInput.trim()) return; subs.add(keywordInput); setKeywordInput(""); }} className="flex gap-2">
              <input type="text" value={keywordInput} onChange={(e) => setKeywordInput(e.target.value)} placeholder="输入关键词后回车，如 AI / 裁员" className="min-w-0 flex-1 rounded-md border border-zinc-300 bg-white px-2 py-1 text-xs outline-none focus:border-zinc-500 dark:border-zinc-700 dark:bg-zinc-800" />
              <button type="submit" className="rounded-md bg-zinc-900 px-3 py-1 text-xs text-white hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900">添加</button>
            </form>
            <div className="mt-3 text-xs text-zinc-500">
              {subs.notifyTo
                ? <>命中后会发邮件到 <span className="font-mono">{subs.notifyTo}</span>，延迟约等于抓取间隔(30分钟内)</>
                : "未配置收件邮箱。后端命中仍会登记去重，配上之后从下次开始发送。"}
            </div>
          </div>
        )}
      </div>

      {/* Error */}
      {error && (
        <div className="mb-5 rounded-md border border-red-300 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-300">
          加载失败: {error}
        </div>
      )}

      {/* Empty */}
      {!loading && articles.length === 0 && !error && (
        <div className="rounded-md border border-dashed border-zinc-300 p-8 text-center text-sm text-zinc-500 dark:border-zinc-700">
          没有匹配的内容
        </div>
      )}

      {isTopicView ? (
        /* ========== 全部Tab: 话题聚合视图 ========== */
        <>
          {/* 发生了什么大事 — 事件聚类 */}
          {events.length > 0 && (
            <div className="mb-6">
              <div className="mb-3 flex items-center gap-2">
                <span className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">发生了什么大事</span>
                <span className="text-base leading-none">⚡</span>
                <div className="ml-auto flex gap-0.5">
                  {[3, 6, 24, 72].map((w) => (
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
              </div>
              <div className="space-y-2">
                {events.map((event, i) => (
                  <EventGroupCard key={i} event={event} windowHours={trackerWindow} onDeleteHeatWord={handleDeleteHeatWord} />
                ))}
              </div>
            </div>
          )}

          {/* 哪些对象值得关注 — 实体列表 */}
          {grouped.length > 0 && (
            <div className="mb-6">
              <div className="mb-3 flex items-center gap-2">
                <span className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">哪些对象值得关注</span>
                <span className="text-base leading-none">📌</span>
                {/* 当 events 区块未渲染(events.length===0)时,关注对象成为顶部首块,
                    这里再放一个时间窗口选择器以便用户随时切窗口。
                    events 已有时,这里仍展示一份(操作直觉:每块都能切自己的窗口),
                    state 是同一个 trackerWindow 共享。 */}
                <div className="ml-auto flex gap-0.5">
                  {[3, 6, 24, 72].map((w) => (
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
              </div>
              <div className="space-y-4">
                {grouped.map(({ topic }) => (
                  <TopicGroup
                    key={`${topic.kind}:${topic.label}`}
                    topic={topic}
                    windowHours={trackerWindow}
                    onSearch={handleSearch}
                    onDeleteHeatWord={handleDeleteHeatWord}
                  />
                ))}
              </div>
            </div>
          )}
          {(hotlist.zhihu.length > 0 || hotlist.baidu.length > 0 || hotlist.weibo.length > 0 || hotlist.sogou.length > 0) && (
            <>
              {grouped.length > 0 && (
                <div className="mb-4 h-px bg-zinc-200 dark:bg-zinc-800" />
              )}
              <HotPanel zhihu={hotlist.zhihu} baidu={hotlist.baidu} weibo={hotlist.weibo} sogou={hotlist.sogou} />
            </>
          )}
        </>
      ) : (
        /* ========== 知乎/B站/搜索: 卡片式时间流 ========== */
        articles.length > 0 && (
          <ul className="space-y-3">
            {articles.map((a, i) => {
              const isRead = read.ids.has(a.id);
              const isStarred = starred.ids.has(a.id);
              return (
                <li key={a.id} className={"relative rounded-lg border bg-white p-4 shadow-sm transition hover:shadow-md dark:bg-zinc-900 " + (isStarred ? "border-zinc-200 border-l-4 border-l-amber-400 dark:border-zinc-800 dark:border-l-amber-500" : "border-zinc-200 dark:border-zinc-800") + (isRead ? " opacity-60" : "")}>
                  <button type="button" onClick={(e) => { e.preventDefault(); e.stopPropagation(); starred.toggle(a.id); }} aria-label={isStarred ? "取消收藏" : "收藏"} className={"absolute right-3 top-3 rounded p-1 text-base transition " + (isStarred ? "text-amber-500" : "text-zinc-300 hover:text-amber-500 dark:text-zinc-600")}>
                    {isStarred ? "★" : "☆"}
                  </button>
                  <button type="button" onClick={(e) => { e.preventDefault(); e.stopPropagation(); copyShareLink(a.id); }} aria-label="复制分享链接" className="absolute right-9 top-3 rounded p-1 text-sm text-zinc-300 transition hover:text-zinc-700 dark:text-zinc-600 dark:hover:text-zinc-200">
                    ↗
                  </button>
                  <Link href={`/article?id=${a.id}`} onClick={() => read.add(a.id)} className="flex gap-3">
                    <span className="shrink-0 select-none font-mono text-sm text-zinc-400 tabular-nums">{String(i + 1).padStart(2, "0")}</span>
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 pr-16">
                        <h2 className={"text-base font-medium leading-snug hover:underline " + (isRead ? "text-zinc-500 dark:text-zinc-500" : "text-zinc-900 dark:text-zinc-100")}>{a.title}</h2>
                        {(a.heat || a.heat_value > 0) && <HeatBadge sourceKey={a.source_key} heat={a.heat} value={a.heat_value} prevValue={a.prev_heat_value} />}
                        {topIdsBySource.has(a.id) && (
                          <span className="inline-flex shrink-0 items-center gap-0.5 rounded-full bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-400" title={`同源 Top ${topIdsBySource.get(a.id)}`}>
                            🏆<span className="font-semibold tabular-nums">{topIdsBySource.get(a.id)}</span>
                          </span>
                        )}
                        {isSurging(a) && (
                          <span className="inline-flex shrink-0 items-center rounded-full bg-orange-100 px-1.5 py-0.5 text-xs font-medium text-orange-700 dark:bg-orange-950 dark:text-orange-400" title="飙升">🚀</span>
                        )}
                      </div>
                      {a.content && <p className="mt-1.5 line-clamp-2 text-[13px] leading-relaxed text-zinc-600 dark:text-zinc-400">{a.content}</p>}
                      <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-500">
                        <span>{SOURCE_LABELS[a.source_key] ?? a.source_key}</span>
                        <span>{formatTime(a.published_at)}</span>
                      </div>
                    </div>
                  </Link>
                </li>
              );
            })}
          </ul>
        )
      )}

      {/* 加载更多 — 只在显示文章列表的视图(知乎/百度/微博 tab + 搜索)出现。
          全部 tab 是话题聚合视图,articles 仅用作分组输入,加载更多没意义。 */}
      {!isTopicView && hasMore && !error && (
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

      {toast && (
        <div role="status" className="fixed bottom-6 left-1/2 z-50 -translate-x-1/2 rounded-md bg-zinc-900 px-4 py-2 text-sm text-white shadow-lg dark:bg-zinc-100 dark:text-zinc-900">
          {toast}
        </div>
      )}
    </main>
  );
}

function windowedLatestPublishedMs(items: Article[]): number {
  let latest = 0;
  for (const it of items) {
    const ts = new Date(it.published_at).getTime();
    if (Number.isFinite(ts) && ts > latest) latest = ts;
  }
  return latest > 0 ? latest : 0;
}
