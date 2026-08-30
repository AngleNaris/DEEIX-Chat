"use client";

import { Download, FileX, LoaderCircle } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import * as React from "react";

import { Button } from "@/components/ui/button";
import {
  fetchPublicFileShareContent,
  getPublicFileShare,
  type PublicFileShareDTO,
} from "@/shared/api/file";
import {
  FilePreviewBody,
  type PreviewDialogFile,
  useFilePreviewContent,
} from "@/shared/components/file-preview/preview-dialog";
import { formatBytes } from "@/shared/lib/file-display";

export function PublicFilePage() {
  const t = useTranslations("files.sharePage");
  const shareID = useSearchParams().get("share_id")?.trim() ?? "";
  const [data, setData] = React.useState<PublicFileShareDTO | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [notFound, setNotFound] = React.useState(false);

  React.useEffect(() => {
    if (!shareID) {
      setLoading(false);
      setNotFound(true);
      return;
    }
    let cancelled = false;
    void getPublicFileShare(shareID)
      .then((result) => {
        if (!cancelled) setData(result);
      })
      .catch(() => {
        if (!cancelled) setNotFound(true);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [shareID]);

  const file = React.useMemo<PreviewDialogFile | null>(() => data ? {
    fileID: data.file_id,
    fileName: data.file_name,
    mimeType: data.mime_type,
    sizeBytes: data.size_bytes,
  } : null, [data]);
  const loadContent = React.useCallback(() => fetchPublicFileShareContent(shareID), [shareID]);
  const { state, download } = useFilePreviewContent(file, loadContent);

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <LoaderCircle className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (notFound || !data || !file) {
    return (
      <div className="flex min-h-screen items-center justify-center p-6 text-center">
        <div>
          <FileX className="mx-auto size-8 text-muted-foreground" />
          <p className="mt-3 text-sm text-muted-foreground">{t("notFound")}</p>
        </div>
      </div>
    );
  }

  return (
    <main className="flex h-full min-h-screen w-full min-w-0 flex-col overflow-hidden p-3 sm:p-5 md:p-8">
      <header className="flex min-w-0 shrink-0 items-center gap-3 border-b border-border/50 pb-3">
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-sm font-medium text-foreground">{data.file_name}</h1>
          <p className="mt-0.5 text-[11px] text-muted-foreground">
            {data.mime_type || t("unknownType")} · {formatBytes(data.size_bytes)} · {data.expires_at
              ? t("expiresAt", { value: new Date(data.expires_at).toLocaleString() })
              : t("neverExpires")}
          </p>
        </div>
        <Button type="button" size="sm" variant="outline" disabled={state.status !== "ready"} onClick={download}>
          <Download className="size-3.5" />
          {t("download")}
        </Button>
      </header>

      <section className="min-h-0 flex-1 overflow-auto pt-3" aria-label={t("preview")}>
        <div className="min-h-full border border-border/60 bg-background p-3 sm:p-5">
          <FilePreviewBody file={file} state={state} onDownload={download} />
        </div>
      </section>
    </main>
  );
}
