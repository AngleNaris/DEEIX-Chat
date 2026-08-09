export type ConversationRoleMCPDefaultMode = "inherit" | "custom";

export type ConversationRoleReasoningEffort = "low" | "medium" | "high" | "xhigh" | "max";

export type ConversationRoleDTO = {
  id: number;
  publicID: string;
  name: string;
  description: string;
  systemPrompt: string;
  model: string;
  provider: string;
  mcpDefaultMode: ConversationRoleMCPDefaultMode;
  defaultMCPToolIDs: number[];
  defaultSkillIDs: number[];
  color: string;
  icon: string;
  groupName: string;
  reasoningEffort: string;
  sortOrder: number;
  pinned: boolean;
  status: string;
  createdAt: string;
  updatedAt: string;
};

export type CreateConversationRoleRequest = {
  name: string;
  description?: string;
  systemPrompt?: string;
  model?: string;
  provider?: string;
  mcpDefaultMode?: ConversationRoleMCPDefaultMode;
  defaultMCPToolIDs?: number[];
  defaultSkillIDs?: number[];
  color?: string;
  icon?: string;
  groupName?: string;
  reasoningEffort?: ConversationRoleReasoningEffort;
  pinned?: boolean;
};

export type UpdateConversationRoleRequest = {
  name?: string;
  description?: string;
  systemPrompt?: string;
  model?: string;
  provider?: string;
  mcpDefaultMode?: ConversationRoleMCPDefaultMode;
  defaultMCPToolIDs?: number[];
  defaultSkillIDs?: number[];
  color?: string;
  icon?: string;
  status?: "active" | "archived";
  groupName?: string;
  reasoningEffort?: ConversationRoleReasoningEffort;
  pinned?: boolean;
};

export type ReorderConversationRolesRequest = {
  roleIDs: string[];
};
