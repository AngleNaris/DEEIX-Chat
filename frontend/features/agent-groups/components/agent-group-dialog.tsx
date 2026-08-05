"use client";

import * as React from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
import { cn } from "@/lib/utils";
import { Check, ChevronDown, ChevronUp, Crown, Plus, Sparkles, Trash2, Users } from "lucide-react";
import { listConversationRoles } from "@/shared/api/roles";
import type { ConversationRoleDTO } from "@/shared/api/roles.types";
import { listPublicModels } from "@/shared/api/model";
import type { PublicModelDTO } from "@/shared/api/model.types";
import {
  addAgentGroupMember,
  changeAgentGroupSupervisor,
  createAgentGroup,
  deleteAgentGroup,
  getAgentGroup,
  removeAgentGroupMember,
  reorderAgentGroupMembers,
  updateAgentGroup,
  updateAgentGroupMember,
} from "@/shared/api/agent-groups";
import type {
  AgentGroupDTO,
  AgentGroupMemberDTO,
  AgentGroupMemberRequest,
} from "@/shared/api/agent-groups.types";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";

export type AgentGroupProjectOption = {
  publicID: string;
  name: string;
};

export type AgentGroupMemberDraft = {
  /** 本地列表渲染键；已持久化成员与 publicID 对应，新建成员仅本地存在。 */
  key: string;
  publicID?: string;
  rolePublicID: string;
  roleName: string;
  roleIcon: string;
  roleColor: string;
  roleModel: string;
  roleProvider: string;
  memberType: "supervisor" | "worker";
  enabled: boolean;
  modelOverride: string;
  dutyInstruction: string;
};

export type AgentGroupDraft = {
  publicID?: string;
  projectID: string;
  projectName: string;
  name: string;
  description: string;
  coordinationPrompt: string;
  supervisor: AgentGroupMemberDraft | null;
  workers: AgentGroupMemberDraft[];
};

let agentGroupDraftKeyCounter = 0;

function nextAgentGroupMemberKey(): string {
  agentGroupDraftKeyCounter += 1;
  return `member-${Date.now().toString(36)}-${agentGroupDraftKeyCounter}`;
}

function agentGroupMemberDraftFromDTO(item: AgentGroupMemberDTO): AgentGroupMemberDraft {
  return {
    key: item.publicID,
    publicID: item.publicID,
    rolePublicID: item.rolePublicID,
    roleName: item.roleName,
    roleIcon: item.roleIcon,
    roleColor: item.roleColor,
    roleModel: item.roleModel,
    roleProvider: item.roleProvider,
    memberType: item.memberType,
    enabled: item.enabled,
    modelOverride: item.modelOverride,
    dutyInstruction: item.dutyInstruction,
  };
}

export function agentGroupDraftFromDTO(group: AgentGroupDTO): AgentGroupDraft {
  const supervisor = group.members.find((member) => member.memberType === "supervisor") ?? null;
  return {
    publicID: group.publicID,
    projectID: group.projectID,
    projectName: group.projectName,
    name: group.name,
    description: group.description,
    coordinationPrompt: group.coordinationPrompt,
    supervisor: supervisor ? agentGroupMemberDraftFromDTO(supervisor) : null,
    workers: group.members
      .filter((member) => member.memberType === "worker")
      .map(agentGroupMemberDraftFromDTO),
  };
}

export function emptyAgentGroupMemberDraft(role: ConversationRoleDTO): AgentGroupMemberDraft {
  return {
    key: nextAgentGroupMemberKey(),
    rolePublicID: role.publicID,
    roleName: role.name,
    roleIcon: role.icon,
    roleColor: role.color,
    roleModel: role.model,
    roleProvider: role.provider,
    memberType: "worker",
    enabled: true,
    modelOverride: "",
    dutyInstruction: "",
  };
}

