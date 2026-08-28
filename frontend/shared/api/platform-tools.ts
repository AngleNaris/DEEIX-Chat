import { authedRequest } from "@/shared/api/authed-client";
import type { PlatformToolApproval } from "@/shared/model/platform-tool-approval";

export type { PlatformToolApproval } from "@/shared/model/platform-tool-approval";

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

export async function getPlatformToolWriteApproval(
  accessToken: string,
  approvalID: string,
): Promise<PlatformToolApproval | null> {
  const data = await authedRequest<ApprovalResponse>(
    `/api/v1/platform-tools/approvals/${encodeURIComponent(approvalID)}`,
    { accessToken },
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
