import { authedFetch, authedRequest } from "@/shared/api/authed-client";
import { ApiError, pathParam } from "@/shared/api/http-client";
import { readConversationStream } from "@/shared/api/conversation";
import type {
  AddAgentGroupMemberRequest,
  AgentGroupDTO,
  AgentGroupFeatureDTO,
  AgentGroupMemberDTO,
  AgentGroupRunDetailDTO,
  ChangeAgentGroupSupervisorRequest,
  CreateAgentGroupRequest,
  ReorderAgentGroupMembersRequest,
  UpdateAgentGroupMemberRequest,
  UpdateAgentGroupRequest,
} from "@/shared/api/agent-groups.types";
import type { GroupStreamEvent, StreamMessageEvent } from "@/shared/api/conversation.types";

export async function listAgentGroups(
  accessToken: string,
  projectPublicID: string,
): Promise<AgentGroupDTO[]> {
  return authedRequest<AgentGroupDTO[]>(
    `/api/v1/conversation-agent-groups?projectID=${encodeURIComponent(projectPublicID)}`,
    {
      accessToken,
    },
    true,
  );
}

export async function getAgentGroup(
  accessToken: string,
  groupPublicID: string,
): Promise<AgentGroupDTO> {
  return authedRequest<AgentGroupDTO>(
    `/api/v1/conversation-agent-groups/${pathParam(groupPublicID)}`,
    {
      accessToken,
    },
    true,
  );
}

