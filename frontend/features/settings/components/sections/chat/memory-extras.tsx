"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import { Check, Copy, ExternalLink, Eye, FileCode2, Pencil, Plus, Share2, ShieldX, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ArtifactShareLink } from "@/shared/components/artifact-share-link";
import {
  ARTIFACT_IFRAME_PERMISSIONS,
  ArtifactPreviewDialog,
} from "@/shared/components/artifact-preview-dialog";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { useLocalizedErrorMessage } from "@/i18n/use-localized-error";
import {
  createArtifactShare,
  deleteArtifact,
  getArtifact,
  listArtifacts,
  revokeArtifactShare,
  artifactShareUrl,
  type ArtifactDetailDTO,
  type ArtifactListItemDTO,
} from "@/shared/api/artifacts";
import {
  createDocCard,
  deleteDocCard,
  listDocCards,
  updateDocCard,
  type DocCardDTO,
} from "@/shared/api/doc-cards";
import { listConversationProjects } from "@/shared/api/conversation";
import type { ConversationProjectDTO } from "@/shared/api/conversation.types";
import { listConversationRoles } from "@/shared/api/roles";
import type { ConversationRoleDTO } from "@/shared/api/roles.types";
import { SettingsSection } from "@/shared/components/settings-layout";
import { buildArtifactPreviewDocument } from "@/features/chat/model/chat-artifacts";
import {
  captureHTMLVisualThemeSnapshot,
  type HTMLVisualThemeSnapshot,
} from "@/shared/lib/html-visual-theme";
import { resolveStoredArtifactPreviewKind } from "@/shared/lib/artifact-preview";
import { useTheme } from "@/shared/components/theme-provider";

function AiBadge({ show }: { show: boolean }) {
  const t = useTranslations("settings.chatPage.docCards");
  if (!show) return null;
  return (
    <span className="mr-1 inline-block rounded-sm bg-violet-500/10 px-1 py-px align-baseline text-[10px] font-medium text-violet-600 dark:text-violet-400">
      {t("aiSource")}
    </span>
  );
}

type DocCardEditorField = "title" | "content" | "keywords" | "category" | "projectId" | "roleId" | "enabled";

