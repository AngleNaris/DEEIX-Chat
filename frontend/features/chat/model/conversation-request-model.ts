type ConversationAgentGroupTarget = {
  agentGroupID?: string | null;
} | null | undefined;

export type ConversationSubmissionModels = {
  optimisticMessageModel: string;
  createConversationModel: string;
  streamRequestModel: string;
};

export function resolveAgentGroupSubmissionTarget(
  conversation: ConversationAgentGroupTarget,
  fallbackIsAgentGroupConversation: boolean,
): boolean {
  return conversation
    ? Boolean(conversation.agentGroupID?.trim())
    : fallbackIsAgentGroupConversation;
}

export function resolveConversationRequestModel(
  platformModelName: string,
  isAgentGroupConversation: boolean,
): string {
  return isAgentGroupConversation ? "" : platformModelName.trim();
}

export function resolveConversationSubmissionModels(
  platformModelName: string,
  isAgentGroupConversation: boolean,
): ConversationSubmissionModels {
  const requestModel = resolveConversationRequestModel(
    platformModelName,
    isAgentGroupConversation,
  );
  return {
    optimisticMessageModel: requestModel,
    createConversationModel: requestModel,
    streamRequestModel: requestModel,
  };
}
