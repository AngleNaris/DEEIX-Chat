"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { useSearchParams } from "next/navigation";
import { FileCode2, Loader2, Maximize2, Minimize2 } from "lucide-react";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { getSharedArtifact, type PublicSharedArtifactDTO } from "@/shared/api/artifacts";

type PreviewWidth = "full" | "fixed";

/**
 * PublicArtifactPage 公开制品分享页（免登录）：HTML 制品用 sandbox iframe 渲染，
 * JS/CSS/文本展示源码。预览宽度支持全宽/固定宽度居中切换，
 * 初始宽度取分享链接中的 preview_width 参数（分享者设置的默认）。
 */
export function PublicArtifactPage() {
  const t = useTranslations("share.artifact");
  const searchParams = useSearchParams();
  const shareId = searchParams.get("artifact_id") ?? "";
  const [data, setData] = React.useState<PublicSharedArtifactDTO | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [notFound, setNotFound] = React.useState(false);
  const [previewWidth, setPreviewWidth] = React.useState<PreviewWidth>(() =>
    searchParams.get("preview_width") === "fixed" ? "fixed" : "full",
  );

  React.useEffect(() => {
    if (!shareId) {
      setNotFound(true);
      setLoading(false);
      return;
    }
    let cancelled = false;
    void (async () => {
      try {
        const result = await getSharedArtifact(shareId);
        if (!cancelled) setData(result);
      } catch {
        if (!cancelled) setNotFound(true);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [shareId]);

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Loader2 className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (notFound || !data) {
    return (
      <div className="flex min-h-screen items-center justify-center p-6">
        <div className="text-center">
          <FileCode2 className="mx-auto size-8 text-muted-foreground" />
          <p className="mt-3 text-sm text-muted-foreground">{t("notFound")}</p>
        </div>
      </div>
    );
  }

  return (
    <main className="mx-auto flex min-h-screen w-full max-w-4xl flex-col gap-4 p-4 md:p-8">
      <header className="flex flex-wrap items-center gap-2">
        <FileCode2 className="size-4 text-muted-foreground" />
        <h1 className="min-w-0 flex-1 truncate text-base font-semibold">{data.title}</h1>
        <span className="rounded-sm bg-muted/70 px-1.5 py-0.5 text-[11px] font-medium uppercase text-muted-foreground">
          {data.kind}
        </span>
        <span className="text-[11px] text-muted-foreground">{data.created_at}</span>
      </header>

      {data.kind === "html" ? (
        <Tabs defaultValue="preview" className="flex min-h-0 flex-1 flex-col">
          <div className="flex items-center justify-between gap-2">
            <TabsList className="w-fit">
              <TabsTrigger value="preview">{t("preview")}</TabsTrigger>
              <TabsTrigger value="source">{t("source")}</TabsTrigger>
            </TabsList>
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  aria-label={previewWidth === "full" ? t("previewWidthFixed") : t("previewWidthFull")}
                  onClick={() => setPreviewWidth((prev) => (prev === "full" ? "fixed" : "full"))}
                >
                  {previewWidth === "full" ? (
                    <Minimize2 className="size-3" />
                  ) : (
                    <Maximize2 className="size-3" />
                  )}
                </button>
              </TooltipTrigger>
              <TooltipContent side="bottom">
                {previewWidth === "full" ? t("previewWidthFixed") : t("previewWidthFull")}
              </TooltipContent>
            </Tooltip>
          </div>
          <TabsContent value="preview" className="min-h-0 flex-1">
            <iframe
              title={data.title}
              sandbox="allow-scripts"
              srcDoc={data.code}
              className={
                previewWidth === "fixed"
                  ? "mx-auto block h-[70vh] w-full max-w-3xl rounded-lg border border-border/60 bg-white"
                  : "h-[70vh] w-full rounded-lg border border-border/60 bg-white"
              }
            />
          </TabsContent>
          <TabsContent value="source" className="min-h-0 flex-1">
            <pre className="h-[70vh] w-full overflow-auto rounded-lg border border-border/60 bg-muted/30 p-4 text-xs leading-relaxed">
              {data.code}
            </pre>
          </TabsContent>
        </Tabs>
      ) : (
        <pre className="h-[70vh] w-full overflow-auto rounded-lg border border-border/60 bg-muted/30 p-4 text-xs leading-relaxed">
          {data.code}
        </pre>
      )}
    </main>
  );
}
