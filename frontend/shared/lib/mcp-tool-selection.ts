import type { MCPToolDTO } from "@/shared/api/mcp.types";

// 附件处理器模式（image/audio/file），单选约束与后端 IsValidAttachmentMode 保持一致。
const ATTACHMENT_PROCESSOR_MODES = new Set(["image", "audio", "file"]);

function isAttachmentProcessor(tool: MCPToolDTO | undefined): boolean {
  return Boolean(tool && ATTACHMENT_PROCESSOR_MODES.has(tool.attachmentInputMode));
}

export function hasMultipleImageAttachmentProcessors(toolIDs: number[], tools: MCPToolDTO[]): boolean {
  const selectedIDs = new Set(toolIDs);
  let processorCount = 0;
  for (const tool of tools) {
    if (selectedIDs.has(tool.id) && isAttachmentProcessor(tool)) {
      processorCount += 1;
      if (processorCount > 1) {
        return true;
      }
    }
  }
  return false;
}

export function normalizeImageAttachmentProcessorSelection(
  toolIDs: number[],
  tools: MCPToolDTO[],
): number[] {
  const toolsByID = new Map(tools.map((tool) => [tool.id, tool]));
  let processorSelected = false;
  return toolIDs.filter((toolID) => {
    const tool = toolsByID.get(toolID);
    if (!isAttachmentProcessor(tool)) {
      return true;
    }
    if (processorSelected) {
      return false;
    }
    processorSelected = true;
    return true;
  });
}
