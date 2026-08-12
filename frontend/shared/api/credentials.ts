import { authedRequest } from "@/shared/api/authed-client";
import { pathParam } from "@/shared/api/http-client";

export type CredentialType = "ssh" | "api_key" | "generic";

export type CredentialDTO = {
  public_id: string;
  name: string;
  type: CredentialType;
  description: string;
  meta?: Record<string, string>;
  created_at: string;
  updated_at: string;
};

export type CredentialInput = {
  name: string;
  type: CredentialType;
  description?: string;
  /** 创建必填；更新时留空表示不修改密钥。 */
  value?: string;
  meta?: Record<string, string>;
};

type CredentialListResponse = {
  results: CredentialDTO[];
};

type CredentialResponse = {
  credential: CredentialDTO;
};

export async function listCredentials(accessToken: string): Promise<CredentialDTO[]> {
  const data = await authedRequest<CredentialListResponse>("/api/v1/credentials", {
    method: "GET",
    accessToken,
  });
  return data.results ?? [];
}

export async function createCredential(accessToken: string, input: CredentialInput): Promise<CredentialDTO> {
  const data = await authedRequest<CredentialResponse>("/api/v1/credentials", {
    method: "POST",
    accessToken,
    body: JSON.stringify(input),
  });
  return data.credential;
}

export async function updateCredential(
  accessToken: string,
  publicID: string,
  input: CredentialInput,
): Promise<CredentialDTO> {
  const data = await authedRequest<CredentialResponse>(`/api/v1/credentials/${pathParam(publicID)}`, {
    method: "PUT",
    accessToken,
    body: JSON.stringify(input),
  });
  return data.credential;
}

export async function deleteCredential(accessToken: string, publicID: string): Promise<void> {
  await authedRequest(`/api/v1/credentials/${pathParam(publicID)}`, {
    method: "DELETE",
    accessToken,
  });
}
