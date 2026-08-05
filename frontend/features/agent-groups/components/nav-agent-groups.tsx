"use client";

import { ChevronDown, PencilLine, Trash2, Users } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import * as React from "react";
import { toast } from "sonner";
import { Ellipsis } from "@/components/animate-ui/icons/ellipsis";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent } from "@/components/ui/collapsible";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuItemIcon,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { PlusIcon } from "@/components/ui/plus";
import {
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";
import { useSidebarConversations } from "@/entities/conversation";
import {
  AgentGroupDialog,
  type AgentGroupDraft,
  agentGroupDraftFromDTO,
  emptyAgentGroupDraft,
} from "@/features/agent-groups/components/agent-group-dialog";
import { useChatSession } from "@/features/chat/context/chat-session-context";
import { cn } from "@/lib/utils";
import { deleteAgentGroup, listAgentGroups } from "@/shared/api/agent-groups";
import type { AgentGroupDTO } from "@/shared/api/agent-groups.types";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { useStoredBoolean } from "@/shared/hooks/use-stored-boolean";

const AGENT_GROUPS_OPEN_STORAGE_KEY = "deeix-agent-groups-open";

export function NavAgentGroups() {
  const { isMobile, setOpenMobile } = useSidebar();
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const { items, projects, lastChange } = useSidebarConversations();
  const { requestNewConversation } = useChatSession();
  const [groupsOpen, setGroupsOpen] = useStoredBoolean(AGENT_GROUPS_OPEN_STORAGE_KEY, true);
  const [groups, setGroups] = React.useState<AgentGroupDTO[]>([]);
  const [loadingGroups, setLoadingGroups] = React.useState(false);
  const [draft, setDraft] = React.useState<AgentGroupDraft | null>(null);
  const [openMenuID, setOpenMenuID] = React.useState<string | null>(null);
  const [hoveredMenuID, setHoveredMenuID] = React.useState<string | null>(null);
  const [focusedRowID, setFocusedRowID] = React.useState<string | null>(null);
  const [deletingGroupID, setDeletingGroupID] = React.useState<string | null>(null);
  const [hoveredCreateID, setHoveredCreateID] = React.useState<string | null>(null);

  const activeProjectID =
    pathname === "/chat" ? searchParams.get("project_id") ?? "" : searchParams.get("project") ?? "";

  const loadGroups = React.useCallback(async () => {
    const token = await resolveAccessToken();
    if (!token) {
      return;
    }
    try {
      const results = await Promise.all(projects.map((project) => listAgentGroups(token, project.publicID)));
      const merged = results
        .flat()
        .slice()
        .sort((a, b) => a.sortOrder - b.sortOrder || a.createdAt.localeCompare(b.createdAt));
      setGroups(merged);
    } catch {
      // 群组列表加载失败不影响侧边栏
    } finally {
      setLoadingGroups(false);
    }
  }, [projects]);

  React.useEffect(() => {
    if (projects.length > 0) {
      setLoadingGroups(true);
      void loadGroups();
    } else {
      setGroups([]);
    }
  }, [loadGroups, projects.length]);

  // 会话变化（新建/重命名）后刷新群组列表，保持成员数与配置同步。
  React.useEffect(() => {
    if (projects.length > 0 && lastChange !== null) {
      void loadGroups();
    }
  }, [lastChange, loadGroups, projects.length]);

  const projectOptions = React.useMemo(
    () => projects.map((project) => ({ publicID: project.publicID, name: project.name })),
    [projects],
  );

  const onCreate = React.useCallback(() => {
    if (projects.length === 0) {
      toast.error("请先创建项目");
      return;
    }
    const project = projects.find((item) => item.publicID === activeProjectID) ?? projects[0];
    setDraft(emptyAgentGroupDraft(project.publicID, project.name));
  }, [activeProjectID, projects]);

  const openEdit = React.useCallback((group: AgentGroupDTO) => {
    setDraft(agentGroupDraftFromDTO(group));
  }, []);

  const startGroupChat = React.useCallback(
    (group: AgentGroupDTO) => {
      requestNewConversation({
        projectID: group.projectID,
        agentGroupID: group.publicID,
      });
      router.push(
        `/chat?project_id=${encodeURIComponent(group.projectID)}&agent_group_id=${encodeURIComponent(group.publicID)}`,
      );
      if (isMobile) {
        setOpenMobile(false);
      }
    },
    [isMobile, requestNewConversation, router, setOpenMobile],
  );

  const removeGroup = React.useCallback(
    async (group: AgentGroupDTO) => {
      if (!window.confirm(`确定删除群组「${group.name}」吗？`)) {
        return;
      }
      setDeletingGroupID(group.publicID);
      try {
        const token = await resolveAccessToken();
        if (!token) {
          throw new Error("missing access token");
        }
        await deleteAgentGroup(token, group.publicID);
        toast.success("群组已删除");
        await loadGroups();
      } catch {
        toast.error("删除群组失败：已有会话或运行历史的群组无法删除");
      } finally {
        setDeletingGroupID(null);
      }
    },
    [loadGroups],
  );

  return (
    <>
      <div className="relative z-10 group-data-[collapsible=icon]:pointer-events-none group-data-[collapsible=icon]:opacity-0">
        <Collapsible open={groupsOpen} onOpenChange={setGroupsOpen}>
          <SidebarGroup className="px-2 py-2">
            <div className="group/agent-group-create flex h-8 items-center gap-1">
              <SidebarGroupLabel
                asChild
                className="w-fit max-w-full self-start cursor-pointer gap-1 pr-1 transition-[color,margin,opacity] hover:text-sidebar-foreground"
              >
                <Button
                  type="button"
                  variant="ghost"
                  className="h-8 gap-1 py-0 pl-2 pr-1 text-xs hover:bg-transparent has-[>svg]:pl-2 has-[>svg]:pr-1 dark:hover:bg-transparent"
                  aria-label={groupsOpen ? "收起群组" : "展开群组"}
                  onClick={() => setGroupsOpen(!groupsOpen)}
                >
                  <span className="min-w-0 truncate text-left">群组</span>
                  <ChevronDown
                    aria-hidden
                    className={cn("!size-3 stroke-1.5 transition-transform duration-200", !groupsOpen && "-rotate-90")}
                  />
                </Button>
              </SidebarGroupLabel>
              <SidebarGroupAction
                type="button"
                aria-label="新建群组"
                className="relative top-auto right-auto ml-auto size-7 shrink-0 text-sidebar-foreground/45 opacity-100 transition-[color,opacity,transform] duration-150 after:pointer-events-none hover:bg-transparent hover:text-sidebar-foreground dark:hover:bg-transparent md:opacity-0 md:group-hover/agent-group-create:opacity-100 md:group-has-[:focus-visible]/agent-group-create:opacity-100"
                onClick={onCreate}
              >
                <PlusIcon aria-hidden size={14} strokeWidth={1.8} />
              </SidebarGroupAction>
            </div>
            <CollapsibleContent>
              {projects.length === 0 ? (
                <div className="px-2 py-1 text-xs text-sidebar-foreground/55">请先创建项目</div>
              ) : loadingGroups ? (
                <div className="px-2 py-1 text-xs text-sidebar-foreground/55">加载中…</div>
              ) : groups.length === 0 ? (
                <div className="px-2 py-1 text-xs text-sidebar-foreground/55">暂无群组</div>
              ) : (
                <SidebarMenu className="gap-0.5">
                  {groups.map((group) => {
                    const menuOpen = openMenuID === group.publicID;
                    const menuHovered = hoveredMenuID === group.publicID;
                    const rowFocused = focusedRowID === group.publicID;
                    const deleting = deletingGroupID === group.publicID;
                    const createHovered = hoveredCreateID === group.publicID;
                    const conversationCount = items.filter(
                      (item) => item.agentGroupID === group.publicID,
                    ).length;
                    const showActions = isMobile || rowFocused || menuHovered || menuOpen;

                    return (
                      <SidebarMenuItem key={group.publicID}>
                        <div
                          className="group/agent-group-row relative"
                          title={`${group.name}（${group.projectName}）`}
                          onFocus={(event) => {
                            setFocusedRowID(
                              event.target instanceof HTMLElement && event.target.matches(":focus-visible")
                                ? group.publicID
                                : null,
                            );
                          }}
                          onBlur={(event) => {
                            const nextTarget = event.relatedTarget;
                            if (!(nextTarget instanceof Node) || !event.currentTarget.contains(nextTarget)) {
                              setFocusedRowID(null);
                            }
                          }}
                        >
                          <Button
                            type="button"
                            variant="ghost"
                            className={cn(
                              "flex h-8 w-full min-w-0 items-center gap-0 rounded-md px-0 text-sm font-normal outline-hidden ring-sidebar-ring transition-colors focus-visible:ring-2 pr-16",
                              "text-sidebar-foreground group-hover/agent-group-row:bg-sidebar-accent group-hover/agent-group-row:text-sidebar-accent-foreground",
                            )}
                            aria-label={`编辑群组「${group.name}」`}
                            onClick={() => openEdit(group)}
                          >
                            <span className="flex h-8 w-8 shrink-0 items-center justify-center">
                              <Users
                                aria-hidden
                                className="size-4 shrink-0 text-sidebar-foreground/55"
                                strokeWidth={1.6}
                              />
                            </span>
                            <span className="ml-1 min-w-0 flex-1 truncate text-left">{group.name}</span>
                            <span className="mr-1 shrink-0 text-[10px] text-sidebar-foreground/45">
                              {conversationCount}
                            </span>
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            aria-label={`以「${group.name}」开始群组对话`}
                            title={`以「${group.name}」开始群组对话`}
                            tabIndex={showActions ? undefined : -1}
                            className={cn(
                              "absolute top-0 right-8 z-10 h-8 w-8 text-sidebar-foreground/45 opacity-0 transition-[color,opacity] duration-150 hover:bg-transparent hover:text-sidebar-foreground group-hover/agent-group-row:opacity-100 dark:hover:bg-transparent",
                              showActions && "opacity-100",
                            )}
                            onMouseEnter={() => setHoveredCreateID(group.publicID)}
                            onMouseLeave={() => setHoveredCreateID(null)}
                            onClick={(event) => {
                              event.preventDefault();
                              event.stopPropagation();
                              void startGroupChat(group);
                            }}
                          >
                            <PlusIcon
                              aria-hidden
                              size={16}
                              strokeWidth={1.6}
                              animate={createHovered ? "default" : undefined}
                            />
                          </Button>
                          <DropdownMenu
                            modal={false}
                            open={menuOpen}
                            onOpenChange={(open) => setOpenMenuID(open ? group.publicID : null)}
                          >
                            <DropdownMenuTrigger asChild>
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon"
                                aria-label="群组操作"
                                title="群组操作"
                                tabIndex={showActions ? undefined : -1}
                                className={cn(
                                  "absolute top-0 right-0 z-10 h-8 w-8 text-sidebar-foreground/45 opacity-0 transition-[color,opacity] duration-150 hover:bg-transparent hover:text-sidebar-foreground group-hover/agent-group-row:opacity-100 data-[state=open]:text-sidebar-foreground dark:hover:bg-transparent",
                                  showActions && "opacity-100",
                                )}
                                onMouseEnter={() => setHoveredMenuID(group.publicID)}
                                onMouseLeave={() => setHoveredMenuID(null)}
                              >
                                <Ellipsis aria-hidden size={16} strokeWidth={1.4} animate={menuHovered ? "pulse" : undefined} />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end" className="w-max min-w-36 max-w-[calc(100vw-2rem)]">
                              <DropdownMenuItem
                                onSelect={(event) => {
                                  event.preventDefault();
                                  openEdit(group);
                                }}
                              >
                                <DropdownMenuItemIcon icon={PencilLine} className="text-current" />
                                编辑群组
                              </DropdownMenuItem>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem
                                variant="destructive"
                                disabled={deleting}
                                onSelect={(event) => {
                                  event.preventDefault();
                                  void removeGroup(group);
                                }}
                              >
                                <DropdownMenuItemIcon icon={Trash2} className="text-current" />
                                删除群组
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </div>
                      </SidebarMenuItem>
                    );
                  })}
                </SidebarMenu>
              )}
            </CollapsibleContent>
          </SidebarGroup>
        </Collapsible>
      </div>
      <AgentGroupDialog
        draft={draft}
        setDraft={setDraft}
        onOpenChange={(open) => !open && setDraft(null)}
        onSaved={() => void loadGroups()}
        onDeleted={() => void loadGroups()}
        projects={projectOptions}
      />
    </>
  );
}