function DocCardEditorDialog({
  open,
  editing,
  initial,
  projects,
  roles,
  saving,
  onOpenChange,
  onChange,
  onSubmit,
}: {
  open: boolean;
  editing: DocCardDTO | null;
  initial: { title: string; content: string; keywords: string; category: string; projectId: number | null; roleId: number | null; enabled: boolean };
  projects: ConversationProjectDTO[];
  roles: ConversationRoleDTO[];
  saving: boolean;
  onOpenChange: (open: boolean) => void;
  onChange: (field: DocCardEditorField, value: string | boolean | number | null) => void;
  onSubmit: () => void;
}) {
  const t = useTranslations("settings.chatPage.docCards");
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!saving) onOpenChange(next); }}>
      <DialogContent className="sm:max-w-[520px]">
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            onSubmit();
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
                maxLength={128}
                placeholder={t("namePlaceholder")}
                value={initial.title}
                disabled={saving}
                onChange={(event) => onChange("title", event.target.value)}
              />
            </div>
            <div className="space-y-1">
              <p className="text-xs text-muted-foreground">{t("keywords")}</p>
              <Input
                maxLength={200}
                placeholder={t("keywordsPlaceholder")}
                value={initial.keywords}
                disabled={saving}
                onChange={(event) => onChange("keywords", event.target.value)}
              />
            </div>
            <div className="space-y-1">
              <p className="text-xs text-muted-foreground">{t("content")}</p>
              <Textarea
                maxLength={20000}
                placeholder={t("contentPlaceholder")}
                value={initial.content}
                disabled={saving}
                className="h-28 resize-none overflow-y-auto"
                onChange={(event) => onChange("content", event.target.value)}
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">{t("category")}</p>
                <Input
                  maxLength={64}
                  placeholder={t("categoryPlaceholder")}
                  value={initial.category}
                  disabled={saving}
                  onChange={(event) => onChange("category", event.target.value)}
                />
              </div>
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">{t("binding")}</p>
                <Select
                  value={initial.projectId === null ? "0" : String(initial.projectId)}
                  disabled={saving}
                  onValueChange={(value) => onChange("projectId", value === "0" ? null : Number(value))}
                >
                  <SelectTrigger className="h-8 text-xs">
                    <SelectValue placeholder={t("projectGlobal")} />
                  </SelectTrigger>
                  <SelectContent className="max-h-56 overflow-y-auto">
                    <SelectItem value="0">{t("projectGlobal")}</SelectItem>
                    {projects.map((project) => (
                      <SelectItem key={project.publicID} value={String(project.id)}>
                        {project.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="space-y-1">
              <p className="text-xs text-muted-foreground">{t("role")}</p>
              <Select
                value={initial.roleId === null ? "0" : String(initial.roleId)}
                disabled={saving}
                onValueChange={(value) => onChange("roleId", value === "0" ? null : Number(value))}
              >
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue placeholder={t("roleGlobal")} />
                </SelectTrigger>
                <SelectContent className="max-h-56 overflow-y-auto">
                  <SelectItem value="0">{t("roleGlobal")}</SelectItem>
                  {roles.map((role) => (
                    <SelectItem key={role.publicID} value={String(role.id)}>
                      {role.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex items-center justify-between">
              <p className="text-xs text-muted-foreground">{t("enabled")}</p>
              <Switch
                checked={initial.enabled}
                disabled={saving}
                onCheckedChange={(checked) => onChange("enabled", checked)}
              />
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" disabled={saving} onClick={() => onOpenChange(false)}>
              {t("cancel")}
            </Button>
            <Button type="submit" disabled={saving || !initial.title.trim() || !initial.content.trim()}>
              {saving ? t("saving") : t("save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/**
 * DocCardSection 文档卡片（lorebook 式）：用户/ AI 创建的关键字触发文档，
 * 用户消息命中关键字时注入模型上下文。
 */
export function DocCardSection() {
  const t = useTranslations("settings.chatPage.docCards");
  const tNav = useTranslations("common.navigation");
  const resolveErrorMessage = useLocalizedErrorMessage();
  const [cards, setCards] = React.useState<DocCardDTO[]>([]);
  const [projects, setProjects] = React.useState<ConversationProjectDTO[]>([]);
  const [roles, setRoles] = React.useState<ConversationRoleDTO[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [dialogOpen, setDialogOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<DocCardDTO | null>(null);
  const [title, setTitle] = React.useState("");
  const [content, setContent] = React.useState("");
  const [keywords, setKeywords] = React.useState("");
  const [category, setCategory] = React.useState("");
  const [projectId, setProjectId] = React.useState<number | null>(null);
  const [roleId, setRoleId] = React.useState<number | null>(null);
  const [enabled, setEnabled] = React.useState(true);
  const [saving, setSaving] = React.useState(false);

  const load = React.useCallback(async () => {
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      setCards(await listDocCards(token));
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, []);

  const loadBindings = React.useCallback(async () => {
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      setProjects(await listConversationProjects(token));
      setRoles(await listConversationRoles(token));
    } catch {
      // ignore
    }
  }, []);

  React.useEffect(() => {
    void (async () => {
      setLoading(true);
      await Promise.all([load(), loadBindings()]);
    })();
  }, [load, loadBindings]);

  const openCreate = () => {
    setEditing(null);
    setTitle("");
    setContent("");
    setKeywords("");
    setCategory("");
    setProjectId(null);
    setRoleId(null);
    setEnabled(true);
    setDialogOpen(true);
  };

  const openEdit = (card: DocCardDTO) => {
    setEditing(card);
    setTitle(card.title);
    setContent(card.content);
    setKeywords(card.keywords.join(", "));
    setCategory(card.category ?? "");
    setProjectId(card.project_id ?? null);
    setRoleId(card.role_id ?? null);
    setEnabled(card.enabled);
    setDialogOpen(true);
  };

  const submit = async () => {
    if (!title.trim() || !content.trim()) return;
    setSaving(true);
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      const input = {
        title: title.trim(),
        content: content.trim(),
        keywords: keywords.split(/[,，]/).map((item) => item.trim()).filter(Boolean),
        category: category.trim(),
        projectId,
        roleId,
        enabled,
      };
      if (editing) {
        await updateDocCard(token, editing.card_id, input);
      } else {
        await createDocCard(token, input);
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

  const remove = async (card: DocCardDTO) => {
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      await deleteDocCard(token, card.card_id);
      setCards((prev) => prev.filter((item) => item.card_id !== card.card_id));
      toast.success(t("deleted"));
    } catch (error) {
      toast.error(t("deleteFailed"), { description: resolveErrorMessage(error) });
    }
  };

  return (
    <>
      <header className="mb-4">
        <div className="flex items-start justify-between gap-4">
          <h1 className="min-w-0 text-xl font-semibold tracking-[-0.03em] text-foreground md:text-2xl">{tNav("docCards")}</h1>
          <Button size="sm" variant="default" className="shrink-0" onClick={openCreate}>
            <Plus className="size-4" />
            {t("addCard")}
          </Button>
        </div>
      </header>
      <SettingsSection title="">
      <div className="space-y-2">

        <DocCardEditorDialog
          open={dialogOpen}
          editing={editing}
          saving={saving}
          projects={projects}
          roles={roles}
          initial={{ title, content, keywords, category, projectId, roleId, enabled }}
          onOpenChange={setDialogOpen}
          onChange={(field, value) => {
            if (field === "title") setTitle(String(value));
            else if (field === "content") setContent(String(value));
            else if (field === "keywords") setKeywords(String(value));
            else if (field === "category") setCategory(String(value));
            else if (field === "projectId") setProjectId(value === null ? null : Number(value));
            else if (field === "roleId") setRoleId(value === null ? null : Number(value));
            else setEnabled(Boolean(value));
          }}
          onSubmit={() => void submit()}
        />

        {loading ? (
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-28 w-full rounded-lg" />
            ))}
          </div>
        ) : cards.length === 0 ? (
          <div className="flex h-9 items-center rounded-md bg-muted/30 px-2.5">
            <p className="text-xs text-muted-foreground">{t("empty")}</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {cards.map((card) => (
              <div
                key={card.card_id}
                className="group flex flex-col gap-2 rounded-lg border border-border/55 bg-card p-3 transition-colors hover:border-border"
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="flex min-w-0 items-center gap-1.5">
                    <AiBadge show={card.updated_by === "ai"} />
                    <span className="truncate text-sm font-medium text-foreground/90">{card.title}</span>
                    {!card.enabled && (
                      <span className="shrink-0 rounded-sm bg-destructive/10 px-1 py-px text-[10px] text-destructive">
                        {t("disabled")}
                      </span>
                    )}
                  </div>
                  <div className="flex shrink-0 gap-0.5">
                    <button
                      type="button"
                      className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                      onClick={() => openEdit(card)}
                      aria-label={t("edit")}
                    >
                      <Pencil className="h-3 w-3" />
                    </button>
                    <button
                      type="button"
                      className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-destructive"
                      onClick={() => void remove(card)}
                      aria-label={t("delete")}
                    >
                      <Trash2 className="h-3 w-3" />
                    </button>
                  </div>
                </div>
                <p className="line-clamp-3 min-h-0 flex-1 whitespace-pre-wrap text-xs leading-5 text-muted-foreground">
                  {card.content}
                </p>
                <div className="flex flex-wrap items-center gap-1">
                  {card.category && (
                    <span className="rounded-sm bg-amber-500/10 px-1 py-px text-[10px] text-amber-700 dark:text-amber-400">
                      {card.category}
                    </span>
                  )}
                  {card.project_id !== null && card.project_id !== undefined && (
                    <span className="rounded-sm bg-sky-500/10 px-1 py-px text-[10px] text-sky-700 dark:text-sky-400">
                      {projects.find((project) => project.id === card.project_id)?.name ?? t("boundProject")}
                    </span>
                  )}
                  {card.role_id !== null && card.role_id !== undefined && (
                    <span className="rounded-sm bg-violet-500/10 px-1 py-px text-[10px] text-violet-700 dark:text-violet-400">
                      {roles.find((role) => role.id === card.role_id)?.name ?? t("boundRole")}
                    </span>
                  )}
                  {card.keywords.slice(0, 5).map((kw) => (
                    <span key={kw} className="rounded-sm bg-muted/60 px-1 py-px text-[10px] text-muted-foreground">
                      {kw}
                    </span>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
      </SettingsSection>
    </>
  );
}

/**
 * ArtifactCardThumb 制品卡片缩略图区：
 * - 已有 thumbnail → 直接渲染 <img>；
 * - 否则通过 IntersectionObserver 在卡片靠近视口时懒加载制品详情，
 *   html/css/js 以缩放后的沙箱 iframe 实时预览作为缩略图；text 或加载失败保留图标占位。
 * 整块区域可点击，点击打开制品预览弹窗。
 */
function ArtifactCardThumb({
  item,
  onOpen,
}: {
  item: ArtifactListItemDTO;
  onOpen: () => void;
}) {
  const t = useTranslations("settings.chatPage.artifacts");
  const { resolvedTheme } = useTheme();
  const wrapperRef = React.useRef<HTMLDivElement | null>(null);
  const [detail, setDetail] = React.useState<ArtifactDetailDTO | null>(null);
  const [themeSnapshot, setThemeSnapshot] = React.useState<HTMLVisualThemeSnapshot>({
    colorScheme: "light",
    variables: [],
  });
  // 已拉取到的制品详情缓存（按 artifact_id），避免重复请求。
  const cacheRef = React.useRef<Map<string, ArtifactDetailDTO>>(new Map());

  React.useEffect(() => {
    setThemeSnapshot(captureHTMLVisualThemeSnapshot(resolvedTheme));
  }, [resolvedTheme]);

  React.useEffect(() => {
    const wrapper = wrapperRef.current;
    if (!wrapper) {
      return;
    }
    // 已有缩略图或纯文本制品无需实时预览。
    if (item.thumbnail || item.kind === "text") {
      return;
    }
    let disposed = false;
    const observer = new IntersectionObserver(
      (entries) => {
        const entry = entries[0];
        if (!entry?.isIntersecting || disposed) {
          return;
        }
        observer.disconnect();
        const cached = cacheRef.current.get(item.artifact_id);
        if (cached) {
          setDetail(cached);
          return;
        }
        void (async () => {
          try {
            const token = await resolveAccessToken();
            if (!token || disposed) {
              return;
            }
            const result = await getArtifact(token, item.artifact_id);
            if (disposed) {
              return;
            }
            cacheRef.current.set(item.artifact_id, result);
            setDetail(result);
          } catch {
            // 拉取失败时保持图标占位（detail 保持 null）。
          }
        })();
      },
      { rootMargin: "200px" },
    );
    observer.observe(wrapper);
    return () => {
      disposed = true;
      observer.disconnect();
    };
  }, [item.artifact_id, item.kind, item.thumbnail]);

  const previewKind = detail ? resolveStoredArtifactPreviewKind(detail.kind) : null;

  const previewHTML = React.useMemo(() => {
    if (!detail || !previewKind) {
      return "";
    }
    return buildArtifactPreviewDocument(previewKind, detail.code, themeSnapshot);
  }, [detail, previewKind, themeSnapshot]);

  return (
    <div
      ref={wrapperRef}
      role="button"
      tabIndex={0}
      aria-label={`${t("view")}: ${item.title}`}
      className="h-full w-full cursor-pointer overflow-hidden"
      onClick={onOpen}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onOpen();
        }
      }}
    >
      {item.thumbnail ? (
        <img
          src={item.thumbnail}
          alt=""
          aria-hidden="true"
          loading="lazy"
          decoding="async"
          className="h-full w-full select-none object-cover object-top"
        />
      ) : detail && previewKind ? (
        <div className="relative h-full w-full overflow-hidden">
          <iframe
            title={item.title}
            allow={ARTIFACT_IFRAME_PERMISSIONS}
            sandbox="allow-scripts"
            referrerPolicy="no-referrer"
            srcDoc={previewHTML}
            tabIndex={-1}
            aria-hidden="true"
            style={{
              width: "456px",
              height: "128px",
              position: "absolute",
              top: "50%",
              left: "50%",
              transform: "translate(-50%, -50%) scale(0.62)",
              pointerEvents: "none",
              border: "none",
            }}
            className="block"
          />
        </div>
      ) : (
        <div className="flex h-full items-center justify-center gap-1.5 text-muted-foreground">
          <FileCode2 className="size-4" />
          <span className="text-[10px] font-medium uppercase">{item.kind}</span>
        </div>
      )}
    </div>
  );
}

/**
 * ArtifactsSection 已保存制品（AI 生成的 HTML/JS 等）：缩略图预览/分享/撤销分享/删除。
 */
export function ArtifactsSection() {
  const t = useTranslations("settings.chatPage.artifacts");
  const tNav = useTranslations("common.navigation");
  const resolveErrorMessage = useLocalizedErrorMessage();
  const [items, setItems] = React.useState<ArtifactListItemDTO[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [busy, setBusy] = React.useState<string | null>(null);
  const [shareDialog, setShareDialog] = React.useState<ArtifactListItemDTO | null>(null);
  const [copiedArtifactID, setCopiedArtifactID] = React.useState<string | null>(null);
  const [previewTarget, setPreviewTarget] = React.useState<{
    artifact_id: string;
    title: string;
    shareUrl: string | null;
  } | null>(null);

  const load = React.useCallback(async () => {
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      const result = await listArtifacts(token);
      setItems(result.items);
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

  const share = async (item: ArtifactListItemDTO) => {
    if (busy) return;
    setBusy(item.artifact_id);
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      const share = await createArtifactShare(token, item.artifact_id);
      const updated: ArtifactListItemDTO = {
        ...item,
        share: {
          share_id: share.share_id,
          status: "active",
          title_snapshot: share.title_snapshot,
          created_at: share.created_at,
        },
      };
      setItems((prev) => prev.map((entry) => entry.artifact_id === item.artifact_id ? updated : entry));
      setShareDialog(updated);
    } catch (error) {
      toast.error(t("shareFailed"), { description: resolveErrorMessage(error) });
    } finally {
      setBusy(null);
    }
  };

  const revoke = async (item: ArtifactListItemDTO) => {
    if (busy) return;
    setBusy(item.artifact_id);
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      await revokeArtifactShare(token, item.artifact_id);
      setItems((prev) => prev.map((entry) => entry.artifact_id === item.artifact_id ? { ...entry, share: null } : entry));
      toast.success(t("revoked"));
    } catch (error) {
      toast.error(t("revokeFailed"), { description: resolveErrorMessage(error) });
    } finally {
      setBusy(null);
    }
  };

  const remove = async (item: ArtifactListItemDTO) => {
    if (busy) return;
    setBusy(item.artifact_id);
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      await deleteArtifact(token, item.artifact_id);
      setItems((prev) => prev.filter((entry) => entry.artifact_id !== item.artifact_id));
      toast.success(t("deleted"));
    } catch (error) {
      toast.error(t("deleteFailed"), { description: resolveErrorMessage(error) });
    } finally {
      setBusy(null);
    }
  };

  const shareLink = (item: ArtifactListItemDTO): string | null => {
    if (!item.share) return null;
    return artifactShareUrl(item.share.share_id);
  };

  const openPreview = (item: ArtifactListItemDTO) => {
    setPreviewTarget({
      artifact_id: item.artifact_id,
      title: item.title,
      shareUrl: shareLink(item),
    });
  };

  const copyLink = async (item: ArtifactListItemDTO) => {
    const url = shareLink(item);
    if (!url) return;
    try {
      await navigator.clipboard.writeText(url);
      setCopiedArtifactID(item.artifact_id);
      window.setTimeout(() => {
        setCopiedArtifactID((prev) => (prev === item.artifact_id ? null : prev));
      }, 1500);
    } catch {
      toast.error(t("copyFailed"));
    }
  };

  return (
    <>
      <header className="mb-4">
        <div className="flex items-start justify-between gap-4">
          <h1 className="min-w-0 text-xl font-semibold tracking-[-0.03em] text-foreground md:text-2xl">{tNav("artifacts")}</h1>
        </div>
      </header>
      <SettingsSection title="">
      <div className="space-y-2">
        {loading ? (
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-48 w-full rounded-lg" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="flex h-9 items-center rounded-md bg-muted/30 px-2.5">
            <p className="text-xs text-muted-foreground">{t("empty")}</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {items.map((item) => (
              <div
                key={item.artifact_id}
                className="group flex flex-col gap-2 rounded-lg border border-border/55 bg-card p-3 transition-colors hover:border-border"
              >
                <div className="h-28 shrink-0 overflow-hidden rounded-md border border-border/40 bg-muted/20">
                  <ArtifactCardThumb item={item} onOpen={() => openPreview(item)} />
                </div>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    className="min-w-0 flex-1 truncate text-left text-sm font-medium text-foreground/90 transition-colors hover:text-foreground"
                    onClick={() => openPreview(item)}
                    aria-label={`${t("view")}: ${item.title}`}
                  >
                    {item.title}
                  </button>
                  <span className="shrink-0 text-[10px] text-muted-foreground">{item.updated_at}</span>
                  <div className="flex shrink-0 items-center gap-0.5">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                          onClick={() => openPreview(item)}
                          aria-label={t("view")}
                        >
                          <Eye className="h-3 w-3" />
                        </button>
                      </TooltipTrigger>
                      <TooltipContent side="bottom">{t("view")}</TooltipContent>
                    </Tooltip>
                    {item.share ? (
                      <>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <a
                              href={shareLink(item) ?? ""}
                              target="_blank"
                              rel="noreferrer"
                              className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                              aria-label={t("openLink")}
                            >
                              <ExternalLink className="h-3 w-3" />
                            </a>
                          </TooltipTrigger>
                          <TooltipContent side="bottom">{t("openLink")}</TooltipContent>
                        </Tooltip>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                              onClick={() => void copyLink(item)}
                              disabled={busy === item.artifact_id}
                              aria-label={t("copyLink")}
                            >
                              {copiedArtifactID === item.artifact_id ? (
                                <Check className="h-3 w-3 text-emerald-600 dark:text-emerald-400" />
                              ) : (
                                <Copy className="h-3 w-3" />
                              )}
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="bottom">{t("copyLink")}</TooltipContent>
                        </Tooltip>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-destructive"
                              onClick={() => void revoke(item)}
                              disabled={busy === item.artifact_id}
                              aria-label={t("revokeShare")}
                            >
                              <ShieldX className="h-3 w-3" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="bottom">{t("revokeShare")}</TooltipContent>
                        </Tooltip>
                      </>
                    ) : (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                            onClick={() => void share(item)}
                            disabled={busy === item.artifact_id}
                            aria-label={t("share")}
                          >
                            <Share2 className="h-3 w-3" />
                          </button>
                        </TooltipTrigger>
                        <TooltipContent side="bottom">{t("share")}</TooltipContent>
                      </Tooltip>
                    )}
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-destructive"
                          onClick={() => void remove(item)}
                          disabled={busy === item.artifact_id}
                          aria-label={t("delete")}
                        >
                          <Trash2 className="h-3 w-3" />
                        </button>
                      </TooltipTrigger>
                      <TooltipContent side="bottom">{t("delete")}</TooltipContent>
                    </Tooltip>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <Dialog open={Boolean(shareDialog)} onOpenChange={(next) => { if (!next) setShareDialog(null); }}>
        <DialogContent className="flex min-w-0 max-h-[calc(100svh-2rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-[460px]">
          <div className="min-w-0 flex-1 space-y-4 overflow-y-auto p-5 pb-4">
            <DialogHeader>
              <DialogTitle>{t("shareTitle")}</DialogTitle>
              <DialogDescription>{t("shareDescription")}</DialogDescription>
            </DialogHeader>
            <ArtifactShareLink share={shareDialog?.share ?? null} />
          </div>
          <DialogFooter className="shrink-0 border-t border-border/60 px-5 py-3">
            <Button type="button" variant="ghost" onClick={() => setShareDialog(null)}>
              {t("close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ArtifactPreviewDialog
        open={Boolean(previewTarget)}
        onOpenChange={(next) => { if (!next) setPreviewTarget(null); }}
        artifactId={previewTarget?.artifact_id ?? null}
        title={previewTarget?.title ?? ""}
        shareUrl={previewTarget?.shareUrl ?? null}
      />
      </SettingsSection>
    </>
  );
}
