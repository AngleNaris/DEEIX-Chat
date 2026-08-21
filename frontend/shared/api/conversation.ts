import type {
  ConversationRuns,
  MessageProcessTraceResponse,
  MessageTraceBlockResponse,
  MessageTraceEventResponse,
} from "@deeix/api-contract";
import { authedFetch, authedRequest } from "@/shared/api/authed-client";
import {
  DEFAULT_CONVERSATION_STREAM_IDLE_TIMEOUT_MS,
  readRecoverableSequencedJSONStream,
  readSequencedJSONStream,
} from "@/shared/api/conversation-stream-reader";
import { apiRequest, ApiError, ApiNetworkError, pathParam } from "@/shared/api/http-client";
import type { PagePayload } from "@/shared/api/common.types";
import type {
  BatchSetConversationProjectRequest,
  BatchSetConversationProjectResult,
  ContextArtifactDTO,
  ConversationDefaultModelCandidateDTO,
  ConversationDTO,
  ConversationExportDTO,
  ConversationPreviewMessageDTO,
  ConversationProjectDTO,
  ConversationProjectFilter,
  ConversationProjectStatusFilter,
  ConversationRunDTO,
  ConversationSearchPageDTO,
  ConversationShareDTO,
  ConversationShareFilter,
  ConversationStarredFilter,
  ConversationStatusFilter,
  CreateConversationProjectRequest,
  CreateConversationRequest,
  CreateConversationShareRequest,
  DeleteConversationData,
  MediaImageRequest,
  MediaVideoExtensionRequest,
  MediaVideoRequest,
  MessageDTO,
  MessageFeedbackResult,
  MessageProcessTraceDTO,
  PublicSharedConversationDTO,
  RenameConversationRequest,
  ReorderConversationProjectsRequest,
  RevokeConversationSharesRequest,
  RevokeConversationSharesResult,
  SendMessageRequest,
  SendMessageResult,
  SetConversationArchiveRequest,
  SetConversationProjectRequest,
  SetConversationStarRequest,
  SetMessageFeedbackRequest,
  StreamMessageEvent,
  GroupStreamEvent,
  TraceBlockDTO,
  UpdateConversationLabelsRequest,
  UpdateConversationProjectRequest,
  UpdateMessageRequest,
} from "@/shared/api/conversation.types";

type RawTraceBlock = MessageTraceBlockResponse;

type RawProcessTrace = Omit<
  MessageProcessTraceResponse,
  "events" | "process" | "promptTrace" | "tools" | "upstreamThink"
> & {
  process?: RawTraceBlock;
  tools?: RawTraceBlock;
  upstreamThink?: RawTraceBlock;
  promptTrace?: MessageProcessTraceDTO["promptTrace"];
  events?: RawTraceEvent[];
};

type RawTraceEvent = MessageTraceEventResponse;

function normalizeTraceBlock(block: unknown): TraceBlockDTO | undefined {
  if (!block || typeof block !== "object") {
    return undefined;
  }
  const raw = block as RawTraceBlock;
  return {
    title: raw.title ?? "",
    summary: raw.summary ?? "",
    contentMarkdown: raw.contentMarkdown ?? "",
    status: raw.status ?? "",
    stage: raw.stage,
    roundID: raw.roundID,
    parentEventID: raw.parentEventID,
    updatedAt: raw.updatedAt ?? "",
    payloadJSON: raw.payloadJSON,
  };
}

function normalizeTraceEvent(event: unknown) {
  if (!event || typeof event !== "object") {
    return undefined;
  }
  const raw = event as RawTraceEvent;
  return {
    eventID: raw.eventID ?? "",
    eventType: raw.eventType ?? "",
    phase: raw.phase ?? "",
    stage: raw.stage,
    roundID: raw.roundID,
    parentEventID: raw.parentEventID,
    title: raw.title ?? "",
    summary: raw.summary ?? "",
    contentMarkdown: raw.contentMarkdown ?? "",
    status: raw.status ?? "",
    seq: raw.seq ?? 0,
    startedAt: raw.startedAt ?? "",
    endedAt: raw.endedAt,
    updatedAt: raw.updatedAt ?? "",
    payloadJSON: raw.payloadJSON,
  };
}

