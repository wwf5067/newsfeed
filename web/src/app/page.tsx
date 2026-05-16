"use client";

import { useEffect, useState } from "react";

type Article = {
  id: number;
  source_key: string;
  url: string;
  title: string;
  content: string;
  author: string;
  published_at: string;
  fetched_at: string;
};

type ListResp = {
  items: Article[];
  limit: number;
  offset: number;
};

// 静态导出时不能用 SSR,所以走 client fetch。
// 开发环境由 next.config rewrites 代理到 localhost:8080,
// 生产环境由 Nginx 反代,前端永远访问相对路径 /api/v1/*。
async function fetchArticles(): Promise<Article[]> {
  const res = await fetch("/api/v1/articles?limit=50", { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data: ListResp = await res.json();
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

export default function Home() {
  const [articles, setArticles] = useState<Article[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetchArticles()
      .then((items) => setArticles(items))
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false));
  }, []);

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-8">
      <header className="mb-6 flex items-baseline justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">Newsfeed</h1>
        <span className="text-sm text-zinc-500">
          {loading ? "加载中…" : `共 ${articles.length} 条`}
        </span>
      </header>

      {error && (
        <div className="mb-6 rounded-md border border-red-300 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-300">
          加载失败: {error}
          <div className="mt-1 text-xs opacity-70">
            请确认 Go API 服务正在 :8080 运行(make run-api)
          </div>
        </div>
      )}

      <ul className="space-y-3">
        {articles.map((a, i) => (
          <li
            key={a.id}
            className="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm transition hover:shadow-md dark:border-zinc-800 dark:bg-zinc-900"
          >
            <a
              href={a.url}
              target="_blank"
              rel="noreferrer"
              className="flex gap-3"
            >
              <span className="shrink-0 select-none font-mono text-sm text-zinc-400 tabular-nums">
                {String(i + 1).padStart(2, "0")}
              </span>
              <div className="min-w-0 flex-1">
                <h2 className="text-base font-medium leading-snug text-zinc-900 group-hover:underline dark:text-zinc-100">
                  {a.title}
                </h2>
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
        ))}
      </ul>
    </main>
  );
}
