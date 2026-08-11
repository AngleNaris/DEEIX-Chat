import type { ChatAreaMessage, MessageAttachment } from "@/features/chat/types/messages";
import type { PendingAttachment } from "@/features/chat/types/chat-runtime";
import { resolvePersistedPublicID, toPendingAttachment } from "./message-submit";

// 连续改图（ChatGPT 式）：纯文字输入时自动把「上一张 AI 生成的图」作为编辑输入，
// 参考图只包含「上一张生成图 + 本次用户输入（含新上传图）」，不会带入会话历史中的其他图。
// 仅对支持 image_edit 的模型生效；普通模型完全不参与。

function isImageAttachment(
  item: Pick<MessageAttachment | PendingAttachment, "fileCategory" | "mimeType" | "detectedMime">,
): boolean {
  const mimeType = item.mimeType?.toLowerCase() || "";
  const detectedMime = item.detectedMime?.toLowerCase() || "";
  return (
    item.fileCategory === "image" ||
    mimeType.startsWith("image/") ||
    detectedMime.startsWith("image/")
  );
}

// 仅当「最后一条成功的 assistant 消息」是 AI 生成的图片时才返回其图片附件；
// 若最后一条成功回复是普通文字（比如图后闲聊过），返回 null，避免误把历史图带入编辑。
export function resolveLatestGeneratedImageAttachments(messages: ChatAreaMessage[]): MessageAttachment[] | null {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index];
    if (message.role !== "assistant" || message.isPending || message.isStreaming) {
      continue;
    }
    const status = message.status?.trim().toLowerCase() || "success";
    if (status !== "success" || resolvePersistedPublicID(message.publicID) === null) {
      continue;
    }
    if (message.contentType !== "image") {
      return null;
    }
    const images = (message.attachments ?? []).filter(isImageAttachment);
    return images.length > 0 ? images : null;
  }
  return null;
}

export type ImageEditSubmissionAttachments = {
  // 合成后用于任务决策与提交载荷的附件列表。
  attachments: PendingAttachment[];
  // 纯文字自动带入上一张生成图的模式（输入框显示可关闭提示）。
  autoEditActive: boolean;
};

export function resolveImageEditSubmissionAttachments(options: {
  messages: ChatAreaMessage[];
  userAttachments: PendingAttachment[];
  supportsImageEdit: boolean;
  requestedResponseType?: string;
  dismissed?: boolean;
}): ImageEditSubmissionAttachments {
  const { messages, userAttachments, supportsImageEdit, requestedResponseType = "", dismissed = false } = options;
  const fallback = { attachments: userAttachments, autoEditActive: false };
  // 普通模型（不支持 image_edit）完全不参与连续改图。
  if (!supportsImageEdit || dismissed) {
    return fallback;
  }
  const latestImages = resolveLatestGeneratedImageAttachments(messages);
  if (latestImages === null || latestImages.length === 0) {
    return fallback;
  }
  const latestPending = latestImages.map(toPendingAttachment);

  if (userAttachments.length === 0) {
    // 纯文字提交：显式请求图片/视频生成时不接管，其余情况自动进入「编辑上一张生成图」。
    if (requestedResponseType === "image" || requestedResponseType === "video") {
      return fallback;
    }
    return { attachments: latestPending, autoEditActive: true };
  }

  // 用户已带附件：仅当本次会走 image_edit（含图片附件）时，把上一张生成图去重后放到最前作为主图。
  if (!userAttachments.some(isImageAttachment)) {
    return fallback;
  }
  const merged = [
    ...latestPending.filter((item) => !userAttachments.some((user) => user.fileID === item.fileID)),
    ...userAttachments,
  ];
  return { attachments: merged, autoEditActive: false };
}
