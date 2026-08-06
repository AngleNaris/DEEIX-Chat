"use client";

import * as React from "react";
import { PencilLine, Users } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Spinner } from "@/components/ui/spinner";
import {
  AgentGroupDialog,
  agentGroupDraftFromDTO,
  type AgentGroupDraft,
} from "@/features/agent-groups/components/agent-group-dialog";
import { cn } from "@/lib/utils";
import { getAgentGroup } from "@/shared/api/agent-groups";
import type { AgentGroupDTO, AgentGroupMemberDTO } from "@/shared/api/agent-groups.types";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { useIsMobile } from "@/shared/hooks/use-mobile";

/** 成员有效模型：优先模型覆盖，其次角色默认模型。 */
function resolveMemberModel(member: AgentGroupMemberDTO): { name: string; overridden: boolean } {
  const override = member.modelOverride?.trim() ?? "";
  if (override) {
    return { name: override, overridden: true };
  }
  return { name: member.roleModel?.trim() || "未设置", overridden: false };
}

function GroupMemberRow({ member }: { member: AgentGroupMemberDTO }) {
  const model = resolveMemberModel(member);
  return (
    <div className="flex min-w-0 items-center gap-2 py-1">
      <span
        className="flex size-5 shrink-0 items-center justify-center rounded-sm text-[10px] leading-none"
        style={{ backgroundColor: member.roleColor || "var(--muted)" }}
      >
        {member.roleIcon || "✦"}
      </span>
      <span className="flex min-w-0 flex-1 items-center gap-1.5">
        <span className="min-w-0 truncate text-xs font-medium">{member.roleName}</span>
        {member.memberType === "supervisor" ? (
          <span className="shrink-0 rounded bg-muted px-1 py-px text-[9px] leading-3 text-muted-foreground">
            主管
          </span>
        ) : null}
      </span>
      <span
        className={cn(
          "max-w-36 shrink-0 truncate font-mono text-[10px]",
          model.overridden ? "text-foreground" : "text-muted-foreground",
        )}
        title={model.overridden ? `${member.roleName} 覆盖模型` : `${member.roleName} 角色默认模型`}
      >
        {model.overridden ? "★ " : ""}
        {model.name}
      </span>
    </div>
  );
}

