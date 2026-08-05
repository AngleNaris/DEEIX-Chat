"use client";

import * as React from "react";
import {
  closestCenter,
  DndContext,
  KeyboardSensor,
  PointerSensor,
  type DragEndEvent,
  type DragStartEvent,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Ellipsis } from "@/components/animate-ui/icons/ellipsis";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
} from "@/components/ui/collapsible";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuItemIcon,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { GripVerticalIcon, type GripVerticalIconHandle } from "@/components/ui/grip-vertical";
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
import { RoleDialog, EMPTY_ROLE_DRAFT, type RoleDraft } from "@/features/roles/components/role-dialog";
import { useLayoutActiveConversation } from "@/features/layouts/hooks/use-layout-active-conversation";
import { cn } from "@/lib/utils";
import { ChevronDown, PencilLine, Trash2 } from "lucide-react";
import {
  createConversationRole,
  deleteConversationRole,
  listConversationRoles,
  reorderConversationRoles,
  updateConversationRole,
} from "@/shared/api/roles";
import type { ConversationRoleDTO } from "@/shared/api/roles.types";
import type { ConversationDTO } from "@/shared/api/conversation.types";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { useStoredBoolean } from "@/shared/hooks/use-stored-boolean";

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

type RoleSortableRenderProps = {
  attributes: ReturnType<typeof useSortable>["attributes"];
  listeners: ReturnType<typeof useSortable>["listeners"];
  isDragging: boolean;
};

function RoleSortableItem({
  children,
  disabled,
  roleID,
}: {
  children: (props: RoleSortableRenderProps) => React.ReactNode;
  disabled: boolean;
  roleID: string;
}) {
  const {
    attributes,
    isDragging,
    listeners,
    setNodeRef,
    transform,
    transition,
  } = useSortable({
    id: roleID,
    disabled,
  });
  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  } satisfies React.CSSProperties;

  return (
    <SidebarMenuItem
      ref={setNodeRef}
      data-sidebar-motion-key={`role-${roleID}`}
      style={style}
      className={cn("transition-opacity", isDragging && "opacity-45")}
    >
      {children({ attributes, isDragging, listeners })}
    </SidebarMenuItem>
  );
}

type RoleInlineActionProps = React.ComponentPropsWithoutRef<"button"> & {
  label: string;
  visible: boolean;
  onHoverChange?: (hovered: boolean) => void;
};

const RoleInlineAction = React.forwardRef<HTMLButtonElement, RoleInlineActionProps>(function RoleInlineAction({
  label,
  visible,
  onHoverChange,
  tabIndex,
  onClick,
  onMouseEnter,
  onMouseLeave,
  className,
  children,
  ...props
}, ref) {
  return (
    <Button
      {...props}
      ref={ref}
      type="button"
      variant="ghost"
      size="icon"
      aria-label={label}
      title={label}
      tabIndex={tabIndex ?? (visible ? undefined : -1)}
      className={cn(
        "absolute top-0 z-10 text-sidebar-foreground/45 opacity-0 transition-[color,opacity] duration-150 hover:bg-transparent hover:text-sidebar-foreground group-hover/role-row:opacity-100 data-[state=open]:text-sidebar-foreground dark:hover:bg-transparent",
        visible && "opacity-100",
        className,
      )}
      onMouseEnter={(event) => {
        onMouseEnter?.(event);
        onHoverChange?.(true);
      }}
      onMouseLeave={(event) => {
        onMouseLeave?.(event);
        onHoverChange?.(false);
      }}
      onClick={(event) => {
        onClick?.(event);
        event.preventDefault();
        event.stopPropagation();
      }}
    >
      {children}
    </Button>
  );
});