function normalizeProcessTrace(trace: unknown): MessageProcessTraceDTO | undefined {
  if (!trace || typeof trace !== "object") {
    return undefined;
  }
  const raw = trace as RawProcessTrace;
  return {
    enabled: Boolean(raw.enabled),
    status: raw.status ?? "",
    process: normalizeTraceBlock(raw.process),
    tools: normalizeTraceBlock(raw.tools),
    upstreamThink: normalizeTraceBlock(raw.upstreamThink),
    promptTrace: raw.promptTrace,
    events: Array.isArray(raw.events) ? raw.events.map(normalizeTraceEvent).filter((event): event is NonNullable<ReturnType<typeof normalizeTraceEvent>> => Boolean(event)) : undefined,
  };
}

function normalizeStreamEvent(rawEvent: unknown): StreamMessageEvent {
  if (!rawEvent || typeof rawEvent !== "object") {
    throw new ApiError("stream event is invalid", 500);
  }

  const event = rawEvent as StreamMessageEvent & {
    block?: unknown;
    trace?: unknown;
  };

  if (event.type === "process_update" || event.type === "upstream_think_delta") {
    return {
      ...event,
      block: normalizeTraceBlock(event.block),
      trace: normalizeProcessTrace(event.trace),
    };
  }

  return event;
}

const GROUP_STREAM_EVENT_TYPES: ReadonlySet<string> = new Set([
  "group_step_started",
  "group_step_output_delta",
  "group_step_completed",
  "group_step_failed",
  "group_step_retry_started",
  "group_run_paused",
  "group_run_completed",
  "group_run_abandoned",
]);

export function isGroupStreamEvent(event: StreamMessageEvent): event is GroupStreamEvent {
  return GROUP_STREAM_EVENT_TYPES.has(event.type);
}

// isGroupStreamAwareEvent 判断事件是否携带群组 Actor 元数据（§15：现有事件附加可选字段）。
export function isGroupStreamAwareEvent(event: StreamMessageEvent): boolean {
  return "groupRunID" in event && typeof event.groupRunID === "string" && event.groupRunID.trim() !== "";
}

function handleStreamEvent(event: StreamMessageEvent, options: ConversationStreamOptions, responseStatus: number): SendMessageResult | null {
  if (event.type === "file_proc") {
    options.onFileProc?.(event.message);
    return null;
  }

  if (event.type === "rag_search") {
    options.onRagSearch?.(event.message);
    return null;
  }

  if (event.type === "compact_done") {
    options.onCompactDone?.({
      method: event.method,
      freed_tokens: event.freed_tokens,
      kept_turns: event.kept_turns,
      summary_preview: event.summary_preview,
    });
    return null;
  }

  if (event.type === "process_update") {
    options.onProcessUpdate?.(event);
    return null;
  }

  if (event.type === "upstream_think_delta") {
    options.onUpstreamThinkDelta?.(event);
    return null;
  }

  // 群组运行期间内部 Actor 回合转发的中间状态（方案 §15）：
  // rag_search/file_proc 复用普通语义；其余状态事件透传给 onStatus。
  if (event.type === "status") {
    const message = event.message?.trim() || "";
    if (event.status === "rag_search") {
      options.onRagSearch?.(message);
    } else if (event.status === "file_proc") {
      options.onFileProc?.(message);
    } else {
      options.onStatus?.(event);
    }
    return null;
  }

  if (event.type === "tool_call" || event.type === "tool_result") {
    options.onToolEvent?.(event);
    return null;
  }

  if (isGroupStreamEvent(event)) {
    options.onGroupEvent?.(event);
    return null;
  }

  if (event.type === "delta") {
    if (event.replace) {
      options.onTextSnapshot?.(event.delta);
    } else {
      options.onDelta?.(event.delta);
    }
    return null;
  }

  if (event.type === "usage") {
    options.onUsage?.(event);
    return null;
  }

  if (event.type === "media_status") {
    options.onMediaStatus?.(event);
    return null;
  }

  if (event.type === "media_image_delta") {
    options.onMediaImageDelta?.(event);
    return null;
  }

  if (event.type === "moderation_checking") {
    options.onModerationChecking?.(event);
    return null;
  }

  if (event.type === "moderation_blocked") {
    options.onModerationBlocked?.(event);
    throw new ApiError(
      "content blocked by moderation",
      responseStatus,
      {
        eventID: event.eventID,
        direction: event.direction,
        categories: event.categories,
      },
      "content_moderation.blocked",
    );
  }

  if (event.type === "completed") {
    return event.data;
  }

  if (event.type === "error" && event.data) {
    options.onInterrupted?.(event);
    return event.data;
  }

  throw new ApiError(event.message || "stream failed", responseStatus, event.debug, event.errorCode);
}

