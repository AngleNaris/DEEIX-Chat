"use client";

import * as React from "react";
import { useTranslations } from "next-intl";

export type AgentGroupRunLabels = {
  group: string;
  prepare: string;
  resuming: string;
  waitingModel: string;
  thinking: string;
  executingTool: string;
  assembling: string;
  done: string;
  failed: string;
  paused: string;
  blocked: string;
  canceled: string;
  interrupted: string;
  retry: string;
  stop: string;
  abandon: string;
  attemptHistory: string;
  decisionDelegate: string;
  decisionFinish: string;
  decisionTargetMember: string;
  decisionInstruction: string;
  decisionExpectedOutcome: string;
  decisionAnswer: string;
  attempt: (attemptNumber: number) => string;
  waitingSeconds: (seconds: number) => string;
};

export function useAgentGroupRunLabels(): AgentGroupRunLabels {
  const t = useTranslations("chat.agentGroupRun");

  return React.useMemo(
    () => ({
      group: t("group"),
      prepare: t("prepare"),
      resuming: t("resuming"),
      waitingModel: t("waitingModel"),
      thinking: t("thinking"),
      executingTool: t("executingTool"),
      assembling: t("assembling"),
      done: t("done"),
      failed: t("failed"),
      paused: t("paused"),
      blocked: t("blocked"),
      canceled: t("canceled"),
      interrupted: t("interrupted"),
      retry: t("retry"),
      stop: t("stop"),
      abandon: t("abandon"),
      attemptHistory: t("attemptHistory"),
      decisionDelegate: t("decisionDelegate"),
      decisionFinish: t("decisionFinish"),
      decisionTargetMember: t("decisionTargetMember"),
      decisionInstruction: t("decisionInstruction"),
      decisionExpectedOutcome: t("decisionExpectedOutcome"),
      decisionAnswer: t("decisionAnswer"),
      attempt: (attemptNumber: number) => t("attempt", { attemptNumber }),
      waitingSeconds: (seconds: number) => t("waitingSeconds", { seconds }),
    }),
    [t],
  );
}
