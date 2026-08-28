"use client";

import * as React from "react";
import { PencilLine, Plus, Trash2, Users } from "lucide-react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

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
import { Button } from "@/components/ui/button";
import { CenteredEmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useSidebarConversations } from "@/entities/conversation";
import {
  AgentGroupDialog,
  type AgentGroupDraft,
  agentGroupDraftFromDTO,
  emptyAgentGroupDraft,
} from "@/features/agent-groups/components/agent-group-dialog";
import { useAgentGroupFeature } from "@/features/agent-groups/context/agent-group-feature-context";
import { resolveAgentGroupFeatureAccess } from "@/features/agent-groups/model/agent-group-feature";
import { useChatSession } from "@/features/chat/context/chat-session-context";
import { deleteAgentGroup, listAgentGroups } from "@/shared/api/agent-groups";
import type { AgentGroupDTO } from "@/shared/api/agent-groups.types";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";

function AgentGroupCard({
  group,
  deleting,
  onStart,
  onEdit,
  onDelete,
}: {
  group: AgentGroupDTO;
  deleting: boolean;
  onStart: (group: AgentGroupDTO) => void;
  onEdit: (group: AgentGroupDTO) => void;
  onDelete: (group: AgentGroupDTO) => void;
}) {
  const supervisor = group.members.find((member) => member.memberType === "supervisor");

  return (
    <div
      role="button"
      tabIndex={0}
      className="group flex min-h-16 min-w-0 items-center gap-2.5 rounded-lg bg-muted/35 px-3 py-2.5 text-left transition-colors hover:bg-muted/55 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45"
      onClick={() => onEdit(group)}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onEdit(group);
        }
      }}
    >
      <div className="flex size-7 shrink-0 items-center justify-center text-muted-foreground">
        <Users className="size-4.5" strokeWidth={1.8} />
      </div>

      <div className="grid min-w-0 flex-1 gap-0.5">
        <div className="flex min-w-0 items-center gap-1.5">
          <h3 className="min-w-0 truncate text-sm font-medium text-foreground">{group.name}</h3>
          <span className="shrink-0 text-[10px] text-muted-foreground/70">
            {group.members.length} 成员
          </span>
        </div>
        <p className="min-w-0 truncate text-xs leading-5 text-muted-foreground">
          {group.description || (supervisor ? `主管：${supervisor.roleName}` : "")}
        </p>
      </div>

      <div className="flex shrink-0 items-center gap-0.5">
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-7 text-muted-foreground transition-opacity hover:bg-background/80 hover:text-foreground md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100"
          aria-label={`以「${group.name}」开始群组对话`}
          title={`以「${group.name}」开始群组对话`}
          onClick={(event) => {
            event.stopPropagation();
            onStart(group);
          }}
        >
          <Plus className="h-3.5 w-3.5" strokeWidth={1.6} />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-7 text-muted-foreground transition-opacity hover:bg-background/80 hover:text-foreground md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100"
          aria-label={`编辑群组「${group.name}」`}
          title="编辑群组"
          onClick={(event) => {
            event.stopPropagation();
            onEdit(group);
          }}
        >
          <PencilLine className="h-3.5 w-3.5" strokeWidth={1.6} />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-7 text-muted-foreground transition-opacity hover:bg-background/80 hover:text-destructive md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100"
          aria-label={`删除群组「${group.name}」`}
          title="删除群组"
          disabled={deleting}
          onClick={(event) => {
            event.stopPropagation();
            onDelete(group);
          }}
        >
          <Trash2 className="h-3.5 w-3.5" strokeWidth={1.6} />
        </Button>
      </div>
    </div>
  );
}

function AgentGroupListSkeleton() {
  return (
    <div className="grid gap-4 md:ml-13 md:w-[calc(100%-3.25rem)] md:grid-cols-2">
      {Array.from({ length: 8 }).map((_, index) => (
        <div key={index} className="flex min-h-16 items-center gap-2.5 rounded-lg bg-muted/35 px-3 py-2.5">
          <Skeleton className="size-7 shrink-0 rounded-md bg-muted/55" />
          <div className="min-w-0 flex-1 space-y-1.5">
            <Skeleton className="h-4 w-32 rounded-full bg-muted/55" />
            <Skeleton className="h-3 w-4/5 rounded-full bg-muted/35" />
          </div>
        </div>
      ))}
    </div>
  );
}

