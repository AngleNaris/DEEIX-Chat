"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import { Pencil, Plus, ScrollText, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
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
  createDynamicPrompt,
  deleteDynamicPrompt,
  listDynamicPrompts,
  updateDynamicPrompt,
  type DynamicPromptDTO,
} from "@/shared/api/dynamic-prompts";
import { SettingsSection } from "@/shared/components/settings-layout";

function AiBadge({ show }: { show: boolean }) {
  const t = useTranslations("settings.chatPage.dynamicPrompts");
  if (!show) return null;
  return (
    <span className="mr-1 inline-block rounded-sm bg-violet-500/10 px-1 py-px align-baseline text-[10px] font-medium text-violet-600 dark:text-violet-400">
      {t("aiSource")}
    </span>
  );
}

export type DynamicPromptsSectionHandle = {
  openCreate: () => void;
};

/**
 * DynamicPromptsSection 动态提示词（命名脚本/文本）：提示词中可插入
 * {{script: name}} 标签引用，发送时展开（js 沙箱执行 / text 直插）。
 * title 传空串时隐藏区块标题（供 skills-prompt 页面以 Tab 形式嵌入）。
 * variant="cards" 时以两列卡片网格展示（与技能/提示词 tab 一致）。
 */
export const DynamicPromptsSection = React.forwardRef<
  DynamicPromptsSectionHandle,
  { title?: string; variant?: "rows" | "cards"; query?: string }
