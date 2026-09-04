export type GroupAttemptEventPolicy = {
  stepStatus: string;
  attemptStatus: string;
  detailSnapshot?: boolean;
};

export function normalizeGroupStreamSeq(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? Math.floor(value) : 0;
}

export function isTerminalAttemptStatus(status: string): boolean {
  return status === "success" || status === "error" || status === "failed" || status === "interrupted" || status === "canceled";
}

export function isTerminalStepStatus(status: string): boolean {
  return status === "success" || status === "failed" || status === "error" || status === "interrupted" || status === "canceled";
}

export function shouldApplyGroupAttemptEvent(
  policy: GroupAttemptEventPolicy,
  eventType: string,
): boolean {
  if (isTerminalAttemptStatus(policy.attemptStatus)) {
    return false;
  }
  if (isTerminalStepStatus(policy.stepStatus)) {
    return eventType === "group_step_retry_started" && !policy.detailSnapshot;
  }
  if (policy.detailSnapshot) {
    return eventType === "group_step_started" || eventType === "group_step_completed" || eventType === "group_step_failed";
  }
  return true;
}