export function emptyAgentGroupDraft(projectID: string, projectName: string): AgentGroupDraft {
  return {
    projectID,
    projectName,
    name: "",
    description: "",
    coordinationPrompt: "",
    supervisor: null,
    workers: [],
  };
}

function toMemberRequest(member: AgentGroupMemberDraft): AgentGroupMemberRequest {
  return {
    rolePublicID: member.rolePublicID,
    modelOverride: member.modelOverride || undefined,
    dutyInstruction: member.dutyInstruction || undefined,
  };
}

type SelectorOption = {
  value: string;
  label: string;
  leading?: React.ReactNode;
};

function MemberSingleSelector({
  disabled,
  icon: Icon,
  loading,
  onChange,
  options,
  placeholder,
  value,
}: {
  disabled: boolean;
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>;
  loading: boolean;
  onChange: (value: string) => void;
  options: SelectorOption[];
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
            {selected?.leading ?? <Icon className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.7} />}
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
                  {option.leading ?? <Icon className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.7} />}
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

function RoleChip({ color, icon, name }: { color: string; icon: string; name: string }) {
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <span
        className="flex size-4 shrink-0 items-center justify-center rounded-sm text-[10px] leading-none"
        style={{ backgroundColor: color || "var(--muted)" }}
      >
        {icon || "✦"}
      </span>
      <span className="truncate">{name}</span>
    </span>
  );
}

/** 模型覆盖选择器：空值 = 继承角色默认模型，选中项 = 自定义覆盖。 */
function ModelOverrideSelector({
  disabled,
  member,
  models,
  onModelChange,
}: {
  disabled: boolean;
  member: AgentGroupMemberDraft;
  models: PublicModelDTO[];
  onModelChange: (modelOverride: string) => void;
}) {
  const overrideOptions: SelectorOption[] = [
    { value: "", label: `继承角色默认：${member.roleModel || "未设置"}` },
    ...models.map((model) => ({
      value: model.platformModelName,
      label: `${model.platformModelName}${model.vendor ? ` · ${model.vendor}` : ""}`,
    })),
  ];
  const overridden = Boolean(member.modelOverride);

  return (
    <div className="min-w-0 flex-1">
      <MemberSingleSelector
        value={member.modelOverride}
        placeholder="模型覆盖…"
        icon={Sparkles}
        loading={false}
        disabled={disabled}
        options={overrideOptions}
        onChange={onModelChange}
      />
      <p className="mt-0.5 truncate text-[11px] text-muted-foreground">
        {overridden ? `自定义覆盖：${member.modelOverride}` : `继承角色默认：${member.roleModel || "未设置"}`}
      </p>
    </div>
  );
}

function MemberDutyInput({
  disabled,
  member,
  onDutyChange,
}: {
  disabled: boolean;
  member: AgentGroupMemberDraft;
  onDutyChange: (dutyInstruction: string) => void;
}) {
  return (
    <Textarea
      value={member.dutyInstruction}
      maxLength={4000}
      placeholder="成员职责说明……"
      className="min-h-14 resize-y text-xs"
      disabled={disabled}
      onChange={(event) => onDutyChange(event.target.value)}
    />
  );
}

