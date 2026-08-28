export type PlatformToolApprovalDisplayState = "checking" | "pending" | "approved" | "rejected" | "failed" | "expired";

type ApprovalStateSource = {
  approval_id: string;
  status: "pending" | "approved" | "rejected" | "failed" | "expired";
};

export function resolvePlatformToolApprovalDisplayState(
  status: string | undefined,
): PlatformToolApprovalDisplayState {
  if (status === "approved" || status === "rejected" || status === "failed" || status === "expired") {
    return status;
  }
  return "pending";
}

export function resolvePlatformToolApprovalLookupFailure(status: number | undefined): PlatformToolApprovalDisplayState {
  return status === 404 ? "expired" : "pending";
}

export function collectPendingPlatformApprovalIDs(approvals: ApprovalStateSource[]): string[] {
  return approvals.filter((approval) => approval.status === "pending").map((approval) => approval.approval_id);
}

export function initializePlatformApprovalDisplayStates(
  approvals: ApprovalStateSource[],
  previous: Record<string, PlatformToolApprovalDisplayState>,
): Record<string, PlatformToolApprovalDisplayState> {
  const next = { ...previous };
  for (const approval of approvals) {
    next[approval.approval_id] = approval.status === "pending" ? (previous[approval.approval_id] ?? "checking") : approval.status;
  }
  return next;
}
