import { authedRequest } from "@/shared/api/authed-client";
import { apiRequest } from "@/shared/api/http-client";
import { pathParam } from "@/shared/api/http-client";

export type ArtifactKind = "html" | "js" | "css" | "text";

export type ArtifactShareDTO = {
  share_id: string;
  status: string;
  title_snapshot: string;
  created_at: string;
};

export type ArtifactListItemDTO = {
  artifact_id: string;
  kind: ArtifactKind;
  title: string;
  code: string;
  conversation_id: number;
  message_id: number;
  share?: ArtifactShareDTO | null;
  created_at: string;
  updated_at: string;
};

export type ArtifactDetailDTO = {
  artifact_id: string;
  kind: ArtifactKind;
  title: string;
  code: string;
  conversation_id: number;
  message_id: number;
  created_at: string;
  updated_at: string;
};

export type PublicSharedArtifactDTO = {
  share_id: string;
  title: string;
  kind: ArtifactKind;
  code: string;
  created_at: string;
};

export type CreateArtifactInput = {
  artifactId?: string;
  title: string;
  kind: ArtifactKind;
  code: string;
};

export async function listArtifacts(
  accessToken: string,
  page = 1,
  pageSize = 20,
): Promise<{ total: number; page: number; items: ArtifactListItemDTO[] }> {
  return authedRequest(`/api/v1/artifacts?page=${page}&page_size=${pageSize}`, {
    method: "GET",
    accessToken,
  });
}

export async function createArtifact(accessToken: string, input: CreateArtifactInput): Promise<ArtifactDetailDTO> {
  const body: Record<string, unknown> = { title: input.title, kind: input.kind, code: input.code };
  if (input.artifactId) {
    body.artifactId = input.artifactId;
  }
  return authedRequest("/api/v1/artifacts", {
    method: "POST",
    accessToken,
    body,
  });
}

export async function deleteArtifact(accessToken: string, artifactId: string): Promise<{ deleted: boolean }> {
  return authedRequest(`/api/v1/artifacts/${pathParam(artifactId)}`, {
    method: "DELETE",
    accessToken,
  });
}

export async function createArtifactShare(accessToken: string, artifactId: string): Promise<ArtifactShareDTO> {
  return authedRequest(`/api/v1/artifacts/${pathParam(artifactId)}/share`, {
    method: "POST",
    accessToken,
  });
}

export async function getArtifactShare(accessToken: string, artifactId: string): Promise<ArtifactShareDTO> {
  return authedRequest(`/api/v1/artifacts/${pathParam(artifactId)}/share`, {
    method: "GET",
    accessToken,
  });
}

export async function revokeArtifactShare(accessToken: string, artifactId: string): Promise<{ revoked: boolean }> {
  return authedRequest(`/api/v1/artifacts/${pathParam(artifactId)}/share`, {
    method: "DELETE",
    accessToken,
  });
}

// 公开分享（免认证）。
export async function getSharedArtifact(shareId: string): Promise<PublicSharedArtifactDTO> {
  return apiRequest(`/api/v1/shared-artifacts/${pathParam(shareId)}`, {
    method: "GET",
  });
}

// ArtifactPreviewWidth 制品分享/预览的宽度模式：全宽或固定宽度居中。
export type ArtifactPreviewWidth = "full" | "fixed";

/**
 * artifactShareUrl 生成制品分享链接（绝对 URL，含域名）。
 * previewWidth 可选：分享页打开时的默认预览宽度（full=全宽 / fixed=固定宽度居中）。
 * SSR 环境（window 不可用）退回相对路径。
 */
export function artifactShareUrl(shareId: string, previewWidth?: ArtifactPreviewWidth): string {
  const path = `/share/artifact?artifact_id=${encodeURIComponent(shareId)}`;
  const query = previewWidth ? `${path}&preview_width=${encodeURIComponent(previewWidth)}` : path;
  if (typeof window === "undefined") {
    return query;
  }
  return `${window.location.origin}${query}`;
}