type ListConversationsOptions = {
  page?: number;
  pageSize?: number;
  status?: ConversationStatusFilter;
  starred?: ConversationStarredFilter;
  share?: ConversationShareFilter;
  project?: ConversationProjectFilter;
  query?: string;
};

type SearchConversationsOptions = {
  page?: number;
  pageSize?: number;
  query?: string;
  signal?: AbortSignal;
};

type ListConversationProjectsOptions = {
  status?: ConversationProjectStatusFilter;
};

type DeleteConversationProjectOptions = {
  deleteConversations?: boolean;
  deleteFiles?: boolean;
};

type DeleteConversationOptions = {
  deleteFiles?: boolean;
};

type ListConversationRunsOptions = {
  page?: number;
  pageSize?: number;
};

// Conversation metadata
export async function listConversations(
  accessToken: string,
  options: ListConversationsOptions = {},
): Promise<PagePayload<ConversationDTO>> {
  const page = options.page && options.page > 0 ? options.page : 1;
  const pageSize = options.pageSize && options.pageSize > 0 ? options.pageSize : 20;
  const status = options.status?.trim() || "active";
  const starred = options.starred?.trim() || "all";
  const share = options.share?.trim() || "all";
  const project = options.project?.trim() || "all";
  const query = options.query?.trim() || "";
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
    status,
    starred,
    share,
    project,
  });
  if (query) {
    params.set("q", query);
  }
  const data = await authedRequest<PagePayload<ConversationDTO>>(
    `/api/v1/conversations?${params.toString()}`,
    {
      accessToken,
    },
    true,
  );
  return {
    total: data.total ?? 0,
    results: data.results ?? [],
  };
}

export async function searchConversations(
  accessToken: string,
  options: SearchConversationsOptions = {},
): Promise<ConversationSearchPageDTO> {
  const page = options.page && options.page > 0 ? options.page : 1;
  const pageSize = options.pageSize && options.pageSize > 0 ? options.pageSize : 20;
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  });
  const query = options.query?.trim() || "";
  if (query) {
    params.set("q", query);
  }
  const data = await authedRequest<ConversationSearchPageDTO>(
    `/api/v1/conversations/search?${params.toString()}`,
    { accessToken, signal: options.signal },
    true,
  );
  return {
    hasMore: data.hasMore ?? false,
    results: data.results ?? [],
  };
}

