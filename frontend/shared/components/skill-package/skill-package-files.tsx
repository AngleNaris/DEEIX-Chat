"use client";

import * as React from "react";
import { Binary, FileText, Loader2 } from "lucide-react";
import { useTranslations } from "next-intl";

import { cn } from "@/lib/utils";
import type { PackageFile } from "@/shared/api/skills.types";

/**
 * SkillPackageFilesViewer 展示技能包的文件清单；点击文本文件由 fetchFile 加载内容（只读）。
 * 二进制文件不可预览（后端返回 415），按钮禁用。
 */
export function SkillPackageFilesViewer({
  fetchFile,
  files,
  namespace,
}: {
  fetchFile: (path: string) => Promise<string>;
  files: PackageFile[] | undefined;
  namespace?: "prompts" | "adminPrompts";
}) {
  const t = useTranslations(namespace ?? "prompts");
  const [activePath, setActivePath] = React.useState<string | null>(null);
  const [content, setContent] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [loadFailed, setLoadFailed] = React.useState(false);
  const requestSeqRef = React.useRef(0);

  React.useEffect(() => {
    requestSeqRef.current += 1;
    setActivePath(null);
    setContent("");
    setLoading(false);
    setLoadFailed(false);
  }, [fetchFile, files]);

  const openFile = React.useCallback(
    async (file: PackageFile) => {
      if (file.kind === "binary") return;
      const requestSeq = requestSeqRef.current + 1;
      requestSeqRef.current = requestSeq;
      setActivePath(file.path);
      setContent("");
      setLoadFailed(false);
      setLoading(true);
      try {
        const nextContent = await fetchFile(file.path);
        if (requestSeqRef.current === requestSeq) {
          setContent(nextContent);
        }
      } catch {
        if (requestSeqRef.current === requestSeq) {
          setLoadFailed(true);
        }
      } finally {
        if (requestSeqRef.current === requestSeq) {
          setLoading(false);
        }
      }
    },
    [fetchFile],
  );

  const list = files ?? [];

  return (
    <div className="space-y-1">
      <p className="text-xs text-muted-foreground">
        {t("skillPackageFiles")} ({list.length})
      </p>
      <div className="flex h-56 gap-2">
        <ul className="w-2/5 min-w-0 shrink-0 space-y-0.5 overflow-y-auto rounded-md border bg-muted/25 p-1.5 text-xs">
          {list.length === 0 ? (
            <li className="px-1 py-1 text-muted-foreground">{t("skillPackageNoFiles")}</li>
          ) : null}
          {list.map((file) => {
            const binary = file.kind === "binary";
            return (
              <li key={file.path}>
                <button
                  type="button"
                  disabled={binary}
                  title={binary ? t("skillPackageBinaryUnavailable") : undefined}
                  className={cn(
                    "flex w-full min-w-0 items-center gap-1.5 rounded px-1.5 py-1 text-left",
                    binary ? "cursor-not-allowed text-muted-foreground/60" : "hover:bg-muted",
                    activePath === file.path && "bg-muted",
                  )}
                  onClick={() => void openFile(file)}
                >
                  {binary ? (
                    <Binary className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.6} />
                  ) : (
                    <FileText className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.6} />
                  )}
                  <span className="min-w-0 flex-1 truncate font-mono text-[11px]">{file.path}</span>
                </button>
              </li>
            );
          })}
        </ul>
        <div className="min-w-0 flex-1 overflow-y-auto rounded-md border bg-muted/25 p-2 text-xs">
          {activePath ? (
            loading ? (
              <div className="flex h-full items-center justify-center">
                <Loader2 className="size-4 animate-spin text-muted-foreground" />
              </div>
            ) : loadFailed ? (
              <p className="text-destructive">{t("skillPackageFileLoadFailed")}</p>
            ) : (
              <pre className="whitespace-pre-wrap break-all leading-5 text-foreground/85">{content}</pre>
            )
          ) : (
            <p className="text-muted-foreground">{t("skillPackageFileSelectHint")}</p>
          )}
        </div>
      </div>
    </div>
  );
}