function WorkerRow({
  disabled,
  index,
  member,
  models,
  onEnabledChange,
  onDutyChange,
  onModelChange,
  onMove,
  onRemove,
  total,
}: {
  disabled: boolean;
  index: number;
  member: AgentGroupMemberDraft;
  models: PublicModelDTO[];
  onEnabledChange: (enabled: boolean) => void;
  onDutyChange: (dutyInstruction: string) => void;
  onModelChange: (modelOverride: string) => void;
  onMove: (direction: -1 | 1) => void;
  onRemove: () => void;
  total: number;
}) {
  return (
    <div
      className={cn(
        "space-y-1.5 rounded-md border border-border/60 bg-muted/20 p-2.5",
        !member.enabled && "opacity-60",
      )}
    >
      <div className="flex items-center gap-2">
        <RoleChip color={member.roleColor} icon={member.roleIcon} name={member.roleName} />
        <div className="ml-auto flex shrink-0 items-center gap-0.5">
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-6"
            aria-label="上移"
            title="上移"
            disabled={disabled || index === 0}
            onClick={onMove.bind(null, -1)}
          >
            <ChevronUp className="size-3.5" strokeWidth={1.7} />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-6"
            aria-label="下移"
            title="下移"
            disabled={disabled || index === total - 1}
            onClick={onMove.bind(null, 1)}
          >
            <ChevronDown className="size-3.5" strokeWidth={1.7} />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-6 text-muted-foreground hover:text-destructive"
            aria-label="移除成员"
            title="移除成员"
            disabled={disabled}
            onClick={onRemove}
          >
            <Trash2 className="size-3.5" strokeWidth={1.7} />
          </Button>
        </div>
      </div>
      <div className="flex items-center gap-2">
        <div className="flex items-center gap-1.5">
          <Switch
            checked={member.enabled}
            disabled={disabled}
            aria-label="启用成员"
            onCheckedChange={onEnabledChange}
          />
          <span className="text-[11px] text-muted-foreground">{member.enabled ? "启用" : "停用"}</span>
        </div>
        <ModelOverrideSelector disabled={disabled} member={member} models={models} onModelChange={onModelChange} />
      </div>
      <MemberDutyInput disabled={disabled} member={member} onDutyChange={onDutyChange} />
    </div>
  );
}

function SupervisorSection({
  createMode,
  disabled,
  draft,
  models,
  roles,
  setDraft,
}: {
  createMode: boolean;
  disabled: boolean;
  draft: AgentGroupDraft;
  models: PublicModelDTO[];
  roles: ConversationRoleDTO[];
  setDraft: React.Dispatch<React.SetStateAction<AgentGroupDraft>>;
}) {
  const update = (patch: Partial<AgentGroupMemberDraft>) => {
    setDraft((prev) => (prev?.supervisor ? { ...prev, supervisor: { ...prev.supervisor, ...patch } } : prev));
  };
  const supervisor = draft.supervisor;

  return (
    <div className="space-y-2">
      <p className="flex items-center gap-1 text-xs text-muted-foreground">
        <Crown className="size-3.5 shrink-0" strokeWidth={1.7} />
        主管
      </p>
      {createMode ? (
        <MemberSingleSelector
          value={supervisor?.rolePublicID ?? ""}
          placeholder="选择主管角色…"
          icon={Crown}
          loading={false}
          disabled={disabled}
          options={roles.map((role) => ({
            value: role.publicID,
            label: role.name,
            leading: (
              <span
                className="flex size-3.5 shrink-0 items-center justify-center rounded-sm text-[10px] leading-none"
                style={{ backgroundColor: role.color || "var(--muted)" }}
              >
                {role.icon || "✦"}
              </span>
            ),
          }))}
          onChange={(rolePublicID) => {
            const role = roles.find((item) => item.publicID === rolePublicID);
            if (!role) {
              return;
            }
            const member = emptyAgentGroupMemberDraft(role);
            setDraft((prev) => ({ ...prev, supervisor: { ...member, memberType: "supervisor", enabled: true } }));
          }}
        />
      ) : supervisor ? (
        <div className="space-y-1.5 rounded-md border border-border/60 bg-muted/20 p-2.5">
          <div className="flex items-center gap-2">
            <RoleChip color={supervisor.roleColor} icon={supervisor.roleIcon} name={supervisor.roleName} />
            <div className="ml-auto shrink-0">
              <ChangeSupervisorMenu
                disabled={disabled}
                workers={draft.workers}
                setDraft={setDraft}
              />
            </div>
          </div>
          <ModelOverrideSelector disabled={disabled} member={supervisor} models={models} onModelChange={(value) => update({ modelOverride: value })} />
          <MemberDutyInput disabled={disabled} member={supervisor} onDutyChange={(value) => update({ dutyInstruction: value })} />
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">主管缺失，请重新加载群组</p>
      )}
    </div>
  );
}

