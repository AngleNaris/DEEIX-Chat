"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import { ExternalLink, Save } from "lucide-react";

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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { CopyActionButton } from "@/shared/components/copy-action";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { useLocalizedErrorMessage } from "@/i18n/use-localized-error";
import { extractArtifactsFromContent, type ChatArtifact } from "@/features/chat/model/chat-artifacts";
import type { ChatAreaMessage } from "@/features/chat/types/messages";
import {
  artifactShareUrl,
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

// ArtifactMessageSource 提取代码块所需的消息字段。
export type ArtifactMessageSource = Pick<
  ChatAreaMessage,
  "content" | "isStreaming" | "key" | "publicID" | "runID" | "updatedAt"
>;

/**
 * SaveArtifactButton 消息操作栏按钮：把 AI 回复中的代码块保存为制品并生成分享链接。
 * 消息中无可保存的 artifact 时返回 null。
 */
export function SaveArtifactButton({ message }: { message?: ArtifactMessageSource }) {
  const t = useTranslations("chat.messages");
  const resolveErrorMessage = useLocalizedErrorMessage();
  const [open, setOpen] = React.useState(false);
  const [artifacts, setArtifacts] = React.useState<ChatArtifact[]>([]);
  const [selectedIndex, setSelectedIndex] = React.useState(0);
  const [title, setTitle] = React.useState("");
  const [saving, setSaving] = React.useState(false);
  const [share, setShare] = React.useState<ArtifactShareDTO | null>(null);

  const extracted = React.useMemo(() => (message ? extractArtifactsFromContent(message) : []), [message]);
  const saveable = extracted.filter((artifact) => artifact.complete && artifact.code.trim());

  const openDialog = () => {
    setArtifacts(saveable);
    setSelectedIndex(0);
    setTitle(defaultArtifactTitle(saveable[0]));
    setShare(null);
    setOpen(true);
  };

  const submit = async () => {
    const artifact = artifacts[selectedIndex];
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
      const createdShare = await createArtifactShare(token, saved.artifactPublicID);
      setShare(createdShare);
      toast.success(t("artifactSaved"));
    } catch (error) {
      toast.error(t("artifactSaveFailed"), { description: resolveErrorMessage(error) });
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      {saveable.length > 0 && (
        <button
          type="button"
          className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          onClick={openDialog}
          aria-label={t("saveArtifact")}
          title={t("saveArtifact")}
        >
          <Save className="h-3 w-3" />
        </button>
      )}
      <Dialog open={open} onOpenChange={(next) => { if (!saving) setOpen(next); }}>
        <DialogContent className="sm:max-w-[460px]">
          <div className="space-y-4">
            <DialogHeader>
              <DialogTitle>{t("saveArtifactTitle")}</DialogTitle>
              <DialogDescription>{t("saveArtifactDescription")}</DialogDescription>
            </DialogHeader>

            {artifacts.length > 1 && (
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">{t("artifactSelect")}</p>
                <Select
                  value={String(selectedIndex)}
                  onValueChange={(value) => {
                    const index = Number(value);
                    setSelectedIndex(index);
                    setTitle(defaultArtifactTitle(artifacts[index]));
                  }}
                >
                  <SelectTrigger className="h-8 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {artifacts.map((artifact, index) => (
                      <SelectItem key={artifact.id ?? index} value={String(index)}>
                        {artifact.kind} #{index + 1}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            <div className="space-y-1">
              <p className="text-xs text-muted-foreground">{t("artifactTitleLabel")}</p>
              <Input
                autoFocus
                maxLength={255}
                value={title}
                disabled={saving}
                onChange={(event) => setTitle(event.target.value)}
              />
            </div>

            {share ? (
              <div className="flex items-center gap-2 rounded-md bg-muted/30 px-2.5 py-2">
                <span className="min-w-0 flex-1 truncate text-[11px] text-muted-foreground">
                  {artifactShareUrl(share.share_id)}
                </span>
                <CopyActionButton
                  value={artifactShareUrl(share.share_id)}
                  messages={{ copied: t("artifactLinkCopied"), failed: t("copyFailed") }}
                  iconClassName="size-3"
                />
                <a
                  href={artifactShareUrl(share.share_id)}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  aria-label={t("artifactOpenLink")}
                >
                  <ExternalLink className="size-3" />
                </a>
              </div>
            ) : null}

            <DialogFooter>
              <Button type="button" variant="ghost" disabled={saving} onClick={() => setOpen(false)}>
                {t("cancel")}
              </Button>
              <Button type="button" disabled={saving || !title.trim()} onClick={() => void submit()}>
                {saving ? t("artifactSaving") : t("artifactSaveAndShare")}
              </Button>
            </DialogFooter>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

function defaultArtifactTitle(artifact: ChatArtifact | undefined): string {
  if (!artifact) return "";
  const firstLine = artifact.code.trim().split("\n")[0] ?? "";
  const cleaned = firstLine.replace(/^[\/#\*\-<\s]+/, "").trim();
  if (cleaned && cleaned.length <= 40) {
    return cleaned;
  }
  return `${artifact.kind} artifact`;
}
