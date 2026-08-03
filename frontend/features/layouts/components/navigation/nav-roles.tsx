"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ChevronDown, LoaderCircle, MoreHorizontal, PencilLine, Plus, Trash2, Sparkles } from "lucide-react";
import { PlusIcon } from "@/components/ui/plus";
import { cn } from "@/lib/utils";
import { listConversationRoles, createConversationRole, updateConversationRole, deleteConversationRole } from "@/shared/api/roles";
import type { ConversationRoleDTO } from "@/shared/api/roles.types";
import type { ConversationDTO } from "@/shared/api/conversation.types";
import { useSidebarConversations } from "@/entities/conversation";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { useStoredBoolean } from "@/shared/hooks/use-stored-boolean";
import { RoleDialog, EMPTY_ROLE_DRAFT, type RoleDraft } from "@/features/roles/components/role-dialog";

const ROLES_OPEN_STORAGE_KEY = "deeix-roles-open";

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "刚刚";
  if (mins < 60) return `${mins} 分钟前`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days} 天前`;
  return new Date(iso).toLocaleDateString();
}

export function NavRoles() {
  const t = useTranslations("recent.projects");
  const router = useRouter();
  const { items, prependNewConversation } = useSidebarConversations();
  const [roles, setRoles] = React.useState<ConversationRoleDTO[]>([]);
  const [rolesOpen, setRolesOpen] = useStoredBoolean(ROLES_OPEN_STORAGE_KEY, true);
  const [draft, setDraft] = React.useState<RoleDraft | null>(null);
  const [expandedRoleIDs, setExpandedRoleIDs] = React.useState<Set<string>>(new Set());
  const [startingRoleID, setStartingRoleID] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const [openMenuID, setOpenMenuID] = React.useState<string | null>(null);

  const loadRoles = React.useCallback(async () => {
    const token = await resolveAccessToken();
    if (!token) return;
    try {
      const roleItems = await listConversationRoles(token, { status: "active" });
      setRoles(roleItems);
    } catch {
      // 角色列表加载失败不影响侧边栏
    }
  }, []);

  React.useEffect(() => {
    void loadRoles();
  }, [loadRoles, items.length]);

  const conversationsByRole = React.useMemo(() => {
    const map = new Map<string, ConversationDTO[]>();
    for (const item of items) {
      if (item.roleID) {
        const list = map.get(item.roleID) ?? [];
        list.push(item);
        map.set(item.roleID, list);
      }
    }
    for (const list of map.values()) {
      list.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
    }
    return map;
  }, [items]);

  const toggleRole = (roleID: string) => {
    setExpandedRoleIDs((prev) => {
      const next = new Set(prev);
      if (next.has(roleID)) {
        next.delete(roleID);
      } else {
        next.add(roleID);
      }
      return next;
    });
  };

  const startRoleChat = async (role: ConversationRoleDTO) => {
    if (startingRoleID) {
      return;
    }
    setStartingRoleID(role.publicID);
    try {
      const item = await prependNewConversation(undefined, undefined, role.publicID);
      if (item?.publicID) {
        router.push(`/chat?conversation_id=${item.publicID}`);
      } else {
        toast.error("创建对话失败");
      }
    } catch {
      toast.error("创建对话失败");
    } finally {
      setStartingRoleID(null);
    }
  };

  const commitDraft = React.useCallback(async () => {
    if (!draft) {
      return;
    }
    if (!draft.name.trim()) {
      toast.error("角色名称不能为空");
      return;
    }
    setSubmitting(true);
    try {
      const token = await resolveAccessToken();
      const payload = {
        name: draft.name.trim(),
        description: draft.description,
        systemPrompt: draft.systemPrompt,
        model: draft.model,
        provider: "",
        mcpDefaultMode: draft.mcpDefaultMode,
        defaultMCPToolIDs: draft.defaultMCPToolIDs,
        defaultSkillIDs: draft.defaultSkillIDs,
        color: draft.color,
        icon: draft.icon,
      };
      if (draft.publicID) {
        await updateConversationRole(token, draft.publicID, payload);
        toast.success("角色已更新");
      } else {
        await createConversationRole(token, payload);
        toast.success("角色已创建");
      }
      setDraft(null);
      await loadRoles();
    } catch {
      toast.error("保存角色失败");
    } finally {
      setSubmitting(false);
    }
  }, [draft, loadRoles]);

  const removeRole = React.useCallback(
    async (role: ConversationRoleDTO) => {
      if (!window.confirm(`确定删除角色「${role.name}」吗？`)) {
        return;
      }
      try {
        const token = await resolveAccessToken();
        await deleteConversationRole(token, role.publicID);
        toast.success("角色已删除");
        await loadRoles();
      } catch {
        toast.error("删除角色失败");
      }
    },
    [loadRoles],
  );

  return (
    <>
      <div className="relative z-10 group-data-[collapsible=icon]:pointer-events-none group-data-[collapsible=icon]:opacity-0">
        <Collapsible open={rolesOpen} onOpenChange={setRolesOpen}>
          <SidebarGroup className="px-2 py-2">
            <div className="flex h-8 items-center gap-1">
              <SidebarGroupLabel
                asChild
                className="w-fit max-w-full self-start cursor-pointer gap-1 pr-1 transition-[color,margin,opacity] hover:text-sidebar-foreground"
              >
                <Button
                  type="button"
                  variant="ghost"
                  className="h-8 gap-1 py-0 pl-2 pr-1 text-xs hover:bg-transparent has-[>svg]:pl-2 has-[>svg]:pr-1 dark:hover:bg-transparent"
                  aria-label={rolesOpen ? "收起角色" : "展开角色"}
                  onClick={() => setRolesOpen(!rolesOpen)}
                >
                  <span className="min-w-0 truncate text-left">角色</span>
                  <ChevronDown
                    aria-hidden
                    className={cn("!size-3 stroke-1.5 transition-transform duration-200", !rolesOpen && "-rotate-90")}
                  />
                </Button>
              </SidebarGroupLabel>
              <SidebarGroupAction
                type="button"
                aria-label="新建角色"
                className="relative top-auto right-auto ml-auto size-7 shrink-0 text-sidebar-foreground/45 opacity-100 transition-[color,opacity,transform] duration-150 after:pointer-events-none hover:bg-transparent hover:text-sidebar-foreground dark:hover:bg-transparent md:opacity-0 md:group-hover/project-create:opacity-100 md:group-has-[:focus-visible]/project-create:opacity-100"
                onClick={() => setDraft({ ...EMPTY_ROLE_DRAFT })}
              >
                <PlusIcon aria-hidden size={14} strokeWidth={1.8} />
              </SidebarGroupAction>
            </div>
            <CollapsibleContent>
              {roles.length === 0 ? (
                <div className="px-2 py-1 text-xs text-sidebar-foreground/55">
                  {t("empty") || "暂无角色"}
                </div>
              ) : (
                <SidebarMenu className="gap-0.5">
                  {roles.map((role) => {
                    const expanded = expandedRoleIDs.has(role.publicID);
                    const conversations = conversationsByRole.get(role.publicID) ?? [];
                    const starting = startingRoleID === role.publicID;
                    const menuOpen = openMenuID === role.publicID;
                    return (
                      <SidebarMenuItem key={role.publicID}>
                        <Collapsible open={expanded} onOpenChange={() => toggleRole(role.publicID)}>
                          <div className="group/role-row flex h-8 w-full items-center gap-0.5 rounded-md px-1 pr-2 text-sm transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground">
                            <CollapsibleTrigger asChild>
                              <button
                                type="button"
                                className="flex h-8 min-w-0 flex-1 items-center gap-1.5 py-1.5 text-left"
                                aria-expanded={expanded}
                                aria-label={role.name}
                                onClick={() => toggleRole(role.publicID)}
                              >
                                <span
                                  className="flex size-6 shrink-0 items-center justify-center rounded text-sm"
                                  style={{ backgroundColor: role.color || "var(--muted)" }}
                                >
                                  {role.icon ? (
                                    <span className="text-sm leading-none">{role.icon}</span>
                                  ) : (
                                    <Sparkles className="size-3.5 text-muted-foreground" strokeWidth={1.8} />
                                  )}
                                </span>
                                <span className="min-w-0 flex-1 truncate text-xs">{role.name}</span>
                              </button>
                            </CollapsibleTrigger>
                            <span className="shrink-0 text-[10px] text-muted-foreground/70">{conversations.length}</span>
                            <div className="flex shrink-0 items-center">
                              <button
                                type="button"
                                aria-label={`以「${role.name}」开始新对话`}
                                className={cn(
                                  "flex size-6 items-center justify-center rounded-md text-sidebar-foreground/45 transition-colors hover:bg-sidebar-accent-foreground/10 hover:text-sidebar-foreground",
                                  starting && "pointer-events-none",
                                )}
                                onClick={() => void startRoleChat(role)}
                                disabled={Boolean(startingRoleID)}
                              >
                                {starting ? (
                                  <LoaderCircle className="size-3.5 animate-spin" />
                                ) : (
                                  <Plus className="size-4" strokeWidth={1.8} />
                                )}
                              </button>
                              <DropdownMenu open={menuOpen} onOpenChange={(open) => setOpenMenuID(open ? role.publicID : null)}>
                                <DropdownMenuTrigger asChild>
                                  <button
                                    type="button"
                                    aria-label="角色操作"
                                    className="flex size-6 items-center justify-center rounded-md text-sidebar-foreground/45 transition-colors hover:bg-sidebar-accent-foreground/10 hover:text-sidebar-foreground"
                                  >
                                    <MoreHorizontal className="size-4" strokeWidth={1.8} />
                                  </button>
                                </DropdownMenuTrigger>
                                <DropdownMenuContent align="start" className="w-40">
                                  <DropdownMenuItem
                                    onClick={() =>
                                      setDraft({
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
                                    <PencilLine className="mr-2 size-4" strokeWidth={1.7} />
                                    编辑角色
                                  </DropdownMenuItem>
                                  <DropdownMenuSeparator />
                                  <DropdownMenuItem
                                    className="text-destructive focus:text-destructive"
                                    onClick={() => void removeRole(role)}
                                  >
                                    <Trash2 className="mr-2 size-4" strokeWidth={1.7} />
                                    删除角色
                                  </DropdownMenuItem>
                                </DropdownMenuContent>
                              </DropdownMenu>
                            </div>
                          </div>
                          <CollapsibleContent>
                            <div className="pb-1 pl-7 pr-1">
                              {conversations.length === 0 ? (
                                <p className="px-2 py-1 text-[11px] text-muted-foreground/60">暂无对话</p>
                              ) : (
                                conversations.map((conversation) => (
                                  <a
                                    key={conversation.publicID}
                                    href={`/chat?conversation_id=${conversation.publicID}`}
                                    className="flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-xs text-sidebar-foreground/85 transition-colors hover:bg-accent"
                                  >
                                    <span className="min-w-0 flex-1 truncate">{conversation.title}</span>
                                    <span className="shrink-0 text-[10px] text-muted-foreground/60">
                                      {relativeTime(conversation.updatedAt)}
                                    </span>
                                  </a>
                                ))
                              )}
                            </div>
                          </CollapsibleContent>
                        </Collapsible>
                      </SidebarMenuItem>
                    );
                  })}
                </SidebarMenu>
              )}
            </CollapsibleContent>
          </SidebarGroup>
        </Collapsible>
      </div>
      <RoleDialog draft={draft} setDraft={setDraft} onOpenChange={(open) => !open && setDraft(null)} onSubmit={commitDraft} />
    </>
  );
}