export function AgentGroupsPage() {
  const t = useTranslations("common.navigation");
  const router = useRouter();
  const { enabled: agentGroupsEnabled, status: agentGroupFeatureStatus } = useAgentGroupFeature();
  const featureAccess = resolveAgentGroupFeatureAccess(agentGroupFeatureStatus);
  const { lastChange } = useSidebarConversations();
  const { requestNewConversation } = useChatSession();
  const [groups, setGroups] = React.useState<AgentGroupDTO[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [loadFailed, setLoadFailed] = React.useState(false);
  const [draft, setDraft] = React.useState<AgentGroupDraft | null>(null);
  const [deleteTarget, setDeleteTarget] = React.useState<AgentGroupDTO | null>(null);
  const [deletingGroupID, setDeletingGroupID] = React.useState<string | null>(null);

  const loadGroups = React.useCallback(async () => {
    if (!agentGroupsEnabled) {
      return;
    }
    const token = await resolveAccessToken();
    if (!token) {
      setLoadFailed(true);
      setLoading(false);
      return;
    }
    try {
      // 群组已全局化：单次拉取当前用户全部群组，不再按项目遍历。
      const groups = await listAgentGroups(token);
      setGroups(
        groups.slice().sort((a, b) => a.sortOrder - b.sortOrder || a.createdAt.localeCompare(b.createdAt)),
      );
      setLoadFailed(false);
    } catch {
      setLoadFailed(true);
    } finally {
      setLoading(false);
    }
  }, [agentGroupsEnabled]);

  React.useEffect(() => {
    if (featureAccess.allowListRequests) {
      void loadGroups();
    }
  }, [featureAccess.allowListRequests, loadGroups]);

  React.useEffect(() => {
    if (featureAccess.redirectDirectRoute) {
      router.replace("/chat");
    }
  }, [featureAccess.redirectDirectRoute, router]);

  // 会话变化（新建/重命名）后刷新群组列表，保持成员数与配置同步。
  React.useEffect(() => {
    if (lastChange !== null) {
      void loadGroups();
    }
  }, [lastChange, loadGroups]);

  const onCreate = React.useCallback(() => {
    setDraft(emptyAgentGroupDraft());
  }, []);

  const openEdit = React.useCallback((group: AgentGroupDTO) => {
    setDraft(agentGroupDraftFromDTO(group));
  }, []);

  const startGroupChat = React.useCallback(
    (group: AgentGroupDTO) => {
      // 管理页面无角色/项目上下文，创建纯群组会话。
      requestNewConversation({ agentGroupID: group.publicID });
      router.push(`/chat?agent_group_id=${encodeURIComponent(group.publicID)}`);
    },
    [requestNewConversation, router],
  );

  const confirmDelete = React.useCallback(async () => {
    if (!deleteTarget) {
      return;
    }
    setDeletingGroupID(deleteTarget.publicID);
    try {
      const token = await resolveAccessToken();
      if (!token) {
        throw new Error("missing access token");
      }
      await deleteAgentGroup(token, deleteTarget.publicID);
      toast.success("群组已删除");
      setDeleteTarget(null);
      await loadGroups();
    } catch {
      toast.error("删除群组失败：已有会话或运行历史的群组无法删除");
    } finally {
      setDeletingGroupID(null);
    }
  }, [deleteTarget, loadGroups]);

  if (!agentGroupsEnabled) {
    if (agentGroupFeatureStatus !== "loading") {
      return null;
    }
    return (
      <div className="flex h-full min-h-0 w-full flex-1 flex-col overflow-hidden">
        <div className="mx-auto h-full min-h-0 w-full max-w-[912px] flex-1 overflow-y-auto px-3 pb-8 pt-6 md:pt-15">
          <AgentGroupListSkeleton />
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 w-full flex-1 flex-col overflow-hidden">
      <div className="mx-auto flex h-full min-h-0 w-full max-w-[912px] flex-1 flex-col px-3 pb-8 pt-6 md:pt-15">
        <header className="ml-0 md:ml-13 md:w-[calc(100%-3.25rem)]">
          <div className="flex items-start justify-between gap-4">
            <h1 className="min-w-0 text-xl font-semibold tracking-[-0.03em] text-foreground md:text-2xl">
              {t("agentGroups")}
            </h1>
            <Button size="sm" variant="default" className="shrink-0" onClick={onCreate}>
              <Plus className="size-4" />
              新建群组
            </Button>
          </div>
        </header>

        <section className="mt-6 flex min-h-0 flex-1 flex-col overflow-hidden">
          {loading ? (
            <div className="h-full min-h-0 flex-1 overflow-y-auto pr-2">
              <AgentGroupListSkeleton />
            </div>
          ) : loadFailed ? (
            <div className="flex h-full min-h-0 w-full flex-1 items-center justify-center">
              <div className="flex flex-col items-center gap-3 px-3 text-center text-xs text-muted-foreground">
                <span>群组加载失败</span>
                <button
                  type="button"
                  className="underline underline-offset-4 transition-colors hover:text-foreground"
                  onClick={() => {
                    setLoading(true);
                    void loadGroups();
                  }}
                >
                  重试
                </button>
              </div>
            </div>
          ) : groups.length === 0 ? (
            <div className="flex h-full min-h-0 w-full items-center justify-center">
              <CenteredEmptyState
                title="还没有群组"
                description="创建群组，让多个角色协作完成一次对话"
              />
            </div>
          ) : (
            <div className="h-full min-h-0 w-full flex-1 overflow-y-auto pr-2" data-sidebar-scroll-root="true">
              <div className="grid gap-4 md:ml-13 md:w-[calc(100%-3.25rem)] md:grid-cols-2">
                {groups.map((group) => (
                  <AgentGroupCard
                    key={group.publicID}
                    group={group}
                    deleting={deletingGroupID === group.publicID}
                    onStart={(target) => void startGroupChat(target)}
                    onEdit={openEdit}
                    onDelete={setDeleteTarget}
                  />
                ))}
              </div>
            </div>
          )}
        </section>
      </div>

      <AgentGroupDialog
        draft={draft}
        setDraft={setDraft}
        onOpenChange={(open) => !open && setDraft(null)}
        onSaved={() => void loadGroups()}
        onDeleted={() => void loadGroups()}
      />

      <AlertDialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除群组</AlertDialogTitle>
            <AlertDialogDescription>
              确定删除群组「{deleteTarget?.name ?? ""}」吗？此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={() => void confirmDelete()}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
