import type {
  BatchSetConversationProjectRequest as ContractBatchSetConversationProjectRequest,
  BatchSetConversationProjectResponse,
  ContextArtifactResponse,
  ConversationDefaultModelCandidateResponse,
  ConversationDeleteResponse,
  ConversationExportResponse,
  ConversationProjectResponse,
  ConversationPreviewMessageResponse,
  ConversationResponse,
  ConversationSearchPageResponse,
  ConversationSearchResultResponse,
  ConversationShareResponse,
  CreateConversationProjectRequest as ContractCreateConversationProjectRequest,
  CreateConversationRequest as ContractCreateConversationRequest,
  CreateConversationShareRequest as ContractCreateConversationShareRequest,
  MessageBillingCostResponse,
  MessageFeedbackResponse,
  MessageProcessTraceResponse,
  MessagePromptTraceBlockResponse,
  MessagePromptTraceResponse,
  MessagePromptTraceSourceResponse,
  MessageResponse,
  MessageTraceBlockResponse,
  MessageTraceEventResponse,
  ModelProbeDebugResponse,
  PublicSharedConversationResponse,
  PublicSharedMessageResponse,
  RenameConversationRequest as ContractRenameConversationRequest,
  ReorderConversationProjectsRequest as ContractReorderConversationProjectsRequest,
  RevokeConversationSharesRequest as ContractRevokeConversationSharesRequest,
  RevokeConversationSharesResponse,
  RunResponse,
  SendMessageRequest as ContractSendMessageRequest,
  SendMessageResponse,
  SetConversationArchiveRequest as ContractSetConversationArchiveRequest,
  SetConversationProjectRequest as ContractSetConversationProjectRequest,
  SetConversationStarRequest as ContractSetConversationStarRequest,
  SetMessageFeedbackRequest as ContractSetMessageFeedbackRequest,
  UpdateConversationProjectRequest as ContractUpdateConversationProjectRequest,
  UpdateConversationLabelsRequest as ContractUpdateConversationLabelsRequest,
  UpdateMessageRequest as ContractUpdateMessageRequest,
} from "@deeix/api-contract";
import type { UserStorageQuotaDTO } from "@/shared/api/file.types";

export type ConversationDTO = ConversationResponse & {
  roleID?: string;
  roleName?: string;
};

export type ConversationSearchResultDTO = ConversationSearchResultResponse;

export type ConversationSearchPageDTO = Omit<ConversationSearchPageResponse, "results"> & {
  results: ConversationSearchResultDTO[];
};

export type ConversationPreviewMessageDTO = ConversationPreviewMessageResponse;

export type ConversationDefaultModelCandidateDTO = ConversationDefaultModelCandidateResponse;

export type ConversationStatusFilter = "active" | "archived" | "all";
export type ConversationStarredFilter = "all" | "starred" | "unstarred";
export type ConversationShareFilter = "all" | "shared" | "unshared";
export type ConversationProjectFilter = "all" | "unassigned" | string;
export type ConversationProjectStatusFilter = "active" | "archived" | "all";
export type ConversationProjectMCPDefaultMode = "inherit" | "custom";

export type ConversationProjectDTO = Omit<ConversationProjectResponse, "mcpDefaultMode"> & {
  id: number;
  mcpDefaultMode: ConversationProjectMCPDefaultMode;
};

export type MessageDTO = Omit<
  MessageResponse,
  | "billingCost"
  | "modelIcon"
  | "modelVendor"
  | "platformModelName"
  | "processTrace"
  | "upstreamModelName"
> & {
  branchReason: "default" | "retry" | "edit";
  platformModelName?: string;
  upstreamModelName?: string;
  modelVendor?: string;
  modelIcon?: string;
  processTrace?: MessageProcessTraceDTO;
  myFeedback: "up" | "down" | "";
  billingCost?: MessageBillingCostDTO;
};

export type ConversationRunDTO = Omit<RunResponse, "taskType">;

export type ConversationExportDTO = Omit<
  ConversationExportResponse,
  "compatibility" | "conversation" | "messages" | "runs"
> & {
  conversation: ConversationDTO;
  messages: MessageDTO[];
  runs: ConversationRunDTO[];
  compatibility: ConversationExportResponse["compatibility"];
};

export type MessageBillingCostDTO = MessageBillingCostResponse;

export type TraceBlockDTO = MessageTraceBlockResponse;

export type PromptTraceBlockDTO = Omit<MessagePromptTraceBlockResponse, "sourceRefs"> & {
  sourceRefs?: PromptTraceSourceDTO[];
};

export type PromptTraceSourceDTO = MessagePromptTraceSourceResponse;

export type ContextArtifactDTO = ContextArtifactResponse;

