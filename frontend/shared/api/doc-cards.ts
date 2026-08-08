import { authedRequest } from "@/shared/api/authed-client";
import { pathParam } from "@/shared/api/http-client";

export type DocCardDTO = {
  card_id: string;
  title: string;
  content: string;
  keywords: string[];
  enabled: boolean;
  updated_by: string;
  updated_at: string;
};

export type UpsertDocCardInput = {
  title: string;
  content: string;
  keywords: string[];
  enabled?: boolean;
};

export async function listDocCards(accessToken: string): Promise<DocCardDTO[]> {
  return authedRequest("/api/v1/doc-cards", {
    method: "GET",
    accessToken,
  });
}

export async function createDocCard(accessToken: string, input: UpsertDocCardInput): Promise<DocCardDTO> {
  return authedRequest("/api/v1/doc-cards", {
    method: "POST",
    accessToken,
    body: input,
  });
}

export async function updateDocCard(accessToken: string, cardId: string, input: UpsertDocCardInput): Promise<DocCardDTO> {
  return authedRequest(`/api/v1/doc-cards/${pathParam(cardId)}`, {
    method: "PUT",
    accessToken,
    body: input,
  });
}

export async function deleteDocCard(accessToken: string, cardId: string): Promise<{ deleted: boolean }> {
  return authedRequest(`/api/v1/doc-cards/${pathParam(cardId)}`, {
    method: "DELETE",
    accessToken,
  });
}
