"use client";

import { ExternalLink, Link2, LoaderCircle, Unlink } from "lucide-react";
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
import { useLocalizedErrorMessage } from "@/i18n/use-localized-error";
import {
  createFileShare,
  type FileShareDTO,
  fileShareURL,
  getFileShare,
  revokeFileShare,
} from "@/shared/api/file";
import type { FileObjectDTO } from "@/shared/api/file.types";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { CopyActionButton } from "@/shared/components/copy-action";

export function FileShareDialog({
  file,
  open,
  onOpenChange,
}: {
  file: FileObjectDTO | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const t = useTranslations("files.shareDialog");
  const resolveErrorMessage = useLocalizedErrorMessage();
  const [share, setShare] = React.useState<FileShareDTO | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [mutating, setMutating] = React.useState(false);
  const restoreFocusRef = React.useRef<HTMLElement | null>(null);

  React.useEffect(() => {
    if (!open || !file) {
      setShare(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    void (async () => {
      try {
        const token = await resolveAccessToken();
        if (!token) throw new Error(t("sessionExpired"));
        const result = await getFileShare(token, file.fileID);
        if (!cancelled) setShare(result);
      } catch (error) {
        if (!cancelled) toast.error(t("loadFailed"), { description: resolveErrorMessage(error) });
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [file, open, resolveErrorMessage, t]);

  const create = async () => {
    if (!file) return;
    setMutating(true);
    try {
      const token = await resolveAccessToken();
      if (!token) throw new Error(t("sessionExpired"));
      const result = await createFileShare(token, file.fileID);
      setShare(result);
      toast.success(t("created"));
    } catch (error) {
      toast.error(t("createFailed"), { description: resolveErrorMessage(error) });
    } finally {
      setMutating(false);
    }
  };

  const revoke = async () => {
    if (!file) return;
    setMutating(true);
    try {
      const token = await resolveAccessToken();
      if (!token) throw new Error(t("sessionExpired"));
      await revokeFileShare(token, file.fileID);
      setShare((current) => current ? { ...current, status: "revoked" } : current);
      toast.success(t("revoked"));
    } catch (error) {
      toast.error(t("revokeFailed"), { description: resolveErrorMessage(error) });
    } finally {
      setMutating(false);
    }
  };

  const active = share?.status === "active" && Boolean(share.share_id);
  const shareURL = active && share?.share_id ? fileShareURL(share.share_id) : "";

  return (
    <Dialog open={open} onOpenChange={(next) => !mutating && onOpenChange(next)}>
      <DialogContent
        className="flex min-w-0 max-h-[calc(100svh-2rem)] max-w-[calc(100vw-1.5rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-[460px]"
        onOpenAutoFocus={() => {
          const activeElement = document.activeElement;
          restoreFocusRef.current = activeElement instanceof HTMLElement && activeElement !== document.body
            ? activeElement
            : null;
        }}
        onCloseAutoFocus={(event) => {
          const restoreTarget = restoreFocusRef.current;
          restoreFocusRef.current = null;
          if (!restoreTarget?.isConnected) return;
          event.preventDefault();
          restoreTarget.focus({ preventScroll: true });
        }}
      >
        <div className="min-w-0 flex-1 space-y-4 overflow-y-auto p-5">
          <DialogHeader>
            <DialogTitle>{t("title")}</DialogTitle>
            <DialogDescription>{t("description", { name: file?.fileName ?? "" })}</DialogDescription>
          </DialogHeader>

          {loading ? (
            <div className="flex min-h-24 items-center justify-center text-muted-foreground">
              <LoaderCircle className="size-4 animate-spin" />
            </div>
          ) : active ? (
            <div className="space-y-2">
              <p className="text-xs text-muted-foreground">
                {share.expires_at
                  ? t("expiresAt", { value: new Date(share.expires_at).toLocaleString() })
                  : t("neverExpires")}
              </p>
              <div className="flex min-w-0 items-start gap-2 border-y border-border/50 py-2">
                <Link2 className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                <span dir="ltr" className="min-w-0 flex-1 break-all text-xs leading-5 text-muted-foreground">
                  {shareURL}
                </span>
                <CopyActionButton
                  value={shareURL}
                  messages={{ copied: t("copied"), failed: t("copyFailed") }}
                  iconClassName="size-3.5"
                />
                <a
                  href={shareURL}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  aria-label={t("open")}
                  title={t("open")}
                >
                  <ExternalLink className="size-3.5" />
                </a>
              </div>
            </div>
          ) : (
            <div className="flex min-h-24 items-center justify-center text-center text-xs text-muted-foreground">
              {share?.status === "expired" ? t("expired") : t("inactive")}
            </div>
          )}
        </div>

        <DialogFooter className="shrink-0 border-t border-border/60 px-5 py-3">
          <Button type="button" variant="ghost" disabled={mutating} onClick={() => onOpenChange(false)}>
            {t("close")}
          </Button>
          {active ? (
            <Button type="button" variant="outline" disabled={mutating} onClick={() => void revoke()}>
              {mutating ? <LoaderCircle className="size-4 animate-spin" /> : <Unlink className="size-4" />}
              {t("revoke")}
            </Button>
          ) : (
            <Button type="button" disabled={mutating || loading || !file} onClick={() => void create()}>
              {mutating ? <LoaderCircle className="size-4 animate-spin" /> : <Link2 className="size-4" />}
              {t("create")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
