"use client";

import { useEffect, useState, use } from "react";
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

export default function ArticlePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const [article, setArticle] = useState<Article | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch(`/api/v1/articles/${id}`, { cache: "no-store" });
        if (res.status === 404) {
          if (!cancelled) setError("not_found");
          return;
        }
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data: Article = await res.json();
        if (!cancelled) setArticle(data);
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

        {article.content && (
          <p className="mb-6 whitespace-pre-wrap text-base leading-relaxed text-zinc-700 dark:text-zinc-300">
            {article.content}
          </p>
        )}

        <div className="border-t border-zinc-200 pt-4 dark:border-zinc-800">
          <a
            href={article.url}
            target="_blank"
            rel="noreferrer"
            className="inline-block rounded-md bg-zinc-900 px-4 py-2 text-sm text-white transition hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
          >
            访问原文 →
          </a>
        </div>
      </article>
    </main>
  );
}
