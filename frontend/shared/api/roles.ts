import { authedRequest } from "@/shared/api/authed-client";
import { pathParam } from "@/shared/api/http-client";
import type {
  ConversationRoleDTO,
  CreateConversationRoleRequest,
  ReorderConversationRolesRequest,
  UpdateConversationRoleRequest,
} from "@/shared/api/roles.types";

type ListConversationRolesOptions = {
  status?: "active" | "archived" | "all";
};

export async function listConversationRoles(
  accessToken: string,
  options: ListConversationRolesOptions = {},
): Promise<ConversationRoleDTO[]> {
  const status = options.status?.trim() || "active";
  return authedRequest<ConversationRoleDTO[]>(
    `/api/v1/conversation-roles?status=${encodeURIComponent(status)}`,
    {
      accessToken,
    },
    true,
  );
}

export async function getConversationRole(
  accessToken: string,
  rolePublicID: string,
): Promise<ConversationRoleDTO> {
  return authedRequest<ConversationRoleDTO>(
    `/api/v1/conversation-roles/${pathParam(rolePublicID)}`,
    {
      accessToken,
    },
    true,
  );
}

export async function createConversationRole(
  accessToken: string,
  payload: CreateConversationRoleRequest,
): Promise<ConversationRoleDTO> {
  return authedRequest<ConversationRoleDTO>(
    "/api/v1/conversation-roles",
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function updateConversationRole(
  accessToken: string,
  rolePublicID: string,
  payload: UpdateConversationRoleRequest,
): Promise<ConversationRoleDTO> {
  return authedRequest<ConversationRoleDTO>(
    `/api/v1/conversation-roles/${pathParam(rolePublicID)}`,
    {
      method: "PATCH",
      accessToken,
      body: payload,
    },
    true,
  );
}

export async function deleteConversationRole(
  accessToken: string,
  rolePublicID: string,
): Promise<{ deleted: boolean }> {
  return authedRequest<{ deleted: boolean }>(
    `/api/v1/conversation-roles/${pathParam(rolePublicID)}`,
    {
      method: "DELETE",
      accessToken,
    },
    true,
  );
}

export async function reorderConversationRoles(
  accessToken: string,
  payload: ReorderConversationRolesRequest,
): Promise<{ reordered: boolean }> {
  return authedRequest<{ reordered: boolean }>(
    "/api/v1/conversation-roles/reorder",
    {
      method: "POST",
      accessToken,
      body: payload,
    },
    true,
  );
}
