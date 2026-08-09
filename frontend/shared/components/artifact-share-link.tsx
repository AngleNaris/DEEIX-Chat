"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { ExternalLink } from "lucide-react";

import { CopyActionButton } from "@/shared/components/copy-action";
import {
  artifactShareUrl,
  type ArtifactPreviewWidth,
  type ArtifactShareDTO,
} from "@/shared/api/artifacts";

/**
 * ArtifactShareLink 分享链接区块：预览宽度选择（全宽/固定宽度居中）+ 绝对链接 + 复制 + 打开。
 * 宽度选择会实时拼进分享 URL（preview_width 参数），作为分享页打开时的默认预览宽度。
 */
export function ArtifactShareLink({
  share,
  disabled = false,
}: {
  share: ArtifactShareDTO | null;
  disabled?: boolean;
}) {
  const t = useTranslations("chat.artifacts");
  const [previewWidth, setPreviewWidth] = React.useState<ArtifactPreviewWidth>("full");

  if (!share) {
    return null;
  }

  const url = artifactShareUrl(share.share_id, previewWidth);

  return (
    <div className="min-w-0 space-y-2">
      <div className="space-y-1">
        <p className="text-xs text-muted-foreground">{t("previewWidth")}</p>
        <div className="flex flex-wrap gap-1">
          {(["full", "fixed"] as const).map((width) => (
            <button
              key={width}
              type="button"
              disabled={disabled}
              className={
                previewWidth === width
                  ? "inline-flex h-7 items-center rounded-md bg-foreground/10 px-2.5 text-xs font-medium text-foreground transition-colors"
                  : "inline-flex h-7 items-center rounded-md px-2.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              }
              onClick={() => setPreviewWidth(width)}
            >
              {width === "full" ? t("previewWidthFull") : t("previewWidthFixed")}
            </button>
          ))}
        </div>
      </div>
      <div className="flex min-w-0 items-center gap-2 overflow-hidden rounded-md bg-muted/30 px-2.5 py-2">
        <span dir="ltr" className="min-w-0 flex-1 truncate text-[11px] text-muted-foreground">
          {url}
        </span>
        <CopyActionButton
          value={url}
          messages={{ copied: t("linkCopied"), failed: t("copyFailed") }}
          iconClassName="size-3"
        />
        <a
          href={url}
          target="_blank"
          rel="noreferrer"
          className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          aria-label={t("openLink")}
        >
          <ExternalLink className="size-3" />
        </a>
      </div>
    </div>
  );
}
