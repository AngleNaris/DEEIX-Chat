export type ConversationRoleMCPDefaultMode = "inherit" | "custom";

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
};

export type ReorderConversationRolesRequest = {
  roleIDs: string[];
};