>(function DynamicPromptsSection({ title, variant = "rows", query = "" }, ref) {
  const t = useTranslations("settings.chatPage.dynamicPrompts");
  const resolveErrorMessage = useLocalizedErrorMessage();
  const [items, setItems] = React.useState<DynamicPromptDTO[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [dialogOpen, setDialogOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<DynamicPromptDTO | null>(null);
  const [name, setName] = React.useState("");
  const [kind, setKind] = React.useState<"js" | "text">("js");
  const [content, setContent] = React.useState("");
  const [enabled, setEnabled] = React.useState(true);
  const [saving, setSaving] = React.useState(false);

  const load = React.useCallback(async () => {
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      setItems(await listDynamicPrompts(token));
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
    setKind("js");
    setContent("");
    setEnabled(true);
    setDialogOpen(true);
  }, []);

  React.useImperativeHandle(ref, () => ({ openCreate }), [openCreate]);

  const openEdit = (item: DynamicPromptDTO) => {
    setEditing(item);
    setName(item.name);
    setKind(item.kind);
    setContent(item.content);
    setEnabled(item.enabled);
    setDialogOpen(true);
  };

  const submit = async () => {
    if (!name.trim() || !content.trim()) return;
    setSaving(true);
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      const input = { name: name.trim(), kind, content: content.trim(), enabled };
      if (editing) {
        await updateDynamicPrompt(token, editing.prompt_id, input);
      } else {
        await createDynamicPrompt(token, input);
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

  const remove = async (item: DynamicPromptDTO) => {
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      await deleteDynamicPrompt(token, item.prompt_id);
      setItems((prev) => prev.filter((entry) => entry.prompt_id !== item.prompt_id));
      toast.success(t("deleted"));
    } catch (error) {
      toast.error(t("deleteFailed"), { description: resolveErrorMessage(error) });
    }
  };

  const toggleEnabled = async (item: DynamicPromptDTO, enabled: boolean) => {
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      await updateDynamicPrompt(token, item.prompt_id, { name: item.name, kind: item.kind, content: item.content, enabled });
      setItems((prev) => prev.map((entry) => (entry.prompt_id === item.prompt_id ? { ...entry, enabled } : entry)));
    } catch (error) {
      toast.error(t("saveFailed"), { description: resolveErrorMessage(error) });
    }
  };

  const normalizedQuery = query.trim().toLowerCase();
  const visibleItems = normalizedQuery
    ? items.filter((item) => item.name.toLowerCase().includes(normalizedQuery) || item.content.toLowerCase().includes(normalizedQuery))
    : items;

  return (
    <SettingsSection title={title ?? t("sectionTitle")}>
      <div className="space-y-2">
        {variant === "rows" ? (
          <div className="flex h-8 items-center justify-end gap-3">
            <Button size="sm" variant="ghost" className="h-7 gap-1.5 px-2 text-xs" onClick={openCreate}>
              <Plus className="h-3.5 w-3.5" />
              {t("addPrompt")}
            </Button>
          </div>
        ) : null}

        <Dialog open={dialogOpen} onOpenChange={(next) => { if (!saving) setDialogOpen(next); }}>
          <DialogContent className="sm:max-w-[520px]">
            <form
              className="space-y-4"
              onSubmit={(event) => {
                event.preventDefault();
                void submit();
              }}
            >
              <DialogHeader>
                <DialogTitle>{editing ? t("editTitle") : t("addTitle")}</DialogTitle>
                <DialogDescription>{t("addDescription")}</DialogDescription>
              </DialogHeader>

              <div className="space-y-3">
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">{t("name")}</p>
                  <Input
                    autoFocus
                    maxLength={64}
                    placeholder={t("namePlaceholder")}
                    value={name}
                    disabled={saving}
                    onChange={(event) => setName(event.target.value)}
                  />
                </div>
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">{t("kind")}</p>
                  <Select value={kind} onValueChange={(value) => setKind(value as "js" | "text")} disabled={saving}>
                    <SelectTrigger className="h-8 text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="js">{t("kindJs")}</SelectItem>
                      <SelectItem value="text">{t("kindText")}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">{t("content")}</p>
                  <Textarea
                    maxLength={20000}
                    placeholder={kind === "js" ? t("contentJsPlaceholder") : t("contentTextPlaceholder")}
                    value={content}
                    disabled={saving}
                    className="h-28 resize-none overflow-y-auto font-mono text-xs"
                    onChange={(event) => setContent(event.target.value)}
                  />
                </div>
                <div className="flex items-center justify-between">
                  <p className="text-xs text-muted-foreground">{t("enabled")}</p>
                  <Switch checked={enabled} disabled={saving} onCheckedChange={setEnabled} />
                </div>
                <p className="text-[11px] leading-relaxed text-muted-foreground">{t("usageHint")}</p>
              </div>

              <DialogFooter>
                <Button type="button" variant="ghost" disabled={saving} onClick={() => setDialogOpen(false)}>
                  {t("cancel")}
                </Button>
                <Button type="submit" disabled={saving || !name.trim() || !content.trim()}>
                  {saving ? t("saving") : t("save")}
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>

        {loading ? (
          <div className={variant === "cards" ? "grid gap-4 md:ml-13 md:w-[calc(100%-3.25rem)] md:grid-cols-2" : "space-y-1"}>
            {variant === "cards"
              ? Array.from({ length: 4 }).map((_, index) => (
                  <div key={index} className="flex min-h-16 items-center gap-2.5 rounded-lg bg-muted/35 px-3 py-2.5">
                    <Skeleton className="size-7 shrink-0 rounded-md bg-muted/55" />
                    <div className="min-w-0 flex-1 space-y-1.5">
                      <Skeleton className="h-4 w-32 rounded-full bg-muted/55" />
                      <Skeleton className="h-3 w-4/5 rounded-full bg-muted/35" />
                    </div>
                  </div>
                ))
              : (
                  <>
                    <Skeleton className="h-9 w-full rounded-md" />
                    <Skeleton className="h-9 w-4/5 rounded-md" />
                  </>
                )}
          </div>
        ) : visibleItems.length === 0 ? (
          <div className="flex h-9 items-center rounded-md bg-muted/30 px-2.5">
            <p className="text-xs text-muted-foreground">{t("empty")}</p>
          </div>
        ) : variant === "cards" ? (
          <div className="grid gap-4 md:ml-13 md:w-[calc(100%-3.25rem)] md:grid-cols-2">
            {visibleItems.map((item) => (
              <div
                key={item.prompt_id}
                className="group flex min-h-16 min-w-0 items-center gap-2.5 rounded-lg bg-muted/35 px-3 py-2.5 transition-colors hover:bg-muted/55"
              >
                <div className="flex size-7 shrink-0 items-center justify-center text-muted-foreground">
                  <ScrollText className="size-4.5" strokeWidth={1.8} />
                </div>
                <div className="grid min-w-0 flex-1 gap-0.5">
                  <div className="flex min-w-0 items-center gap-1.5">
                    <AiBadge show={item.updated_by === "ai"} />
                    <h3 className="min-w-0 truncate text-sm font-medium text-foreground">{item.name}</h3>
                    <span className="shrink-0 rounded-sm bg-muted/70 px-1 py-px text-[10px] font-medium uppercase text-muted-foreground">
                      {item.kind}
                    </span>
                  </div>
                  <p className="min-w-0 truncate text-xs leading-5 text-muted-foreground">
                    {t("usageTag")}{item.name}
                    <span className="text-muted-foreground/55">{t("separator")}{item.content}</span>
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <button
                    type="button"
                    className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-background/80 hover:text-foreground"
                    onClick={() => openEdit(item)}
                    aria-label={t("edit")}
                  >
                    <Pencil className="h-3.5 w-3.5" strokeWidth={1.6} />
                  </button>
                  <button
                    type="button"
                    className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-background/80 hover:text-destructive"
                    onClick={() => void remove(item)}
                    aria-label={t("delete")}
                  >
                    <Trash2 className="h-3.5 w-3.5" strokeWidth={1.6} />
                  </button>
                  <Switch
                    size="sm"
                    checked={item.enabled}
                    onCheckedChange={(enabled) => void toggleEnabled(item, enabled)}
                    aria-label={t("enabled")}
                  />
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="space-y-1">
            {visibleItems.map((item) => (
              <div key={item.prompt_id} className="group flex min-h-9 items-center gap-2 rounded-md px-2 py-1.5 transition-colors hover:bg-muted/40">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-xs leading-5">
                    <AiBadge show={item.updated_by === "ai"} />
                    <span className="font-medium text-foreground/80">{item.name}</span>
                    <span className="ml-1 inline-block rounded-sm bg-muted/70 px-1 py-px align-baseline text-[10px] font-medium uppercase text-muted-foreground">
                      {item.kind}
                    </span>
                    <span className="text-muted-foreground">{t("separator")}{item.content}</span>
                  </p>
                  <p className="mt-0.5 text-[10px] text-muted-foreground">
                    {t("usageTag")}{item.name}
                  </p>
                </div>
                <div className="flex shrink-0 gap-0.5 opacity-100 transition-opacity md:opacity-0 md:group-hover:opacity-100">
                  <button
                    type="button"
                    className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                    onClick={() => openEdit(item)}
                    aria-label={t("edit")}
                  >
                    <Pencil className="h-3 w-3" />
                  </button>
                  <button
                    type="button"
                    className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-destructive"
                    onClick={() => void remove(item)}
                    aria-label={t("delete")}
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </SettingsSection>
  );
});
