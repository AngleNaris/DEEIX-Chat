"use client";

import { ExternalLink, FileCode2, Loader2 } from "lucide-react";
import { useTranslations } from "next-intl";
import * as React from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  type ArtifactPreviewKind,
  buildArtifactPreviewDocument,
} from "@/features/chat/model/chat-artifacts";
import { useLocalizedErrorMessage } from "@/i18n/use-localized-error";
import {
  type ArtifactDetailDTO,
  getArtifact,
} from "@/shared/api/artifacts";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { CopyActionButton } from "@/shared/components/copy-action";
import { useTheme } from "@/shared/components/theme-provider";
import { resolveStoredArtifactPreviewKind } from "@/shared/lib/artifact-preview";
import {
  captureHTMLVisualThemeSnapshot,
  type HTMLVisualThemeSnapshot,
} from "@/shared/lib/html-visual-theme";

// 与 chat-artifact.tsx 的 ARTIFACT_IFRAME_PERMISSIONS 保持一致：沙箱 iframe 的 allow 属性。
export const ARTIFACT_IFRAME_PERMISSIONS = [
  "accelerometer 'none'",
  "autoplay 'none'",
  "camera 'none'",
  "clipboard-read 'none'",
  "clipboard-write 'none'",
  "encrypted-media 'none'",
  "fullscreen 'none'",
  "geolocation 'none'",
  "gyroscope 'none'",
  "microphone 'none'",
  "midi 'none'",
  "payment 'none'",
  "serial 'none'",
  "usb 'none'",
  "bluetooth 'none'",
].join("; ");

type ArtifactPreviewDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  artifactId: string | null;
  title: string;
  shareUrl?: string | null;
};

/**
 * ArtifactPreviewDialog 制品详情预览弹窗：无需分享即可直接查看。
 * html/css/js 通过 buildArtifactPreviewDocument 在沙箱 iframe 中渲染；text 以纯文本代码视图展示。
 */
export function ArtifactPreviewDialog({
  open,
  onOpenChange,
  artifactId,
  title,
  shareUrl = null,
}: ArtifactPreviewDialogProps) {
  const t = useTranslations("settings.chatPage.artifacts");
  const resolveErrorMessage = useLocalizedErrorMessage();
  const { resolvedTheme } = useTheme();
  const [detail, setDetail] = React.useState<ArtifactDetailDTO | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [loadError, setLoadError] = React.useState<string | null>(null);
  const [themeSnapshot, setThemeSnapshot] = React.useState<HTMLVisualThemeSnapshot>({
    colorScheme: "light",
    variables: [],
  });
  const loadedIDRef = React.useRef<string | null>(null);
  const requestIDRef = React.useRef(0);

  React.useEffect(() => {
    setThemeSnapshot(captureHTMLVisualThemeSnapshot(resolvedTheme));
  }, [resolvedTheme]);

  const loadDetail = React.useCallback(
    async (id: string) => {
      const requestID = ++requestIDRef.current;
      setLoading(true);
      setLoadError(null);
      try {
        const token = await resolveAccessToken();
        if (!token) {
          if (requestIDRef.current === requestID) {
            setLoadError(t("viewDialogLoadFailed"));
          }
          return;
        }
        const result = await getArtifact(token, id);
        if (requestIDRef.current === requestID) {
          setDetail(result);
          loadedIDRef.current = id;
        }
      } catch (error) {
        if (requestIDRef.current === requestID) {
          setLoadError(resolveErrorMessage(error) || t("viewDialogLoadFailed"));
        }
      } finally {
        if (requestIDRef.current === requestID) {
          setLoading(false);
        }
      }
    },
    [resolveErrorMessage, t],
  );

  // 打开时按 artifactId 拉取详情；重复打开同一制品复用已加载结果，切换制品时重置并重新拉取。
  React.useEffect(() => {
    if (!open || !artifactId) {
      return;
    }
    if (loadedIDRef.current === artifactId) {
      return;
    }
    setDetail(null);
    void loadDetail(artifactId);
  }, [artifactId, loadDetail, open]);

  const previewKind: ArtifactPreviewKind | null = detail
    ? resolveStoredArtifactPreviewKind(detail.kind)
    : null;

  const previewHTML = React.useMemo(() => {
    if (!detail || !previewKind) {
      return "";
    }
    return buildArtifactPreviewDocument(previewKind, detail.code, themeSnapshot);
  }, [detail, previewKind, themeSnapshot]);

  const isText = Boolean(detail && !previewKind);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[calc(100svh-2rem)] max-h-none w-[min(920px,calc(100%-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-[920px]">
        <DialogHeader className="shrink-0 gap-1 border-b border-border/60 px-5 py-4">
          <div className="flex min-w-0 items-center gap-2">
            <DialogTitle className="min-w-0 flex-1 truncate">{title}</DialogTitle>
            {detail && (
              <span className="shrink-0 rounded-sm bg-muted/70 px-1.5 py-0.5 text-[10px] font-medium uppercase text-muted-foreground">
                {detail.kind}
              </span>
            )}
          </div>
        </DialogHeader>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {loading ? (
            <div className="flex h-full min-h-[320px] flex-col items-center justify-center gap-2 text-muted-foreground">
              <Loader2 className="size-5 animate-spin" />
              <p className="text-xs">{t("viewDialogLoading")}</p>
            </div>
          ) : loadError ? (
            <div className="flex h-full min-h-[320px] flex-col items-center justify-center gap-3 px-6 text-center">
              <FileCode2 className="size-5 text-muted-foreground" />
              <p className="text-xs text-muted-foreground">{loadError}</p>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  if (artifactId) {
                    void loadDetail(artifactId);
                  }
                }}
              >
                {t("viewDialogRetry")}
              </Button>
            </div>
          ) : detail && previewKind ? (
            <iframe
              title={title}
              allow={ARTIFACT_IFRAME_PERMISSIONS}
              sandbox="allow-scripts"
              referrerPolicy="no-referrer"
              srcDoc={previewHTML}
              className="h-full min-h-0 w-full bg-background"
            />
          ) : isText ? (
            <pre className="min-h-full whitespace-pre-wrap p-5 font-mono text-xs leading-5 text-foreground">
              {detail?.code}
            </pre>
          ) : null}
        </div>

        <DialogFooter className="shrink-0 border-t border-border/60 px-5 py-3">
          <div className="flex w-full flex-wrap items-center justify-end gap-2">
            {shareUrl && (
              <a
                href={shareUrl}
                target="_blank"
                rel="noreferrer"
                className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                aria-label={t("openLink")}
                title={t("openLink")}
              >
                <ExternalLink className="size-3.5" />
              </a>
            )}
            {detail && (
              <CopyActionButton
                value={detail.code}
                messages={{
                  copied: t("viewDialogCopied"),
                  failed: t("viewDialogCopyFailed"),
                }}
                variant="outline"
                size="sm"
                className="gap-1.5"
              >
                {t("viewDialogCopy")}
              </CopyActionButton>
            )}
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("close")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
