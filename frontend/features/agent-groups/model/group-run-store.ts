"use client";

import * as React from "react";

import { mergeUpstreamThinkBlock } from "@/features/chat/model/upstream-think-store";
import type { ChatTraceBlock } from "@/features/chat/types/messages";
import type { GroupStreamEvent, StreamMessageEvent } from "@/shared/api/conversation.types";
import type { AgentGroupRunDetailDTO } from "@/shared/api/agent-groups.types";

// Agent 群组运行实时状态（方案 §16.4-§16.8）。
// 模块级 Map 以父流的 clientRunID 为键；每个 Attempt 通过 attemptID 复合键隔离思考内容，
// 保证主管与各成员的思考/正文互不合并。事件携带单调递增 seq，刷新恢复时按序重放即可重建时间线。

type UpstreamThinkDeltaEvent = Extract<StreamMessageEvent, { type: "upstream_think_delta" }>;

export type GroupRunAttemptState = {
  attemptID: string;
  attemptNumber: number;
  status: string; // pending|running|success|error|interrupted|canceled
  think?: ChatTraceBlock;
  tools?: ChatTraceBlock;
  output: string;
  errorCode?: string;
  errorMessage?: string;
  startedAt: string;
  endedAt?: string;
  updatedAt: string;
  lastEventAt: number;
};

export type GroupRunStepState = {
  stepID: string;
  sequence: number;
  stepType: string; // supervisor_decide | member_execute
  actor: {
    memberID: string;
    name: string;
    type: string; // supervisor | worker
    icon: string;
    color: string;
    model: string;
  };
  status: string; // pending|running|interrupted|success|failed|canceled
  attempts: GroupRunAttemptState[];
  startedAt: string;
  endedAt?: string;
  updatedAt: string;
  lastEventAt: number;
};

export type GroupRunState = {
  groupRunID: string;
  status: string; // pending|running|paused_retryable|blocked|completed|abandoned
  steps: GroupRunStepState[];
  currentStepID: string | null;
  currentAttemptID: string | null;
  errorCode?: string;
  errorMessage?: string;
  // 占位符运行（§16.8）：用户在发送消息后立即看到真实阶段，事件到达前无真实 groupRunID。
  resuming?: boolean;
  // 重试流进行中（§16.10）：输入框保持锁定，直到重试结束（成功/再次暂停）或放弃。
  retrying?: boolean;
  startedAt: string;
  endedAt?: string;
  updatedAt: string;
  lastEventAt: number;
};

type GroupRunListener = () => void;

// 运行结算通知（§16.10）：重试/放弃完成后 trace 调用 notifyGroupRunSettled，
// app-chat-area 订阅后 reload 消息列表（避免穿透 ChatArea 的 React.memo comparator 传 prop）。
type SettledListener = (clientRunID: string) => void;

const runs = new Map<string, GroupRunState>();
const listeners = new Map<string, Set<GroupRunListener>>();
const settledListeners = new Set<SettledListener>();

function nowISO() {
  return new Date().toISOString();
}

function nowTimestamp() {
  return typeof performance !== "undefined" ? performance.now() : Date.now();
}

function normalizeRunID(runID: string | null | undefined) {
  return runID?.trim() || "";
}

function notify(runID: string) {
  listeners.get(runID)?.forEach((listener) => {
    listener();
  });
}

function subscribe(runID: string, listener: GroupRunListener) {
  if (!runID) {
    return () => {};
  }
  let listenersForRun = listeners.get(runID);
  if (!listenersForRun) {
    listenersForRun = new Set();
    listeners.set(runID, listenersForRun);
  }
  listenersForRun.add(listener);
  return () => {
    listenersForRun.delete(listener);
    if (listenersForRun.size === 0) {
      listeners.delete(runID);
    }
  };
}

function eventMeta(event: GroupStreamEvent) {
  return {
    groupRunID: event.groupRunID?.trim() || "",
    stepID: event.stepID?.trim() || "",
    attemptID: event.attemptID?.trim() || "",
    attemptNumber: typeof event.attemptNumber === "number" && event.attemptNumber > 0 ? event.attemptNumber : 1,
    sequence: typeof event.sequence === "number" && event.sequence > 0 ? event.sequence : 1,
    stepType: event.stepType?.trim() || "member_execute",
    actor: {
      memberID: event.actorMemberID?.trim() || "",
      name: event.actorName?.trim() || "",
      type: event.actorType?.trim() || "worker",
      icon: event.actorIcon?.trim() || "",
      color: event.actorColor?.trim() || "",
      model: event.model?.trim() || "",
    },
  };
}

