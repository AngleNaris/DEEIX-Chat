"use client";

import { Loader2 } from "lucide-react";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useLocalizedErrorMessage } from "@/i18n/use-localized-error";
import {
  getArtifact,
  updateArtifact,
  type ArtifactDetailDTO,
  type ArtifactKind,
} from "@/shared/api/artifacts";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";

type ArtifactEditorValue = {
  title: string;
  kind: ArtifactKind;
  code: string;
};

const EMPTY_VALUE: ArtifactEditorValue = {
  title: "",
  kind: "html",
  code: "",
};

export function ArtifactEditorDialog({
  open,
  artifactId,
  onOpenChange,
  onSaved,
}: {
  open: boolean;
  artifactId: string | null;
  onOpenChange: (open: boolean) => void;
  onSaved: (artifact: ArtifactDetailDTO) => void;
}) {
  const t = useTranslations("settings.chatPage.artifacts");
  const resolveErrorMessage = useLocalizedErrorMessage();
  const [value, setValue] = React.useState<ArtifactEditorValue>(EMPTY_VALUE);
  const [loading, setLoading] = React.useState(false);
  const [saving, setSaving] = React.useState(false);
  const canSave = Boolean(value.title.trim() && value.code.trim()) && !loading && !saving;

  React.useEffect(() => {
    if (!open || !artifactId) {
      setValue(EMPTY_VALUE);
      return;
    }

    let cancelled = false;
    setLoading(true);
    void (async () => {
      try {
        const token = await resolveAccessToken();
        if (!token) {
          throw new Error(t("editAuthRequired"));
        }
        const artifact = await getArtifact(token, artifactId);
        if (!cancelled) {
          setValue({
            title: artifact.title,
            kind: artifact.kind,
            code: artifact.code,
          });
        }
      } catch (error) {
        if (!cancelled) {
          toast.error(t("editLoadFailed"), { description: resolveErrorMessage(error) });
          onOpenChange(false);
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [artifactId, onOpenChange, open, resolveErrorMessage, t]);

  const save = async () => {
    if (!artifactId || !canSave) {
      return;
    }
    setSaving(true);
    try {
      const token = await resolveAccessToken();
      if (!token) {
        throw new Error(t("editAuthRequired"));
      }
      const artifact = await updateArtifact(token, artifactId, {
        title: value.title.trim(),
        kind: value.kind,
        code: value.code,
      });
      onSaved(artifact);
      toast.success(t("updated"));
      onOpenChange(false);
    } catch (error) {
      toast.error(t("updateFailed"), { description: resolveErrorMessage(error) });
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!saving) onOpenChange(next); }}>
      <DialogContent className="flex h-[calc(100svh-2rem)] max-h-none w-[min(960px,calc(100%-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-[960px]">
        <form
          className="flex min-h-0 flex-1 flex-col"
          onSubmit={(event) => {
            event.preventDefault();
            void save();
          }}
        >
          <DialogHeader className="shrink-0 border-b border-border/60 px-5 py-4">
            <DialogTitle>{t("editTitle")}</DialogTitle>
            <DialogDescription>{t("editDescription")}</DialogDescription>
          </DialogHeader>

          {loading ? (
            <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
              <Loader2 className="size-5 animate-spin" />
              <p className="text-xs">{t("editLoading")}</p>
            </div>
          ) : (
            <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-hidden p-5">
              <div className="grid shrink-0 gap-4 md:grid-cols-[minmax(0,1fr)_180px]">
                <div className="space-y-1.5">
                  <p className="text-xs text-muted-foreground">{t("name")}</p>
                  <Input
                    autoFocus
                    maxLength={255}
                    value={value.title}
                    disabled={saving}
                    placeholder={t("namePlaceholder")}
                    onChange={(event) => setValue((current) => ({ ...current, title: event.target.value }))}
                  />
                </div>
                <div className="space-y-1.5">
                  <p className="text-xs text-muted-foreground">{t("kind")}</p>
                  <Select
                    value={value.kind}
                    disabled={saving}
                    onValueChange={(kind) => setValue((current) => ({ ...current, kind: kind as ArtifactKind }))}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="html">{t("kindHtml")}</SelectItem>
                      <SelectItem value="js">{t("kindJs")}</SelectItem>
                      <SelectItem value="css">{t("kindCss")}</SelectItem>
                      <SelectItem value="text">{t("kindText")}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>

              <div className="flex min-h-0 flex-1 flex-col gap-1.5">
                <p className="text-xs text-muted-foreground">{t("code")}</p>
                <Textarea
                  maxLength={262144}
                  value={value.code}
                  disabled={saving}
                  spellCheck={false}
                  placeholder={t("codePlaceholder")}
                  className="min-h-[280px] flex-1 resize-none overflow-auto font-mono text-xs leading-5"
                  onChange={(event) => setValue((current) => ({ ...current, code: event.target.value }))}
                />
              </div>
            </div>
          )}

          <DialogFooter className="shrink-0 border-t border-border/60 px-5 py-3">
            <Button type="button" variant="ghost" disabled={saving} onClick={() => onOpenChange(false)}>
              {t("cancel")}
            </Button>
            <Button type="submit" disabled={!canSave}>
              {saving ? t("saving") : t("save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
