"use client";

import * as React from "react";

import type { GroupRunState, GroupRunStepState } from "@/features/agent-groups/model/group-run-store";
import {
  abortGroupRunRetry,
  notifyGroupRunSettled,
  registerGroupRunRetryAbort,
  setGroupRunRetrying,
  synthesizeGroupRunPausedState,
  upsertGroupRunEvent,
} from "@/features/agent-groups/model/group-run-store";
import {
  abandonAgentGroupRun,
  retryAgentGroupRunStep,
} from "@/shared/api/agent-groups";
import type { GroupStreamEvent } from "@/shared/api/conversation.types";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";

// useAgentGroupStepActions 承载失败步骤的重试 / 停止 / 放弃交互（§16.10）。
// 重试可从消息 meta 按钮或失败步骤 trace 发起，retrying 标记与 AbortController
// 均写入共享 store：任一入口发起的重试，其余入口都能感知并停止。
// run/step 可空（消息 meta 入口在运行未暂停时无目标步骤）：无可重试步骤时
// handleRetry 直接返回，由调用方回退到常规重试语义。
export function useAgentGroupStepActions({
  clientRunID,
  run,
  step,
}: {
  clientRunID: string;
  run?: GroupRunState;
  step?: GroupRunStepState;
}) {
  const [abandoning, setAbandoning] = React.useState(false);
  const [actionError, setActionError] = React.useState("");
  const abortRef = React.useRef<AbortController | null>(null);
  const retryRequestIDRef = React.useRef("");
  const retrying = Boolean(run?.retrying);

  // 清理：卸载时中断在途重试流（服务端 Cancelable 断开后自动回到 paused_retryable）。
  React.useEffect(() => {
    return () => {
      abortRef.current?.abort();
      registerGroupRunRetryAbort(clientRunID, null);
    };
  }, [clientRunID]);

  const handleRetry = React.useCallback(async () => {
    if (!run || !step) {
      return;
    }
    setActionError("");
    const controller = new AbortController();
    abortRef.current = controller;
    registerGroupRunRetryAbort(clientRunID, controller);
    retryRequestIDRef.current = window.crypto.randomUUID().replaceAll("-", "");
    // §16.10 输入锁：重试进行中保持锁定，直到结算（成功/再次暂停/停止/放弃）。
    setGroupRunRetrying(clientRunID, true);
    let streamSettled = false;
    try {
      const accessToken = await resolveAccessToken();
      const result = await retryAgentGroupRunStep(
        accessToken,
        run.groupRunID,
        step.stepID,
        retryRequestIDRef.current,
        {
          signal: controller.signal,
          onGroupEvent: (event) => upsertGroupRunEvent(clientRunID, event),
          // 最终答案由服务端写入顶层消息，结算后经 reload 展示，无需本地合并。
          onDelta: () => {},
          onStreamError: () => {},
        },
      );
      streamSettled = true;
      if (result.status === "error" && result.message) {
        setActionError(result.message);
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") {
        // 停止重试：本地镜像服务端结局（Attempt 中断 → paused_retryable）。
        synthesizeGroupRunPausedState(clientRunID);
      } else {
        setActionError(error instanceof Error ? error.message : "retry failed");
      }
    } finally {
      abortRef.current = null;
      registerGroupRunRetryAbort(clientRunID, null);
      setGroupRunRetrying(clientRunID, false);
      if (streamSettled) {
        notifyGroupRunSettled(clientRunID);
      }
    }
  }, [clientRunID, run, step]);

  const handleStopRetry = React.useCallback(() => {
    abortRef.current?.abort();
    abortGroupRunRetry(clientRunID);
  }, [clientRunID]);

  const handleAbandon = React.useCallback(async () => {
    if (!run) {
      return;
    }
    setAbandoning(true);
    setActionError("");
    try {
      const accessToken = await resolveAccessToken();
      await abandonAgentGroupRun(accessToken, run.groupRunID);
      upsertGroupRunEvent(clientRunID, {
        type: "group_run_abandoned",
        groupRunID: run.groupRunID,
      } satisfies GroupStreamEvent);
    } catch (error) {
      setActionError(error instanceof Error ? error.message : "abandon failed");
    } finally {
      setAbandoning(false);
      notifyGroupRunSettled(clientRunID);
    }
  }, [clientRunID, run]);

  return {
    retrying,
    abandoning,
    actionError,
    handleRetry,
    handleStopRetry,
    handleAbandon,
  };
}