function createAttempt(meta: ReturnType<typeof eventMeta>): GroupRunAttemptState {
  const timestamp = nowTimestamp();
  return {
    attemptID: meta.attemptID,
    attemptNumber: meta.attemptNumber,
    status: "running",
    output: "",
    startedAt: nowISO(),
    updatedAt: nowISO(),
    lastEventAt: timestamp,
  };
}

function createStep(meta: ReturnType<typeof eventMeta>): GroupRunStepState {
  const timestamp = nowTimestamp();
  return {
    stepID: meta.stepID,
    sequence: meta.sequence,
    stepType: meta.stepType,
    actor: meta.actor,
    status: "running",
    attempts: meta.attemptID ? [createAttempt(meta)] : [],
    startedAt: nowISO(),
    updatedAt: nowISO(),
    lastEventAt: timestamp,
  };
}

function createRun(meta: ReturnType<typeof eventMeta>): GroupRunState {
  const timestamp = nowTimestamp();
  return {
    groupRunID: meta.groupRunID,
    status: "running",
    steps: meta.stepID ? [createStep(meta)] : [],
    currentStepID: meta.stepID || null,
    currentAttemptID: meta.attemptID || null,
    startedAt: nowISO(),
    updatedAt: nowISO(),
    lastEventAt: timestamp,
  };
}

function findStep(run: GroupRunState, stepID: string) {
  if (!stepID) {
    return undefined;
  }
  return run.steps.find((step) => step.stepID === stepID);
}

function findAttempt(step: GroupRunStepState | undefined, attemptID: string) {
  if (!step || !attemptID) {
    return undefined;
  }
  return step.attempts.find((attempt) => attempt.attemptID === attemptID);
}

// mapStep 只替换目标 step，返回新 steps 数组；没有变更时返回原数组。
function mapStep(run: GroupRunState, stepID: string, update: (step: GroupRunStepState) => GroupRunStepState) {
  const target = findStep(run, stepID);
  if (!target) {
    return run.steps;
  }
  const next = update(target);
  if (next === target) {
    return run.steps;
  }
  return run.steps.map((step) => (step.stepID === stepID ? next : step));
}

function touchRun(run: GroupRunState, patch: Partial<GroupRunState>): GroupRunState {
  return {
    ...run,
    ...patch,
    updatedAt: nowISO(),
    lastEventAt: nowTimestamp(),
  };
}

function touchStep(step: GroupRunStepState, patch: Partial<GroupRunStepState>): GroupRunStepState {
  return {
    ...step,
    ...patch,
    updatedAt: nowISO(),
    lastEventAt: nowTimestamp(),
  };
}

function touchAttempt(attempt: GroupRunAttemptState, patch: Partial<GroupRunAttemptState>): GroupRunAttemptState {
  return {
    ...attempt,
    ...patch,
    updatedAt: nowISO(),
    lastEventAt: nowTimestamp(),
  };
}