export type PromptTraceDTO = Omit<MessagePromptTraceResponse, "blocks"> & {
  blocks: PromptTraceBlockDTO[];
};

export type ReasoningDeltaDTO = {
  event_type: string;
  item_id?: string;
  status?: string;
  kind: "summary_text" | "content_text" | "signature";
  signature?: string;
  encrypted_content?: string;
};

// Agent 群组流式事件的可选 Actor 元数据（方案 §15）。
// 后端在群组运行期间为现有事件（process_update/upstream_think_delta/工具事件/usage）
// 与 8 个群组事件附加相同的一组公共字段；字段均为可选以容忍省略。
export type GroupStreamEventMeta = {
  groupRunID?: string;
  stepID?: string;
  attemptID?: string;
  attemptNumber?: number;
  sequence?: number;
  stepType?: string;
  status?: string;
  actorMemberID?: string;
  actorName?: string;
  actorType?: string;
  actorIcon?: string;
  actorColor?: string;
  model?: string;
};

export type MessageProcessTraceDTO = Omit<
  MessageProcessTraceResponse,
  "events" | "process" | "promptTrace" | "tools" | "upstreamThink"
> & {
  process?: TraceBlockDTO;
  tools?: TraceBlockDTO;
  upstreamThink?: TraceBlockDTO;
  promptTrace?: PromptTraceDTO;
  events?: TraceEventDTO[];
};

export type TraceEventDTO = MessageTraceEventResponse;

export type CreateConversationRequest = ContractCreateConversationRequest & {
  roleID?: string;
};

export type CreateConversationProjectRequest = Omit<ContractCreateConversationProjectRequest, "mcpDefaultMode"> & {
  mcpDefaultMode?: ConversationProjectMCPDefaultMode;
};

export type UpdateConversationProjectRequest = Omit<ContractUpdateConversationProjectRequest, "mcpDefaultMode"> & {
  mcpDefaultMode?: ConversationProjectMCPDefaultMode;
};

export type ReorderConversationProjectsRequest = ContractReorderConversationProjectsRequest;

export type SetConversationProjectRequest = ContractSetConversationProjectRequest;

export type BatchSetConversationProjectRequest = ContractBatchSetConversationProjectRequest;

export type BatchSetConversationProjectResult = BatchSetConversationProjectResponse;

export type ConversationOptions = Record<string, unknown>;

export type UpstreamDebugInfo = ModelProbeDebugResponse;

export type RenameConversationRequest = ContractRenameConversationRequest;

export type UpdateConversationLabelsRequest = ContractUpdateConversationLabelsRequest;

export type SetConversationStarRequest = ContractSetConversationStarRequest;

export type SetConversationArchiveRequest = ContractSetConversationArchiveRequest;

export type DeleteConversationData = Omit<ConversationDeleteResponse, "quota"> & {
  quota?: UserStorageQuotaDTO;
};

export type CreateConversationShareRequest = ContractCreateConversationShareRequest;

export type ConversationShareDTO = ConversationShareResponse;

export type RevokeConversationSharesRequest = ContractRevokeConversationSharesRequest;

export type RevokeConversationSharesResult = RevokeConversationSharesResponse;

export type PublicSharedMessageDTO = Omit<PublicSharedMessageResponse, "processTrace"> & {
  processTrace?: MessageProcessTraceDTO;
};

// 分享快照中的群组中间过程时间线（后端由 chat_agent_group_runs/steps/attempts 脱敏重建）。
export type PublicSharedGroupRunTimelineDTO = {
  groupRunID: string;
  status: string;
  steps: Array<{
    stepID: string;
    sequence: number;
    stepType: string;
    actor: {
      memberID: string;
      name: string;
      type: string;
      icon: string;
      color: string;
      model: string;
    };
    status: string;
    attempts: Array<{
      attemptID: string;
      attemptNumber: number;
      status: string;
      output: string;
      errorCode?: string;
      thinkMarkdown?: string;
      toolCallsJSON?: string;
      startedAt: string;
      endedAt?: string | null;
      updatedAt: string;
    }>;
    startedAt: string;
    endedAt?: string | null;
    updatedAt: string;
  }>;
  currentStepID: string | null;
  currentAttemptID: string | null;
  errorCode?: string;
  startedAt: string;
  endedAt?: string | null;
  updatedAt: string;
};

export type PublicSharedConversationDTO = Omit<PublicSharedConversationResponse, "messages"> & {
  messages: PublicSharedMessageDTO[];
  groupRuns?: Record<string, PublicSharedGroupRunTimelineDTO>;
};

export type SetMessageFeedbackRequest = ContractSetMessageFeedbackRequest;