function RoleDragHandle({
  attributes,
  disabled,
  label,
  listeners,
  visible,
}: {
  attributes: RoleSortableRenderProps["attributes"];
  disabled: boolean;
  label: string;
  listeners: RoleSortableRenderProps["listeners"];
  visible: boolean;
}) {
  const iconRef = React.useRef<GripVerticalIconHandle>(null);

  return (
    <Button
      {...attributes}
      {...listeners}
      type="button"
      variant="ghost"
      size="icon"
      aria-label={label}
      title={label}
      disabled={disabled}
      className={cn(
        "absolute right-0 top-0 z-20 cursor-grab text-sidebar-foreground/45 opacity-0 transition-[color,opacity] duration-150 hover:bg-transparent hover:text-sidebar-foreground active:cursor-grabbing group-hover/role-row:opacity-100 disabled:cursor-not-allowed disabled:text-sidebar-foreground/40 dark:hover:bg-transparent",
        visible && "opacity-100",
      )}
      style={{ touchAction: "none" }}
      onMouseEnter={() => iconRef.current?.startAnimation()}
      onMouseLeave={() => iconRef.current?.stopAnimation()}
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
      }}
    >
      <GripVerticalIcon aria-hidden ref={iconRef} size={14} className="size-4 text-current" />
    </Button>
  );
}

function RoleTreeButton({
  actionPaddingClassName,
  active,
  color,
  contentID,
  count,
  expanded,
  icon,
  name,
  onHoverChange,
  onToggleExpanded,
}: {
  actionPaddingClassName: string;
  active: boolean;
  color: string;
  contentID: string;
  count: number;
  expanded: boolean;
  icon: string;
  name: string;
  onHoverChange: (hovered: boolean) => void;
  onToggleExpanded: () => void;
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      className={cn(
        "flex h-8 w-full min-w-0 items-center gap-0 rounded-md px-0 text-sm font-normal outline-hidden ring-sidebar-ring transition-colors focus-visible:ring-2",
        active
          ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground"
          : "text-sidebar-foreground group-hover/role-row:bg-sidebar-accent group-hover/role-row:text-sidebar-accent-foreground",
        actionPaddingClassName,
      )}
      aria-controls={contentID}
      aria-expanded={expanded}
      aria-label={name}
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
        onToggleExpanded();
      }}
      onMouseEnter={() => onHoverChange(true)}
      onMouseLeave={() => onHoverChange(false)}
    >
      <span className="flex h-8 w-8 shrink-0 items-center justify-center">
        <span
          className="flex size-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none"
          style={{ backgroundColor: color || "var(--muted)" }}
        >
          {icon || "✦"}
        </span>
      </span>
      <span className="ml-1 min-w-0 flex-1 truncate text-left">{name}</span>
      <span className="mr-1 shrink-0 text-[10px] text-sidebar-foreground/45">{count}</span>
      <span className="flex h-8 w-4 shrink-0 items-center justify-center">
        <ChevronDown
          aria-hidden
          className={cn(
            "size-3.5 text-muted-foreground/60 transition-transform duration-200",
            expanded && "rotate-180",
          )}
        />
      </span>
    </Button>
  );
}