// upsertGroupRunEvent 将群组流事件应用到实时运行状态（§15 协议）。
export function upsertGroupRunEvent(clientRunID: string | null | undefined, event: GroupStreamEvent) {
  const runID = normalizeRunID(clientRunID);
  if (!runID || !event.groupRunID) {
    return undefined;
  }
  const meta = eventMeta(event);
  let run = runs.get(runID) ?? createRun(meta);
  // 占位符运行（groupRunID 为空）收到第一个事件时回填真实 ID，并清除 resuming 标记。
  if (!run.groupRunID && meta.groupRunID) {
    run = touchRun(run, { groupRunID: meta.groupRunID, resuming: undefined });
  }

  switch (event.type) {
    case "group_step_started":
    case "group_step_retry_started": {
      // 新步骤或同一步骤的新 Attempt（重试）：原地追加，不复制成功步骤。
      if (!findStep(run, meta.stepID)) {
        run = {
          ...run,
          steps: [...run.steps, createStep(meta)],
          currentStepID: meta.stepID,
          currentAttemptID: meta.attemptID || null,
          status: "running",
          errorCode: undefined,
          errorMessage: undefined,
          updatedAt: nowISO(),
          lastEventAt: nowTimestamp(),
        };
      } else {
        let touched = false;
        const steps = mapStep(run, meta.stepID, (step) => {
          // 重试：同一逻辑步骤内追加新 Attempt，原失败/成功步骤就地恢复 running。
          if (step.status === "success" || step.status === "failed") {
            touched = true;
            return touchStep(step, { status: "running", endedAt: undefined });
          }
          return step;
        });
        if (!findAttempt(findStep(run, meta.stepID), meta.attemptID)) {
          const stepsWithAttempt = steps.map((step) =>
            step.stepID === meta.stepID ? { ...step, attempts: [...step.attempts, createAttempt(meta)] } : step,
          );
          run = {
            ...run,
            steps: stepsWithAttempt,
            currentStepID: meta.stepID,
            currentAttemptID: meta.attemptID || run.currentAttemptID,
            status: "running",
            updatedAt: nowISO(),
            lastEventAt: nowTimestamp(),
          };
        } else if (touched) {
          run = {
            ...run,
            steps,
            currentStepID: meta.stepID,
            currentAttemptID: meta.attemptID || run.currentAttemptID,
            status: "running",
            updatedAt: nowISO(),
            lastEventAt: nowTimestamp(),
          };
        }
      }
      break;
    }
    case "group_step_output_delta": {
      if (!findAttempt(findStep(run, meta.stepID), meta.attemptID)) {
        // 防御：delta 先于 started 到达（理论上不会），仍要避免丢弃正文。
        if (!findStep(run, meta.stepID)) {
          run = { ...run, steps: [...run.steps, createStep(meta)] };
        }
        const step = findStep(run, meta.stepID)!;
        if (!findAttempt(step, meta.attemptID)) {
          run = {
            ...run,
            steps: run.steps.map((item) =>
              item.stepID === meta.stepID ? { ...item, attempts: [...item.attempts, createAttempt(meta)] } : item,
            ),
          };
        }
      }
      const delta = typeof event.delta === "string" ? event.delta : "";
      if (delta) {
        run = {
          ...run,
          steps: mapStep(run, meta.stepID, (step) => {
            if (findAttempt(step, meta.attemptID)) {
              return {
                ...step,
                status: "running",
                attempts: step.attempts.map((attempt) =>
                  attempt.attemptID === meta.attemptID
                    ? touchAttempt(attempt, { status: "running", output: attempt.output + delta })
                    : attempt,
                ),
              };
            }
            return step;
          }),
          status: "running",
          updatedAt: nowISO(),
          lastEventAt: nowTimestamp(),
        };
      }
      break;
    }
    case "group_step_completed": {
      run = {
        ...run,
        steps: mapStep(run, meta.stepID, (step) => {
          if (!findAttempt(step, meta.attemptID)) {
            return step;
          }
          return {
            ...step,
            status: "success",
            endedAt: nowISO(),
            attempts: step.attempts.map((attempt) =>
              attempt.attemptID === meta.attemptID
                ? touchAttempt(attempt, {
                    status: "success",
                    endedAt: nowISO(),
                    output:
                      typeof event.outputMarkdown === "string" && event.outputMarkdown.trim() !== ""
                        ? event.outputMarkdown
                        : attempt.output,
                  })
                : attempt,
            ),
          };
        }),
        currentStepID: null,
        currentAttemptID: null,
        status: "running",
        updatedAt: nowISO(),
        lastEventAt: nowTimestamp(),
      };
      break;
    }
    case "group_step_failed": {
      const attemptStatus = event.status === "canceled" ? "canceled" : event.status === "interrupted" ? "interrupted" : "error";
      run = {
        ...run,
        steps: mapStep(run, meta.stepID, (step) => {
          if (!findAttempt(step, meta.attemptID)) {
            return step;
          }
          return {
            ...step,
            status: event.status || "failed",
            endedAt: nowISO(),
            attempts: step.attempts.map((attempt) =>
              attempt.attemptID === meta.attemptID
                ? touchAttempt(attempt, {
                    status: attemptStatus,
                    endedAt: nowISO(),
                    errorCode: event.errorCode,
                    errorMessage: event.message,
                  })
                : attempt,
            ),
          };
        }),
        errorCode: event.errorCode,
        errorMessage: event.message,
        updatedAt: nowISO(),
        lastEventAt: nowTimestamp(),
      };
      break;
    }
    case "group_run_paused": {
      run = touchRun(run, {
        status: event.status || "paused_retryable",
        errorCode: event.errorCode || run.errorCode,
      });
      break;
    }
    case "group_run_completed": {
      run = touchRun(run, { status: "completed", endedAt: nowISO() });
      break;
    }
    case "group_run_abandoned": {
      run = touchRun(run, { status: "abandoned", endedAt: nowISO() });
      break;
    }
  }

  runs.set(runID, run);
  notify(runID);
  return run;
}

