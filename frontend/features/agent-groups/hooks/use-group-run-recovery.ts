"use client";

import * as React from "react";

import {
  importGroupRunDetail,
  readLiveGroupRun,
} from "@/features/agent-groups/model/group-run-store";
import { getAgentGroupRunDetailByClientRunID } from "@/shared/api/agent-groups";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";

// useGroupRunRecovery 刷新恢复（§16.7：刷新页面后恢复相同运行时间线）。
// 群组会话加载后，若最后一条 assistant 消息的运行不在实时 store 中（页面刷新导致
// 内存状态丢失），通过 lookup 端点重建时间线，使暂停/阻塞步骤恢复“重试 / 放弃”操作。
// 实时 store 已有运行（本会话内的事件流）时跳过，避免用无思考内容的 DTO 覆盖在途/终态时间线。
export function useGroupRunRecovery(options: {
  conversationPublicID?: string;
  isGroupConversation: boolean;
  lastAssistantRunID?: string;
}) {
  const { conversationPublicID, isGroupConversation, lastAssistantRunID } = options;
  const [recoveredRunID, setRecoveredRunID] = React.useState<string | null>(null);

  React.useEffect(() => {
    const runID = lastAssistantRunID?.trim();
    if (!isGroupConversation || !runID || !conversationPublicID?.trim()) {
      return;
    }
    if (readLiveGroupRun(runID)) {
      return;
    }
    let cancelled = false;
    void (async () => {
      try {
        const accessToken = await resolveAccessToken();
        const detail = await getAgentGroupRunDetailByClientRunID(
          accessToken,
          conversationPublicID,
          runID,
        );
        if (cancelled) {
          return;
        }
        importGroupRunDetail(runID, detail);
        setRecoveredRunID(runID);
      } catch {
        // 404（无该运行）或网络失败：静默，时间线保持为空。
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [conversationPublicID, isGroupConversation, lastAssistantRunID]);

  return { recoveredRunID };
}