function GroupConfigPanelBody({
  group,
  groupName,
  loading,
  failed,
  onEdit,
}: {
  group: AgentGroupDTO | null;
  groupName: string;
  loading: boolean;
  failed: boolean;
  onEdit: () => void;
}) {
  const supervisor = group?.members.find((member) => member.publicID === group.supervisorMemberID);
  const sortedMembers = React.useMemo(
    () =>
      group?.members
        .slice()
        .sort((a, b) => a.sortOrder - b.sortOrder || a.createdAt.localeCompare(b.createdAt)) ?? [],
    [group],
  );

  if (loading) {
    return (
      <div className="flex min-h-32 items-center justify-center gap-2 text-xs text-muted-foreground">
        <Spinner className="size-4" />
        加载群组配置…
      </div>
    );
  }

  if (failed || !group) {
    return (
      <div className="flex min-h-32 flex-col items-center justify-center gap-2 px-4 text-center">
        <span className="max-w-full truncate text-sm font-medium">{groupName}</span>
        <span className="text-xs text-muted-foreground">群组配置加载失败</span>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <span className="flex size-6 shrink-0 items-center justify-center rounded-md bg-muted/60 text-muted-foreground">
          <Users aria-hidden className="size-3.5" strokeWidth={1.7} />
        </span>
        <span className="min-w-0 flex-1 truncate text-sm font-medium">{group.name}</span>
        <span className="shrink-0 rounded bg-muted px-1.5 py-px font-mono text-[10px] text-muted-foreground">
          v{group.revision}
        </span>
      </div>

      {supervisor ? (
        <div className="mt-1 flex items-center gap-2 rounded-md border border-border/60 bg-muted/20 px-2 py-1.5">
          <span
            className="flex size-6 shrink-0 items-center justify-center rounded-full text-xs leading-none"
            style={{ backgroundColor: supervisor.roleColor || "var(--muted)" }}
          >
            {supervisor.roleIcon || "✦"}
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1">
              <span className="min-w-0 truncate text-xs font-medium">{supervisor.roleName}</span>
              <span className="shrink-0 rounded bg-primary/10 px-1 py-px text-[9px] leading-3 text-primary">
                主管
              </span>
            </div>
            <div className="truncate font-mono text-[10px] text-muted-foreground">
              {resolveMemberModel(supervisor).name}
            </div>
          </div>
        </div>
      ) : null}

      <div className="mt-0.5 flex items-center justify-between gap-2 px-0.5">
        <span className="min-w-0 truncate text-[10px] text-muted-foreground">
          共 {sortedMembers.length} 名成员
          {group.description ? ` · ${group.description}` : ""}
        </span>
      </div>

      <div className="min-h-0 overflow-y-auto rounded-md border border-border/60 bg-muted/20 px-2 py-1">
        {sortedMembers.map((member) => (
          <GroupMemberRow key={member.publicID} member={member} />
        ))}
      </div>

      <Button
        type="button"
        variant="outline"
        size="sm"
        className="mt-1.5 h-8 w-full justify-start gap-1.5 text-xs"
        onClick={onEdit}
      >
        <PencilLine className="size-3.5" strokeWidth={1.7} />
        编辑群组
      </Button>
    </div>
  );
}

/**
 * 群组会话头部“群组配置”入口（只读）：桌面 Popover、移动端 Sheet。
 * 展示群组名称、主管、成员数量、每个成员的有效模型与配置版本；
 * 底部提供“编辑群组”命令，复用 AgentGroupDialog。
 */
export function AgentGroupConfigButton({
  groupPublicID,
  groupName = "",
}: {
  groupPublicID: string;
  groupName?: string;
}) {
  const isMobile = useIsMobile();
  const [open, setOpen] = React.useState(false);
  const [group, setGroup] = React.useState<AgentGroupDTO | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [failed, setFailed] = React.useState(false);
  const [draft, setDraft] = React.useState<AgentGroupDraft | null>(null);

  React.useEffect(() => {
    if (!open) {
      return;
    }
    let cancelled = false;
    setLoading(true);
    setFailed(false);
    void resolveAccessToken().then(async (token) => {
      if (!token) {
        if (!cancelled) {
          setFailed(true);
          setLoading(false);
        }
        return;
      }
      try {
        const data = await getAgentGroup(token, groupPublicID);
        if (!cancelled) {
          setGroup(data);
        }
      } catch {
        if (!cancelled) {
          setFailed(true);
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    });
    return () => {
      cancelled = true;
    };
  }, [open, groupPublicID]);

  const openEdit = React.useCallback(() => {
    if (!group) {
      return;
    }
    setDraft(agentGroupDraftFromDTO(group));
  }, [group]);

  const trigger = (
    <Button
      type="button"
      variant="ghost"
      aria-label={`群组配置：${groupName}`}
      title="群组配置"
      className="inline-flex h-7 max-w-44 shrink-0 items-center gap-1.5 rounded-lg px-2 text-xs font-normal text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
    >
      <Users aria-hidden className="size-3.5 shrink-0" strokeWidth={1.8} />
      <span className="min-w-0 truncate">{groupName || "群组"}</span>
    </Button>
  );

  const panel = (
    <GroupConfigPanelBody
      group={group}
      groupName={groupName}
      loading={loading}
      failed={failed}
      onEdit={() => void openEdit()}
    />
  );

  return (
    <>
      {isMobile ? (
        <Sheet open={open} onOpenChange={setOpen}>
          <SheetTrigger asChild>{trigger}</SheetTrigger>
          <SheetContent side="bottom" className="gap-0 px-4 pt-4 pb-6">
            <SheetHeader className="pb-2">
              <SheetTitle className="text-sm">群组配置</SheetTitle>
            </SheetHeader>
            <div className="min-h-0 overflow-y-auto px-1">{panel}</div>
          </SheetContent>
        </Sheet>
      ) : (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>{trigger}</PopoverTrigger>
          <PopoverContent
            align="start"
            side="bottom"
            sideOffset={8}
            className="w-[min(22rem,calc(100vw-2rem))] p-3"
          >
            {panel}
          </PopoverContent>
        </Popover>
      )}
      <AgentGroupDialog
        draft={draft}
        setDraft={setDraft}
        onOpenChange={(nextOpen) => !nextOpen && setDraft(null)}
        onSaved={(saved) => setGroup((current) => (current?.publicID === saved.publicID ? saved : current))}
        onDeleted={() => undefined}
      />
    </>
  );
}