export async function createAgentGroup(
  accessToken: string,
  payload: CreateAgentGroupRequest,
): Promise<AgentGroupDTO> {
  return authedRequest<AgentGroupDTO>(
    "/api/v1/conversation-agent-groups",
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function updateAgentGroup(
  accessToken: string,
  groupPublicID: string,
  payload: UpdateAgentGroupRequest,
): Promise<AgentGroupDTO> {
  return authedRequest<AgentGroupDTO>(
    `/api/v1/conversation-agent-groups/${pathParam(groupPublicID)}`,
    {
      method: "PATCH",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function deleteAgentGroup(
  accessToken: string,
  groupPublicID: string,
): Promise<{ deleted: boolean }> {
  return authedRequest<{ deleted: boolean }>(
    `/api/v1/conversation-agent-groups/${pathParam(groupPublicID)}`,
    {
      method: "DELETE",
      accessToken,
    },
    true,
  );
}

export async function getAgentGroupFeature(
  accessToken: string,
): Promise<AgentGroupFeatureDTO> {
  return authedRequest<AgentGroupFeatureDTO>(
    "/api/v1/conversation-agent-groups/feature",
    {
      accessToken,
    },
    true,
  );
}

export async function addAgentGroupMember(
  accessToken: string,
  groupPublicID: string,
  payload: AddAgentGroupMemberRequest,
): Promise<AgentGroupDTO> {
  return authedRequest<AgentGroupDTO>(
    `/api/v1/conversation-agent-groups/${pathParam(groupPublicID)}/members`,
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function updateAgentGroupMember(
  accessToken: string,
  groupPublicID: string,
  memberPublicID: string,
  payload: UpdateAgentGroupMemberRequest,
): Promise<AgentGroupDTO> {
  return authedRequest<AgentGroupDTO>(
    `/api/v1/conversation-agent-groups/${pathParam(groupPublicID)}/members/${pathParam(memberPublicID)}`,
    {
      method: "PATCH",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function removeAgentGroupMember(
  accessToken: string,
  groupPublicID: string,
  memberPublicID: string,
): Promise<AgentGroupDTO> {
  return authedRequest<AgentGroupDTO>(
    `/api/v1/conversation-agent-groups/${pathParam(groupPublicID)}/members/${pathParam(memberPublicID)}`,
    {
      method: "DELETE",
      accessToken,
    },
    true,
  );
}

export async function reorderAgentGroupMembers(
  accessToken: string,
  groupPublicID: string,
  payload: ReorderAgentGroupMembersRequest,
): Promise<AgentGroupDTO> {
  return authedRequest<AgentGroupDTO>(
    `/api/v1/conversation-agent-groups/${pathParam(groupPublicID)}/members/reorder`,
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function changeAgentGroupSupervisor(
  accessToken: string,
  groupPublicID: string,
  payload: ChangeAgentGroupSupervisorRequest,
): Promise<AgentGroupDTO> {
  return authedRequest<AgentGroupDTO>(
    `/api/v1/conversation-agent-groups/${pathParam(groupPublicID)}/supervisor`,
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export type { AgentGroupDTO, AgentGroupMemberDTO };

// ---- 群组运行控制（重试 / 取消 / 放弃 / 详情恢复）----

// AgentGroupRetryStreamOptions 重试流回调（与首次运行的群组流式协议一致）。
export type AgentGroupRetryStreamOptions = {
  signal?: AbortSignal;
  onGroupEvent?: (event: GroupStreamEvent) => void;
  onToolEvent?: (event: Extract<StreamMessageEvent, { type: "tool_call" | "tool_result" }>) => void;
  onUpstreamThinkDelta?: (event: Extract<StreamMessageEvent, { type: "upstream_think_delta" }>) => void;
  onDelta?: (delta: string) => void;
  onStatus?: (event: Extract<StreamMessageEvent, { type: "status" }>) => void;
  onUsage?: (event: Extract<StreamMessageEvent, { type: "usage" }>) => void;
  onStreamError?: (event: Extract<StreamMessageEvent, { type: "error" }>) => void;
};

export type AgentGroupRetryStreamResult = {
  status: "completed" | "paused_retryable" | "error";
  errorCode?: string;
  message?: string;
};

export async function getAgentGroupRunDetail(
  accessToken: string,
  runPublicID: string,
): Promise<AgentGroupRunDetailDTO> {
  return authedRequest<AgentGroupRunDetailDTO>(
    `/api/v1/conversation-agent-group-runs/${pathParam(runPublicID)}`,
    { accessToken },
    true,
  );
}

export async function getAgentGroupRunDetailByClientRunID(
  accessToken: string,
  conversationPublicID: string,
  clientRunID: string,
): Promise<AgentGroupRunDetailDTO> {
  return authedRequest<AgentGroupRunDetailDTO>(
    `/api/v1/conversation-agent-group-runs/lookup?conversationID=${encodeURIComponent(conversationPublicID)}&clientRunID=${encodeURIComponent(clientRunID)}`,
    { accessToken },
    true,
  );
}

export async function cancelAgentGroupRun(
  accessToken: string,
  runPublicID: string,
): Promise<{ canceled: boolean }> {
  return authedRequest<{ canceled: boolean }>(
    `/api/v1/agent-group-runs/${pathParam(runPublicID)}/cancel`,
    { method: "POST", accessToken },
    true,
  );
}

export async function abandonAgentGroupRun(
  accessToken: string,
  runPublicID: string,
): Promise<{ status: "abandoned" }> {
  return authedRequest<{ status: "abandoned" }>(
    `/api/v1/agent-group-runs/${pathParam(runPublicID)}/abandon`,
    { method: "POST", accessToken },
    true,
  );
}

// retryAgentGroupRunStep 从暂停的失败步骤原地重试（retryRequestID 防止双击创建两个 Attempt）。
// 流式消费 NDJSON：群组事件 → onGroupEvent；正文增量 → onDelta；
// completed/error 事件的 data 携带最终状态（completed / paused_retryable）。
export async function retryAgentGroupRunStep(
  accessToken: string,
  runPublicID: string,
  stepPublicID: string,
  retryRequestID: string,
  options: AgentGroupRetryStreamOptions = {},
): Promise<AgentGroupRetryStreamResult> {
  const response = await authedFetch(
    `/api/v1/agent-group-runs/${pathParam(runPublicID)}/steps/${pathParam(stepPublicID)}/retry`,
    {
      method: "POST",
      accessToken,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ retryRequestID }),
      signal: options.signal,
    },
    true,
  );
  try {
    const result = await readConversationStream(response, {
      signal: options.signal,
      onGroupEvent: options.onGroupEvent,
      onToolEvent: options.onToolEvent,
      onUpstreamThinkDelta: options.onUpstreamThinkDelta,
      onDelta: options.onDelta,
      onStatus: options.onStatus,
      onUsage: options.onUsage,
      onInterrupted: options.onStreamError,
    });
    // 重试端点的 completed/error 事件 data 形如 { status: "completed" | "paused_retryable" }，
    // 与通用 SendMessageResult 形状不同，这里只读取 status 字段。
    const status = (result as { status?: string } | null)?.status;
    if (typeof status === "string") {
      return {
        status: status === "completed" ? "completed" : "paused_retryable",
      };
    }
    return { status: "completed" };
  } catch (error) {
    if (error instanceof ApiError) {
      return { status: "error", errorCode: error.errorCode, message: error.rawMessage };
    }
    throw error;
  }
}
