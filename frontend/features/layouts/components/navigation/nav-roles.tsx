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
import { usePathname, useRouter } from "next/navigation";
import { toast } from "sonner";
import { AnimatePresence, motion, type Transition } from "motion/react";
import { ChevronDown, PencilLine, Pin, PinOff, Star, StarOff, Trash2 } from "lucide-react";

import { Ellipsis } from "@/components/animate-ui/icons/ellipsis";
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
import { Collapsible } from "@/components/ui/collapsible";
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
  SidebarMenuSub,
  SidebarMenuSubItem,
  useSidebar,
} from "@/components/ui/sidebar";
import {
  ConversationLabelsManagerDialog,
  ConversationShareDialog,
  sharePatchFromDTO,
  type ConversationLabelsTarget,
  useConversationExport,
  useSidebarConversations,
} from "@/entities/conversation";
import { useChatSession } from "@/features/chat";
import { SidebarConversationItem } from "@/features/layouts/components/navigation/sidebar-conversation-item";
import { useLayoutActiveConversation } from "@/features/layouts/hooks/use-layout-active-conversation";
import { useSidebarConversationNavigation } from "@/features/layouts/hooks/use-sidebar-conversation-navigation";
import type {
  SidebarConversationDeleteTarget,
  SidebarConversationRenameTarget,
} from "@/features/layouts/types/navigation";
import { RoleDialog, EMPTY_ROLE_DRAFT, type RoleDraft } from "@/features/roles/components/role-dialog";
import { useSettingsChatPreferences } from "@/features/settings";
import { cn } from "@/lib/utils";
import { CollapsibleMotionContent } from "@/shared/components/collapsible-motion-content";
import { DeleteFilesOption } from "@/shared/components/delete-files-option";
import { useDialogSnapshot } from "@/shared/hooks/use-dialog-snapshot";
import { useStoredBoolean } from "@/shared/hooks/use-stored-boolean";
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
import { isReasoningEffortLevel } from "@/shared/lib/reasoning-effort";

const ROLES_OPEN_STORAGE_KEY = "deeix-roles-open";
const ROLE_EXPANDED_IDS_STORAGE_KEY = "deeix.sidebar.roles.expanded";
const ROLE_GROUPS_COLLAPSED_STORAGE_KEY = "deeix.sidebar.roles.groups.collapsed";
const ROLE_TREE_ACCORDION_TRANSITION: Transition = {
  duration: 0.26,
  ease: [0.22, 1, 0.36, 1],
};
const ROLE_TREE_ACCORDION_MASK_STYLE = {
  maskImage: "linear-gradient(black var(--mask-stop), transparent var(--mask-stop))",
  WebkitMaskImage: "linear-gradient(black var(--mask-stop), transparent var(--mask-stop))",
  overflow: "hidden",
} satisfies React.CSSProperties;

function readStoredRoleIDSet(): Set<string> {
  if (typeof window === "undefined") {
    return new Set();
  }
  try {
    const parsed = JSON.parse(window.localStorage.getItem(ROLE_EXPANDED_IDS_STORAGE_KEY) ?? "[]") as unknown;
    return Array.isArray(parsed)
      ? new Set(parsed.filter((item): item is string => typeof item === "string" && item.trim().length > 0))
      : new Set();
  } catch {
    return new Set();
  }
}

function hasStoredRoleIDSet(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  try {
    return window.localStorage.getItem(ROLE_EXPANDED_IDS_STORAGE_KEY) !== null;
  } catch {
    return false;
  }
}

function writeStoredRoleIDSet(value: Set<string>): void {
  try {
    window.localStorage.setItem(ROLE_EXPANDED_IDS_STORAGE_KEY, JSON.stringify(Array.from(value)));
  } catch {
    // 侧边栏持久化在浏览器存储不可用时忽略。
  }
}

function readStoredCollapsedRoleGroups(): Set<string> {
  if (typeof window === "undefined") {
    return new Set();
  }
  try {
    const parsed = JSON.parse(window.localStorage.getItem(ROLE_GROUPS_COLLAPSED_STORAGE_KEY) ?? "[]") as unknown;
    return Array.isArray(parsed)
      ? new Set(parsed.filter((item): item is string => typeof item === "string" && item.length > 0))
      : new Set();
  } catch {
    return new Set();
  }
}