// upsertLiveGroupRunThink 将带 Attempt 上下文的 thinking 增量写入对应 Attempt（§15：按 Attempt ID 隔离）。
export function upsertLiveGroupRunThink(
  clientRunID: string | null | undefined,
  event: UpstreamThinkDeltaEvent & { attemptID?: string; stepID?: string },
) {
  const runID = normalizeRunID(clientRunID);
  const stepID = event.stepID?.trim() || "";
  const attemptID = event.attemptID?.trim() || "";
  if (!runID || !attemptID || !stepID) {
    return undefined;
  }
  const run = runs.get(runID);
  if (!run) {
    return undefined;
  }
  const nextRun: GroupRunState = {
    ...run,
    steps: mapStep(run, stepID, (step) => {
      if (!findAttempt(step, attemptID)) {
        return step;
      }
      return {
        ...step,
        status: step.status === "success" ? step.status : "running",
        attempts: step.attempts.map((attempt) =>
          attempt.attemptID === attemptID
            ? touchAttempt(attempt, { status: "running", think: mergeUpstreamThinkBlock(attempt.think, event) })
            : attempt,
        ),
      };
    }),
    status: run.status === "completed" || run.status === "abandoned" ? run.status : "running",
    updatedAt: nowISO(),
    lastEventAt: nowTimestamp(),
  };
  runs.set(runID, nextRun);
  notify(runID);
  return nextRun;
}

// 群组转发的工具调用增量事件（§15：tool_call / tool_result 附加 stepID/attemptID/actor 元数据）。
type GroupToolCallEvent = Extract<StreamMessageEvent, { type: "tool_call" | "tool_result" }> & {
  stepID?: string;
  attemptID?: string;
};

// LiveGroupToolCall 是群组 Attempt 工具块内部的最小调用记录（仅展示所需字段，不保存敏感参数细节）。
type LiveGroupToolCall = {
  tool_call_id: string;
  name: string;
  status: string;
  input?: string;
  error?: string;
};

const ACTIVE_TOOL_STATUSES = new Set(["requested", "streaming", "queued", "in_progress", "searching"]);

function parseGroupToolCalls(block: ChatTraceBlock | undefined): LiveGroupToolCall[] {
  if (!block?.payloadJson) {
    return [];
  }
  try {
    const parsed = JSON.parse(block.payloadJson) as { tool_calls?: LiveGroupToolCall[] };
    return Array.isArray(parsed.tool_calls) ? parsed.tool_calls : [];
  } catch {
    return [];
  }
}

function buildGroupToolBlock(calls: LiveGroupToolCall[]): ChatTraceBlock {
  const hasActive = calls.some((call) => ACTIVE_TOOL_STATUSES.has(call.status?.trim() || ""));
  return {
    title: "",
    summary: "",
    contentMarkdown: "",
    status: hasActive ? "streaming" : "completed",
    payloadJson: JSON.stringify({ tool_calls: calls }),
  };
}

