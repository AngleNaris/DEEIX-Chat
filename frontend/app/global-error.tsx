"use client";

import * as React from "react";

// 根布局错误边界（global-error.tsx）。根布局出错时 Provider 全部不可用，
// 必须自带 <html>/<body> 且不能依赖任何 Provider，因此使用内置双语文案。
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  React.useEffect(() => {
    console.error("[global-error] digest:", error.digest, error);
  }, [error]);

  return (
    <html lang="en" className="h-full">
      <body className="flex h-full min-h-svh items-center justify-center bg-background px-6 antialiased">
        <div className="flex max-w-md flex-col items-center gap-3 text-center">
          <p className="text-sm font-medium text-foreground">页面加载失败 / Something went wrong</p>
          <p className="text-xs text-muted-foreground">请点击重试恢复页面 / Click retry to recover</p>
          <button
            type="button"
            className="mt-1 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={() => reset()}
          >
            重试 / Retry
          </button>
        </div>
      </body>
    </html>
  );
}
