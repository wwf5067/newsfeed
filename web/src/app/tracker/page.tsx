"use client";

import { Suspense, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useIdSet, READ_KEY } from "@/lib/useIdSet";

type Subscription = {
  id: number;
  keyword: string;
};

type TrackerStorylineResp = {
  term: string;
  window: { hours: number };
  summary: string[];
  sources: { source_key: string; count: number }[];
  items: {
    id: number;
    title: string;
    source_key: string;
    heat: string;
    heat_value: number;
    published_at: string; // 后端返回的 ISO 时间,前端按本地时区分组
  }[];
  momentum: "up" | "flat" | "down";
  score_delta: number;
  new_count: number; // 窗口内新出现的文章数,用于 chip 展示"新增 N 条"
  total_articles: number;
};

type RelatedTrackerResp = {
  term: string;
  items: {
    label: string;
    kind: "entity" | "keyword";
    score: number;
    count: number;
    momentum: "up" | "flat" | "down";
  }[];
};

const SOURCE_LABELS: Record<string, string> = {
  zhihu_hot: "知乎",
  bilibili_popular: "B站",
};

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

// 时间窗口选项。0 表示"全部",后端 sinceHours=0 不限时间。
// 默认 2160 小时 = 90 天 = 现行 retention 上限,等同于"自部署以来"。
const WINDOW_OPTIONS: { hours: number; label: string }[] = [
  { hours: 24, label: "24 小时" },
  { hours: 168, label: "7 天" },
  { hours: 720, label: "30 天" },
  { hours: 2160, label: "90 天" },
  { hours: 0, label: "全部" },
];
const DEFAULT_WINDOW = 2160;

function windowLabel(hours: number): string {
  const opt = WINDOW_OPTIONS.find((o) => o.hours === hours);
  if (opt) return opt.label;
  return hours > 0 ? `${hours} 小时` : "全部";
}

// formatTimeShort 渲染条目右侧的时间(本地时区,HH:MM)。
function formatTimeShort(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
  } catch {
    return "";
  }
}