// ensureLiveGroupRunPlaceholder 在用户发送消息后立即创建占位运行（§16.8：模型未返回 Token 时
// 前端必须显示真实阶段；placeholder 无真实 groupRunID，收到第一个事件后回填）。
export function ensureLiveGroupRunPlaceholder(
  clientRunID: string | null | undefined,
  options: { resuming?: boolean } = {},
) {
  const runID = normalizeRunID(clientRunID);
  if (!runID) {
    return undefined;
  }
  const existing = runs.get(runID);
  if (existing) {
    return existing;
  }
  const timestamp = nowTimestamp();
  const run: GroupRunState = {
    groupRunID: "",
    status: "pending",
    steps: [],
    currentStepID: null,
    currentAttemptID: null,
    resuming: Boolean(options.resuming),
    startedAt: nowISO(),
    updatedAt: nowISO(),
    lastEventAt: timestamp,
  };
  runs.set(runID, run);
  notify(runID);
  return run;
}

// upsertLiveGroupRunTool 将带 Attempt 上下文的工具调用增量写入对应 Attempt 的工具块（§16.4/§16.9）。
// tool_call → 追加 requested 调用；tool_result → 就地更新 status/error；重复事件防御性忽略。
export function upsertLiveGroupRunTool(clientRunID: string | null | undefined, event: GroupToolCallEvent) {
  const runID = normalizeRunID(clientRunID);
  const stepID = event.stepID?.trim() || "";
  const attemptID = event.attemptID?.trim() || "";
  if (!runID || !stepID || !attemptID) {
    return undefined;
  }
  const run = runs.get(runID);
  if (!run) {
    return undefined;
  }
  const nextRun: GroupRunState = {
    ...run,
    steps: mapStep(run, stepID, (step) => {
      if (!findAttempt(step, attemptID)) {
        return step;
      }
      return {
        ...step,
        status: step.status === "success" ? step.status : "running",
        attempts: step.attempts.map((attempt) => {
          if (attempt.attemptID !== attemptID) {
            return attempt;
          }
          const calls = parseGroupToolCalls(attempt.tools);
          if (event.type === "tool_call") {
            const callID = event.tool_call_id?.trim() || "";
            if (!callID || calls.some((call) => call.tool_call_id === callID)) {
              return attempt;
            }
            calls.push({
              tool_call_id: callID,
              name: event.tool_name?.trim() || "",
              status: "requested",
              input: event.arguments?.trim() ? event.arguments : undefined,
            });
          } else {
            const callID = event.tool_call_id?.trim() || "";
            const target = calls.find((call) => call.tool_call_id === callID);
            if (!target) {
              return attempt;
            }
            target.status = event.status === "error" ? "error" : "success";
            if (event.error?.trim()) {
              target.error = event.error;
            }
          }
          return touchAttempt(attempt, {
            status: "running",
            tools: buildGroupToolBlock(calls),
          });
        }),
      };
    }),
    status: run.status === "completed" || run.status === "abandoned" ? run.status : "running",
    updatedAt: nowISO(),
    lastEventAt: nowTimestamp(),
  };
  runs.set(runID, nextRun);
  notify(runID);
  return nextRun;
}

export function readLiveGroupRun(clientRunID: string | null | undefined) {
  const runID = normalizeRunID(clientRunID);
  return runID ? runs.get(runID) : undefined;
}

// setGroupRunRetrying 标记/清除重试流进行中（§16.10 输入锁：暂停→重试期间保持锁定）。
export function setGroupRunRetrying(clientRunID: string | null | undefined, retrying: boolean) {
  const runID = normalizeRunID(clientRunID);
  const run = runs.get(runID);
  if (!runID || !run || run.retrying === retrying) {
    return undefined;
  }
  const next = touchRun(run, { retrying });
  runs.set(runID, next);
  notify(runID);
  return next;
}

// 重试流的 AbortController 以 clientRunID 注册（§16.10）：重试可能从消息 meta 或
// 失败步骤 trace 发起，停止按钮需要能中断在途流，无论发起方是谁。
const retryAbortControllers = new Map<string, AbortController>();

export function registerGroupRunRetryAbort(clientRunID: string | null | undefined, controller: AbortController | null) {
  const runID = normalizeRunID(clientRunID);
  if (!runID) {
    return;
  }
  if (controller) {
    retryAbortControllers.set(runID, controller);
  } else {
    retryAbortControllers.delete(runID);
  }
}

