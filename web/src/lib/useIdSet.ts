"use client";

import { useCallback, useEffect, useState } from "react";

// localStorage 共享 key,首页/实体页/详情页都用同一个 key,达成跨页面已读/收藏一致。
export const READ_KEY = "read_ids";
export const STARRED_KEY = "starred_ids";

// useIdSet 把 localStorage 里的 number[] 暴露成 Set,提供 has / add / toggle 三个操作。
// 已读、收藏共用同一份逻辑,storageKey 不同即可。
//
// 实现要点:
// - useEffect 里读 localStorage,SSR 时 useState 初始为空 Set 不会 crash
// - 写入用 setIds 函数式更新 + 同步 persist,避免并发写丢失
// - 任意 localStorage 异常(隐私模式 / 配额满 / 非法 JSON)静默忽略,内存态仍可用
export function useIdSet(storageKey: string) {
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
      /* ignore */
    }
  }, [storageKey]);

  const persist = useCallback(
    (next: Set<number>) => {
      try {
        localStorage.setItem(storageKey, JSON.stringify([...next]));
      } catch {
        /* ignore */
      }
    },
    [storageKey],
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
    [persist],
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
    [persist],
  );

  return { ids, add, toggle };
}
