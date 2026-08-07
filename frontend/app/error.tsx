"use client";

import * as React from "react";
import Link from "next/link";
import { useTranslations } from "next-intl";

import { CenteredEmptyState } from "@/components/ui/empty-state";

// 全局错误边界（App Router error.tsx，位于根布局之下、所有 Provider 之内）。
// 客户端渲染错误在应用内恢复为可操作界面，避免 Next.js 回退为整页刷新；
// 「重试」只重新渲染当前段，不刷新页面。根布局自身错误由 global-error.tsx 处理。
export default function GlobalAppError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  // errorPage 文案定义在 common.json 内，命名空间需带 common 前缀（messages.common.errorPage）。
  const t = useTranslations("common.errorPage");

  React.useEffect(() => {
    // digest 可关联服务端日志；错误被边界捕获后用户无需刷新即可恢复。
    console.error("[app-error] digest:", error.digest, error);
  }, [error]);

  return (
    <CenteredEmptyState
      title={t("title")}
      description={
        <>
          {t("prefix")}{" "}
          <button
            type="button"
            className="rounded-sm font-medium text-foreground underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={() => reset()}
          >
            {t("retry")}
          </button>{" "}
          {t("or")}{" "}
          <Link
            href="/chat"
            className="rounded-sm font-medium text-foreground underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {t("backToChat")}
          </Link>
        </>
      }
    />
  );
}
