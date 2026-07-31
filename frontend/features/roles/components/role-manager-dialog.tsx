"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
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
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ScrollText, Sparkles, Trash2, PencilLine, Plus, CopyPlus } from "lucide-react";
import { listAvailableMCPTools } from "@/shared/api/mcp";
import type { MCPToolDTO } from "@/shared/api/mcp.types";
import { listPublicModels } from "@/shared/api/model";
import type { PublicModelDTO } from "@/shared/api/model.types";
import { listConversationProjects } from "@/shared/api/conversation";
import type { ConversationProjectDTO } from "@/shared/api/conversation.types";
import {
  createConversationRole,
  deleteConversationRole,
  listConversationRoles,
  updateConversationRole,
} from "@/shared/api/roles";
import type { ConversationRoleDTO } from "@/shared/api/roles.types";
import { listVisibleSkills } from "@/shared/api/skills";
import type { SkillSummaryDTO } from "@/shared/api/skills.types";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { cn } from "@/lib/utils";

const ICON_PRESETS = ["✨", "🦉", "🦊", "🐉", "🧙", "⚔️", "🎨", "🎬", "📝", "💡", "🔮", "🛡️", "🎵", "🧪", "🗡️", "👾", "🤖", "🌙", "⚡", "💎"];

type RoleDraft = {
  publicID?: string;
  name: string;
  description: string;
  systemPrompt: string;
  model: string;
  mcpDefaultMode: "inherit" | "custom";
  defaultMCPToolIDs: number[];
  defaultSkillIDs: number[];
  color: string;
  icon: string;
};

const EMPTY_DRAFT: RoleDraft = {
  name: "",
  description: "",
  systemPrompt: "",
  model: "",
  mcpDefaultMode: "inherit",
  defaultMCPToolIDs: [],
  defaultSkillIDs: [],
  color: "",
  icon: "",
};

