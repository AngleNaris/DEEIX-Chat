"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Sparkles, CopyPlus, Box, Globe2, SlidersHorizontal, Wrench } from "lucide-react";
import data from "@emoji-mart/data";
import Picker from "@emoji-mart/react";
import {
  Dialog,
  DialogCollapsible,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ProjectDefaultSelector } from "@/features/layouts/components/navigation/project-dialog";
import { listAvailableMCPTools } from "@/shared/api/mcp";
import type { MCPToolDTO } from "@/shared/api/mcp.types";
import { listPublicModels } from "@/shared/api/model";
import type { PublicModelDTO } from "@/shared/api/model.types";
import { listConversationProjects } from "@/shared/api/conversation";
import type { ConversationProjectDTO } from "@/shared/api/conversation.types";
import { listVisibleSkills } from "@/shared/api/skills";
import type { SkillSummaryDTO } from "@/shared/api/skills.types";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { cn } from "@/lib/utils";

const ICON_PRESETS = ["✨", "🦉", "🦊", "🐉", "🧙", "⚔️", "🎨", "🎬", "📝", "💡", "🔮", "🛡️", "🎵", "🧪", "🗡️", "👾", "🤖", "🌙", "⚡", "💎"];

export type RoleDraft = {
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

export const EMPTY_ROLE_DRAFT: RoleDraft = {
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
  setDraft: React.Dispatch<React.SetStateAction<RoleDraft>>;
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
  const [iconPickerOpen, setIconPickerOpen] = React.useState(false);

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
          <div className="flex items-center gap-2">
            <Input
              value={draft.icon}
              maxLength={32}
              placeholder="emoji 或图标名"
              onChange={(event) => update("icon", event.target.value)}
              disabled={submitting}
              className="min-w-0 flex-1"
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-9 shrink-0"
              onClick={() => setIconPickerOpen((open) => !open)}
              disabled={submitting}
            >
              {draft.icon ? <span className="text-sm leading-none">{draft.icon}</span> : <Sparkles className="size-4" strokeWidth={1.8} />}
              <span className="ml-1.5 text-xs">选择 Emoji</span>
            </Button>
          </div>
          {iconPickerOpen ? (
            <div className="relative z-50 overflow-hidden rounded-md border border-border/60 bg-background">
              <Picker
                data={data}
                onEmojiSelect={(emoji: { native?: string }) => {
                  if (emoji?.native) {
                    update("icon", emoji.native);
                  }
                  setIconPickerOpen(false);
                }}
                theme="auto"
                previewPosition="none"
                skinTonePosition="none"
                navPosition="top"
                searchPosition="sticky"
                style={{ width: "100%", maxHeight: 260 }}
              />
            </div>
          ) : null}
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
        <p className="text-xs text-muted-foreground">默认 MCP 工具</p>
        <div className="flex w-full items-center gap-1 rounded-md bg-muted/60 p-1">
          <Button
            type="button"
            aria-pressed={draft.mcpDefaultMode === "inherit"}
            variant="ghost"
            size="sm"
            className={draft.mcpDefaultMode === "inherit"
              ? "h-7 min-w-0 gap-1.5 rounded-sm bg-background px-2 text-foreground shadow-sm hover:bg-background"
              : "h-7 min-w-0 gap-1.5 rounded-sm px-2 text-muted-foreground hover:bg-transparent hover:text-foreground"}
            disabled={submitting}
            onClick={() => update("mcpDefaultMode", "inherit")}
          >
            <Globe2 className="size-3.5 shrink-0" strokeWidth={1.7} />
            <span className="truncate">继承全局</span>
          </Button>
          <Button
            type="button"
            aria-pressed={draft.mcpDefaultMode === "custom"}
            variant="ghost"
            size="sm"
            className={draft.mcpDefaultMode === "custom"
              ? "h-7 min-w-0 gap-1.5 rounded-sm bg-background px-2 text-foreground shadow-sm hover:bg-background"
              : "h-7 min-w-0 gap-1.5 rounded-sm px-2 text-muted-foreground hover:bg-transparent hover:text-foreground"}
            disabled={submitting}
            onClick={() => update("mcpDefaultMode", "custom")}
          >
            <SlidersHorizontal className="size-3.5 shrink-0" strokeWidth={1.7} />
            <span className="truncate">自定义</span>
          </Button>
        </div>
        <DialogCollapsible open={draft.mcpDefaultMode === "custom"}>
          <ProjectDefaultSelector
            icon={Wrench}
            label="选择 MCP 工具"
            description="为该角色预设可用的 MCP 工具"
            emptyLabel="选择 MCP 工具…"
            searchPlaceholder="搜索 MCP 工具…"
            options={mcpTools.map((tool) => ({
              id: tool.id,
              label: tool.displayName || tool.name,
              detail: tool.serverName ?? "",
            }))}
            selectedIDs={draft.defaultMCPToolIDs}
            selectionLimit={128}
            loading={false}
            disabled={submitting}
            onChange={(defaultMCPToolIDs) => update("defaultMCPToolIDs", defaultMCPToolIDs)}
          />
        </DialogCollapsible>
        <DialogCollapsible open={draft.mcpDefaultMode === "inherit"}>
          <p className="pt-1 text-[11px] leading-4 text-muted-foreground">继承全局 MCP 工具配置</p>
        </DialogCollapsible>
      </div>
      <div className="space-y-2 border-t border-border/60 pt-3">
        <ProjectDefaultSelector
          icon={Box}
          label="默认 Skills"
          description="为该角色预设技能"
          emptyLabel="选择技能…"
          searchPlaceholder="搜索技能…"
          options={skills.map((skill) => ({
            id: skill.id,
            label: skill.title,
            detail: skill.description.trim() || (skill.trigger ? `/${skill.trigger}` : ""),
          }))}
          selectedIDs={draft.defaultSkillIDs}
          selectionLimit={128}
          loading={false}
          disabled={submitting}
          onChange={(defaultSkillIDs) => update("defaultSkillIDs", defaultSkillIDs)}
        />
      </div>
    </div>
  );
}