function writeStoredCollapsedRoleGroups(value: Set<string>): void {
  try {
    window.localStorage.setItem(ROLE_GROUPS_COLLAPSED_STORAGE_KEY, JSON.stringify(Array.from(value)));
  } catch {
    // 侧边栏持久化在浏览器存储不可用时忽略。
  }
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
      <span className="ml-1 min-w-0 flex-1 truncate text-left">
        {name}
        {count > 0 ? <span className="text-sidebar-foreground/45">（{count}）</span> : null}
      </span>
    </Button>
  );
}

export function NavRoles() {
  const router = useRouter();
  const pathname = usePathname();
  const { isMobile, setOpenMobile } = useSidebar();
  const onNavigate = useSidebarConversationNavigation();
  const activeConversationID = useLayoutActiveConversation();
  const { deleteFilesByDefault: deleteConversationFilesByDefault } = useSettingsChatPreferences();
  const { requestNewConversation } = useChatSession();
  const {
    items,
    projects,
    setStarByPublicID,
    renameByPublicID,
    regenerateTitleByPublicID,
    updateLabelsByPublicID,
    setProjectByPublicID,
    archiveByPublicID,
    deleteByPublicID,
    touchByPublicID,
  } = useSidebarConversations();
  const [roles, setRoles] = React.useState<ConversationRoleDTO[]>([]);
  const [rolesOpen, setRolesOpen] = useStoredBoolean(ROLES_OPEN_STORAGE_KEY, true);
  const [draft, setDraft] = React.useState<RoleDraft | null>(null);
  const [expandedRoleIDs, setExpandedRoleIDs] = React.useState<Set<string>>(readStoredRoleIDSet);
  const [collapsedRoleGroups, setCollapsedRoleGroups] = React.useState<Set<string>>(readStoredCollapsedRoleGroups);
  const [submitting, setSubmitting] = React.useState(false);
  const [openMenuID, setOpenMenuID] = React.useState<string | null>(null);
  const [hoveredRoleMenuID, setHoveredRoleMenuID] = React.useState<string | null>(null);
  const [hoveredRoleCreateID, setHoveredRoleCreateID] = React.useState<string | null>(null);
  const [hoveredRoleRowID, setHoveredRoleRowID] = React.useState<string | null>(null);
  const [focusedRoleRowID, setFocusedRoleRowID] = React.useState<string | null>(null);
  const [draggingRoleID, setDraggingRoleID] = React.useState<string | null>(null);
  const [savingRoleOrder, setSavingRoleOrder] = React.useState(false);
  const [conversationRenameTarget, setConversationRenameTarget] = React.useState<SidebarConversationRenameTarget>(null);
  const [labelsTarget, setLabelsTarget] = React.useState<ConversationLabelsTarget | null>(null);
  const [conversationDeleteTarget, setConversationDeleteTarget] = React.useState<SidebarConversationDeleteTarget>(null);
  const [deleteConversationFiles, setDeleteConversationFiles] = React.useState(false);
  const [shareTarget, setShareTarget] = React.useState<{
    publicID: string;
    title: string;
  } | null>(null);
  const [renameValue, setRenameValue] = React.useState("");
  const [autoRenamingConversationID, setAutoRenamingConversationID] = React.useState<string | null>(null);
  const expandedRoleIDsRef = React.useRef(expandedRoleIDs);
  const activeRevealedRoleIDsRef = React.useRef(new Set<string>());
  const hasStoredExpandedRoleIDsRef = React.useRef(hasStoredRoleIDSet());
  const deleteConversationFilesID = React.useId();
  const rolesContentID = React.useId();
  const stableConversationDeleteTarget = useDialogSnapshot(conversationDeleteTarget);
  const stableShareTarget = useDialogSnapshot(shareTarget);
  const onExportConversation = useConversationExport({
    successMessage: "会话已导出",
    failureMessage: "导出失败",
  });
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

  const roleGroups = React.useMemo(() => {
    const named = new Map<string, ConversationRoleDTO[]>();
    const ungrouped: ConversationRoleDTO[] = [];
    const pinned: ConversationRoleDTO[] = [];
    for (const role of roles) {
      if (role.pinned) {
        pinned.push(role);
        continue;
      }
      const name = role.groupName?.trim() || "";
      if (!name) {
        ungrouped.push(role);
        continue;
      }
      const list = named.get(name) ?? [];
      list.push(role);
      named.set(name, list);
    }
    const groups = Array.from(named.entries()).map(([name, list]) => ({ name, roles: list }));
    return { pinned, groups, ungrouped };
  }, [roles]);

  const roleGroupOptions = React.useMemo(
    () => Array.from(new Set(roles.map((role) => role.groupName?.trim() || "").filter((name) => name.length > 0))),
    [roles],
  );
  const showRoleGroupHeaders = roleGroups.groups.length > 0;
  const groupNameOfRole = React.useCallback(
    (roleID: string) => roles.find((role) => role.publicID === roleID)?.groupName?.trim() || "",
    [roles],
  );

  const updateExpandedRoleIDs = React.useCallback(
    (updater: (previous: Set<string>) => Set<string>, persist = false) => {
      const next = updater(expandedRoleIDsRef.current);
      expandedRoleIDsRef.current = next;
      setExpandedRoleIDs(next);
      if (persist) {
        hasStoredExpandedRoleIDsRef.current = true;
        writeStoredRoleIDSet(
          new Set(Array.from(next).filter((roleID) => !activeRevealedRoleIDsRef.current.has(roleID))),
        );
      }
    },
    [],
  );

  const ensureRoleExpanded = React.useCallback(
    (roleID: string, persist = false) => {
      if (persist) {
        activeRevealedRoleIDsRef.current.delete(roleID);
      }
      updateExpandedRoleIDs((previous) => {
        if (previous.has(roleID)) {
          return previous;
        }
        const next = new Set(previous);
        next.add(roleID);
        return next;
      }, persist);
    },
    [updateExpandedRoleIDs],
  );

  const toggleRole = React.useCallback(
    (roleID: string) => {
      activeRevealedRoleIDsRef.current.delete(roleID);
      updateExpandedRoleIDs((previous) => {
        const next = new Set(previous);
        if (next.has(roleID)) {
          next.delete(roleID);
        } else {
          next.add(roleID);
        }
        return next;
      }, true);
    },
    [updateExpandedRoleIDs],
  );

  React.useEffect(() => {
    if (!activeRoleID || hasStoredExpandedRoleIDsRef.current || activeRevealedRoleIDsRef.current.has(activeRoleID)) {
      return;
    }
    activeRevealedRoleIDsRef.current.add(activeRoleID);
    ensureRoleExpanded(activeRoleID, false);
  }, [activeRoleID, ensureRoleExpanded]);

  const toggleRoleGroup = React.useCallback((groupName: string) => {
    setCollapsedRoleGroups((previous) => {
      const next = new Set(previous);
      if (next.has(groupName)) {
        next.delete(groupName);
      } else {
        next.add(groupName);
      }
      writeStoredCollapsedRoleGroups(next);
      return next;
    });
  }, []);

  const startRoleChat = React.useCallback(
    (role: ConversationRoleDTO) => {
      ensureRoleExpanded(role.publicID, true);
      requestNewConversation({ roleID: role.publicID });
      const targetHref = `/chat?role_id=${encodeURIComponent(role.publicID)}`;
      if (pathname === "/chat") {
        // 同路由仅查询参数变化：pushState 只更新 URL，不经过路由导航，
        // 任何浏览器都不会触发整页加载；会话重置由 ChatSession revision 驱动。
        window.history.pushState(null, "", targetHref);
      } else {
        router.push(targetHref);
      }
      if (isMobile) {
        setOpenMobile(false);
      }
    },
    [ensureRoleExpanded, isMobile, pathname, requestNewConversation, router, setOpenMobile],
  );

  const onRoleDragStart = React.useCallback((event: DragStartEvent) => {
    setDraggingRoleID(String(event.active.id));
  }, []);

  const onRoleDragCancel = React.useCallback(() => {
    setDraggingRoleID(null);
  }, []);

  const onRoleDragEnd = React.useCallback(
    async (event: DragEndEvent) => {
      setDraggingRoleID(null);

      const { active, over } = event;
      if (!over || active.id === over.id || savingRoleOrder) {
        return;
      }

      const activeRoleID = String(active.id);
      const overRoleID = String(over.id);
      const activeGroupName = groupNameOfRole(activeRoleID);
      if (activeGroupName !== groupNameOfRole(overRoleID)) {
        // 跨分组拖拽不支持，直接取消。
        return;
      }
      const group = roleGroups.groups.find((item) => item.name === activeGroupName)
        ?? (activeGroupName === "" ? { name: "", roles: roleGroups.ungrouped } : null);
      if (!group || group.roles.length < 2) {
        return;
      }

      const groupRoleIDs = group.roles.map((role) => role.publicID);
      const fromIndex = groupRoleIDs.indexOf(activeRoleID);
      const toIndex = groupRoleIDs.indexOf(overRoleID);
      if (fromIndex < 0 || toIndex < 0 || fromIndex === toIndex) {
        return;
      }

      const reorderedGroupRoleIDs = arrayMove(groupRoleIDs, fromIndex, toIndex);
      const roleByID = new Map(roles.map((role) => [role.publicID, role]));
      const nextRoles = roles.map((role) =>
        groupNameOfRole(role.publicID) === activeGroupName ? roleByID.get(reorderedGroupRoleIDs.shift() ?? "") ?? role : role,
      );
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
    },
    [groupNameOfRole, loadRoles, roleGroups, roles, savingRoleOrder],
  );

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
        groupName: draft.groupName,
        pinned: draft.pinned,
        reasoningEffort:
          isReasoningEffortLevel(draft.reasoningEffort) && draft.reasoningEffort
            ? draft.reasoningEffort
            : undefined,
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

  // pinRole 置顶/取消置顶角色。
  const pinRole = React.useCallback(
    async (role: ConversationRoleDTO) => {
      try {
        const token = await resolveAccessToken();
        await updateConversationRole(token, role.publicID, { pinned: !role.pinned });
        toast.success(role.pinned ? "已取消置顶" : "已置顶");
        await loadRoles();
      } catch {
        toast.error("置顶操作失败");
      }
    },
    [loadRoles],
  );

  // renderRoleRow 渲染单个角色行（置顶区 / 命名分组 / 未分组共用）。
  const renderRoleRow = (role: ConversationRoleDTO, canSortRoleGroup: boolean, keyPrefix: string) => {
    const expanded = expandedRoleIDs.has(role.publicID);
    const conversations = conversationsByRole.get(role.publicID) ?? [];
    const menuOpen = openMenuID === role.publicID;
    const rowHovered = hoveredRoleRowID === role.publicID;
    const rowFocused = focusedRoleRowID === role.publicID;
    const createHovered = hoveredRoleCreateID === role.publicID;
    const menuHovered = hoveredRoleMenuID === role.publicID;
    const rowDragging = draggingRoleID === role.publicID;
    const roleActionPaddingClassName = canSortRoleGroup ? "pr-24" : "pr-16";
    const roleCreateActionClassName = canSortRoleGroup ? "right-16" : "right-8";
    const roleMenuActionClassName = canSortRoleGroup ? "right-8" : "right-0";
    const showRoleActions = isMobile || (!rowDragging && (rowHovered || rowFocused || menuHovered || menuOpen));
    const roleConversationContentID = `sidebar-role-${role.publicID}-conversations`;

    return (
      <RoleSortableItem
        key={`${keyPrefix}:${role.publicID}`}
        roleID={role.publicID}
        disabled={!canSortRoleGroup || savingRoleOrder}
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
              {canSortRoleGroup ? (
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
                onHoverChange={(hovered) => setHoveredRoleCreateID(hovered ? role.publicID : null)}
                onClick={() => startRoleChat(role)}
              >
                <PlusIcon aria-hidden size={16} strokeWidth={1.6} animate={createHovered ? "default" : undefined} />
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
                        groupName: role.groupName ?? "",
                        reasoningEffort: role.reasoningEffort ?? "",
                        pinned: role.pinned,
                      });
                    }}
                  >
                    <DropdownMenuItemIcon icon={PencilLine} className="text-current" />
                    编辑角色
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    onSelect={(event) => {
                      event.preventDefault();
                      void pinRole(role);
                    }}
                  >
                    <DropdownMenuItemIcon icon={role.pinned ? PinOff : Pin} className="text-current" />
                    {role.pinned ? "取消置顶" : "置顶角色"}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onSelect={(event) => {
                      event.preventDefault();
                      void removeRole(role);
                    }}
                  >
                    <DropdownMenuItemIcon icon={Trash2} className="text-destructive" />
                    删除角色
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
            <AnimatePresence initial={false}>
              {expanded ? (
                <motion.div
                  key={`${role.publicID}-conversations`}
                  id={roleConversationContentID}
                  initial={{ height: 0, opacity: 0, "--mask-stop": "0%", y: 6 }}
                  animate={{ height: "auto", opacity: 1, "--mask-stop": "100%", y: 0 }}
                  exit={{ height: 0, opacity: 0, "--mask-stop": "0%", y: 6 }}
                  transition={ROLE_TREE_ACCORDION_TRANSITION}
                  style={ROLE_TREE_ACCORDION_MASK_STYLE}
                >
                  <SidebarMenuSub className="mx-0 w-full translate-x-0 gap-0.5 border-l-0 px-0 py-0.5">
                    {conversations.length === 0 ? (
                      <SidebarMenuSubItem>
                        <div className="w-full rounded-md py-1 pl-8 pr-2 text-xs text-sidebar-foreground/55">
                          暂无对话
                        </div>
                      </SidebarMenuSubItem>
                    ) : (
                      conversations.map((conversation) => {
                        const title = conversation.title || "未命名对话";
                        return (
                          <SidebarConversationItem
                            key={conversation.publicID}
                            active={activeConversationID === conversation.publicID}
                            item={{
                              publicID: conversation.publicID,
                              title,
                              url: `/chat?conversation_id=${conversation.publicID}`,
                              shareActive:
                                conversation.shareStatus === "active" &&
                                Boolean(conversation.shareID?.trim()),
                              labelsJSON: conversation.labelsJSON,
                            }}
                            starAction={{
                              label: conversation.isStarred ? "取消收藏" : "收藏",
                              icon: conversation.isStarred ? StarOff : Star,
                              onSelect: (targetPublicID) => {
                                void setStarByPublicID(targetPublicID, !conversation.isStarred);
                              },
                            }}
                            projectMenu={{
                              label: "移动到项目",
                              unassignedLabel: "未分配项目",
                              currentProjectID: conversation.projectID,
                              projects,
                              onSelect: (targetPublicID, targetProjectID) => {
                                void setProjectByPublicID(targetPublicID, targetProjectID);
                              },
                            }}
                            isTransferring={false}
                            isRenaming={conversationRenameTarget?.publicID === conversation.publicID}
                            renameValue={
                              conversationRenameTarget?.publicID === conversation.publicID
                                ? renameValue
                                : title
                            }
                            rowClassName="w-full"
                            linkClassName="pl-8"
                            onRenameValueChange={setRenameValue}
                            onRenameCommit={onRenameConversationCommit}
                            onRenameCancel={onRenameConversationCancel}
                            onAutoRename={onAutoRenameConversation}
                            isAutoRenaming={autoRenamingConversationID === conversation.publicID}
                            onManageLabels={() => setLabelsTarget(conversation)}
                            onRename={onRenameConversation}
                            onArchive={onArchiveConversation}
                            onShare={(publicID, shareTitle) =>
                              setShareTarget({ publicID, title: shareTitle })
                            }
                            onExport={onExportConversation}
                            onDelete={onDeleteConversation}
                            onNavigate={onNavigate}
                            menuTriggerID={`role-conversation-menu-trigger-${conversation.publicID}`}
                          />
                        );
                      })
                    )}
                  </SidebarMenuSub>
                </motion.div>
              ) : null}
            </AnimatePresence>
          </>
        )}
      </RoleSortableItem>
    );
  };

  const onRenameConversation = React.useCallback((publicID: string, currentTitle: string) => {
    setConversationRenameTarget({ publicID, currentTitle });
    setRenameValue(currentTitle);
  }, []);

  const onRenameConversationCancel = React.useCallback(() => {
    setConversationRenameTarget(null);
    setRenameValue("");
  }, []);

  const onRenameConversationCommit = React.useCallback(
    async (publicID: string, currentTitle: string) => {
      const nextTitle = renameValue.trim();
      if (!nextTitle || nextTitle === currentTitle) {
        onRenameConversationCancel();
        return;
      }
      await renameByPublicID(publicID, nextTitle);
      onRenameConversationCancel();
    },
    [onRenameConversationCancel, renameByPublicID, renameValue],
  );

  const onAutoRenameConversation = React.useCallback(
    async (publicID: string) => {
      if (autoRenamingConversationID) {
        return;
      }
      setAutoRenamingConversationID(publicID);
      try {
        const updated = await regenerateTitleByPublicID(publicID);
        if (updated) {
          onRenameConversationCancel();
        }
      } catch {
        // 保留当前重命名输入框，用户可重试或手动修改。
      } finally {
        setAutoRenamingConversationID(null);
      }
    },
    [autoRenamingConversationID, onRenameConversationCancel, regenerateTitleByPublicID],
  );

  const onArchiveConversation = React.useCallback(
    async (publicID: string) => {
      await archiveByPublicID(publicID, true);
      if (activeConversationID === publicID) {
        router.push("/chat");
      }
    },
    [activeConversationID, archiveByPublicID, router],
  );

  const onDeleteConversation = React.useCallback((publicID: string, title: string) => {
    setDeleteConversationFiles(deleteConversationFilesByDefault);
    setConversationDeleteTarget({ publicID, title });
  }, [deleteConversationFilesByDefault]);

  const confirmDeleteConversation = React.useCallback(async () => {
    if (!conversationDeleteTarget) {
      return;
    }
    const ok = await deleteByPublicID(conversationDeleteTarget.publicID, { deleteFiles: deleteConversationFiles });
    if (ok && activeConversationID === conversationDeleteTarget.publicID) {
      router.push("/chat");
    }
    setConversationDeleteTarget(null);
    setDeleteConversationFiles(false);
  }, [activeConversationID, conversationDeleteTarget, deleteByPublicID, deleteConversationFiles, router]);

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
                  aria-controls={rolesContentID}
                  aria-expanded={rolesOpen}
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
            <CollapsibleMotionContent id={rolesContentID} open={rolesOpen}>
              {roles.length === 0 ? (
                <div className="px-2 py-1 text-xs text-sidebar-foreground/55">暂无角色</div>
              ) : (
                <DndContext
                  sensors={roleSortSensors}
                  collisionDetection={closestCenter}
                  onDragStart={onRoleDragStart}
                  onDragEnd={(event) => void onRoleDragEnd(event)}
                  onDragCancel={onRoleDragCancel}
                >
                  {roleGroups.pinned.length > 0 ? (
                    <div className="space-y-0.5">
                      <div className="px-1 pb-0.5 pt-1">
                        <p className="flex items-center gap-1 px-2 text-[10px] font-medium uppercase tracking-wide text-sidebar-foreground/50">
                          <Pin aria-hidden className="size-3" />
                          置顶
                        </p>
                      </div>
                      <SortableContext items={roleGroups.pinned.map((role) => role.publicID)} strategy={verticalListSortingStrategy}>
                        <SidebarMenu className="gap-0.5">
                          {roleGroups.pinned.map((role) => renderRoleRow(role, false, "pinned"))}
                        </SidebarMenu>
                      </SortableContext>
                    </div>
                  ) : null}

                  {roleGroups.groups.map((group) => {
                    const groupCollapsed = collapsedRoleGroups.has(group.name);
                    const groupRoleIDs = group.roles.map((role) => role.publicID);
                    const canSortRoleGroup = group.roles.length >= 2;
                    return (
                      <React.Fragment key={group.name}>
                        {showRoleGroupHeaders ? (
                          <div className="px-1 pb-0.5 pt-1">
                            <Button
                              type="button"
                              variant="ghost"
                              className="flex h-7 w-full min-w-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-sidebar-foreground/75 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                              aria-expanded={!groupCollapsed}
                              onClick={() => toggleRoleGroup(group.name)}
                            >
                              <ChevronDown
                                aria-hidden
                                className={cn(
                                  "size-3.5 shrink-0 text-muted-foreground/60 transition-transform duration-200",
                                  !groupCollapsed && "rotate-180",
                                )}
                              />
                              <span className="min-w-0 flex-1 truncate text-left">{group.name}</span>
                              <span className="shrink-0 text-[10px] text-sidebar-foreground/45">{group.roles.length}</span>
                            </Button>
                          </div>
                        ) : null}
                        <AnimatePresence initial={false}>
                          {!groupCollapsed ? (
                            <motion.div
                              key={`${group.name}-roles`}
                              initial={{ height: 0, opacity: 0, "--mask-stop": "0%", y: 6 }}
                              animate={{ height: "auto", opacity: 1, "--mask-stop": "100%", y: 0 }}
                              exit={{ height: 0, opacity: 0, "--mask-stop": "0%", y: 6 }}
                              transition={ROLE_TREE_ACCORDION_TRANSITION}
                              style={ROLE_TREE_ACCORDION_MASK_STYLE}
                            >
                              <SortableContext items={groupRoleIDs} strategy={verticalListSortingStrategy}>
                                <SidebarMenu className="gap-0.5">
                                  {group.roles.map((role) => renderRoleRow(role, canSortRoleGroup, `group:${group.name}`))}
                                </SidebarMenu>
                              </SortableContext>
                            </motion.div>
                          ) : null}
                        </AnimatePresence>
                      </React.Fragment>
                    );
                  })}

                  {roleGroups.ungrouped.length > 0 ? (
                    <SortableContext items={roleGroups.ungrouped.map((role) => role.publicID)} strategy={verticalListSortingStrategy}>
                      <SidebarMenu className="gap-0.5">
                        {roleGroups.ungrouped.map((role) => renderRoleRow(role, roleGroups.ungrouped.length >= 2, "ungrouped"))}
                      </SidebarMenu>
                    </SortableContext>
                  ) : null}
                </DndContext>
              )}
            </CollapsibleMotionContent>
          </SidebarGroup>
        </Collapsible>
      </div>
      <RoleDialog
        draft={draft}
        setDraft={setDraft}
        onOpenChange={(open) => !open && setDraft(null)}
        onSubmit={commitDraft}
        groupOptions={roleGroupOptions}
      />

      <AlertDialog
        open={Boolean(conversationDeleteTarget)}
        onOpenChange={(open) => {
          if (!open) {
            setConversationDeleteTarget(null);
            setDeleteConversationFiles(false);
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除会话</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除会话「{stableConversationDeleteTarget?.title || "未命名对话"}」吗？
            </AlertDialogDescription>
            <DeleteFilesOption
              id={deleteConversationFilesID}
              checked={deleteConversationFiles}
              onCheckedChange={setDeleteConversationFiles}
            />
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => void confirmDeleteConversation()}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <ConversationLabelsManagerDialog
        target={labelsTarget}
        onTargetChange={setLabelsTarget}
        onUpdateLabels={updateLabelsByPublicID}
      />

      {stableShareTarget ? (
        <ConversationShareDialog
          open={Boolean(shareTarget)}
          onOpenChange={(open) => !open && setShareTarget(null)}
          conversationPublicID={stableShareTarget.publicID}
          conversationTitle={stableShareTarget.title}
          onShareChange={(share) => {
            touchByPublicID(stableShareTarget.publicID, sharePatchFromDTO(share));
          }}
        />
      ) : null}
    </>
  );
}