/** 更换主管：把某个工作成员提升为主管（原主管自动转为工作成员）。 */
function ChangeSupervisorMenu({
  disabled,
  workers,
  setDraft,
}: {
  disabled: boolean;
  workers: AgentGroupMemberDraft[];
  setDraft: React.Dispatch<React.SetStateAction<AgentGroupDraft>>;
}) {
  return (
    <MemberSingleSelector
      value=""
      placeholder="更换主管…"
      icon={Crown}
      loading={false}
      disabled={disabled || workers.length === 0}
      options={workers.map((worker) => ({
        value: worker.key,
        label: worker.roleName,
        leading: (
          <span
            className="flex size-3.5 shrink-0 items-center justify-center rounded-sm text-[10px] leading-none"
            style={{ backgroundColor: worker.roleColor || "var(--muted)" }}
          >
            {worker.roleIcon || "✦"}
          </span>
        ),
      }))}
      onChange={(key) => {
        setDraft((prev) => {
          if (!prev?.supervisor) {
            return prev;
          }
          const promoted = prev.workers.find((worker) => worker.key === key);
          if (!promoted) {
            return prev;
          }
          const oldSupervisor: AgentGroupMemberDraft = {
            ...prev.supervisor,
            memberType: "worker",
          };
          return {
            ...prev,
            supervisor: { ...promoted, memberType: "supervisor", enabled: true },
            workers: [oldSupervisor, ...prev.workers.filter((worker) => worker.key !== key)],
          };
        });
      }}
    />
  );
}