export function RoleDialog({
  draft,
  setDraft,
  onOpenChange,
  onSubmit,
}: {
  draft: RoleDraft | null;
  setDraft: React.Dispatch<React.SetStateAction<RoleDraft | null>>;
  onOpenChange: (open: boolean) => void;
  onSubmit: () => void | Promise<void>;
}) {
  const t = useTranslations("recent.projects");
  const [models, setModels] = React.useState<PublicModelDTO[]>([]);
  const [mcpTools, setMCPTools] = React.useState<MCPToolDTO[]>([]);
  const [skills, setSkills] = React.useState<SkillSummaryDTO[]>([]);
  const [projects, setProjects] = React.useState<ConversationProjectDTO[]>([]);
  const [loading, setLoading] = React.useState(false);

  const open = draft !== null;

  React.useEffect(() => {
    if (!open) {
      return;
    }
    let cancelled = false;
    setLoading(true);
    void resolveAccessToken().then(async (token) => {
      if (!token) {
        return;
      }
      try {
        const [modelItems, toolItems, skillItems, projectItems] = await Promise.all([
          listPublicModels(token),
          listAvailableMCPTools(token),
          listVisibleSkills(token, { page: 1, pageSize: 100 }).then((page) => page.results),
          listConversationProjects(token),
        ]);
        if (!cancelled) {
          setModels(modelItems);
          setMCPTools(toolItems);
          setSkills(skillItems);
          setProjects(projectItems);
        }
      } catch {
        toast.error(t("loadFailed") || "加载数据失败");
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    });
    return () => {
      cancelled = true;
    };
  }, [open, t]);

  const submitting = loading;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setDraft(null);
          onOpenChange(false);
        }
      }}
    >
      <DialogContent className="overflow-hidden sm:max-w-2xl">
        {draft ? (
          <>
            <DialogHeader>
              <DialogTitle>{draft.publicID ? "编辑角色" : "新建角色"}</DialogTitle>
              <DialogDescription>
                角色自带提示词、默认模型与图标；保存后可在左侧角色区直接开始对话
              </DialogDescription>
            </DialogHeader>
            <RoleForm
              draft={draft}
              setDraft={setDraft}
              models={models}
              mcpTools={mcpTools}
              skills={skills}
              projects={projects}
              submitting={submitting}
            />
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setDraft(null)} disabled={submitting}>
                取消
              </Button>
              <Button type="button" onClick={() => void onSubmit()} disabled={submitting || !draft.name.trim()}>
                {draft.publicID ? "保存修改" : "创建角色"}
              </Button>
            </DialogFooter>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