function RoleForm({
  draft,
  setDraft,
  models,
  mcpTools,
  skills,
  projects,
  submitting,
}: {
  draft: RoleDraft;
  setDraft: (next: RoleDraft) => void;
  models: PublicModelDTO[];
  mcpTools: MCPToolDTO[];
  skills: SkillSummaryDTO[];
  projects: ConversationProjectDTO[];
  submitting: boolean;
}) {
  const update = <K extends keyof RoleDraft>(key: K, value: RoleDraft[K]) => {
    setDraft({ ...draft, [key]: value });
  };
  const importFromProject = (project: ConversationProjectDTO | null) => {
    if (!project) {
      return;
    }
    setDraft({
      ...draft,
      name: draft.name || project.name,
      description: project.description,
      systemPrompt: project.systemPrompt,
      mcpDefaultMode: project.mcpDefaultMode,
      defaultMCPToolIDs: project.defaultMCPToolIDs.slice(),
      defaultSkillIDs: project.defaultSkillIDs.slice(),
      color: project.color || draft.color,
      icon: project.icon || draft.icon,
    });
  };
  const toggleMCPTool = (toolID: number) => {
    update(
      "defaultMCPToolIDs",
      draft.defaultMCPToolIDs.includes(toolID)
        ? draft.defaultMCPToolIDs.filter((id) => id !== toolID)
        : [...draft.defaultMCPToolIDs, toolID],
    );
  };
  const toggleSkill = (skillID: number) => {
    update(
      "defaultSkillIDs",
      draft.defaultSkillIDs.includes(skillID)
        ? draft.defaultSkillIDs.filter((id) => id !== skillID)
        : [...draft.defaultSkillIDs, skillID],
    );
  };

  return (
    <div className="min-h-0 space-y-4 overflow-y-auto px-0.5">
      {projects.length > 0 ? (
        <div className="space-y-1 rounded-md border border-border/60 bg-muted/20 p-2.5">
          <Label className="text-xs text-muted-foreground">从项目复制配置</Label>
          <div className="flex items-center gap-2">
            <select
              defaultValue=""
              disabled={submitting}
              onChange={(event) => {
                const project = projects.find((item) => item.publicID === event.target.value) ?? null;
                importFromProject(project);
                event.target.value = "";
              }}
              className="h-8 min-w-0 flex-1 rounded-md border border-input bg-transparent px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
            >
              <option value="">选择要复制的项目…</option>
              {projects.map((project) => (
                <option key={project.publicID} value={project.publicID}>
                  {project.icon ? `${project.icon} ` : ""}{project.name}
                </option>
              ))}
            </select>
            <CopyPlus className="size-4 shrink-0 text-muted-foreground" strokeWidth={1.7} />
          </div>
          <p className="text-[11px] leading-4 text-muted-foreground">复制项目的提示词、MCP 工具、技能与配色到角色，再按需修改</p>
        </div>
      ) : null}
      <div className="space-y-1">
        <Label className="text-xs text-muted-foreground">名称</Label>
        <Input
          value={draft.name}
          maxLength={80}
          placeholder="角色名称"
          onChange={(event) => update("name", event.target.value)}
          disabled={submitting}
          required
        />
      </div>
      <div className="space-y-1">
        <Label className="text-xs text-muted-foreground">描述</Label>
        <Input
          value={draft.description}
          maxLength={255}
          placeholder="角色描述"
          onChange={(event) => update("description", event.target.value)}
          disabled={submitting}
        />
      </div>
      <div className="space-y-1">
        <Label className="text-xs text-muted-foreground">系统提示词</Label>
        <Textarea
          value={draft.systemPrompt}
          maxLength={12000}
          placeholder="角色的人格、能力与行为规则……"
          className="min-h-32 max-h-72 resize-y overflow-y-auto"
          onChange={(event) => update("systemPrompt", event.target.value)}
          disabled={submitting}
        />
      </div>
      <div className="space-y-1">
        <Label className="text-xs text-muted-foreground">默认模型</Label>
        <select
          value={draft.model}
          onChange={(event) => update("model", event.target.value)}
          disabled={submitting}
          className="h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
        >
          <option value="">(跟随全局默认)</option>
          {models.map((model) => (
            <option key={model.platformModelName} value={model.platformModelName}>
              {model.platformModelName} {model.vendor ? `· ${model.vendor}` : ""}
            </option>
          ))}
        </select>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">图标</Label>
          <Input
            value={draft.icon}
            maxLength={32}
            placeholder="emoji 或图标名"
            onChange={(event) => update("icon", event.target.value)}
            disabled={submitting}
          />
          <div className="flex flex-wrap gap-1 pt-1">
            {ICON_PRESETS.map((icon) => {
              const selected = draft.icon === icon;
              return (
                <button
                  key={icon}
                  type="button"
                  disabled={submitting}
                  onClick={() => update("icon", selected ? "" : icon)}
                  aria-label={`图标 ${icon}`}
                  className={cn(
                    "flex size-7 items-center justify-center rounded-md border text-sm transition-colors",
                    selected
                      ? "border-primary bg-primary/10"
                      : "border-border/60 hover:border-primary/40 hover:bg-accent",
                  )}
                >
                  {icon}
                </button>
              );
            })}
          </div>
        </div>
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">颜色</Label>
          <div className="flex items-center gap-2">
            <input
              type="color"
              value={draft.color || "#8b5cf6"}
              onChange={(event) => update("color", event.target.value)}
              disabled={submitting}
              className="h-9 w-10 shrink-0 cursor-pointer rounded-md border border-input bg-transparent p-1"
            />
            <Input
              value={draft.color}
              maxLength={32}
              placeholder="#8b5cf6"
              onChange={(event) => update("color", event.target.value)}
              disabled={submitting}
            />
          </div>
        </div>
      </div>
      <div className="space-y-2 border-t border-border/60 pt-3">
        <div className="flex items-center justify-between">
          <Label className="text-xs text-muted-foreground">默认 MCP 工具</Label>
          <select
            value={draft.mcpDefaultMode}
            onChange={(event) => update("mcpDefaultMode", event.target.value as "inherit" | "custom")}
            disabled={submitting}
            className="h-8 rounded-md border border-input bg-transparent px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
          >
            <option value="inherit">继承全局</option>
            <option value="custom">自定义</option>
          </select>
        </div>
        {draft.mcpDefaultMode === "custom" ? (
          <div className="max-h-28 space-y-1 overflow-y-auto rounded-md border border-border/60 p-2">
            {mcpTools.length === 0 ? (
              <p className="px-2 py-3 text-center text-xs text-muted-foreground">暂无可用 MCP 工具</p>
            ) : (
              mcpTools.map((tool) => {
                const selected = draft.defaultMCPToolIDs.includes(tool.id);
                return (
                  <label
                    key={tool.id}
                    className="flex min-h-8 cursor-pointer items-center gap-2 rounded px-2 py-1 text-xs transition-colors hover:bg-accent"
                  >
                    <input
                      type="checkbox"
                      checked={selected}
                      onChange={() => toggleMCPTool(tool.id)}
                      disabled={submitting}
                      className="size-3.5 shrink-0 accent-primary"
                    />
                    <span className="min-w-0 flex-1 truncate">
                      {tool.displayName || tool.name}
                      {tool.serverName ? <span className="ml-1 text-muted-foreground">({tool.serverName})</span> : null}
                    </span>
                  </label>
                );
              })
            )}
          </div>
        ) : null}
      </div>
      <div className="space-y-2 border-t border-border/60 pt-3">
        <Label className="text-xs text-muted-foreground">默认 Skills</Label>
        <div className="max-h-28 space-y-1 overflow-y-auto rounded-md border border-border/60 p-2">
          {skills.length === 0 ? (
            <p className="px-2 py-3 text-center text-xs text-muted-foreground">暂无可用技能</p>
          ) : (
            skills.map((skill) => {
              const selected = draft.defaultSkillIDs.includes(skill.id);
              return (
                <label
                  key={skill.id}
                  className="flex min-h-8 cursor-pointer items-center gap-2 rounded px-2 py-1 text-xs transition-colors hover:bg-accent"
                >
                  <input
                    type="checkbox"
                    checked={selected}
                    onChange={() => toggleSkill(skill.id)}
                    disabled={submitting}
                    className="size-3.5 shrink-0 accent-primary"
                  />
                  <span className="min-w-0 flex-1 truncate">
                    {skill.trigger || skill.title}
                    <span className="ml-1 text-muted-foreground">{skill.description}</span>
                  </span>
                </label>
              );
            })
          )}
        </div>
      </div>
    </div>
  );
}

