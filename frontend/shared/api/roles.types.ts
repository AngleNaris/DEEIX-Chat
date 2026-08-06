export type ConversationRoleMCPDefaultMode = "inherit" | "custom";

export type ConversationRoleReasoningEffort = "low" | "medium" | "high" | "xhigh";

export type ConversationRoleDTO = {
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
};

export type ReorderConversationRolesRequest = {
  roleIDs: string[];
};