export async function getConversationPreviewMessages(
  accessToken: string,
  conversationPublicID: string,
  signal?: AbortSignal,
): Promise<ConversationPreviewMessageDTO[]> {
  return authedRequest<ConversationPreviewMessageDTO[]>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/messages/preview`,
    { accessToken, signal },
    true,
  );
}

export async function getConversationDefaultModelCandidate(
  accessToken: string,
): Promise<ConversationDefaultModelCandidateDTO> {
  return authedRequest<ConversationDefaultModelCandidateDTO>(
    "/api/v1/conversations/default-model-candidate",
    {
      accessToken,
    },
    true,
  );
}

export async function listConversationProjects(
  accessToken: string,
  options: ListConversationProjectsOptions = {},
): Promise<ConversationProjectDTO[]> {
  const status = options.status?.trim() || "active";
  return authedRequest<ConversationProjectDTO[]>(
    `/api/v1/conversation-projects?status=${encodeURIComponent(status)}`,
    {
      accessToken,
    },
    true,
  );
}

export async function createConversationProject(
  accessToken: string,
  payload: CreateConversationProjectRequest,
): Promise<ConversationProjectDTO> {
  return authedRequest<ConversationProjectDTO>(
    "/api/v1/conversation-projects",
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function updateConversationProject(
  accessToken: string,
  projectPublicID: string,
  payload: UpdateConversationProjectRequest,
): Promise<ConversationProjectDTO> {
  return authedRequest<ConversationProjectDTO>(
    `/api/v1/conversation-projects/${pathParam(projectPublicID)}`,
    {
      method: "PATCH",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function deleteConversationProject(
  accessToken: string,
  projectPublicID: string,
  options: DeleteConversationProjectOptions = {},
): Promise<DeleteConversationData> {
  const params = new URLSearchParams();
  if (options.deleteConversations) {
    params.set("delete_conversations", "true");
  }
  if (options.deleteFiles) {
    params.set("delete_files", "true");
  }
  const query = params.toString();
  return authedRequest<DeleteConversationData>(
    `/api/v1/conversation-projects/${pathParam(projectPublicID)}${query ? `?${query}` : ""}`,
    {
      method: "DELETE",
      accessToken,
    },
    true,
  );
}

export async function reorderConversationProjects(
  accessToken: string,
  payload: ReorderConversationProjectsRequest,
): Promise<ConversationProjectDTO[]> {
  return authedRequest<ConversationProjectDTO[]>(
    "/api/v1/conversation-projects/reorder",
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function setConversationProject(
  accessToken: string,
  conversationPublicID: string,
  payload: SetConversationProjectRequest,
): Promise<ConversationDTO> {
  return authedRequest<ConversationDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/project`,
    {
      method: "PATCH",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function batchSetConversationProject(
  accessToken: string,
  payload: BatchSetConversationProjectRequest,
): Promise<BatchSetConversationProjectResult> {
  return authedRequest<BatchSetConversationProjectResult>(
    "/api/v1/conversations/project",
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function createConversation(
  accessToken: string,
  payload: CreateConversationRequest,
): Promise<ConversationDTO> {
  return authedRequest<ConversationDTO>(
    "/api/v1/conversations",
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function getConversation(
  accessToken: string,
  conversationPublicID: string,
): Promise<ConversationDTO> {
  return authedRequest<ConversationDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}`,
    {
      accessToken,
    },
    true,
  );
}

export async function exportConversation(
  accessToken: string,
  conversationPublicID: string,
): Promise<ConversationExportDTO> {
  return authedRequest<ConversationExportDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/export`,
    {
      accessToken,
    },
    true,
  );
}

export async function exportAllConversations(accessToken: string): Promise<Blob> {
  const response = await authedFetch("/api/v1/conversations/export", { accessToken });
  if (!response.ok) {
    throw new Error(`export failed: ${response.status}`);
  }
  return response.blob();
}

export async function renameConversation(
  accessToken: string,
  conversationPublicID: string,
  payload: RenameConversationRequest,
): Promise<ConversationDTO> {
  return authedRequest<ConversationDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/title`,
    {
      method: "PATCH",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function updateConversationLabels(
  accessToken: string,
  conversationPublicID: string,
  payload: UpdateConversationLabelsRequest,
): Promise<ConversationDTO> {
  return authedRequest<ConversationDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/labels`,
    {
      method: "PATCH",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function regenerateConversationTitle(
  accessToken: string,
  conversationPublicID: string,
): Promise<ConversationDTO> {
  return authedRequest<ConversationDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/title/regenerate`,
    {
      method: "POST",
      accessToken,
    },
    true,
  );
}

export async function setConversationStar(
  accessToken: string,
  conversationPublicID: string,
  payload: SetConversationStarRequest,
): Promise<ConversationDTO> {
  return authedRequest<ConversationDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/star`,
    {
      method: "PATCH",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function setConversationArchive(
  accessToken: string,
  conversationPublicID: string,
  payload: SetConversationArchiveRequest,
): Promise<ConversationDTO> {
  return authedRequest<ConversationDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/archive`,
    {
      method: "PATCH",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function deleteConversation(
  accessToken: string,
  conversationPublicID: string,
  options: DeleteConversationOptions = {},
): Promise<DeleteConversationData> {
  const params = new URLSearchParams();
  if (options.deleteFiles) {
    params.set("delete_files", "true");
  }
  const query = params.toString();
  return authedRequest<DeleteConversationData>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}${query ? `?${query}` : ""}`,
    {
      method: "DELETE",
      accessToken,
    },
    true,
  );
}

export async function getConversationShare(
  accessToken: string,
  conversationPublicID: string,
): Promise<ConversationShareDTO> {
  return authedRequest<ConversationShareDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/share`,
    {
      accessToken,
    },
    true,
  );
}

export async function createConversationShare(
  accessToken: string,
  conversationPublicID: string,
  payload: CreateConversationShareRequest = {},
): Promise<ConversationShareDTO> {
  return authedRequest<ConversationShareDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/share`,
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function regenerateConversationShare(
  accessToken: string,
  conversationPublicID: string,
  payload: CreateConversationShareRequest = {},
): Promise<ConversationShareDTO> {
  return authedRequest<ConversationShareDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/share/regenerate`,
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function revokeConversationShare(
  accessToken: string,
  conversationPublicID: string,
): Promise<ConversationShareDTO> {
  return authedRequest<ConversationShareDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/share`,
    {
      method: "DELETE",
      accessToken,
    },
    true,
  );
}

export async function revokeConversationShares(
  accessToken: string,
  payload: RevokeConversationSharesRequest,
): Promise<RevokeConversationSharesResult> {
  return authedRequest<RevokeConversationSharesResult>(
    "/api/v1/conversations/shares/revoke",
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function getSharedConversation(shareID: string): Promise<PublicSharedConversationDTO> {
  return apiRequest<PublicSharedConversationDTO>(
    `/api/v1/shared-conversations/${pathParam(shareID)}`,
  );
}

export async function cloneSharedConversation(
  accessToken: string,
  shareID: string,
): Promise<ConversationDTO> {
  return authedRequest<ConversationDTO>(
    `/api/v1/shared-conversations/${pathParam(shareID)}/clone`,
    {
      method: "POST",
      accessToken,
    },
    true,
  );
}

export async function listConversationRuns(
  accessToken: string,
  conversationPublicID: string,
  options: ListConversationRunsOptions = {},
): Promise<PagePayload<ConversationRunDTO>> {
  const page = options.page && options.page > 0 ? options.page : 1;
  const pageSize = options.pageSize && options.pageSize > 0 ? options.pageSize : 20;
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  });
  const data = await authedRequest<PagePayload<ConversationRunDTO>>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/runs?${params.toString()}`,
    {
      accessToken,
    },
    true,
  );
  return {
    total: data.total ?? 0,
    results: data.results ?? [],
  };
}

export async function getContextArtifact(
  accessToken: string,
  artifactID: number,
): Promise<ContextArtifactDTO> {
  return authedRequest<ContextArtifactDTO>(
    `/api/v1/context-artifacts/${pathParam(artifactID)}`,
    {
      accessToken,
    },
    true,
  );
}

// Messages
type ListMessagesOptions = {
  page?: number;
  pageSize?: number;
  tail?: boolean;
  beforeID?: number;
};

export async function listMessagesPage(
  accessToken: string,
  conversationPublicID: string,
  options: ListMessagesOptions = {},
): Promise<PagePayload<MessageDTO>> {
  const page = options.page && options.page > 0 ? options.page : 1;
  const pageSize = options.pageSize && options.pageSize > 0 ? options.pageSize : 100;
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  });
  if (options.tail) {
    params.set("tail", "true");
  }
  if (options.beforeID && options.beforeID > 0) {
    params.set("before_id", String(options.beforeID));
  }
  const data = await authedRequest<PagePayload<MessageDTO>>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/messages?${params.toString()}`,
    {
      accessToken,
    },
    true,
  );
  return {
    total: data.total ?? 0,
    results: data.results ?? [],
  };
}

export async function listMessages(
  accessToken: string,
  conversationPublicID: string,
  page = 1,
  pageSize = 100,
): Promise<MessageDTO[]> {
  const data = await listMessagesPage(accessToken, conversationPublicID, {
    page,
    pageSize,
    tail: page === 1,
  });
  return data.results;
}

export async function sendMessage(
  accessToken: string,
  conversationPublicID: string,
  payload: SendMessageRequest,
): Promise<SendMessageResult> {
  return authedRequest<SendMessageResult>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/messages`,
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function cancelMessageGeneration(
  accessToken: string,
  runID: string,
): Promise<{ canceled: boolean }> {
  return authedRequest<{ canceled: boolean }>(
    `/api/v1/conversation-runs/${pathParam(runID)}/cancel`,
    {
      method: "POST",
      accessToken,
    },
    true,
  );
}

export async function resumeMessageGenerationStream(
  accessToken: string,
  runID: string,
  options: ConversationStreamOptions = {},
): Promise<SendMessageResult | null> {
  const afterSeq = options.afterSeq && options.afterSeq > 0 ? Math.floor(options.afterSeq) : 0;
  const response = await openMessageGenerationResumeResponse(
    accessToken,
    runID,
    afterSeq,
    options.signal,
  );

  if (!response.body) {
    return null;
  }

  return readRecoverableConversationStream(response, accessToken, runID, options);
}

export async function setMessageFeedback(
  accessToken: string,
  messagePublicID: string,
  payload: SetMessageFeedbackRequest,
): Promise<MessageFeedbackResult> {
  return authedRequest<MessageFeedbackResult>(
    `/api/v1/messages/${pathParam(messagePublicID)}/feedback`,
    {
      method: "PUT",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function updateMessage(
  accessToken: string,
  messagePublicID: string,
  payload: UpdateMessageRequest,
): Promise<MessageDTO> {
  return authedRequest<MessageDTO>(
    `/api/v1/messages/${pathParam(messagePublicID)}`,
    {
      method: "PATCH",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function forkConversationFromMessage(
  accessToken: string,
  conversationPublicID: string,
  messagePublicID: string,
): Promise<ConversationDTO> {
  return authedRequest<ConversationDTO>(
    `/api/v1/conversations/${pathParam(conversationPublicID)}/messages/${pathParam(messagePublicID)}/fork`,
    {
      method: "POST",
      accessToken,
    },
    true,
  );
}

export type CompactDoneEvent = {
  method: string;
  freed_tokens: number;
  kept_turns: number;
  summary_preview: string;
};

export type ConversationStreamOptions = {
  signal?: AbortSignal;
  afterSeq?: number;
  onEventSeq?: (seq: number) => void;
  onDelta?: (delta: string) => void;
  onTextSnapshot?: (content: string) => void;
  onFileProc?: (message: string) => void;
  onRagSearch?: (message: string) => void;
  onMediaStatus?: (event: Extract<StreamMessageEvent, { type: "media_status" }>) => void;
  onMediaImageDelta?: (event: Extract<StreamMessageEvent, { type: "media_image_delta" }>) => void;
  onCompactDone?: (event: CompactDoneEvent) => void;
  onProcessUpdate?: (event: Extract<StreamMessageEvent, { type: "process_update" }>) => void;
  onUpstreamThinkDelta?: (event: Extract<StreamMessageEvent, { type: "upstream_think_delta" }>) => void;
  onStatus?: (event: Extract<StreamMessageEvent, { type: "status" }>) => void;
  onToolEvent?: (event: Extract<StreamMessageEvent, { type: "tool_call" | "tool_result" }>) => void;
  onGroupEvent?: (event: GroupStreamEvent) => void;
  onUsage?: (event: Extract<StreamMessageEvent, { type: "usage" }>) => void;
  onInterrupted?: (event: Extract<StreamMessageEvent, { type: "error" }>) => void;
  onModerationChecking?: (event: Extract<StreamMessageEvent, { type: "moderation_checking" }>) => void;
  onModerationBlocked?: (event: Extract<StreamMessageEvent, { type: "moderation_blocked" }>) => void;
};

// readConversationStream 消费 NDJSON 流：群组重试端点复用同一协议
// （completed → 返回 data；error+data → onInterrupted 后返回 data；error 无 data → 抛 ApiError）。
export async function readConversationStream(
  response: Response,
  options: ConversationStreamOptions,
): Promise<SendMessageResult | null> {
  const { result } = await readSequencedJSONStream(response, {
    signal: options.signal,
    afterSeq: options.afterSeq,
    parseEvent: (source) => normalizeStreamEvent(JSON.parse(source)),
    getEventSeq: (event) => event.seq,
    handleEvent: (event, responseStatus) => handleStreamEvent(event, options, responseStatus),
    onEventSeq: options.onEventSeq,
  });
  return result;
}

function shouldRetryGenerationStreamOpen(error: unknown): boolean {
  return error instanceof ApiNetworkError ||
    (error instanceof ApiError && (error.status === 408 || error.status === 429 || error.status >= 500));
}

async function openMessageGenerationResumeResponse(
  accessToken: string,
  runID: string,
  afterSeq: number,
  signal?: AbortSignal,
): Promise<Response> {
  const requestQuery = {
    snapshot: true,
    ...(afterSeq > 0 ? { after: Math.floor(afterSeq) } : {}),
  } satisfies ConversationRuns.StreamList.RequestQuery;
  const query = new URLSearchParams({ snapshot: String(requestQuery.snapshot) });
  if (requestQuery.after !== undefined) {
    query.set("after", String(requestQuery.after));
  }
  return authedFetch(
    `/api/v1/conversation-runs/${pathParam(runID)}/stream?${query.toString()}`,
    {
      method: "GET",
      accessToken,
      signal,
    },
    true,
  );
}

async function readRecoverableConversationStream(
  initialResponse: Response,
  accessToken: string,
  runID: string,
  options: ConversationStreamOptions,
): Promise<SendMessageResult> {
  return readRecoverableSequencedJSONStream({
    initialResponse,
    signal: options.signal,
    afterSeq: options.afterSeq,
    idleTimeoutMS: DEFAULT_CONVERSATION_STREAM_IDLE_TIMEOUT_MS,
    parseEvent: (source) => normalizeStreamEvent(JSON.parse(source)),
    getEventSeq: (event) => event.seq,
    handleEvent: (event, responseStatus) => handleStreamEvent(event, options, responseStatus),
    onEventSeq: options.onEventSeq,
    openRecovery: (afterSeq, signal) =>
      openMessageGenerationResumeResponse(accessToken, runID, afterSeq, signal),
    shouldRetryOpenError: shouldRetryGenerationStreamOpen,
  });
}

function payloadRunID(payload: unknown): string {
  if (!payload || typeof payload !== "object" || !("clientRunID" in payload)) {
    return "";
  }
  const value = (payload as { clientRunID?: unknown }).clientRunID;
  return typeof value === "string" ? value.trim() : "";
}

async function postConversationStream<TPayload>(
  accessToken: string,
  conversationPublicID: string,
  endpointSuffix: string,
  payload: TPayload,
  options: ConversationStreamOptions,
): Promise<SendMessageResult> {
  const response = await authedFetch(
    `/api/v1/conversations/${pathParam(conversationPublicID)}${endpointSuffix}`,
    {
      method: "POST",
      accessToken,
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(payload),
      signal: options.signal,
    },
    true,
  );

  if (!response.body) {
    throw new ApiError("stream body is empty", response.status);
  }

  const runID = payloadRunID(payload);
  const completed = runID
    ? await readRecoverableConversationStream(response, accessToken, runID, options)
    : await readConversationStream(response, options);
  if (completed) {
    return completed;
  }
  throw new ApiError("stream completed without final payload", response.status);
}

export async function streamMessage(
  accessToken: string,
  conversationPublicID: string,
  payload: SendMessageRequest,
  options: ConversationStreamOptions = {},
): Promise<SendMessageResult> {
  return postConversationStream(accessToken, conversationPublicID, "/messages/stream", payload, options);
}

export async function streamImageGeneration(
  accessToken: string,
  conversationPublicID: string,
  payload: MediaImageRequest,
  options: ConversationStreamOptions = {},
): Promise<SendMessageResult> {
  return postConversationStream(
    accessToken,
    conversationPublicID,
    "/media/images/generations/stream",
    payload,
    options,
  );
}

export async function streamImageEdit(
  accessToken: string,
  conversationPublicID: string,
  payload: MediaImageRequest,
  options: ConversationStreamOptions = {},
): Promise<SendMessageResult> {
  return postConversationStream(
    accessToken,
    conversationPublicID,
    "/media/images/edits/stream",
    payload,
    options,
  );
}

export async function streamVideoGeneration(
  accessToken: string,
  conversationPublicID: string,
  payload: MediaVideoRequest,
  options: ConversationStreamOptions = {},
): Promise<SendMessageResult> {
  return postConversationStream(
    accessToken,
    conversationPublicID,
    "/media/videos/generations/stream",
    payload,
    options,
  );
}

export async function streamVideoExtension(
  accessToken: string,
  conversationPublicID: string,
  payload: MediaVideoExtensionRequest,
  options: ConversationStreamOptions = {},
): Promise<SendMessageResult> {
  return postConversationStream(
    accessToken,
    conversationPublicID,
    "/media/videos/extensions/stream",
    payload,
    options,
  );
}
