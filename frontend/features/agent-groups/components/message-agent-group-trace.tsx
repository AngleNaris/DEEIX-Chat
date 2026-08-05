"use client";

import * as React from "react";

import { ChevronDown } from "@/components/animate-ui/icons/chevron-down";
import { X } from "@/components/animate-ui/icons/x";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import { Marker, MarkerContent } from "@/components/ui/marker";
import { Spinner } from "@/components/ui/spinner";
import {
  useAgentGroupRunLabels,
  type AgentGroupRunLabels,
} from "@/features/agent-groups/hooks/use-agent-group-run-labels";
import { useAgentGroupStepActions } from "@/features/agent-groups/hooks/use-agent-group-step-actions";
import type {
  GroupRunState,
  GroupRunStepState,
} from "@/features/agent-groups/model/group-run-store";
import { MessageUpstreamThink } from "@/features/chat/components/message/message-thinking-trace";
import {
  hasActiveToolTraceCalls,
  MessageToolChainTrace,
} from "@/features/chat/components/message/message-tool-trace";
import { TRACE_ROOT_CLASS } from "@/features/chat/components/shared/message-process-trace-shared";
import type { TraceDisplayEvent } from "@/features/chat/model/message-process-trace";
import { cn } from "@/lib/utils";
import { StreamdownRender } from "@/shared/components/markdown/streamdown-render";

// Agent 群组运行时间线（方案 §16.4-§16.10）。
// 复用现有无边框 Trace 布局：每步一个独立 Accordion，内部复用 MessageUpstreamThink /
// MessageToolChainTrace / StreamdownRender；当前运行与失败步骤自动展开，成功步骤在后续
// 步骤开始时自动折叠为摘要；长等待按真实阶段 + 已等待时长显示（不展示虚假百分比）。
// §16.10：失败步骤显示 Actor/模型/错误摘要/部分正文/工具调用/Attempt 编号；
// 重试统一由消息 meta 的重试按钮发起（暂停可重试时原地重试失败步骤），
// 此处仅保留“停止重试”与“放弃本轮”操作；重试流事件写回实时状态，
// 结束后 notifyGroupRunSettled 触发消息列表刷新。

const WAITING_SECONDS_THRESHOLD = 5;
const EMPTY_TRACE_EVENTS: TraceDisplayEvent[] = [];

function nowTimestamp() {
  return typeof performance !== "undefined" ? performance.now() : Date.now();
}

