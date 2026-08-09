export type AgentGroupMemberType = "supervisor" | "worker";

export type AgentGroupMemberReasoningEffort = "low" | "medium" | "high" | "xhigh" | "max";

export type AgentGroupMemberDTO = {
  publicID: string;
  rolePublicID: string;
  roleName: string;
  roleIcon: string;
  roleColor: string;
  roleModel: string;
  roleProvider: string;
  memberType: AgentGroupMemberType;
  enabled: boolean;
  modelOverride: string;
  dutyInstruction: string;
  reasoningEffort: string;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
};

export type AgentGroupDTO = {
  publicID: string;
  projectID: string;
  projectName: string;
  name: string;
  description: string;
  coordinationPrompt: string;
  supervisorMemberID: string;
  sortOrder: number;
  status: string;
  revision: number;
  members: AgentGroupMemberDTO[];
  createdAt: string;
  updatedAt: string;
};

export type AgentGroupMemberRequest = {
  rolePublicID: string;
  modelOverride?: string;
  dutyInstruction?: string;
  reasoningEffort?: AgentGroupMemberReasoningEffort;
};

export type CreateAgentGroupRequest = {
  name: string;
  description?: string;
  coordinationPrompt?: string;
  supervisor: AgentGroupMemberRequest;
  workers?: AgentGroupMemberRequest[];
};

export type UpdateAgentGroupRequest = {
  name?: string;
  description?: string;
  coordinationPrompt?: string;
  sortOrder?: number;
};

export type AddAgentGroupMemberRequest = AgentGroupMemberRequest;

export type UpdateAgentGroupMemberRequest = {
  enabled?: boolean;
  modelOverride?: string;
  dutyInstruction?: string;
  reasoningEffort?: AgentGroupMemberReasoningEffort;
  sortOrder?: number;
};

export type ReorderAgentGroupMembersRequest = {
  orderedPublicIDs: string[];
};

export type ChangeAgentGroupSupervisorRequest = {
  memberPublicID: string;
};

export type AgentGroupFeatureDTO = {
  enabled: boolean;
};

// ---- 群组运行详情（刷新恢复时间线；镜像后端 AgentGroupRunDetailResponse）----

export type AgentGroupRunStatus =
  | "pending"
  | "running"
  | "paused_retryable"
  | "blocked"
  | "completed"
  | "abandoned";

export type AgentGroupRunDTO = {
  publicID: string;
  clientRunID: string;
  conversationID: number;
  groupPublicID: string;
  status: AgentGroupRunStatus;
  errorCode: string;
  errorMessage: string;
  startedAt: string;
  endedAt: string | null;
  createdAt: string;
  updatedAt: string;
};

export type AgentGroupStepAttemptDTO = {
  publicID: string;
  attemptNo: number;
  requestedModel: string;
  resolvedModel: string;
  status: string;
  errorCode: string;
  errorMessage: string;
  outputMarkdown: string;
  startedAt: string;
  endedAt: string | null;
  createdAt: string;
};

export type AgentGroupStepDTO = {
  publicID: string;
  sequence: number;
  stepType: "supervisor_decide" | "member_execute";
  actorMemberPublicID: string;
  actorNameSnapshot: string;
  actorTypeSnapshot: string;
  status: string;
  instruction: string;
  attempts: AgentGroupStepAttemptDTO[];
  createdAt: string;
  updatedAt: string;
};

export type AgentGroupRunDetailDTO = {
  run: AgentGroupRunDTO;
  steps: AgentGroupStepDTO[];
};
