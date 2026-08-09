import { authedRequest } from "@/shared/api/authed-client";

// PlatformToolApproval 平台工具写操作待批准记录（ask 模式）。
export type PlatformToolApproval = {
  approval_id: string;
  tool: string;
  arguments: Record<string, unknown>;
  status: "pending" | "approved" | "rejected" | "expired";
  conversation_id?: number;
  created_at?: string;
};

type ApprovalResponse = {
  approval: string; // 批准记录摘要 JSON 字符串
};

function parseApprovalSummary(raw: string | undefined): PlatformToolApproval | null {
  if (!raw) return null;
  try {
    return JSON.parse(raw) as PlatformToolApproval;
  } catch {
    return null;
  }
}

// parsePendingApprovalsFromToolOutput 从工具调用输出 JSON 中解析待批准记录
// （平台写工具在 ask 模式下返回 {"status":"pending_approval","approval_id":...}）。
export function parsePendingApprovalFromOutput(output: string | undefined): PlatformToolApproval | null {
  const text = output?.trim();
  if (!text) return null;
  try {
    const parsed = JSON.parse(text) as Record<string, unknown>;
    if (parsed?.status !== "pending_approval" || typeof parsed.approval_id !== "string") {
      return null;
    }
    return {
      approval_id: parsed.approval_id,
      tool: typeof parsed.tool === "string" ? parsed.tool : "",
      arguments: {},
      status: "pending",
    };
  } catch {
    return null;
  }
}

export async function approvePlatformToolWrite(
  accessToken: string,
  approvalID: string,
): Promise<PlatformToolApproval | null> {
  const data = await authedRequest<ApprovalResponse>(
    `/api/v1/platform-tools/approvals/${encodeURIComponent(approvalID)}/approve`,
    { accessToken, method: "POST" },
    true,
  );
  return parseApprovalSummary(data?.approval);
}

export async function rejectPlatformToolWrite(
  accessToken: string,
  approvalID: string,
): Promise<PlatformToolApproval | null> {
  const data = await authedRequest<ApprovalResponse>(
    `/api/v1/platform-tools/approvals/${encodeURIComponent(approvalID)}/reject`,
    { accessToken, method: "POST" },
    true,
  );
  return parseApprovalSummary(data?.approval);
}
