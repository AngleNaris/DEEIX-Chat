"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Textarea } from "@/components/ui/textarea";
import {
  Box,
  Check,
  ChevronDown,
  CopyPlus,
  FolderClosed,
  Globe2,
  SlidersHorizontal,
  Sparkles,
  Wrench,
  type LucideIcon,
} from "lucide-react";
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
import { ReasoningEffortSelector } from "@/shared/components/reasoning-effort-selector";
import { REASONING_EFFORT_LEVELS, resolveReasoningEffortForProtocols } from "@/shared/lib/reasoning-effort";
import { parseProtocolsJSON } from "@/shared/lib/model-protocols";

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
  groupName: string;
  reasoningEffort: string;
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
  groupName: "",
  reasoningEffort: "",
};

type RoleSelectorOption = {
  value: string;
  label: string;
  leading?: React.ReactNode;
};

function RoleSingleSelector({
  disabled,
  icon: Icon,
  loading,
  onChange,
  options,
  placeholder,
  value,
}: {
  disabled: boolean;
  icon: LucideIcon;
  loading: boolean;
  onChange: (value: string) => void;
  options: RoleSelectorOption[];
  placeholder: string;
  value: string;
}) {
  const [open, setOpen] = React.useState(false);
  const selected = options.find((option) => option.value === value);

  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
    >
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          className="h-8 w-full justify-between px-3 font-normal shadow-none"
          disabled={disabled || loading}
          aria-expanded={open}
        >
          <span className="flex min-w-0 items-center gap-2">
            {selected?.leading ?? <Icon className="size-3.5 text-muted-foreground" strokeWidth={1.7} />}
            <span className="truncate">{selected?.label ?? placeholder}</span>
          </span>
          <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.7} />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={6} className="w-[min(28rem,calc(100vw-3rem))] p-1.5">
        <div className="max-h-64 space-y-0.5 overflow-y-auto" role="listbox">
          {options.map((option) => {
            const selectedOption = option.value === value;
            return (
              <button
                key={option.value || "__empty__"}
                type="button"
                role="option"
                aria-selected={selectedOption}
                className="flex min-h-9 w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left text-xs transition-colors hover:bg-accent"
                onClick={() => {
                  onChange(option.value);
                  setOpen(false);
                }}
              >
                <span className="flex min-w-0 items-center gap-2">
                  {option.leading ?? <Icon className="size-3.5 text-muted-foreground" strokeWidth={1.7} />}
                  <span className="truncate">{option.label}</span>
                </span>
                {selectedOption ? <Check className="size-3.5 shrink-0 text-primary" strokeWidth={1.8} /> : null}
              </button>
            );
          })}
          {options.length === 0 ? (
            <p className="px-2 py-6 text-center text-xs text-muted-foreground">{placeholder}</p>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function RoleForm({
  draft,
  setDraft,
  models,
  mcpTools,
  skills,
  projects,
  groupOptions,
  submitting,
}: {
  draft: RoleDraft;
  setDraft: React.Dispatch<React.SetStateAction<RoleDraft>>;
  models: PublicModelDTO[];
  mcpTools: MCPToolDTO[];
  skills: SkillSummaryDTO[];
  projects: ConversationProjectDTO[];
  groupOptions: string[];
  submitting: boolean;
}) {
  const update = <K extends keyof RoleDraft>(key: K, value: RoleDraft[K]) => {
    setDraft({ ...draft, [key]: value });
  };
  const selectedModelProtocols = React.useMemo(() => {
    const model = models.find((item) => item.platformModelName === draft.model);
    return model ? parseProtocolsJSON(model.protocolsJSON) : [];
  }, [models, draft.model]);
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
  const [iconPickerOpen, setIconPickerOpen] = React.useState(false);
  const [creatingGroup, setCreatingGroup] = React.useState(false);
  const [newGroupName, setNewGroupName] = React.useState("");
  const confirmNewGroup = () => {
    const name = newGroupName.trim();
    setCreatingGroup(false);
    if (name) {
      update("groupName", name);
    }
  };
  const cancelNewGroup = () => {
    setCreatingGroup(false);
    setNewGroupName("");
  };

  return (
    <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-2">
      {projects.length > 0 ? (
        <div className="space-y-1 rounded-md border border-border/60 bg-muted/20 p-2.5">
          <Label className="text-xs text-muted-foreground">从项目复制配置</Label>
          <RoleSingleSelector
            value=""
            placeholder="选择要复制的项目…"
            icon={CopyPlus}
            loading={submitting}
            disabled={false}
            options={projects.map((project) => ({
              value: project.publicID,
              label: project.name,
              leading: project.icon ? (
                <span className="flex size-3.5 shrink-0 items-center justify-center text-sm leading-none">
                  {project.icon}
                </span>
              ) : undefined,
            }))}
            onChange={(projectID) => {
              const project = projects.find((item) => item.publicID === projectID) ?? null;
              importFromProject(project);
            }}
          />
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
        <Label className="text-xs text-muted-foreground">分组</Label>
        {creatingGroup ? (
          <div className="flex items-center gap-1">
            <Input
              autoFocus
              value={newGroupName}
              maxLength={80}
              placeholder="新分组名称"
              disabled={submitting}
              onChange={(event) => setNewGroupName(event.target.value)}
              onKeyDown={(event) => {
                event.stopPropagation();
                if (event.key === "Enter") {
                  event.preventDefault();
                  confirmNewGroup();
                } else if (event.key === "Escape") {
                  event.preventDefault();
                  cancelNewGroup();
                }
              }}
            />
            <Button
              type="button"
              variant="outline"
              size="icon"
              className="size-8 shrink-0"
              aria-label="确认新建分组"
              disabled={submitting}
              onClick={confirmNewGroup}
            >
              <Check className="size-4" strokeWidth={1.8} />
            </Button>
          </div>
        ) : (
          <RoleSingleSelector
            value={draft.groupName}
            placeholder="未分组"
            icon={FolderClosed}
            loading={submitting}
            disabled={false}
            options={[
              { value: "", label: "未分组" },
              ...Array.from(
                new Set([draft.groupName, ...groupOptions].filter((name) => name.trim())),
              ).map((name) => ({ value: name, label: name })),
              { value: "__new__", label: "新建分组…" },
            ]}
            onChange={(value) => {
              if (value === "__new__") {
                setCreatingGroup(true);
                setNewGroupName("");
              } else {
                update("groupName", value);
              }
            }}
          />
        )}
      </div>
      <div className="space-y-1">
        <Label className="text-xs text-muted-foreground">默认模型</Label>
        <RoleSingleSelector
          value={draft.model}
          placeholder="(跟随全局默认)"
          icon={Sparkles}
          loading={submitting}
          disabled={false}
          options={[
            { value: "", label: "(跟随全局默认)" },
            ...models.map((model) => ({
              value: model.platformModelName,
              label: `${model.platformModelName}${model.vendor ? ` · ${model.vendor}` : ""}`,
            })),
          ]}
          onChange={(value) => update("model", value)}
        />
      </div>
      <div className="space-y-1">
        <Label className="text-xs text-muted-foreground">默认思考强度</Label>
        <ReasoningEffortSelector
          protocols={selectedModelProtocols}
          levels={REASONING_EFFORT_LEVELS}
          value={draft.reasoningEffort}
          onChange={(level) => update("reasoningEffort", level)}
        />
        <p className="text-[11px] leading-4 text-muted-foreground">
          {resolveReasoningEffortForProtocols(selectedModelProtocols)
            ? "留空则跟随用户全局默认"
            : "所选模型端点不支持思考强度时，档位在发送时可能不生效"}
        </p>
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
            <Popover
              open={iconPickerOpen}
              onOpenChange={setIconPickerOpen}
            >
              <PopoverTrigger asChild>
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 min-w-0 flex-1 justify-between px-3 font-normal shadow-none"
                  disabled={submitting}
                  aria-expanded={iconPickerOpen}
                >
                  <span className="flex min-w-0 items-center gap-2">
                    {draft.icon ? (
                      <span className="text-sm leading-none">{draft.icon}</span>
                    ) : (
                      <Sparkles className="size-3.5 text-muted-foreground" strokeWidth={1.8} />
                    )}
                    <span className="truncate text-xs">选择 Emoji</span>
                  </span>
                  <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.7} />
                </Button>
              </PopoverTrigger>
              <PopoverContent
                align="end"
                sideOffset={6}
                collisionPadding={12}
                className="w-[min(24rem,calc(100vw-2rem))] overflow-hidden p-0"
              >
                <Picker
                  data={data}
                  onEmojiSelect={(emoji: { native?: string }) => {
                    if (emoji.native) {
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
              </PopoverContent>
            </Popover>
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
  groupOptions,
}: {
  draft: RoleDraft | null;
  setDraft: React.Dispatch<React.SetStateAction<RoleDraft | null>>;
  onOpenChange: (open: boolean) => void;
  onSubmit: () => void | Promise<void>;
  groupOptions: string[];
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
      <DialogContent className="flex max-h-[min(86vh,760px)] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
        {draft ? (
          <>
            <DialogHeader className="shrink-0 px-5 pb-3 pt-5">
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
              groupOptions={groupOptions}
              submitting={submitting}
            />
            <DialogFooter className="shrink-0 px-5 py-3">
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
