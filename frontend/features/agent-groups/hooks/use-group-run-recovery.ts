"use client";

import * as React from "react";

import {
  shouldStartGroupRunDetailRecovery,
} from "@/features/agent-groups/model/group-run-recovery";
import {
  importGroupRunDetail,
  isGroupRunResumeActive,
  useLiveGroupRun,
} from "@/features/agent-groups/model/group-run-store";
import { getAgentGroupRunDetailByClientRunID } from "@/shared/api/agent-groups";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";

// useGroupRunRecovery 刷新恢复（§16.7：刷新页面后恢复相同运行时间线）。
// 群组会话加载后，若最后一条 assistant 消息的运行不在实时 store 中（页面刷新导致
// 内存状态丢失），通过 lookup 端点重建时间线，使暂停/阻塞步骤恢复“重试 / 放弃”操作。
// 实时 store 已有运行（本会话内的事件流）时跳过，避免用无思考内容的 DTO 覆盖在途/终态时间线。
// 通过 useLiveGroupRun 订阅 store：消息组件重挂载清空运行后（卸载清理）liveRun 变为
// undefined，依赖变化自动重新 lookup，保证刷新/重挂载后重试入口始终可用（§16.10）。
export function useGroupRunRecovery(options: {
  conversationPublicID?: string;
  isGroupConversation: boolean;
  lastAssistantRunID?: string;
  resumeActive?: boolean;
}) {
  const { conversationPublicID, isGroupConversation, lastAssistantRunID, resumeActive = false } = options;
  const runID = lastAssistantRunID?.trim() || "";
  const liveRun = useLiveGroupRun(runID);
  const [recoveredRunID, setRecoveredRunID] = React.useState<string | null>(null);

  React.useEffect(() => {
    if (!isGroupConversation || !runID || !conversationPublicID?.trim()) {
      return;
    }
    if (!shouldStartGroupRunDetailRecovery(liveRun) || resumeActive || isGroupRunResumeActive(runID)) {
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
  }, [conversationPublicID, isGroupConversation, resumeActive, runID, liveRun]);

  return { recoveredRunID };
}