export function abortGroupRunRetry(clientRunID: string | null | undefined) {
  const runID = normalizeRunID(clientRunID);
  retryAbortControllers.get(runID)?.abort();
}

export function clearLiveGroupRun(clientRunID: string | null | undefined) {
  const runID = normalizeRunID(clientRunID);
  if (!runID || !runs.delete(runID)) {
    return;
  }
  notify(runID);
}

export function useLiveGroupRun(clientRunID: string | null | undefined) {
  const runID = normalizeRunID(clientRunID);
  return React.useSyncExternalStore(
    React.useCallback((listener) => subscribe(runID, listener), [runID]),
    React.useCallback(() => readLiveGroupRun(runID), [runID]),
    () => undefined,
  );
}

// subscribeGroupRunSettled 订阅群组运行结算通知（重试成功/失败暂停/放弃后触发）。
export function subscribeGroupRunSettled(listener: SettledListener) {
  settledListeners.add(listener);
  return () => {
    settledListeners.delete(listener);
  };
}

// notifyGroupRunSettled 通知订阅者某次群组运行已结算（trace 在重试/放弃流程结束时调用）。
export function notifyGroupRunSettled(clientRunID: string | null | undefined) {
  const runID = normalizeRunID(clientRunID);
  if (!runID) {
    return;
  }
  settledListeners.forEach((listener) => {
    listener(runID);
  });
}

// importGroupRunDetail 从详情端点重建时间线（刷新恢复，§16.7）。
// 规则：已有运行处于 pending/running/resuming 时跳过（实时事件优先，避免覆盖在途流）；
// 详情不携带 actorIcon/actorColor/model（DTO 无此字段），组件回退到 actorType/requestedModel；
// 非终态运行时 currentStepID 指向最后一个未成功步骤（失败步骤保持展开）。
export function importGroupRunDetail(clientRunID: string | null | undefined, detail: AgentGroupRunDetailDTO) {
  const runID = normalizeRunID(clientRunID);
  if (!runID || !detail?.run?.publicID) {
    return undefined;
  }
  const existing = runs.get(runID);
  if (existing && (existing.status === "pending" || existing.status === "running" || existing.resuming)) {
    return existing;
  }

  const run = detail.run;
  const steps: GroupRunStepState[] = detail.steps.map((step) => {
    const latestAttempt = step.attempts[step.attempts.length - 1];
    return {
      stepID: step.publicID,
      sequence: step.sequence,
      stepType: step.stepType,
      actor: {
        memberID: step.actorMemberPublicID,
        name: step.actorNameSnapshot,
        type: step.actorTypeSnapshot || (step.stepType === "supervisor_decide" ? "supervisor" : "worker"),
        icon: "",
        color: "",
        model: latestAttempt?.requestedModel || "",
      },
      status: step.status,
      attempts: step.attempts.map((attempt) => ({
        attemptID: attempt.publicID,
        attemptNumber: attempt.attemptNo,
        status: attempt.status,
        output: attempt.outputMarkdown || "",
        errorCode: attempt.errorCode || undefined,
        errorMessage: attempt.errorMessage || undefined,
        startedAt: attempt.startedAt,
        endedAt: attempt.endedAt || undefined,
        updatedAt: attempt.createdAt,
        lastEventAt: nowTimestamp(),
      })),
      startedAt: step.createdAt,
      endedAt: step.status === "success" || step.status === "failed" ? step.updatedAt : undefined,
      updatedAt: step.updatedAt,
      lastEventAt: nowTimestamp(),
    };
  });
  const lastUnfinished = [...steps].reverse().find((step) => step.status !== "success");
  const next: GroupRunState = {
    groupRunID: run.publicID,
    status: run.status,
    steps,
    currentStepID: run.status === "completed" || run.status === "abandoned" ? null : lastUnfinished?.stepID || null,
    currentAttemptID: null,
    errorCode: run.errorCode || undefined,
    errorMessage: run.errorMessage || undefined,
    startedAt: run.startedAt,
    endedAt: run.endedAt || undefined,
    updatedAt: run.updatedAt,
    lastEventAt: nowTimestamp(),
  };
  runs.set(runID, next);
  notify(runID);
  return next;
}
