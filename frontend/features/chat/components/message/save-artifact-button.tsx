"use client";

import { Save } from "lucide-react";
import { useTranslations } from "next-intl";
import * as React from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { captureArtifactPreviewThumbnail } from "@/features/chat/model/artifact-thumbnail";
import type { ChatArtifact } from "@/features/chat/model/chat-artifacts";
import { useLocalizedErrorMessage } from "@/i18n/use-localized-error";
import {
  type ArtifactKind,
  type ArtifactShareDTO,
  createArtifact,
  createArtifactShare,
} from "@/shared/api/artifacts";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { ArtifactShareLink } from "@/shared/components/artifact-share-link";

// mapArtifactKind 把可保存的前端预览类型映射为制品存储类型。
function mapArtifactKind(kind: Exclude<ChatArtifact["kind"], "svg">): ArtifactKind {
  if (kind === "javascript") return "js";
  return kind;
}

/**
 * SaveArtifactButton 制品展示面板操作栏按钮：把当前展示的制品保存到制品库并生成分享链接。
 */
export function SaveArtifactButton({
  artifact,
  previewFrameRef,
}: {
  artifact: ChatArtifact | null;
  previewFrameRef: React.RefObject<HTMLIFrameElement | null>;
}) {
  const t = useTranslations("chat.artifacts");
  const tCommon = useTranslations("common.actions");
  const resolveErrorMessage = useLocalizedErrorMessage();
  const [open, setOpen] = React.useState(false);
  const [title, setTitle] = React.useState("");
  const [saving, setSaving] = React.useState(false);
  const [share, setShare] = React.useState<ArtifactShareDTO | null>(null);

  const saveable = Boolean(artifact && artifact.kind !== "svg" && artifact.complete && artifact.code.trim());

  const openDialog = () => {
    setTitle(defaultArtifactTitle(artifact));
    setShare(null);
    setOpen(true);
  };

  const submit = async () => {
    if (!artifact || artifact.kind === "svg" || !title.trim()) return;
    setSaving(true);
    try {
      const token = await resolveAccessToken();
      if (!token) {
        toast.error(t("authTokenMissing"));
        return;
      }
      const thumbnail = await captureArtifactPreviewThumbnail(previewFrameRef.current).catch(
        (error) => {
          console.warn("Failed to capture artifact thumbnail", error);
          return null;
        },
      );
      const saved = await createArtifact(token, {
        title: title.trim(),
        kind: mapArtifactKind(artifact.kind),
        code: artifact.code.trim(),
        thumbnail: thumbnail ?? undefined,
      });
      // 保存成功后直接创建公开分享，用户可复制链接或打开。
      const createdShare = await createArtifactShare(token, saved.artifact_id);
      setShare(createdShare);
      toast.success(t("saveSuccess"));
    } catch (error) {
      toast.error(t("saveFailed"), { description: resolveErrorMessage(error) });
    } finally {
      setSaving(false);
    }
  };

  if (!saveable) {
    return null;
  }

  return (
    <>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-foreground/[0.04] hover:text-foreground"
            onClick={openDialog}
            aria-label={t("save")}
          >
            <Save className="size-3" />
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom">{t("save")}</TooltipContent>
      </Tooltip>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!saving) setOpen(next);
        }}
      >
        <DialogContent className="flex min-w-0 max-h-[calc(100svh-2rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-[460px]">
          <div className="min-w-0 flex-1 space-y-4 overflow-y-auto p-5 pb-4">
            <DialogHeader>
              <DialogTitle>{t("saveTitle")}</DialogTitle>
              <DialogDescription>{t("saveDescription")}</DialogDescription>
            </DialogHeader>

            <div className="space-y-1">
              <p className="text-xs text-muted-foreground">{t("titleLabel")}</p>
              <Input
                autoFocus
                maxLength={255}
                value={title}
                disabled={saving || Boolean(share)}
                onChange={(event) => setTitle(event.target.value)}
              />
            </div>

            {share ? <ArtifactShareLink share={share} disabled={saving} /> : null}
          </div>

          <DialogFooter className="shrink-0 border-t border-border/60 px-5 py-3">
            {!share ? (
              <Button type="button" variant="ghost" disabled={saving} onClick={() => setOpen(false)}>
                {t("cancel")}
              </Button>
            ) : null}
            <Button
              type="button"
              disabled={saving || (!share && !title.trim())}
              onClick={() => {
                if (share) {
                  setOpen(false);
                  return;
                }
                void submit();
              }}
            >
              {saving ? t("saving") : share ? tCommon("confirm") : t("saveAndShare")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function defaultArtifactTitle(artifact: ChatArtifact | null): string {
  if (!artifact) return "";
  const firstLine = artifact.code.trim().split("\n")[0] ?? "";
  const cleaned = firstLine.replace(/^[/#*<\s-]+/, "").trim();
  if (cleaned && cleaned.length <= 40) {
    return cleaned;
  }
  return `${artifact.kind} artifact`;
}
