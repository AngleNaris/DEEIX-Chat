import { authedRequest } from "@/shared/api/authed-client";
import { pathParam } from "@/shared/api/http-client";
import type { PagePayload } from "@/shared/api/common.types";
import type {
  PatchSkillRequest,
  SkillDTO,
  SkillData,
  SkillDeleteData,
  SkillPackageFile,
  SkillPackagePreview,
  SkillPage,
  SkillSummaryDTO,
  SkillSummaryPage,
  WriteSkillRequest,
} from "@/shared/api/skills.types";

type SkillListOptions = {
  ids?: number[];
  query?: string;
  enabled?: boolean;
  page?: number;
  pageSize?: number;
};

function skillListPath(basePath: string, options: SkillListOptions = {}): string {
  const params = new URLSearchParams({
    page: String(options.page ?? 1),
    page_size: String(options.pageSize ?? 50),
  });
  if (options.query?.trim()) params.set("q", options.query.trim());
  for (const id of options.ids ?? []) {
    if (Number.isInteger(id) && id > 0) params.append("id", String(id));
  }
  if (typeof options.enabled === "boolean") params.set("enabled", String(options.enabled));
  return `${basePath}?${params.toString()}`;
}

function normalizePagePayload(data: PagePayload<SkillDTO>): SkillPage {
  return {
    results: data.results ?? [],
    total: data.total ?? 0,
  };
}

function normalizeSummaryPagePayload(data: PagePayload<SkillSummaryDTO>): SkillSummaryPage {
  return {
    results: data.results ?? [],
    total: data.total ?? 0,
  };
}

export async function listVisibleSkills(
  accessToken: string,
  options: SkillListOptions = {},
): Promise<SkillSummaryPage> {
  const data = await authedRequest<PagePayload<SkillSummaryDTO>>(
    skillListPath("/api/v1/skills", options),
    { accessToken },
    true,
  );
  return normalizeSummaryPagePayload(data);
}

export async function getVisibleSkill(accessToken: string, id: number): Promise<SkillData> {
  return authedRequest<SkillData>(`/api/v1/skills/${pathParam(id)}`, { accessToken }, true);
}

export async function listMySkills(accessToken: string, options: SkillListOptions = {}): Promise<SkillPage> {
  const data = await authedRequest<PagePayload<SkillDTO>>(
    skillListPath("/api/v1/skills/mine", options),
    { accessToken },
    true,
  );
  return normalizePagePayload(data);
}

export async function createMySkill(accessToken: string, payload: WriteSkillRequest): Promise<SkillData> {
  return authedRequest<SkillData>("/api/v1/skills/mine", { method: "POST", accessToken, body: payload }, true);
}

export async function updateMySkill(
  accessToken: string,
  id: number,
  payload: PatchSkillRequest,
): Promise<SkillData> {
  return authedRequest<SkillData>(
    `/api/v1/skills/mine/${pathParam(id)}`,
    { method: "PATCH", accessToken, body: payload },
    true,
  );
}

export async function deleteMySkill(accessToken: string, id: number): Promise<SkillDeleteData> {
  return authedRequest<SkillDeleteData>(
    `/api/v1/skills/mine/${pathParam(id)}`,
    { method: "DELETE", accessToken },
    true,
  );
}

export async function listAdminSkills(accessToken: string, options: SkillListOptions = {}): Promise<SkillPage> {
  const data = await authedRequest<PagePayload<SkillDTO>>(
    skillListPath("/api/v1/admin/skills", options),
    { accessToken },
    true,
  );
  return normalizePagePayload(data);
}

export async function createAdminSkill(accessToken: string, payload: WriteSkillRequest): Promise<SkillData> {
  return authedRequest<SkillData>("/api/v1/admin/skills", { method: "POST", accessToken, body: payload }, true);
}

export async function updateAdminSkill(
  accessToken: string,
  id: number,
  payload: PatchSkillRequest,
): Promise<SkillData> {
  return authedRequest<SkillData>(
    `/api/v1/admin/skills/${pathParam(id)}`,
    { method: "PATCH", accessToken, body: payload },
    true,
  );
}

export async function deleteAdminSkill(accessToken: string, id: number): Promise<SkillDeleteData> {
  return authedRequest<SkillDeleteData>(
    `/api/v1/admin/skills/${pathParam(id)}`,
    { method: "DELETE", accessToken },
    true,
  );
}

function packageUploadForm(file: File): FormData {
  const formData = new FormData();
  formData.append("file", file);
  return formData;
}

// packageFilePathParam 将包内相对路径编码为 URL 路径段（保留斜杠层级）。
function packageFilePathParam(path: string): string {
  return path
    .split("/")
    .map((segment) => encodeURIComponent(segment))
    .join("/");
}

export async function previewMySkillPackage(accessToken: string, file: File): Promise<SkillPackagePreview> {
  return authedRequest<SkillPackagePreview>(
    "/api/v1/skills/mine/import/preview",
    { method: "POST", accessToken, body: packageUploadForm(file) },
    true,
  );
}

export async function importMySkillPackage(accessToken: string, file: File): Promise<SkillData> {
  return authedRequest<SkillData>(
    "/api/v1/skills/mine/import",
    { method: "POST", accessToken, body: packageUploadForm(file) },
    true,
  );
}

export async function replaceMySkillPackage(accessToken: string, id: number, file: File): Promise<SkillData> {
  return authedRequest<SkillData>(
    `/api/v1/skills/mine/${pathParam(id)}/package`,
    { method: "POST", accessToken, body: packageUploadForm(file) },
    true,
  );
}

export async function previewAdminSkillPackage(accessToken: string, file: File): Promise<SkillPackagePreview> {
  return authedRequest<SkillPackagePreview>(
    "/api/v1/admin/skills/import/preview",
    { method: "POST", accessToken, body: packageUploadForm(file) },
    true,
  );
}

export async function importAdminSkillPackage(accessToken: string, file: File): Promise<SkillData> {
  return authedRequest<SkillData>(
    "/api/v1/admin/skills/import",
    { method: "POST", accessToken, body: packageUploadForm(file) },
    true,
  );
}

export async function replaceAdminSkillPackage(accessToken: string, id: number, file: File): Promise<SkillData> {
  return authedRequest<SkillData>(
    `/api/v1/admin/skills/${pathParam(id)}/package`,
    { method: "POST", accessToken, body: packageUploadForm(file) },
    true,
  );
}

export async function getSkillPackageFile(accessToken: string, id: number, path: string): Promise<SkillPackageFile> {
  return authedRequest<SkillPackageFile>(
    `/api/v1/skills/${pathParam(id)}/files/${packageFilePathParam(path)}`,
    { accessToken },
    true,
  );
}
