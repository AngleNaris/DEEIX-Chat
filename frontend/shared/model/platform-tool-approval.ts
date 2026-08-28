export type PlatformToolApproval = {
  approval_id: string;
  tool: string;
  arguments: Record<string, unknown>;
  status: "pending" | "approved" | "rejected" | "failed" | "expired";
  conversation_id?: number;
  created_at?: string;
};

const terminalApprovalStatuses = new Set<PlatformToolApproval["status"]>([
  "approved",
  "rejected",
  "failed",
  "expired",
]);

export function parsePlatformToolApprovalFromOutput(output: string | undefined): PlatformToolApproval | null {
  const text = output?.trim();
  if (!text) return null;
  try {
    const parsed = JSON.parse(text) as Record<string, unknown>;
    if (typeof parsed.approval_id !== "string" || parsed.approval_id.trim() === "") {
      return null;
    }
    const rawStatus = typeof parsed.status === "string" ? parsed.status.trim() : "";
    const status: PlatformToolApproval["status"] | null =
      rawStatus === "pending_approval"
        ? "pending"
        : terminalApprovalStatuses.has(rawStatus as PlatformToolApproval["status"])
          ? (rawStatus as PlatformToolApproval["status"])
          : null;
    if (!status) return null;
    const result: PlatformToolApproval = {
      approval_id: parsed.approval_id,
      tool: typeof parsed.tool === "string" ? parsed.tool : "",
      arguments:
        parsed.arguments && typeof parsed.arguments === "object" && !Array.isArray(parsed.arguments)
          ? (parsed.arguments as Record<string, unknown>)
          : {},
      status,
    };
    if (typeof parsed.conversation_id === "number") result.conversation_id = parsed.conversation_id;
    if (typeof parsed.created_at === "string") result.created_at = parsed.created_at;
    return result;
  } catch {
    return null;
  }
}