// formatDateShort 跨天后展示日期(MM-DD)。
function formatDateShort(iso: string): string {
  try {
    const d = new Date(iso);
    return `${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  } catch {
    return "";
  }
}

// 时间分组:今天 / 昨天 / 本周 / 更早。组内按 heat_value 倒序。
type TimeGroup = {
  key: string;
  label: string;
  items: TrackerStorylineResp["items"];
};

function groupByTime(items: TrackerStorylineResp["items"]): TimeGroup[] {
  if (items.length === 0) return [];
  const now = new Date();
  const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const yesterdayStart = todayStart - 24 * 3600 * 1000;
  const weekStart = todayStart - 7 * 24 * 3600 * 1000;

  const today: TimeGroup["items"] = [];
  const yesterday: TimeGroup["items"] = [];
  const week: TimeGroup["items"] = [];
  const older: TimeGroup["items"] = [];

  for (const it of items) {
    const t = new Date(it.published_at).getTime();
    if (t >= todayStart) today.push(it);
    else if (t >= yesterdayStart) yesterday.push(it);
    else if (t >= weekStart) week.push(it);
    else older.push(it);
  }

  // 组内按热度倒序;热度相等时按时间倒序
  const sortByHeat = (arr: TimeGroup["items"]) =>
    arr.sort((a, b) => {
      if (a.heat_value !== b.heat_value) return b.heat_value - a.heat_value;
      return new Date(b.published_at).getTime() - new Date(a.published_at).getTime();
    });

  const groups: TimeGroup[] = [];
  if (today.length) groups.push({ key: "today", label: "今天", items: sortByHeat(today) });
  if (yesterday.length) groups.push({ key: "yesterday", label: "昨天", items: sortByHeat(yesterday) });
  if (week.length) groups.push({ key: "week", label: "本周", items: sortByHeat(week) });
  if (older.length) groups.push({ key: "older", label: "更早", items: sortByHeat(older) });
  return groups;
}

async function fetchTrackerStoryline(term: string, windowHours: number): Promise<TrackerStorylineResp> {
  const params = new URLSearchParams({ term, window: String(windowHours) });
  const res = await fetch(`/api/v1/trackers/storyline?${params.toString()}`, { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: TrackerStorylineResp = await res.json();
  return {
    ...data,
    summary: data.summary ?? [],
    sources: data.sources ?? [],
    items: data.items ?? [],
  };
}

async function addSubscription(term: string): Promise<void> {
  const res = await fetch("/api/v1/subscriptions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ keyword: term }),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);

  const data: { created?: boolean } = await res.json();
  if (data.created === false) {
    throw new Error("already_subscribed");
  }
}

async function listSubscriptions(): Promise<Subscription[]> {
  const res = await fetch("/api/v1/subscriptions", { cache: "no-store" });
  if (res.status === 503) return [];
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: { items: Subscription[] } = await res.json();
  return data.items ?? [];
}

async function deleteSubscription(id: number): Promise<void> {
  const res = await fetch(`/api/v1/subscriptions/${id}`, { method: "DELETE" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
}

async function fetchRelatedTrackers(term: string, windowHours: number): Promise<RelatedTrackerResp> {
  const params = new URLSearchParams({ term, window: String(windowHours) });
  const res = await fetch(`/api/v1/trackers/related?${params.toString()}`, { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: RelatedTrackerResp = await res.json();
  return { ...data, items: data.items ?? [] };
}

function TrackerPageContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const term = (searchParams.get("term") ?? "").trim();
  // window 缺省时用 30 天(从详情页跳过来想看的就是"该实体所有相关文章")
  // window=0 含义是"全部时间"(后端 sinceHours=0 不加时间过滤),也是合法值
  const windowParam = searchParams.get("window");
  const windowHours = (() => {
    if (windowParam === null) return DEFAULT_WINDOW;
    const n = Number(windowParam);
    if (!Number.isFinite(n) || n < 0) return DEFAULT_WINDOW;
    return n;
  })();

  const [data, setData] = useState<TrackerStorylineResp | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [subscribeMsg, setSubscribeMsg] = useState<string | null>(null);
  const [related, setRelated] = useState<RelatedTrackerResp | null>(null);
  const [subscriptionId, setSubscriptionId] = useState<number | null>(null);

  // 跨页面共享的"已读" id 集合,跟首页用同一个 localStorage key
  const read = useIdSet(READ_KEY);

  useEffect(() => {
    if (!term) {
      setError("missing_term");
      setLoading(false);
      return;
    }

    let cancelled = false;
    setLoading(true);
    setError(null);
    setData(null);

    (async () => {
      try {
        const next = await fetchTrackerStoryline(term, windowHours);
        if (!cancelled) setData(next);
        const relatedData = await fetchRelatedTrackers(term, windowHours);
        if (!cancelled) setRelated(relatedData);
        const subscriptions = await listSubscriptions();
        if (!cancelled) {
          const existing = subscriptions.find((item) => item.keyword.toLowerCase() === term.toLowerCase());
          setSubscriptionId(existing?.id ?? null);
        }
      } catch (e) {
        if (!cancelled) setError(String(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [term, windowHours]);

  useEffect(() => {
    if (data?.term) {
      document.title = `${data.term} - Tracker - Newsfeed`;
    }
  }, [data]);

  const timeGroups = useMemo(() => groupByTime(data?.items ?? []), [data?.items]);

  async function handleSubscribe() {
    if (!data?.term || submitting) return;
    setSubmitting(true);
    setSubscribeMsg(null);
    try {
      if (subscriptionId) {
        await deleteSubscription(subscriptionId);
        setSubscriptionId(null);
        setSubscribeMsg(`已取消订阅「${data.term}」`);
      } else {
        await addSubscription(data.term);
        const subscriptions = await listSubscriptions();
        const existing = subscriptions.find((item) => item.keyword.toLowerCase() === data.term.toLowerCase());
        setSubscriptionId(existing?.id ?? null);
        setSubscribeMsg(`已订阅「${data.term}」`);
      }
    } catch (e) {
      if (String(e).includes("already_subscribed")) {
        const subscriptions = await listSubscriptions();
        const existing = subscriptions.find((item) => item.keyword.toLowerCase() === data.term.toLowerCase());
        setSubscriptionId(existing?.id ?? null);
        setSubscribeMsg(`「${data.term}」已经在订阅列表里`);
      } else {
        setSubscribeMsg(`订阅失败: ${String(e)}`);
      }
    } finally {
      setSubmitting(false);
    }
  }

  if (loading) {
    return (
      <main className="mx-auto w-full max-w-4xl px-4 py-8">
        <div className="text-sm text-zinc-500">正在整理时间线…</div>
      </main>
    );
  }

  if (error === "missing_term") {
    return (
      <main className="mx-auto w-full max-w-4xl px-4 py-8">
        <h1 className="mb-4 text-xl font-semibold">缺少追踪词</h1>
        <Link href="/" className="text-sm text-blue-600 hover:underline">
          ← 返回首页
        </Link>
      </main>
    );
  }

  if (error || !data) {
    return (
      <main className="mx-auto w-full max-w-4xl px-4 py-8">
        <div className="rounded-md border border-red-300 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-300">
          加载失败: {error}
        </div>
        <Link href="/" className="mt-4 inline-block text-sm text-blue-600 hover:underline">
          ← 返回首页
        </Link>
      </main>
    );
  }

  return (
    <main className="mx-auto w-full max-w-4xl px-4 py-8">
      <Link href="/" className="mb-6 inline-block text-sm text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100">
        ← 返回首页
      </Link>

      <section className="rounded-xl border border-zinc-200 bg-white p-6 shadow-sm dark:border-zinc-800 dark:bg-zinc-900">
        <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 className="text-2xl font-semibold text-zinc-900 dark:text-zinc-100">{data.term}</h1>
            <p className="mt-1 text-sm text-zinc-500">
              {windowLabel(data.window.hours)}内,共 {data.total_articles} 条相关文章
            </p>
          </div>
          <div className="flex flex-wrap gap-2 text-xs">
            {/* 窗口内真实热度变化 + 新增文章数。
                后端用 article_heat_snapshots 算"窗口起点 vs 当前"的真实增量,
                而非旧实现的"绝对热度累加",所以跟所选窗口对齐(24h 看 24h 内,
                30d 看 30d 内),而且能正常出现 down/flat。
                momentum: up 严格要求 score_delta>0 AND new_count>0;down 只要
                score_delta<0 即可;箭头自带方向所以不再单列 momentum 文字。 */}
            {data.score_delta !== 0 && (
              <span
                className={
                  "rounded-full px-3 py-1 " +
                  (data.score_delta > 0
                    ? "bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300"
                    : "bg-zinc-100 text-zinc-600 dark:bg-zinc-800 dark:text-zinc-400")
                }
                title={`${windowLabel(data.window.hours)}内,所有相关文章的热度净变化(基于 snapshot 首尾差)`}
              >
                {windowLabel(data.window.hours)} {formatSignedHeat(data.score_delta)}{" "}
                {data.score_delta > 0 ? "↑" : "↓"}
              </span>
            )}
            {data.new_count > 0 && (
              <span
                className="rounded-full bg-blue-100 px-3 py-1 text-blue-700 dark:bg-blue-950 dark:text-blue-300"
                title={`${windowLabel(data.window.hours)}内新出现的相关文章数`}
              >
                新增 {data.new_count} 条
              </span>
            )}
            <button
              type="button"
              onClick={handleSubscribe}
              disabled={submitting}
              className="rounded-full bg-zinc-900 px-3 py-1 text-white transition hover:bg-zinc-700 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
            >
              {submitting ? "处理中…" : subscriptionId ? `取消订阅「${data.term}」` : `订阅「${data.term}」`}
            </button>
          </div>
        </div>

        {/* 时间窗口 tab */}
        <div className="mb-5 flex flex-wrap gap-1">
          {WINDOW_OPTIONS.map((opt) => {
            const active = data.window.hours === opt.hours;
            return (
              <button
                key={opt.hours}
                type="button"
                onClick={() => {
                  if (active) return;
                  router.push(`/tracker?term=${encodeURIComponent(term)}&window=${opt.hours}`);
                }}
                className={
                  "rounded-md px-3 py-1 text-xs transition " +
                  (active
                    ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900"
                    : "bg-zinc-100 text-zinc-600 hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-400 dark:hover:bg-zinc-700")
                }
              >
                {opt.label}
              </button>
            );
          })}
        </div>

        {subscribeMsg && (
          <div className="mb-4 rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-600 dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-300">
            {subscribeMsg}
          </div>
        )}

        <div className="mb-6 space-y-2 text-sm leading-relaxed text-zinc-700 dark:text-zinc-300">
          {data.summary.map((line) => (
            <p key={line}>{line}</p>
          ))}
        </div>

        <div className="mb-6 flex flex-wrap gap-2 text-xs text-zinc-500">
          {data.sources.map((source) => (
            <span key={source.source_key} className="rounded-full bg-zinc-100 px-3 py-1 dark:bg-zinc-800">
              {SOURCE_LABELS[source.source_key] ?? source.source_key} {source.count}
            </span>
          ))}
        </div>

        <div className="border-t border-zinc-200 pt-6 dark:border-zinc-800">
          {data.items.length === 0 ? (
            <div className="rounded-md border border-dashed border-zinc-300 p-8 text-center text-sm text-zinc-500 dark:border-zinc-700">
              {windowLabel(data.window.hours)}内没有「{data.term}」的相关内容。
              {data.window.hours !== 0 && (
                <>
                  {" "}
                  <button
                    type="button"
                    onClick={() => router.push(`/tracker?term=${encodeURIComponent(term)}&window=0`)}
                    className="text-blue-600 hover:underline dark:text-blue-400"
                  >
                    试试"全部"时间范围 →
                  </button>
                </>
              )}
            </div>
          ) : (
            <div className="space-y-6">
              {timeGroups.map((g) => (
                <div key={g.key}>
                  <div className="mb-2 flex items-center gap-2">
                    <h3 className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">{g.label}</h3>
                    <span className="text-xs text-zinc-400">{g.items.length} 条</span>
                  </div>
                  <div className="space-y-2">
                    {g.items.map((item) => {
                      const isRead = read.ids.has(item.id);
                      return (
                        <Link
                          key={item.id}
                          href={`/article?id=${item.id}`}
                          onClick={() => read.add(item.id)}
                          className={
                            "flex items-start gap-3 rounded-lg border border-zinc-200 bg-zinc-50 px-4 py-3 transition hover:border-zinc-300 hover:bg-white dark:border-zinc-800 dark:bg-zinc-950 dark:hover:border-zinc-700 dark:hover:bg-zinc-900" +
                            (isRead ? " opacity-60" : "")
                          }
                        >
                          <div className="min-w-0 flex-1">
                            <div
                              className={
                                "text-sm font-medium leading-snug " +
                                (isRead
                                  ? "text-zinc-500 dark:text-zinc-500"
                                  : "text-zinc-900 dark:text-zinc-100")
                              }
                            >
                              {item.title}
                            </div>
                            <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-zinc-500">
                              <span>{SOURCE_LABELS[item.source_key] ?? item.source_key}</span>
                              <span>
                                {g.key === "today" || g.key === "yesterday"
                                  ? formatTimeShort(item.published_at)
                                  : formatDateShort(item.published_at)}
                              </span>
                              {(item.heat || item.heat_value > 0) && (
                                <span className="font-medium text-red-500 tabular-nums dark:text-red-400">
                                  {item.heat || formatHeat(item.heat_value)}
                                </span>
                              )}
                            </div>
                          </div>
                        </Link>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {related && related.items.length > 0 && (
          <div className="mt-8 border-t border-zinc-200 pt-6 dark:border-zinc-800">
            <h2 className="mb-3 text-sm font-semibold text-zinc-900 dark:text-zinc-100">关联话题</h2>
            <div className="flex flex-wrap gap-2">
              {related.items.map((item) => (
                <Link
                  key={`${item.kind}:${item.label}`}
                  href={`/tracker?term=${encodeURIComponent(item.label)}&window=${data.window.hours}`}
                  className="rounded-full bg-zinc-100 px-3 py-1 text-xs text-zinc-700 transition hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-300 dark:hover:bg-zinc-700"
                >
                  {item.label} · {item.count} 条
                </Link>
              ))}
            </div>
          </div>
        )}
      </section>
    </main>
  );
}

export default function TrackerPage() {
  return (
    <Suspense
      fallback={
        <main className="mx-auto w-full max-w-4xl px-4 py-8">
          <div className="text-sm text-zinc-500">加载中…</div>
        </main>
      }
    >
      <TrackerPageContent />
    </Suspense>
  );
}
