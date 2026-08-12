"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import { KeyRound, Pencil, Plus, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { useLocalizedErrorMessage } from "@/i18n/use-localized-error";
import {
  createCredential,
  deleteCredential,
  listCredentials,
  updateCredential,
  type CredentialDTO,
  type CredentialType,
} from "@/shared/api/credentials";
import { SettingsSection } from "@/shared/components/settings-layout";
import { cn } from "@/lib/utils";

/**
 * CredentialsSection 用户凭据管理（系统级）：命名凭据（SSH/API key 等）加密存储，
 * 密钥永不回显——编辑时留空表示不修改。AI 在对话中通过 {{credential: name}}
 * 占位符使用凭据（执行层展开），设置页是密钥的唯一人工录入/查看入口。
 */
export function CredentialsSection() {
  const t = useTranslations("settings.credentialsPage");
  const resolveErrorMessage = useLocalizedErrorMessage();
  const [items, setItems] = React.useState<CredentialDTO[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [dialogOpen, setDialogOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<CredentialDTO | null>(null);
  const [name, setName] = React.useState("");
  const [type, setType] = React.useState<CredentialType>("generic");
  const [description, setDescription] = React.useState("");
  const [value, setValue] = React.useState("");
  const [saving, setSaving] = React.useState(false);
  const [deleteTarget, setDeleteTarget] = React.useState<CredentialDTO | null>(null);
  const [deleting, setDeleting] = React.useState(false);

  const load = React.useCallback(async () => {
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      setItems(await listCredentials(token));
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void (async () => {
      setLoading(true);
      await load();
    })();
  }, [load]);

  const openCreate = React.useCallback(() => {
    setEditing(null);
    setName("");
    setType("generic");
    setDescription("");
    setValue("");
    setDialogOpen(true);
  }, []);

  const openEdit = (item: CredentialDTO) => {
    setEditing(item);
    setName(item.name);
    setType(item.type);
    setDescription(item.description);
    setValue("");
    setDialogOpen(true);
  };

  const submit = async () => {
    if (!name.trim()) return;
    if (!editing && !value.trim()) return;
    setSaving(true);
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      const input = { name: name.trim(), type, description: description.trim(), value: value.trim() };
      if (editing) {
        await updateCredential(token, editing.public_id, input);
      } else {
        await createCredential(token, input);
      }
      toast.success(t("saved"));
      setDialogOpen(false);
      await load();
    } catch (error) {
      toast.error(t("saveFailed"), { description: resolveErrorMessage(error) });
    } finally {
      setSaving(false);
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      await deleteCredential(token, deleteTarget.public_id);
      setItems((prev) => prev.filter((entry) => entry.public_id !== deleteTarget.public_id));
      toast.success(t("deleted"));
      setDeleteTarget(null);
    } catch (error) {
      toast.error(t("deleteFailed"), { description: resolveErrorMessage(error) });
    } finally {
      setDeleting(false);
    }
  };

  const typeLabels: Record<CredentialType, string> = {
    ssh: t("types.ssh"),
    api_key: t("types.apiKey"),
    generic: t("types.generic"),
  };

  return (
    <SettingsSection title={t("title")}>
      <div className="flex items-center justify-between gap-3">
        <p className="min-w-0 text-sm text-muted-foreground">{t("hint")}</p>
        <Button type="button" size="sm" variant="outline" onClick={openCreate} className="shrink-0">
          <Plus className="size-3.5" />
          {t("create")}
        </Button>
      </div>

      <div className="mt-3 space-y-1.5">
        {loading ? (
          <div className="space-y-1.5">
            <Skeleton className="h-12 w-full rounded-lg bg-muted/55" />
            <Skeleton className="h-12 w-full rounded-lg bg-muted/45" />
          </div>
        ) : items.length === 0 ? (
          <div className="rounded-lg border border-dashed border-border/70 px-4 py-8 text-center text-sm text-muted-foreground">
            {t("empty")}
          </div>
        ) : (
          items.map((item) => (
            <div
              key={item.public_id}
              className="flex items-center gap-3 rounded-lg border border-border/60 bg-card/40 px-3 py-2.5"
            >
              <KeyRound className="size-4 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate font-mono text-[13px] font-medium text-foreground">
                    {`{{credential: ${item.name}}}`}
                  </span>
                  <span
                    className={cn(
                      "shrink-0 rounded-sm px-1 py-px text-[10px] font-medium uppercase",
                      "bg-muted/70 text-muted-foreground",
                    )}
                  >
                    {typeLabels[item.type] ?? item.type}
                  </span>
                </div>
                {item.description ? (
                  <p className="mt-0.5 truncate text-xs text-muted-foreground">{item.description}</p>
                ) : null}
              </div>
              <div className="flex shrink-0 items-center gap-0.5">
                <Button type="button" variant="ghost" size="icon-sm" aria-label={t("edit")} onClick={() => openEdit(item)}>
                  <Pencil className="size-3.5" />
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t("delete")}
                  className="text-destructive/80 hover:text-destructive"
                  onClick={() => setDeleteTarget(item)}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </div>
            </div>
          ))
        )}
      </div>

      <Dialog open={dialogOpen} onOpenChange={(next) => { if (!saving) setDialogOpen(next); }}>
        <DialogContent className="sm:max-w-[440px]">
          <DialogHeader>
            <DialogTitle>{editing ? t("editTitle") : t("createTitle")}</DialogTitle>
            <DialogDescription>{t("formHint")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="credential-name">{t("nameLabel")}</Label>
              <Input
                id="credential-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder={t("namePlaceholder")}
                maxLength={64}
              />
              <p className="text-[11px] text-muted-foreground">{t("nameHint")}</p>
            </div>
            <div className="space-y-1.5">
              <Label>{t("typeLabel")}</Label>
              <Select value={type} onValueChange={(next) => setType(next as CredentialType)}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="ssh">{t("types.ssh")}</SelectItem>
                  <SelectItem value="api_key">{t("types.apiKey")}</SelectItem>
                  <SelectItem value="generic">{t("types.generic")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="credential-value">{t("valueLabel")}</Label>
              <Input
                id="credential-value"
                type="password"
                autoComplete="new-password"
                value={value}
                onChange={(event) => setValue(event.target.value)}
                placeholder={editing ? t("valuePlaceholderEdit") : t("valuePlaceholder")}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="credential-description">{t("descriptionLabel")}</Label>
              <Textarea
                id="credential-description"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                placeholder={t("descriptionPlaceholder")}
                maxLength={255}
                rows={2}
              />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDialogOpen(false)} disabled={saving}>
              {t("cancel")}
            </Button>
            <Button
              type="button"
              onClick={() => void submit()}
              disabled={saving || !name.trim() || (!editing && !value.trim())}
            >
              {t("save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleteTarget !== null} onOpenChange={(next) => { if (!next && !deleting) setDeleteTarget(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("deleteDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>{t("cancel")}</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              disabled={deleting}
              onClick={(event) => {
                event.preventDefault();
                void confirmDelete();
              }}
            >
              {t("deleteConfirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  );
}
