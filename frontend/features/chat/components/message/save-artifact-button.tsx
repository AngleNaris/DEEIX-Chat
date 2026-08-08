"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import { Save } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ArtifactShareLink } from "@/shared/components/artifact-share-link";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { useLocalizedErrorMessage } from "@/i18n/use-localized-error";
import type { ChatArtifact } from "@/features/chat/model/chat-artifacts";
import {
  createArtifact,
  createArtifactShare,
  type ArtifactKind,
  type ArtifactShareDTO,
} from "@/shared/api/artifacts";

// mapArtifactKind 把前端预览类型（html/css/javascript）映射为制品存储类型（html/css/js/text）。
function mapArtifactKind(kind: ChatArtifact["kind"]): ArtifactKind {
  if (kind === "javascript") return "js";
  return kind;
}

/**
 * SaveArtifactButton 制品展示面板操作栏按钮：把当前展示的制品保存到制品库并生成分享链接。
 */
export function SaveArtifactButton({ artifact }: { artifact: ChatArtifact | null }) {
  const t = useTranslations("chat.artifacts");
  const resolveErrorMessage = useLocalizedErrorMessage();
  const [open, setOpen] = React.useState(false);
  const [title, setTitle] = React.useState("");
  const [saving, setSaving] = React.useState(false);
  const [share, setShare] = React.useState<ArtifactShareDTO | null>(null);

  const saveable = Boolean(artifact && artifact.complete && artifact.code.trim());

  const openDialog = () => {
    setTitle(defaultArtifactTitle(artifact));
    setShare(null);
    setOpen(true);
  };

  const submit = async () => {
    if (!artifact || !title.trim()) return;
    setSaving(true);
    try {
      const token = await resolveAccessToken();
      if (!token) {
        toast.error(t("authTokenMissing"));
        return;
      }
      const saved = await createArtifact(token, {
        title: title.trim(),
        kind: mapArtifactKind(artifact.kind),
        code: artifact.code.trim(),
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
      <Dialog open={open} onOpenChange={(next) => { if (!saving) setOpen(next); }}>
        <DialogContent className="sm:max-w-[460px]">
          <div className="space-y-4">
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
                disabled={saving}
                onChange={(event) => setTitle(event.target.value)}
              />
            </div>

            {share ? <ArtifactShareLink share={share} disabled={saving} /> : null}

            <DialogFooter>
              <Button type="button" variant="ghost" disabled={saving} onClick={() => setOpen(false)}>
                {t("cancel")}
              </Button>
              <Button type="button" disabled={saving || !title.trim()} onClick={() => void submit()}>
                {saving ? t("saving") : t("saveAndShare")}
              </Button>
            </DialogFooter>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

function defaultArtifactTitle(artifact: ChatArtifact | null): string {
  if (!artifact) return "";
  const firstLine = artifact.code.trim().split("\n")[0] ?? "";
  const cleaned = firstLine.replace(/^[\/#\*\-<\s]+/, "").trim();
  if (cleaned && cleaned.length <= 40) {
    return cleaned;
  }
  return `${artifact.kind} artifact`;
}
