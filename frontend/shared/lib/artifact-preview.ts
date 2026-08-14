export type ArtifactPreviewKind = "html" | "css" | "javascript";

const HTML_LIKE_RE = /^\s*(?:<!doctype\s+html|<html\b|<head\b|<body\b|<(?:article|canvas|div|main|section|style|script|svg)\b)/i;

function normalizeLanguage(language: string): string {
  return language.trim().toLowerCase();
}

/**
 * resolveStoredArtifactPreviewKind 把制品库的存储 kind（html/js/css/text）映射为预览 kind。
 * text 无法渲染为可视化预览，返回 null（调用方按纯文本查看）。
 */
export function resolveStoredArtifactPreviewKind(kind: string): ArtifactPreviewKind | null {
  const normalized = kind.trim().toLowerCase();
  if (["html", "htm", "xhtml"].includes(normalized)) return "html";
  if (["css", "scss", "sass", "less"].includes(normalized)) return "css";
  if (["js", "javascript", "mjs", "cjs"].includes(normalized)) return "javascript";
  return null;
}

export function resolveArtifactPreviewKind(language: string, code: string): ArtifactPreviewKind | null {
  const normalized = normalizeLanguage(language);
  if (["html", "htm", "xhtml"].includes(normalized)) return "html";
  if (["css", "scss", "sass", "less"].includes(normalized)) return "css";
  if (["js", "javascript", "mjs", "cjs"].includes(normalized)) return "javascript";
  if ((!normalized || normalized === "markdown") && HTML_LIKE_RE.test(code)) return "html";
  return null;
}
