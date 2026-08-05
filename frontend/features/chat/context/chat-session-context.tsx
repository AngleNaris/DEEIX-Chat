"use client";

import * as React from "react";

type ChatSessionContextValue = {
  newConversationRevision: number;
  newConversationProjectID: string;
  newConversationRoleID: string;
  newConversationAgentGroupID: string;
  requestNewConversation: (options?: { projectID?: string; roleID?: string; agentGroupID?: string }) => void;
};

const ChatSessionContext = React.createContext<ChatSessionContextValue | null>(null);

export function ChatSessionProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = React.useState({ revision: 0, projectID: "", roleID: "", agentGroupID: "" });
  const requestNewConversation = React.useCallback(
    (options?: { projectID?: string; roleID?: string; agentGroupID?: string }) => {
      setState((prev) => ({
        revision: prev.revision + 1,
        projectID: options?.projectID?.trim() ?? "",
        roleID: options?.roleID?.trim() ?? "",
        agentGroupID: options?.agentGroupID?.trim() ?? "",
      }));
    },
    [],
  );

  const value = React.useMemo(
    () => ({
      newConversationRevision: state.revision,
      newConversationProjectID: state.projectID,
      newConversationRoleID: state.roleID,
      newConversationAgentGroupID: state.agentGroupID,
      requestNewConversation,
    }),
    [requestNewConversation, state.agentGroupID, state.projectID, state.revision, state.roleID],
  );

  return <ChatSessionContext.Provider value={value}>{children}</ChatSessionContext.Provider>;
}

export function useChatSession() {
  const context = React.useContext(ChatSessionContext);
  if (!context) {
    throw new Error("useChatSession must be used within ChatSessionProvider");
  }
  return context;
}