function AgentGroupForm({
  createMode,
  draft,
  models,
  projects,
  roles,
  setDraft,
  submitting,
}: {
  createMode: boolean;
  draft: AgentGroupDraft;
  models: PublicModelDTO[];
  projects?: AgentGroupProjectOption[];
  roles: ConversationRoleDTO[];
  setDraft: React.Dispatch<React.SetStateAction<AgentGroupDraft>>;
  submitting: boolean;
}) {
  const update = <K extends keyof AgentGroupDraft>(key: K, value: AgentGroupDraft[K]) => {
    setDraft({ ...draft, [key]: value });
  };
  const usedRoleIDs = new Set<string>();
  if (draft.supervisor) {
    usedRoleIDs.add(draft.supervisor.rolePublicID);
  }
  for (const worker of draft.workers) {
    usedRoleIDs.add(worker.rolePublicID);
  }
  const availableRoles = roles.filter((role) => !usedRoleIDs.has(role.publicID));

  return (
    <div className="min-h-0 space-y-4 overflow-y-auto px-0.5">
      <div className="space-y-1">
        <Label className="text-xs text-muted-foreground">名称</Label>
        <Input
          value={draft.name}
          maxLength={80}
          placeholder="群组名称"
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
          placeholder="群组描述"
          onChange={(event) => update("description", event.target.value)}
          disabled={submitting}
        />
      </div>
      {createMode ? (
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">所属项目</Label>
          {projects && projects.length > 1 ? (
            <Select
              value={draft.projectID}
              onValueChange={(projectID) => {
                update("projectID", projectID);
                const project = projects.find((item) => item.publicID === projectID);
                if (project) {
                  update("projectName", project.name);
                }
              }}
              disabled={submitting}
            >
              <SelectTrigger className="h-8 text-xs">
                <SelectValue placeholder="选择项目" />
              </SelectTrigger>
              <SelectContent>
                {projects.map((project) => (
                  <SelectItem key={project.publicID} value={project.publicID}>
                    {project.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <p className="px-1 text-xs text-muted-foreground">{draft.projectName}</p>
          )}
        </div>
      ) : null}
      <div className="space-y-1">
        <Label className="text-xs text-muted-foreground">群组协调提示词</Label>
        <Textarea
          value={draft.coordinationPrompt}
          maxLength={12000}
          placeholder="群组分工与协作规则，主管据此分派任务……"
          className="min-h-24 max-h-56 resize-y overflow-y-auto"
          onChange={(event) => update("coordinationPrompt", event.target.value)}
          disabled={submitting}
        />
      </div>
      <div className="space-y-2 border-t border-border/60 pt-3">
        <SupervisorSection
          createMode={createMode}
          disabled={submitting}
          draft={draft}
          models={models}
          roles={roles}
          setDraft={setDraft}
        />
      </div>
      <div className="space-y-2 border-t border-border/60 pt-3">
        <div className="flex items-center gap-1">
          <p className="flex items-center gap-1 text-xs text-muted-foreground">
            <Users className="size-3.5 shrink-0" strokeWidth={1.7} />
            工作成员
          </p>
          <span className="text-[11px] text-muted-foreground/60">{draft.workers.length}/31</span>
          <div className="ml-auto min-w-0">
            <MemberSingleSelector
              value=""
              placeholder="添加成员…"
              icon={Plus}
              loading={false}
              disabled={submitting || availableRoles.length === 0}
              options={availableRoles.map((role) => ({
                value: role.publicID,
                label: role.name,
                leading: (
                  <span
                    className="flex size-3.5 shrink-0 items-center justify-center rounded-sm text-[10px] leading-none"
                    style={{ backgroundColor: role.color || "var(--muted)" }}
                  >
                    {role.icon || "✦"}
                  </span>
                ),
              }))}
              onChange={(rolePublicID) => {
                const role = roles.find((item) => item.publicID === rolePublicID);
                if (!role) {
                  return;
                }
                setDraft((prev) => ({ ...prev, workers: [...prev.workers, emptyAgentGroupMemberDraft(role)] }));
              }}
            />
          </div>
        </div>
        {draft.workers.length === 0 ? (
          <p className="py-1 text-[11px] text-muted-foreground/60">暂无工作成员，可稍后添加</p>
        ) : (
          <div className="space-y-2">
            {draft.workers.map((member, index) => (
              <WorkerRow
                key={member.key}
                disabled={submitting}
                index={index}
                member={member}
                models={models}
                total={draft.workers.length}
                onEnabledChange={(enabled) =>
                  setDraft((prev) => ({
                    ...prev,
                    workers: prev.workers.map((item) => (item.key === member.key ? { ...item, enabled } : item)),
                  }))
                }
                onDutyChange={(dutyInstruction) =>
                  setDraft((prev) => ({
                    ...prev,
                    workers: prev.workers.map((item) => (item.key === member.key ? { ...item, dutyInstruction } : item)),
                  }))
                }
                onModelChange={(modelOverride) =>
                  setDraft((prev) => ({
                    ...prev,
                    workers: prev.workers.map((item) => (item.key === member.key ? { ...item, modelOverride } : item)),
                  }))
                }
                onMove={(direction) =>
                  setDraft((prev) => {
                    const from = prev.workers.findIndex((item) => item.key === member.key);
                    const to = from + direction;
                    if (from < 0 || to < 0 || to >= prev.workers.length) {
                      return prev;
                    }
                    const next = prev.workers.slice();
                    const [moved] = next.splice(from, 1);
                    next.splice(to, 0, moved);
                    return { ...prev, workers: next };
                  })
                }
                onRemove={() =>
                  setDraft((prev) => ({ ...prev, workers: prev.workers.filter((item) => item.key !== member.key) }))
                }
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function agentGroupMemberDraftEquals(a: AgentGroupMemberDraft, b: AgentGroupMemberDTO): boolean {
  return (
    a.enabled === b.enabled &&
    a.modelOverride === b.modelOverride &&
    a.dutyInstruction === b.dutyInstruction
  );
}

/** 编辑保存：PATCH 群组字段 + 按差异调用成员接口（新增 → 换主管 → 删除 → 更新 → 排序）。 */
async function saveExistingAgentGroup(
  token: string,
  draft: AgentGroupDraft,
  originalMembers: AgentGroupMemberDTO[],
): Promise<AgentGroupDTO> {
  if (!draft.publicID) {
    throw new Error("missing group public id");
  }
  let group = await updateAgentGroup(token, draft.publicID, {
    name: draft.name.trim(),
    description: draft.description,
    coordinationPrompt: draft.coordinationPrompt,
  });

  const finalMembers = draft.supervisor ? [draft.supervisor, ...draft.workers] : [...draft.workers];

  // 1. 新增成员（尚无 publicID）。
  for (const member of finalMembers) {
    if (member.publicID) {
      continue;
    }
    group = await addAgentGroupMember(token, group.publicID, toMemberRequest(member));
  }

  // 2. 更换主管（新主管必须已是成员；原主管自动转为工作成员）。
  const originalSupervisorID = originalMembers.find((member) => member.memberType === "supervisor")?.publicID;
  const finalSupervisorID = draft.supervisor?.publicID;
  if (finalSupervisorID && finalSupervisorID !== originalSupervisorID) {
    group = await changeAgentGroupSupervisor(token, group.publicID, { memberPublicID: finalSupervisorID });
  }

  // 3. 移除已删除成员（主管已更换，可安全移除）。
  const finalIDs = new Set(finalMembers.map((member) => member.publicID).filter((id): id is string => Boolean(id)));
  for (const member of originalMembers) {
    if (!finalIDs.has(member.publicID)) {
      group = await removeAgentGroupMember(token, group.publicID, member.publicID);
    }
  }

  // 4. 更新启停/模型覆盖/职责说明。
  for (const member of finalMembers) {
    if (!member.publicID) {
      continue;
    }
    const previous = originalMembers.find((item) => item.publicID === member.publicID);
    if (!previous || agentGroupMemberDraftEquals(member, previous)) {
      continue;
    }
    group = await updateAgentGroupMember(token, group.publicID, member.publicID, {
      enabled: member.enabled,
      modelOverride: member.modelOverride || undefined,
      dutyInstruction: member.dutyInstruction || undefined,
    });
  }

  // 5. 排序：映射本地成员到持久化 publicID（新增成员已拿到 publicID）。
  const idByRolePublicID = new Map<string, string>();
  for (const member of group.members) {
    idByRolePublicID.set(member.rolePublicID, member.publicID);
  }
  const finalOrder = finalMembers
    .map((member) => member.publicID ?? idByRolePublicID.get(member.rolePublicID))
    .filter((id): id is string => Boolean(id));
  const originalOrder = originalMembers.map((member) => member.publicID);
  if (JSON.stringify(originalOrder) !== JSON.stringify(finalOrder)) {
    group = await reorderAgentGroupMembers(token, group.publicID, { orderedPublicIDs: finalOrder });
  }

  return group;
}

export function AgentGroupDialog({
  draft,
  setDraft,
  onOpenChange,
  onSaved,
  onDeleted,
  projects,
}: {
  draft: AgentGroupDraft | null;
  setDraft: React.Dispatch<React.SetStateAction<AgentGroupDraft | null>>;
  onOpenChange: (open: boolean) => void;
  onSaved: (group: AgentGroupDTO) => void | Promise<void>;
  onDeleted: (groupPublicID: string) => void | Promise<void>;
  projects?: AgentGroupProjectOption[];
}) {
  const [roles, setRoles] = React.useState<ConversationRoleDTO[]>([]);
  const [models, setModels] = React.useState<PublicModelDTO[]>([]);
  const [originalMembers, setOriginalMembers] = React.useState<AgentGroupMemberDTO[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [submitting, setSubmitting] = React.useState(false);

  const open = draft !== null;
  const createMode = !draft?.publicID;

  // 打开时加载角色与模型；编辑模式再从服务端刷新群组，避免基于过期列表编辑。
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
        const [roleItems, modelItems] = await Promise.all([
          listConversationRoles(token, { status: "active" }),
          listPublicModels(token),
        ]);
        if (cancelled) {
          return;
        }
        setRoles(roleItems);
        setModels(modelItems);
        if (draft?.publicID) {
          const group = await getAgentGroup(token, draft.publicID);
          if (!cancelled) {
            setOriginalMembers(group.members);
            setDraft(agentGroupDraftFromDTO(group));
          }
        } else {
          setOriginalMembers([]);
        }
      } catch {
        toast.error("加载群组数据失败");
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    });
    return () => {
      cancelled = true;
    };
  }, [open, draft?.publicID, setDraft]);

  const commitDraft = React.useCallback(async () => {
    if (!draft) {
      return;
    }
    if (!draft.name.trim()) {
      toast.error("群组名称不能为空");
      return;
    }
    if (!draft.supervisor) {
      toast.error("请选择主管角色");
      return;
    }
    setSubmitting(true);
    try {
      const token = await resolveAccessToken();
      if (!token) {
        throw new Error("missing access token");
      }
      let group: AgentGroupDTO;
      if (draft.publicID) {
        group = await saveExistingAgentGroup(token, draft, originalMembers);
        toast.success("群组已更新");
      } else {
        group = await createAgentGroup(token, {
          projectID: draft.projectID,
          name: draft.name.trim(),
          description: draft.description,
          coordinationPrompt: draft.coordinationPrompt,
          supervisor: toMemberRequest(draft.supervisor),
          workers: draft.workers.map(toMemberRequest),
        });
        toast.success("群组已创建");
      }
      setDraft(null);
      await onSaved(group);
    } catch {
      toast.error("保存群组失败");
    } finally {
      setSubmitting(false);
    }
  }, [draft, onSaved, originalMembers, setDraft]);

  const removeGroup = React.useCallback(async () => {
    if (!draft?.publicID) {
      return;
    }
    if (!window.confirm(`确定删除群组「${draft.name}」吗？`)) {
      return;
    }
    try {
      const token = await resolveAccessToken();
      if (!token) {
        throw new Error("missing access token");
      }
      await deleteAgentGroup(token, draft.publicID);
      toast.success("群组已删除");
      const publicID = draft.publicID;
      setDraft(null);
      await onDeleted(publicID);
    } catch {
      toast.error("删除群组失败：已有会话或运行历史的群组无法删除");
    }
  }, [draft, onDeleted, setDraft]);

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
              <DialogTitle>{createMode ? "新建群组" : `编辑群组：${draft.name}`}</DialogTitle>
              <DialogDescription>
                群组由主管与若干成员协作完成对话；成员可独立启用、覆盖模型并说明职责
              </DialogDescription>
            </DialogHeader>
            <AgentGroupForm
              createMode={createMode}
              draft={draft}
              models={models}
              projects={projects}
              roles={roles}
              setDraft={setDraft}
              submitting={submitting || loading}
            />
            <DialogFooter className="items-center gap-2">
              {!createMode && draft.publicID ? (
                <Button
                  type="button"
                  variant="ghost"
                  className="mr-auto text-muted-foreground hover:text-destructive"
                  disabled={submitting}
                  onClick={() => void removeGroup()}
                >
                  <Trash2 className="size-3.5" strokeWidth={1.7} />
                  删除群组
                </Button>
              ) : null}
              <Button type="button" variant="ghost" onClick={() => setDraft(null)} disabled={submitting}>
                取消
              </Button>
              <Button
                type="button"
                onClick={() => void commitDraft()}
                disabled={submitting || loading || !draft.name.trim() || !draft.supervisor}
              >
                {createMode ? "创建群组" : "保存修改"}
              </Button>
            </DialogFooter>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
