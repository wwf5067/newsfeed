"use client";

// 文章详情页。
//
// 历史:之前用 /article/[id] 动态路由,但 Next.js `output: export` 静态导出模式下
// 动态路由必须实现 generateStaticParams() 来枚举所有要预渲染的 id。我们的 article id
// 来自 DB 运行时数据,无法在构建期枚举,所以走 query string `/article?id=123` 方案,
// 让所有详情页共用同一个静态 HTML,客户端渲染时按 ?id= 拉数据。
//
// 副作用:useSearchParams 在 Next.js 静态导出下要求外层 <Suspense>,见 default export。

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
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

type HeatPoint = {
  heat_value: number;
  captured_at: string;
};

const SOURCE_LABELS: Record<string, string> = {
  zhihu_hot: "知乎",
  bilibili_popular: "B站",
};

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString("zh-CN", { hour12: false });
  } catch {
    return iso;
  }
}

async function addSubscription(term: string): Promise<{ created?: boolean }> {
  const res = await fetch("/api/v1/subscriptions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ keyword: term }),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

// Sparkline 用纯 SVG 画热度时序。30 行代码,不引图表库。
// width/height 固定,viewBox 让线条按数据范围自适应。
function Sparkline({ points }: { points: HeatPoint[] }) {
  if (points.length < 2) {
    return (
      <div className="text-xs text-zinc-500">
        {points.length === 0 ? "暂无热度历史数据" : "数据点不足,需要至少 2 次抓取"}
      </div>
    );
  }
  const W = 600;
  const H = 60;
  const PAD = 4;
  const values = points.map((p) => p.heat_value);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = max - min || 1; // 防除零
  const stepX = (W - PAD * 2) / (points.length - 1);
  const path = points
    .map((p, i) => {
      const x = PAD + i * stepX;
      const y = H - PAD - ((p.heat_value - min) / range) * (H - PAD * 2);
      return `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
  const last = points[points.length - 1];
  const first = points[0];
  const trend = last.heat_value - first.heat_value;
  const up = trend > 0;
  return (
    <div>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="w-full"
        preserveAspectRatio="none"
        aria-label="热度时序"
      >
        <path
          d={path}
          fill="none"
          stroke={up ? "#10b981" : "#71717a"}
          strokeWidth="1.5"
          vectorEffect="non-scaling-stroke"
        />
      </svg>
      <div className="mt-1 flex justify-between text-xs text-zinc-500">
        <span>{new Date(first.captured_at).toLocaleString("zh-CN", { hour12: false })}</span>
        <span className={up ? "text-emerald-600" : "text-zinc-500"}>
          {up ? "↑" : trend < 0 ? "↓" : "·"} {Math.abs(trend).toLocaleString()}
        </span>
        <span>{new Date(last.captured_at).toLocaleString("zh-CN", { hour12: false })}</span>
      </div>
    </div>
  );
}

function ArticleContent() {
  const searchParams = useSearchParams();
  const id = searchParams.get("id");
  const [article, setArticle] = useState<Article | null>(null);
  const [history, setHistory] = useState<HeatPoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [subscribeMsg, setSubscribeMsg] = useState<string | null>(null);

  useEffect(() => {
    if (!id) {
      setError("missing_id");
      setLoading(false);
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const [aRes, hRes] = await Promise.all([
          fetch(`/api/v1/articles/${id}`, { cache: "no-store" }),
          fetch(`/api/v1/articles/${id}/heat-history?limit=48`, { cache: "no-store" }),
        ]);
        if (aRes.status === 404) {
          if (!cancelled) setError("not_found");
          return;
        }
        if (!aRes.ok) throw new Error(`HTTP ${aRes.status}`);
        const data: Article = await aRes.json();
        if (!cancelled) setArticle(data);
        // heat history 失败不阻塞文章展示
        if (hRes.ok) {
          const hData: { items: HeatPoint[] } = await hRes.json();
          if (!cancelled) setHistory(hData.items ?? []);
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
  }, [id]);

  // 动态设置标签页 title(静态导出限制,无法用 generateMetadata,改用 client-side)
  useEffect(() => {
    if (article) {
      document.title = `${article.title} - Newsfeed`;
    }
  }, [article]);

  if (loading) {
    return (
      <main className="mx-auto w-full max-w-3xl px-4 py-8">
        <div className="text-sm text-zinc-500">加载中…</div>
      </main>
    );
  }

  if (error === "missing_id") {
    return (
      <main className="mx-auto w-full max-w-3xl px-4 py-8">
        <h1 className="mb-4 text-xl font-semibold">缺少文章 id</h1>
        <p className="mb-4 text-sm text-zinc-500">URL 应为 /article?id=123 形式。</p>
        <Link href="/" className="text-sm text-blue-600 hover:underline">
          ← 返回首页
        </Link>
      </main>
    );
  }

  if (error === "not_found") {
    return (
      <main className="mx-auto w-full max-w-3xl px-4 py-8">
        <h1 className="mb-4 text-xl font-semibold">未找到该文章</h1>
        <p className="mb-4 text-sm text-zinc-500">它可能已经过期被清理了。</p>
        <Link href="/" className="text-sm text-blue-600 hover:underline">
          ← 返回首页
        </Link>
      </main>
    );
  }

  if (error || !article) {
    return (
      <main className="mx-auto w-full max-w-3xl px-4 py-8">
        <div className="rounded-md border border-red-300 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-300">
          加载失败:{error}
        </div>
        <Link href="/" className="mt-4 inline-block text-sm text-blue-600 hover:underline">
          ← 返回首页
        </Link>
      </main>
    );
  }

  const sourceLabel = SOURCE_LABELS[article.source_key] ?? article.source_key;

  async function handleSubscribeTitle() {
    if (submitting) return;
    setSubmitting(true);
    setSubscribeMsg(null);
    try {
      const result = await addSubscription(article!.title);
      if (result.created === false) {
        setSubscribeMsg("该标题已经在订阅列表里");
      } else {
        setSubscribeMsg("已按当前标题加入订阅");
      }
    } catch (e) {
      setSubscribeMsg(`订阅失败: ${String(e)}`);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-8">
      <Link
        href="/"
        className="mb-6 inline-block text-sm text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
      >
        ← 返回首页
      </Link>

      <article className="rounded-lg border border-zinc-200 bg-white p-6 shadow-sm dark:border-zinc-800 dark:bg-zinc-900">
        <h1 className="mb-3 text-2xl font-semibold leading-tight text-zinc-900 dark:text-zinc-100">
          {article.title}
        </h1>

        <div className="mb-4 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-zinc-500">
          <span>{sourceLabel}</span>
          {article.author && <span>· {article.author}</span>}
          {article.heat && (
            <span className="rounded-full bg-red-50 px-2 py-0.5 text-xs font-medium text-red-600 dark:bg-red-950 dark:text-red-400">
              {article.heat}
            </span>
          )}
          <span>· {formatTime(article.published_at)}</span>
        </div>

        <div className="mb-6 flex flex-wrap gap-2">
          <button
            type="button"
            onClick={handleSubscribeTitle}
            disabled={submitting}
            className="rounded-full bg-zinc-900 px-3 py-1 text-xs text-white transition hover:bg-zinc-700 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
          >
            {submitting ? "订阅中…" : "订阅这个标题"}
          </button>
          <Link
            href={`/tracker?term=${encodeURIComponent(article.title)}&window=24`}
            className="rounded-full bg-amber-100 px-3 py-1 text-xs text-amber-700 transition hover:bg-amber-200 dark:bg-amber-950 dark:text-amber-300 dark:hover:bg-amber-900"
          >
            查看标题时间线
          </Link>
        </div>

        {subscribeMsg && (
          <div className="mb-6 rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm text-zinc-600 dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-300">
            {subscribeMsg}
          </div>
        )}

        {article.content && (
          <p className="mb-6 whitespace-pre-wrap text-base leading-relaxed text-zinc-700 dark:text-zinc-300">
            {article.content}
          </p>
        )}

        <div className="mb-6 border-t border-zinc-200 pt-4 dark:border-zinc-800">
          <h2 className="mb-3 text-sm font-medium text-zinc-700 dark:text-zinc-300">
            热度趋势
          </h2>
          <Sparkline points={history} />
        </div>

        <div className="border-t border-zinc-200 pt-4 dark:border-zinc-800">
          {/* 长 URL 在窄屏(尤其是手机)上不换行会溢出容器,
              break-all 保证任意位置都能断;按钮宽度自适应内容,
              加 max-w-full 防 flex 父容器下被撑出。*/}
          <a
            href={article.url}
            target="_blank"
            rel="noreferrer"
            className="inline-block max-w-full break-all rounded-md bg-zinc-900 px-4 py-2 text-sm text-white transition hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
          >
            访问原文 →
          </a>
          <div className="mt-2 break-all text-xs text-zinc-400 dark:text-zinc-600">
            {article.url}
          </div>
        </div>
      </article>
    </main>
  );
}

// useSearchParams 在 Next.js 静态导出 + App Router 下要求外层 <Suspense>,
// 否则编译期会报 "useSearchParams() should be wrapped in a suspense boundary"。
export default function ArticlePage() {
  return (
    <Suspense
      fallback={
        <main className="mx-auto w-full max-w-3xl px-4 py-8">
          <div className="text-sm text-zinc-500">加载中…</div>
        </main>
      }
    >
      <ArticleContent />
    </Suspense>
  );
}
