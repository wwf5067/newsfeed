"use client";

import { Suspense, useEffect, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";

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
  }[];
  momentum: "up" | "flat" | "down";
  score_delta: number;
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
  const searchParams = useSearchParams();
  const term = (searchParams.get("term") ?? "").trim();
  const windowValue = Number(searchParams.get("window") ?? "24");
  const windowHours = Number.isFinite(windowValue) && windowValue > 0 ? windowValue : 24;

  const [data, setData] = useState<TrackerStorylineResp | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [subscribeMsg, setSubscribeMsg] = useState<string | null>(null);
  const [related, setRelated] = useState<RelatedTrackerResp | null>(null);
  const [subscriptionId, setSubscriptionId] = useState<number | null>(null);

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
              最近 {data.window.hours} 小时，共 {data.total_articles} 条相关文章
            </p>
          </div>
          <div className="flex flex-wrap gap-2 text-xs">
            <span className="rounded-full bg-zinc-100 px-3 py-1 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300">
              {data.momentum === "up" ? "升温" : data.momentum === "down" ? "回落" : "持平"}
            </span>
            {data.score_delta !== 0 && (
              <span className="rounded-full bg-emerald-100 px-3 py-1 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300">
                聚合热度 {formatSignedHeat(data.score_delta)}
              </span>
            )}
            <Link
              href={`/?q=${encodeURIComponent(data.term)}`}
              className="rounded-full bg-amber-100 px-3 py-1 text-amber-700 transition hover:bg-amber-200 dark:bg-amber-950 dark:text-amber-300 dark:hover:bg-amber-900"
            >
              查看全部相关文章
            </Link>
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

        <div className="space-y-3 border-t border-zinc-200 pt-6 dark:border-zinc-800">
          {data.items.map((item, index) => (
            <Link
              key={item.id}
              href={`/article?id=${item.id}`}
              className="block rounded-lg border border-zinc-200 bg-zinc-50 px-4 py-3 transition hover:border-zinc-300 hover:bg-white dark:border-zinc-800 dark:bg-zinc-950 dark:hover:border-zinc-700 dark:hover:bg-zinc-900"
            >
              <div className="flex items-start gap-3">
                <span className="pt-0.5 font-mono text-xs text-zinc-400">{String(index + 1).padStart(2, "0")}</span>
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium text-zinc-900 dark:text-zinc-100">{item.title}</div>
                  <div className="mt-1 flex flex-wrap gap-2 text-[11px] text-zinc-500">
                    <span>{SOURCE_LABELS[item.source_key] ?? item.source_key}</span>
                    {(item.heat || item.heat_value > 0) && <span>{item.heat || formatHeat(item.heat_value)}</span>}
                  </div>
                </div>
              </div>
            </Link>
          ))}
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
