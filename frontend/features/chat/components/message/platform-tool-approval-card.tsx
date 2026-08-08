"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import { FilePenLine, Loader2, ShieldCheck, ShieldX } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  approvePlatformToolWrite,
  parsePendingApprovalFromOutput,
  rejectPlatformToolWrite,
  type PlatformToolApproval,
} from "@/shared/api/platform-tools";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { useLocalizedErrorMessage } from "@/i18n/use-localized-error";

// ToolTraceCall 与 message-tool-trace 中的 trace 结构一致（buildToolTrace payload）。
type ToolTraceCall = {
  name?: string;
  status?: string;
  input?: string;
  input_preview?: string;
  input_detail?: string;
  output?: string;
  output_text?: string;
  output_preview?: string;
  output_detail?: string;
};

function parsePendingApprovals(payloadJson: string | undefined): { call: ToolTraceCall; approval: PlatformToolApproval }[] {
  if (!payloadJson) return [];
  try {
    const parsed = JSON.parse(payloadJson) as { tool_calls?: ToolTraceCall[] };
    if (!Array.isArray(parsed.tool_calls)) return [];
    const result: { call: ToolTraceCall; approval: PlatformToolApproval }[] = [];
    for (const call of parsed.tool_calls) {
      const output = call.output_detail || call.output || call.output_text || call.output_preview;
      const approval = parsePendingApprovalFromOutput(output);
      if (approval) {
        result.push({ call, approval });
      }
    }
    return result;
  } catch {
    return [];
  }
}

function summarizeArguments(call: ToolTraceCall): string {
  const raw = call.input_detail || call.input || call.input_preview || "";
  const text = raw.trim();
  if (!text) return "";
  try {
    const parsed = JSON.parse(text) as Record<string, unknown>;
    const fileID = typeof parsed.file_id === "string" ? parsed.file_id : "";
    const skillID = typeof parsed.skill_id === "number" ? String(parsed.skill_id) : "";
    const memoryKey = typeof parsed.key === "string" ? parsed.key : "";
    return fileID || skillID || memoryKey || "";
  } catch {
    return text.slice(0, 120);
  }
}

/**
 * PlatformToolApprovalCard 展示 ask 模式下挂起的平台工具写操作（读文件/技能/会话历史之外，
 * 编辑文件/技能等写操作需用户批准）。从工具 trace payload 中提取 pending_approval 记录，
 * 提供批准/拒绝操作；批准后由后端异步执行写操作。
 */
export function PlatformToolApprovalCard({ tracePayloadJson }: { tracePayloadJson?: string }) {
  const t = useTranslations("chat.platformTools");
  const resolveErrorMessage = useLocalizedErrorMessage();
  const [handled, setHandled] = React.useState<Record<string, "approved" | "rejected">>({});
  const [busy, setBusy] = React.useState<string | null>(null);

  const pending = React.useMemo(() => parsePendingApprovals(tracePayloadJson), [tracePayloadJson]);
  if (pending.length === 0) {
    return null;
  }

  const act = async (approvalID: string, approve: boolean) => {
    if (busy) return;
    setBusy(approvalID);
    try {
      const token = await resolveAccessToken();
      if (!token) {
        toast.error(t("authRequired"));
        return;
      }
      if (approve) {
        await approvePlatformToolWrite(token, approvalID);
        setHandled((prev) => ({ ...prev, [approvalID]: "approved" }));
        toast.success(t("approved"));
      } else {
        await rejectPlatformToolWrite(token, approvalID);
        setHandled((prev) => ({ ...prev, [approvalID]: "rejected" }));
        toast.success(t("rejected"));
      }
    } catch (error) {
      toast.error(approve ? t("approveFailed") : t("rejectFailed"), { description: resolveErrorMessage(error) });
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="mt-2 space-y-2">
      {pending.map(({ call, approval }) => {
        const state = handled[approval.approval_id];
        const isBusy = busy === approval.approval_id;
        const target = summarizeArguments(call) || approval.tool;
        return (
          <div
            key={approval.approval_id}
            className="flex flex-col gap-2 rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-2.5"
          >
            <div className="flex items-start gap-2">
              <FilePenLine className="mt-0.5 size-3.5 shrink-0 text-amber-500/80" strokeWidth={1.8} />
              <div className="min-w-0 flex-1">
                <p className="text-xs font-medium text-foreground">{t("title")}</p>
                <p className="mt-0.5 truncate text-[11px] text-muted-foreground">
                  {t("target", { tool: approval.tool, target })}
                </p>
              </div>
            </div>
            {state ? (
              <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                {state === "approved" ? (
                  <ShieldCheck className="size-3.5 text-emerald-500" strokeWidth={1.8} />
                ) : (
                  <ShieldX className="size-3.5 text-muted-foreground" strokeWidth={1.8} />
                )}
                {state === "approved" ? t("approvedHint") : t("rejectedHint")}
              </div>
            ) : (
              <div className="flex justify-end gap-1.5">
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  className="h-6 px-2 text-[11px]"
                  disabled={isBusy}
                  onClick={() => void act(approval.approval_id, false)}
                >
                  {isBusy ? <Loader2 className="size-3 animate-spin" /> : <ShieldX className="size-3" />}
                  {t("reject")}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  className="h-6 px-2 text-[11px]"
                  disabled={isBusy}
                  onClick={() => void act(approval.approval_id, true)}
                >
                  {isBusy ? <Loader2 className="size-3 animate-spin" /> : <ShieldCheck className="size-3" />}
                  {t("approve")}
                </Button>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
