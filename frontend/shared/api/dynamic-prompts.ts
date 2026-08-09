import { authedRequest } from "@/shared/api/authed-client";
import { pathParam } from "@/shared/api/http-client";

export type DynamicPromptDTO = {
  prompt_id: string;
  name: string;
  kind: "js" | "text";
  content: string;
  enabled: boolean;
  updated_by: string;
  updated_at: string;
};

export type UpsertDynamicPromptInput = {
  name: string;
  kind: "js" | "text";
  content: string;
  enabled?: boolean;
};

export async function listDynamicPrompts(accessToken: string): Promise<DynamicPromptDTO[]> {
  return authedRequest("/api/v1/dynamic-prompts", {
    method: "GET",
    accessToken,
  });
}

export async function createDynamicPrompt(accessToken: string, input: UpsertDynamicPromptInput): Promise<DynamicPromptDTO> {
  return authedRequest("/api/v1/dynamic-prompts", {
    method: "POST",
    accessToken,
    body: input,
  });
}

export async function updateDynamicPrompt(
  accessToken: string,
  promptId: string,
  input: UpsertDynamicPromptInput,
): Promise<DynamicPromptDTO> {
  return authedRequest(`/api/v1/dynamic-prompts/${pathParam(promptId)}`, {
    method: "PUT",
    accessToken,
    body: input,
  });
}

export async function deleteDynamicPrompt(accessToken: string, promptId: string): Promise<{ deleted: boolean }> {
  return authedRequest(`/api/v1/dynamic-prompts/${pathParam(promptId)}`, {
    method: "DELETE",
    accessToken,
  });
}

// runDynamicPrompt 执行动态提示词（js 沙箱执行 / text 直返），返回执行结果文本。
export async function runDynamicPrompt(accessToken: string, promptId: string): Promise<string> {
  const data = await authedRequest<{ result: string }>(`/api/v1/dynamic-prompts/${pathParam(promptId)}/run`, {
    method: "POST",
    accessToken,
  });
  return data.result ?? "";
}