// useElapsedSeconds 计算自最后一个流事件以来的等待秒数（§16.8：仅活跃运行期显示）。
function useElapsedSeconds(run: GroupRunState | undefined): number {
  const [seconds, setSeconds] = React.useState(0);

  React.useEffect(() => {
    if (!run || (run.status !== "pending" && run.status !== "running")) {
      setSeconds(0);
      return;
    }
    const update = () => {
      setSeconds(Math.max(0, Math.floor((nowTimestamp() - run.lastEventAt) / 1000)));
    };
    update();
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [run]);

  return seconds;
}

function AgentGroupStageMarker({
  labels,
  streaming,
  seconds,
  stage,
}: {
  labels: AgentGroupRunLabels;
  streaming?: boolean;
  seconds: number;
  stage: string;
}) {
  return (
    <Marker render={<span />} className="inline-flex min-h-0 w-auto text-[13px] font-medium text-muted-foreground">
      <MarkerContent className={cn("min-w-0", streaming && "shimmer")}>
        {labels.group} · {stage}
        {seconds >= WAITING_SECONDS_THRESHOLD ? (
          <span className="text-muted-foreground/70"> · {labels.waitingSeconds(seconds)}</span>
        ) : null}
      </MarkerContent>
    </Marker>
  );
}

function AgentGroupRunStatusLine({
  run,
  labels,
}: {
  run: GroupRunState;
  labels: AgentGroupRunLabels;
}) {
  const statusText = run.status === "blocked" ? labels.blocked : labels.paused;
  return (
    <Marker render={<span />} className="inline-flex min-h-0 w-auto text-[13px] font-medium text-muted-foreground">
      <MarkerContent className="min-w-0">{statusText}</MarkerContent>
    </Marker>
  );
}

function resolveStepStatusText(
  step: GroupRunStepState,
  labels: AgentGroupRunLabels,
): string {
  switch (step.status) {
    case "running": {
      const attempt = step.attempts[step.attempts.length - 1];
      if (attempt?.think?.status === "streaming") {
        return labels.thinking;
      }
      if (hasActiveToolTraceCalls(attempt?.tools?.payloadJson)) {
        return labels.executingTool;
      }
      return labels.waitingModel;
    }
    case "success":
      return labels.done;
    case "failed":
    case "error":
      return labels.failed;
    case "canceled":
      return labels.canceled;
    case "interrupted":
      return labels.interrupted;
    default:
      return "";
  }
}

function AgentGroupStepTrace({
  step,
  run,
  seconds,
  labels,
  clientRunID,
}: {
  step: GroupRunStepState;
  run: GroupRunState;
  seconds: number;
  labels: AgentGroupRunLabels;
  clientRunID: string;
}) {
  const isRunning = step.status === "running";
  const isFailed = step.status === "failed" || step.status === "error";
  const hasNextRunning = run.steps.some((item) => item.sequence > step.sequence && item.status === "running");
  const latestAttempt = step.attempts[step.attempts.length - 1];
  const attemptID = latestAttempt?.attemptID ?? "";
  const [accordionValue, setAccordionValue] = React.useState<string>(isRunning || isFailed ? "open" : "");
  const interactedRef = React.useRef(false);

  // §16.7：当前运行/失败步骤始终自动展开；下一步骤开始后上一成功步骤自动折叠为摘要。
  // 用户手动展开/折叠后停止自动干预，避免抢夺用户意图。
  React.useEffect(() => {
    if (interactedRef.current) {
      return;
    }
    if (isRunning || isFailed) {
      setAccordionValue("open");
    } else if (step.status === "success" && hasNextRunning) {
      setAccordionValue("");
    }
  }, [hasNextRunning, isFailed, isRunning, step.status]);

  // 重试创建的新 Attempt：恢复自动展开管理（§16.7 重试 Attempt 显示在同一个逻辑步骤内）。
  React.useEffect(() => {
    interactedRef.current = false;
  }, [attemptID]);

  // §16.10：只有被暂停/阻塞运行的最后一个未成功步骤可操作（其余成功步骤不可变）。
  // 重试统一由消息 meta 的重试按钮发起，此处只保留“停止重试”与“放弃本轮”。
  const runActionable =
    (run.status === "paused_retryable" || run.status === "blocked") &&
    step.stepID === run.currentStepID &&
    step.status !== "success";
  const canAbandon = runActionable;
  const { retrying, abandoning, actionError, handleStopRetry, handleAbandon } = useAgentGroupStepActions({
    clientRunID,
    run,
    step,
  });

  if (!latestAttempt) {
    return null;
  }

  const open = accordionValue === "open";
  const statusText = resolveStepStatusText(step, labels);
  const actorLabel = step.actor.name?.trim() || labels.group;
  const actorIcon = step.actor.icon?.trim();
  const toolsActive = isRunning && hasActiveToolTraceCalls(latestAttempt.tools?.payloadJson);
  const output = latestAttempt.output?.trim() ?? "";
  const showActions = canAbandon || retrying;
  const priorAttempts = step.attempts.slice(0, -1);

  return (
    <Accordion
      type="single"
      collapsible
      value={accordionValue}
      onValueChange={(value) => {
        interactedRef.current = true;
        setAccordionValue(value || "");
      }}
      className="w-full"
    >
      <AccordionItem value="open" className="border-b-0">
        <AccordionTrigger
          iconPosition="none"
          className="group items-start justify-between gap-1.5 py-0 text-left no-underline hover:no-underline"
        >
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <Marker
                render={<span />}
                className={cn(
                  "inline-flex min-h-0 w-auto text-[13px] font-medium transition-colors",
                  !isRunning && "text-muted-foreground group-hover:text-foreground",
                )}
              >
                <MarkerContent className="min-w-0">
                  {actorIcon ? `${actorIcon} ` : ""}
                  {actorLabel}
                </MarkerContent>
              </Marker>
              <span
                className={cn(
                  "shrink-0 text-[13px] font-medium",
                  isFailed && "text-destructive/85",
                  !isRunning && !isFailed && "text-muted-foreground/62",
                  isRunning && "shimmer",
                )}
              >
                {statusText}
              </span>
            </div>
            <div className="mt-0.5 truncate text-[11px] font-normal leading-4 text-muted-foreground/62">
              {step.actor.model}
              {latestAttempt.attemptNumber > 1 ? ` · ${labels.attempt(latestAttempt.attemptNumber)}` : ""}
              {isRunning && seconds >= WAITING_SECONDS_THRESHOLD ? ` · ${labels.waitingSeconds(seconds)}` : ""}
            </div>
          </div>
          <ChevronDown
            className={cn(
              "mt-0.5 size-3.5 shrink-0 text-muted-foreground transition-transform duration-200 group-hover:text-foreground",
              open && "rotate-180",
            )}
          />
        </AccordionTrigger>
        <AccordionContent className="px-0 pb-0 pt-1.5 duration-[350ms] ease-in-out">
          {latestAttempt.think ? (
            <MessageUpstreamThink
              block={latestAttempt.think}
              streaming={Boolean(isRunning && latestAttempt.think.status === "streaming")}
              autoCollapseReady={!isRunning}
              title={actorLabel}
              subtitle={step.actor.model}
            />
          ) : null}
          {latestAttempt.tools ? (
            <MessageToolChainTrace
              events={EMPTY_TRACE_EVENTS}
              activeToolBlock={latestAttempt.tools}
              streaming={toolsActive}
              autoCollapseReady={!isRunning}
            />
          ) : null}
          {output ? <StreamdownRender content={latestAttempt.output} streaming={Boolean(isRunning)} /> : null}
          {isFailed && latestAttempt.errorMessage?.trim() ? (
            <p className="mt-1.5 text-[12px] leading-5 text-destructive/85">{latestAttempt.errorMessage}</p>
          ) : null}
          {showActions ? (
            <div className="mt-2 flex flex-wrap items-center gap-1.5">
              {retrying ? (
                <Button size="sm" variant="ghost" className="gap-1.5" onClick={handleStopRetry}>
                  <X className="size-3.5" />
                  {labels.stop}
                </Button>
              ) : null}
              {canAbandon ? (
                <Button
                  size="sm"
                  variant="ghost"
                  className="gap-1.5 text-muted-foreground hover:text-destructive"
                  disabled={retrying || abandoning}
                  onClick={handleAbandon}
                >
                  {abandoning ? <Spinner className="size-3.5" /> : null}
                  {labels.abandon}
                </Button>
              ) : null}
            </div>
          ) : null}
          {actionError ? <p className="mt-1.5 text-[12px] leading-5 text-destructive/85">{actionError}</p> : null}
          {priorAttempts.length > 0 ? (
            <Accordion type="multiple" className="mt-2 w-full">
              <AccordionItem value="history" className="border-b-0">
                <AccordionTrigger className="gap-1.5 py-0 text-left text-[12px] text-muted-foreground/70 hover:text-foreground">
                  {labels.attemptHistory}
                </AccordionTrigger>
                <AccordionContent className="space-y-1.5 px-0 pb-0 pt-1">
                  {priorAttempts.map((attempt) => (
                    <div key={attempt.attemptID} className="rounded-md border border-border/60 px-2 py-1.5">
                      <div className="flex items-center gap-1.5 text-[11px] leading-4 text-muted-foreground/70">
                        <span className="font-medium text-foreground/80">{labels.attempt(attempt.attemptNumber)}</span>
                        <span>{resolveStepStatusText({ ...step, attempts: [attempt], status: attempt.status }, labels)}</span>
                        {attempt.errorMessage?.trim() ? (
                          <span className="truncate text-destructive/85">{attempt.errorMessage}</span>
                        ) : null}
                      </div>
                      {attempt.output?.trim() ? (
                        <div className="mt-1 max-h-24 overflow-y-auto">
                          <StreamdownRender content={attempt.output} />
                        </div>
                      ) : null}
                    </div>
                  ))}
                </AccordionContent>
              </AccordionItem>
            </Accordion>
          ) : null}
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  );
}

export function MessageAgentGroupTrace({
  run,
  streaming,
  clientRunID,
}: {
  run?: GroupRunState;
  streaming?: boolean;
  clientRunID: string;
}) {
  const labels = useAgentGroupRunLabels();
  const seconds = useElapsedSeconds(run);

  if (!run) {
    return null;
  }
  if (run.steps.length === 0) {
    // §16.8：占位运行——模型尚未返回任何事件时显示真实阶段。
    return (
      <div className={TRACE_ROOT_CLASS}>
        <AgentGroupStageMarker
          labels={labels}
          streaming={streaming}
          seconds={seconds}
          stage={run.resuming ? labels.resuming : labels.prepare}
        />
      </div>
    );
  }

  return (
    <div className={TRACE_ROOT_CLASS}>
      <div className="w-full space-y-1">
        {run.status === "running" && !run.currentStepID ? (
          <AgentGroupStageMarker labels={labels} streaming={streaming} seconds={seconds} stage={labels.assembling} />
        ) : null}
        {run.steps.map((step) => (
          <AgentGroupStepTrace
            key={step.stepID}
            step={step}
            run={run}
            seconds={seconds}
            labels={labels}
            clientRunID={clientRunID}
          />
        ))}
        {run.status === "paused_retryable" || run.status === "blocked" ? (
          <AgentGroupRunStatusLine run={run} labels={labels} />
        ) : null}
      </div>
    </div>
  );
}