export type UpdateMessageRequest = ContractUpdateMessageRequest;

export type MessageFeedbackResult = Omit<MessageFeedbackResponse, "myFeedback"> & {
  myFeedback: "up" | "down" | "";
};

export type SendMessageRequest = Omit<ContractSendMessageRequest, "options"> & {
  options?: ConversationOptions;
};

export type MediaImageRequest = {
  prompt: string;
  model?: string;
  options?: ConversationOptions;
  clientRunID?: string;
  fileIDs?: string[];
  maskFileID?: string;
  parentMessagePublicID?: string;
  sourceMessagePublicID?: string;
  branchReason?: "default" | "retry" | "edit";
};

export type MediaVideoRequest = {
  prompt: string;
  model?: string;
  options?: ConversationOptions;
  clientRunID?: string;
  fileIDs?: string[];
  parentMessagePublicID?: string;
  sourceMessagePublicID?: string;
  branchReason?: "default" | "retry" | "edit";
};

export type SendMessageResult = Omit<SendMessageResponse, "assistantMessage" | "metadataRefreshHint" | "userMessage"> & {
  userMessage: MessageDTO;
  assistantMessage: MessageDTO;
  metadataRefreshHint?: "pending" | "not_needed" | "skipped_no_titleable_content" | string;
};

export type StreamMessageEvent =
  | {
      type: "file_proc";
      seq?: number;
      message: string;
    }
  | {
      type: "rag_search";
      seq?: number;
      message: string;
    }
  | ({
      type: "process_update";
      seq?: number;
      status: string;
      block?: TraceBlockDTO;
      trace?: MessageProcessTraceDTO;
    } & GroupStreamEventMeta)
  | ({
      type: "upstream_think_delta";
      seq?: number;
      status: string;
      title?: string;
      summary?: string;
      stage?: string;
      roundID?: string;
      eventID?: string;
      kind?: ReasoningDeltaDTO["kind"] | string;
      delta?: string;
      contentMarkdown?: string;
      block?: TraceBlockDTO;
      trace?: MessageProcessTraceDTO;
      reasoning?: ReasoningDeltaDTO;
    } & GroupStreamEventMeta)
  | {
      type: "delta";
      seq?: number;
      delta: string;
    }
  | ({
      type: "usage";
      seq?: number;
      input_tokens: number;
      output_tokens: number;
      cache_read_tokens: number;
      cache_write_tokens: number;
      reasoning_tokens: number;
    } & GroupStreamEventMeta)
  | {
      type: "media_status";
      seq?: number;
      status: string;
      message: string;
      content_type?: string;
    }
  | {
      type: "media_image_delta";
      seq?: number;
      index?: number;
      b64_json: string;
      mime_type?: string;
      revised_prompt?: string;
    }
  | {
      type: "completed";
      seq?: number;
      data: SendMessageResult;
    }
  | {
      type: "compact_done";
      seq?: number;
      method: string;
      freed_tokens: number;
      kept_turns: number;
      summary_preview: string;
    }
  | {
      type: "error";
      seq?: number;
      message: string;
      errorCode?: string;
      debug?: UpstreamDebugInfo;
      data?: SendMessageResult;
    }
  // 群组运行期间内部 Actor 回合转发的中间事件（方案 §15：现有事件附加 Actor 元数据）。
  | ({
      type: "status";
      seq?: number;
      status: string;
      message?: string;
    } & GroupStreamEventMeta)
  | ({
      type: "tool_call";
      seq?: number;
      tool_name?: string;
      tool_call_id?: string;
      arguments?: string;
    } & GroupStreamEventMeta)
  | ({
      type: "tool_result";
      seq?: number;
      tool_name?: string;
      tool_call_id?: string;
      status?: string;
      output?: string;
      error?: string;
    } & GroupStreamEventMeta)
  // Agent 群组 8 个流式事件（方案 §15.1-§15.2）。
  | ({
      type:
        | "group_step_started"
        | "group_step_output_delta"
        | "group_step_completed"
        | "group_step_failed"
        | "group_step_retry_started"
        | "group_run_paused"
        | "group_run_completed"
        | "group_run_abandoned";
      seq?: number;
      delta?: string;
      outputMarkdown?: string;
      errorCode?: string;
      message?: string;
      answer?: string;
      stepCount?: number;
    } & GroupStreamEventMeta);

export type GroupStreamEventType =
  | "group_step_started"
  | "group_step_output_delta"
  | "group_step_completed"
  | "group_step_failed"
  | "group_step_retry_started"
  | "group_run_paused"
  | "group_run_completed"
  | "group_run_abandoned";

export type GroupStreamEvent = Extract<StreamMessageEvent, { type: GroupStreamEventType }>;