export function NavRoles() {
  const t = useTranslations("recent.projects");
  const router = useRouter();
  const { isMobile } = useSidebar();
  const activeConversationID = useLayoutActiveConversation();
  const { items, prependNewConversation } = useSidebarConversations();
  const [roles, setRoles] = React.useState<ConversationRoleDTO[]>([]);
  const [rolesOpen, setRolesOpen] = useStoredBoolean(ROLES_OPEN_STORAGE_KEY, true);
  const [draft, setDraft] = React.useState<RoleDraft | null>(null);
  const [expandedRoleIDs, setExpandedRoleIDs] = React.useState<Set<string>>(new Set());
  const [startingRoleID, setStartingRoleID] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const [openMenuID, setOpenMenuID] = React.useState<string | null>(null);
  const [hoveredRoleMenuID, setHoveredRoleMenuID] = React.useState<string | null>(null);
  const [hoveredRoleCreateID, setHoveredRoleCreateID] = React.useState<string | null>(null);
  const [hoveredRoleRowID, setHoveredRoleRowID] = React.useState<string | null>(null);
  const [focusedRoleRowID, setFocusedRoleRowID] = React.useState<string | null>(null);
  const [draggingRoleID, setDraggingRoleID] = React.useState<string | null>(null);
  const [savingRoleOrder, setSavingRoleOrder] = React.useState(false);

  const roleIDs = React.useMemo(() => roles.map((role) => role.publicID), [roles]);
  const roleSortSensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 4,
      },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

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

  const activeRoleID = React.useMemo(
    () => items.find((item) => item.publicID === activeConversationID)?.roleID ?? "",
    [activeConversationID, items],
  );

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

  const onRoleDragStart = React.useCallback((event: DragStartEvent) => {
    setDraggingRoleID(String(event.active.id));
  }, []);

  const onRoleDragCancel = React.useCallback(() => {
    setDraggingRoleID(null);
  }, []);

  const onRoleDragEnd = React.useCallback(async (event: DragEndEvent) => {
    setDraggingRoleID(null);

    const { active, over } = event;
    if (!over || active.id === over.id || savingRoleOrder) {
      return;
    }

    const fromIndex = roleIDs.indexOf(String(active.id));
    const toIndex = roleIDs.indexOf(String(over.id));
    if (fromIndex < 0 || toIndex < 0) {
      return;
    }

    const nextRoles = arrayMove(roles, fromIndex, toIndex);
    setRoles(nextRoles);
    setSavingRoleOrder(true);
    try {
      const token = await resolveAccessToken();
      if (!token) {
        throw new Error("missing access token");
      }
      await reorderConversationRoles(token, {
        roleIDs: nextRoles.map((role) => role.publicID),
      });
    } catch {
      toast.error("角色排序保存失败");
      await loadRoles();
    } finally {
      setSavingRoleOrder(false);
    }
  }, [loadRoles, roleIDs, roles, savingRoleOrder]);

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
            <div className="group/role-create flex h-8 items-center gap-1">
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
                disabled={submitting}
                className="relative top-auto right-auto ml-auto size-7 shrink-0 text-sidebar-foreground/45 opacity-100 transition-[color,opacity,transform] duration-150 after:pointer-events-none hover:bg-transparent hover:text-sidebar-foreground dark:hover:bg-transparent md:opacity-0 md:group-hover/role-create:opacity-100 md:group-has-[:focus-visible]/role-create:opacity-100"
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
                <DndContext
                  sensors={roleSortSensors}
                  collisionDetection={closestCenter}
                  onDragStart={onRoleDragStart}
                  onDragEnd={(event) => void onRoleDragEnd(event)}
                  onDragCancel={onRoleDragCancel}
                >
                  <SortableContext items={roleIDs} strategy={verticalListSortingStrategy}>
                    <SidebarMenu className="gap-0.5">
                      {roles.map((role) => {
                        const expanded = expandedRoleIDs.has(role.publicID);
                        const conversations = conversationsByRole.get(role.publicID) ?? [];
                        const starting = startingRoleID === role.publicID;
                        const menuOpen = openMenuID === role.publicID;
                        const rowHovered = hoveredRoleRowID === role.publicID;
                        const rowFocused = focusedRoleRowID === role.publicID;
                        const createHovered = hoveredRoleCreateID === role.publicID;
                        const menuHovered = hoveredRoleMenuID === role.publicID;
                        const rowDragging = draggingRoleID === role.publicID;
                        const canSortRoles = roles.length >= 2;
                        const roleActionPaddingClassName = canSortRoles ? "pr-24" : "pr-16";
                        const roleCreateActionClassName = canSortRoles ? "right-16" : "right-8";
                        const roleMenuActionClassName = canSortRoles ? "right-8" : "right-0";
                        const showRoleActions = isMobile || (!rowDragging && (rowHovered || rowFocused || menuHovered || menuOpen));
                        const roleConversationContentID = `sidebar-role-${role.publicID}-conversations`;

                        return (
                          <RoleSortableItem
                            key={role.publicID}
                            roleID={role.publicID}
                            disabled={!canSortRoles || savingRoleOrder}
                          >
                            {({ attributes, isDragging, listeners }) => (
                              <>
                                <div
                                  className="group/role-row relative"
                                  onFocus={(event) => {
                                    setFocusedRoleRowID(
                                      event.target instanceof HTMLElement && event.target.matches(":focus-visible")
                                        ? role.publicID
                                        : null,
                                    );
                                  }}
                                  onBlur={(event) => {
                                    const nextTarget = event.relatedTarget;
                                    if (!(nextTarget instanceof Node) || !event.currentTarget.contains(nextTarget)) {
                                      setFocusedRoleRowID(null);
                                    }
                                  }}
                                >
                                  {canSortRoles ? (
                                    <RoleDragHandle
                                      attributes={attributes}
                                      disabled={savingRoleOrder}
                                      label={`拖动调整「${role.name}」顺序`}
                                      listeners={listeners}
                                      visible={isMobile || rowHovered || rowFocused || isDragging}
                                    />
                                  ) : null}
                                  <RoleTreeButton
                                    actionPaddingClassName={roleActionPaddingClassName}
                                    active={activeRoleID === role.publicID}
                                    color={role.color}
                                    contentID={roleConversationContentID}
                                    count={conversations.length}
                                    expanded={expanded}
                                    icon={role.icon}
                                    name={role.name}
                                    onHoverChange={(hovered) => setHoveredRoleRowID(hovered ? role.publicID : null)}
                                    onToggleExpanded={() => toggleRole(role.publicID)}
                                  />
                                  <RoleInlineAction
                                    label={`以「${role.name}」开始新对话`}
                                    visible={showRoleActions}
                                    className={roleCreateActionClassName}
                                    disabled={Boolean(startingRoleID)}
                                    onHoverChange={(hovered) => setHoveredRoleCreateID(hovered ? role.publicID : null)}
                                    onClick={() => void startRoleChat(role)}
                                  >
                                    {starting ? (
                                      <span className="relative flex size-4 items-center justify-center">
                                        <span className="absolute size-3 animate-spin rounded-full border border-current border-t-transparent" />
                                      </span>
                                    ) : (
                                      <PlusIcon aria-hidden size={16} strokeWidth={1.6} animate={createHovered ? "default" : undefined} />
                                    )}
                                  </RoleInlineAction>
                                  <DropdownMenu
                                    modal={false}
                                    open={menuOpen}
                                    onOpenChange={(open) => setOpenMenuID(open ? role.publicID : null)}
                                  >
                                    <DropdownMenuTrigger asChild>
                                      <RoleInlineAction
                                        label="角色操作"
                                        visible={showRoleActions}
                                        className={roleMenuActionClassName}
                                        onHoverChange={(hovered) => setHoveredRoleMenuID(hovered ? role.publicID : null)}
                                      >
                                        <Ellipsis aria-hidden size={16} strokeWidth={1.4} animate={menuHovered ? "pulse" : undefined} />
                                      </RoleInlineAction>
                                    </DropdownMenuTrigger>
                                    <DropdownMenuContent align="end" className="w-max min-w-36 max-w-[calc(100vw-2rem)]">
                                      <DropdownMenuItem
                                        onSelect={(event) => {
                                          event.preventDefault();
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
                                          });
                                        }}
                                      >
                                        <DropdownMenuItemIcon icon={PencilLine} className="text-current" />
                                        编辑角色
                                      </DropdownMenuItem>
                                      <DropdownMenuSeparator />
                                      <DropdownMenuItem
                                        variant="destructive"
                                        onSelect={(event) => {
                                          event.preventDefault();
                                          void removeRole(role);
                                        }}
                                      >
                                        <DropdownMenuItemIcon icon={Trash2} className="text-current" />
                                        删除角色
                                      </DropdownMenuItem>
                                    </DropdownMenuContent>
                                  </DropdownMenu>
                                </div>
                                {expanded ? (
                                  <div
                                    id={roleConversationContentID}
                                    className="pb-1 pl-8 pr-1"
                                  >
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
                                ) : null}
                              </>
                            )}
                          </RoleSortableItem>
                        );
                      })}
                    </SidebarMenu>
                  </SortableContext>
                </DndContext>
              )}
            </CollapsibleContent>
          </SidebarGroup>
        </Collapsible>
      </div>
      <RoleDialog draft={draft} setDraft={setDraft} onOpenChange={(open) => !open && setDraft(null)} onSubmit={commitDraft} />
    </>
  );
}