export function RoleManagerDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const t = useTranslations("recent.projects");
  const [roles, setRoles] = React.useState<ConversationRoleDTO[]>([]);
  const [projects, setProjects] = React.useState<ConversationProjectDTO[]>([]);
  const [models, setModels] = React.useState<PublicModelDTO[]>([]);
  const [mcpTools, setMCPTools] = React.useState<MCPToolDTO[]>([]);
  const [skills, setSkills] = React.useState<SkillSummaryDTO[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [submitting, setSubmitting] = React.useState(false);
  const [editing, setEditing] = React.useState<RoleDraft | null>(null);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const accessToken = await resolveAccessToken();
      const [roleItems, modelItems, toolItems, skillItems, projectItems] = await Promise.all([
        listConversationRoles(accessToken, { status: "all" }),
        listPublicModels(accessToken),
        listAvailableMCPTools(accessToken),
        listVisibleSkills(accessToken, { page: 1, pageSize: 100 }).then((page) => page.results),
        listConversationProjects(accessToken),
      ]);
      setRoles(roleItems);
      setModels(modelItems);
      setMCPTools(toolItems);
      setSkills(skillItems);
      setProjects(projectItems);
    } catch {
      toast.error(t("loadFailed") || "加载角色失败");
    } finally {
      setLoading(false);
    }
  }, [t]);

  React.useEffect(() => {
    if (open) {
      void load();
    }
  }, [open, load]);

  const close = React.useCallback(() => {
    setEditing(null);
    onOpenChange(false);
  }, [onOpenChange]);

  const commitDraft = React.useCallback(async () => {
    if (!editing) {
      return;
    }
    if (!editing.name.trim()) {
      toast.error("角色名称不能为空");
      return;
    }
    setSubmitting(true);
    try {
      const accessToken = await resolveAccessToken();
      const payload = {
        name: editing.name.trim(),
        description: editing.description,
        systemPrompt: editing.systemPrompt,
        model: editing.model,
        provider: "",
        mcpDefaultMode: editing.mcpDefaultMode,
        defaultMCPToolIDs: editing.defaultMCPToolIDs,
        defaultSkillIDs: editing.defaultSkillIDs,
        color: editing.color,
        icon: editing.icon,
      };
      if (editing.publicID) {
        await updateConversationRole(accessToken, editing.publicID, payload);
      } else {
        await createConversationRole(accessToken, payload);
      }
      toast.success(editing.publicID ? "角色已更新" : "角色已创建");
      setEditing(null);
      await load();
    } catch {
      toast.error("保存角色失败");
    } finally {
      setSubmitting(false);
    }
  }, [editing, load]);

  const removeRole = React.useCallback(
    async (role: ConversationRoleDTO) => {
      if (!window.confirm(`确定删除角色「${role.name}」吗？`)) {
        return;
      }
      try {
        const accessToken = await resolveAccessToken();
        await deleteConversationRole(accessToken, role.publicID);
        toast.success("角色已删除");
        await load();
      } catch {
        toast.error("删除角色失败");
      }
    },
    [load],
  );

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? undefined : close())}>
      <DialogContent className="overflow-hidden sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>角色管理</DialogTitle>
          <DialogDescription>创建和管理独立的助手角色，角色自带提示词、默认模型与图标</DialogDescription>
        </DialogHeader>

        {editing ? (
          <>
            <RoleForm
              draft={editing}
              setDraft={setEditing}
              models={models}
              mcpTools={mcpTools}
              skills={skills}
              projects={projects}
              submitting={submitting}
            />
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setEditing(null)} disabled={submitting}>
                取消
              </Button>
              <Button type="button" onClick={() => void commitDraft()} disabled={submitting || !editing.name.trim()}>
                {editing.publicID ? "保存修改" : "创建角色"}
              </Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-0.5">
              {loading ? (
                <p className="py-8 text-center text-sm text-muted-foreground">加载中…</p>
              ) : roles.length === 0 ? (
                <div className="py-8 text-center">
                  <Sparkles className="mx-auto size-8 text-muted-foreground/50" strokeWidth={1.5} />
                  <p className="mt-2 text-sm text-muted-foreground">还没有角色，点击右下角创建第一个</p>
                </div>
              ) : (
                roles.map((role) => (
                  <div
                    key={role.publicID}
                    className="flex items-center gap-3 rounded-lg border border-border/60 bg-muted/20 px-3 py-2.5"
                  >
                    <div
                      className="flex size-9 shrink-0 items-center justify-center rounded-md text-base"
                      style={{ backgroundColor: role.color || "var(--muted)" }}
                    >
                      {role.icon ? (
                        <span className="text-base">{role.icon}</span>
                      ) : (
                        <ScrollText className="size-4 text-muted-foreground" strokeWidth={1.7} />
                      )}
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium text-foreground">{role.name}</p>
                      <p className="truncate text-xs text-muted-foreground">
                        {role.model || "跟随全局默认模型"}
                        {role.systemPrompt ? ` · ${role.systemPrompt.length} 字提示词` : ""}
                      </p>
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        className="size-8 text-muted-foreground hover:text-foreground"
                        onClick={() =>
                          setEditing({
                            publicID: role.publicID,
                            name: role.name,
                            description: role.description,
                            systemPrompt: role.systemPrompt,
                            model: role.model,
                            mcpDefaultMode: role.mcpDefaultMode,
                            defaultMCPToolIDs: role.defaultMCPToolIDs,
                            defaultSkillIDs: role.defaultSkillIDs,
                            color: role.color,
                            icon: role.icon,
                          })
                        }
                      >
                        <PencilLine className="size-4" strokeWidth={1.7} />
                      </Button>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        className="size-8 text-muted-foreground hover:text-destructive"
                        onClick={() => void removeRole(role)}
                      >
                        <Trash2 className="size-4" strokeWidth={1.7} />
                      </Button>
                    </div>
                  </div>
                ))
              )}
            </div>
            <DialogFooter>
              <Button
                type="button"
                onClick={() =>
                  setEditing({
                    ...EMPTY_DRAFT,
                    model: models[0]?.platformModelName ?? "",
                  })
                }
              >
                <Plus className="mr-1.5 size-4" strokeWidth={1.8} />
                新建角色
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
