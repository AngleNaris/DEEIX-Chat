"use client";

import * as React from "react";
import { Binary, FileArchive, FileText, Loader2, PackageOpen, Upload } from "lucide-react";
import { useTranslations } from "next-intl";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { PackageFile, SkillPackagePreview } from "@/shared/api/skills.types";
import { formatBytes } from "@/shared/lib/file-display";

const ZIP_ACCEPT = ".zip,application/zip,application/x-zip-compressed";

function isZipFile(file: File): boolean {
  return /\.zip$/i.test(file.name);
}

function packageFileIcon(kind: string): React.ReactNode {
  return kind === "binary" ? (
    <Binary className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.6} />
  ) : (
    <FileText className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.6} />
  );
}

/**
 * SkillPackageUploader 选择 zip 技能包并展示解析预览（标题 / SKILL.md / 文件清单），
 * 确认后由父组件执行导入。preview 与导入逻辑由父组件持有（token 来源与 API 因用户/管理侧而异）。
 */
export function SkillPackageUploader({
  fileName,
  importing,
  namespace,
  onImport,
  onReset,
  onSelectFile,
  preview,
  previewing,
  reimport,
}: {
  fileName: string | null;
  importing: boolean;
  namespace?: "prompts" | "adminPrompts";
  onImport: () => void;
  onReset: () => void;
  onSelectFile: (file: File) => void;
  preview: SkillPackagePreview | null;
  previewing: boolean;
  reimport?: boolean;
}) {
  const t = useTranslations(namespace ?? "prompts");
  const inputRef = React.useRef<HTMLInputElement>(null);
  const [pickedName, setPickedName] = React.useState<string | null>(null);
  const [invalidFile, setInvalidFile] = React.useState(false);
  const [dragging, setDragging] = React.useState(false);

  const handleFile = React.useCallback(
    (file: File | undefined | null) => {
      setInvalidFile(false);
      if (!file) return;
      if (!isZipFile(file)) {
        setPickedName(null);
        setInvalidFile(true);
        onReset();
        return;
      }
      setPickedName(file.name);
      onSelectFile(file);
    },
    [onReset, onSelectFile],
  );

  const resetSelection = React.useCallback(() => {
    setPickedName(null);
    setInvalidFile(false);
    if (inputRef.current) {
      inputRef.current.value = "";
    }
    onReset();
  }, [onReset]);

  const previewTitle = preview?.title || preview?.trigger;

  return (
    <div className="space-y-1">
      <p className="text-xs text-muted-foreground">{t("skillPackageLabel")}</p>
      {preview ? (
        <div className="space-y-3">
          <div className="space-y-2 rounded-lg border bg-muted/30 p-3">
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium text-foreground">{previewTitle}</p>
                {preview.description ? (
                  <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">{preview.description}</p>
                ) : null}
                {preview.rootDir ? (
                  <p className="mt-0.5 truncate text-xs text-muted-foreground">
                    {t("skillPackageRootDir")}: {preview.rootDir}
                  </p>
                ) : null}
              </div>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-7 shrink-0 px-2 text-xs"
                disabled={previewing || importing}
                onClick={resetSelection}
              >
                {t("skillPackageReselect")}
              </Button>
            </div>
            <div>
              <p className="text-xs font-medium text-foreground/80">{t("skillMarkdown")}</p>
              <pre className="mt-1 max-h-32 overflow-y-auto whitespace-pre-wrap rounded-md bg-background p-2 text-xs leading-5 text-muted-foreground [field-sizing:fixed]">
                {preview.markdown}
              </pre>
            </div>
            <div>
              <p className="text-xs font-medium text-foreground/80">
                {t("skillPackageFiles")} ({preview.files.length})
              </p>
              <ul className="mt-1 max-h-32 space-y-0.5 overflow-y-auto rounded-md bg-background p-1.5 text-xs">
                {preview.files.map((file: PackageFile) => (
                  <li key={file.path} className="flex min-w-0 items-center gap-1.5 py-0.5">
                    {packageFileIcon(file.kind)}
                    <span className="min-w-0 flex-1 truncate font-mono text-[11px]">{file.path}</span>
                    <span className="shrink-0 text-muted-foreground">{formatBytes(file.size)}</span>
                  </li>
                ))}
              </ul>
            </div>
          </div>
          <Button type="button" className="w-full" disabled={importing} onClick={onImport}>
            {importing ? <Loader2 className="size-4 animate-spin" /> : <PackageOpen className="size-4" strokeWidth={1.8} />}
            {reimport ? t("skillPackageReplace") : t("skillPackageImport")}
          </Button>
        </div>
      ) : (
        <div
          role="button"
          tabIndex={0}
          className={cn(
            "flex cursor-pointer flex-col items-center justify-center gap-1.5 rounded-lg border border-dashed px-4 py-6 text-center transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45",
            dragging ? "border-primary bg-muted/40" : "bg-muted/25 hover:bg-muted/40",
          )}
          onClick={() => inputRef.current?.click()}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              inputRef.current?.click();
            }
          }}
          onDragOver={(event) => {
            event.preventDefault();
            setDragging(true);
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={(event) => {
            event.preventDefault();
            setDragging(false);
            handleFile(event.dataTransfer.files?.[0]);
          }}
        >
          <Upload className="size-5 text-muted-foreground" strokeWidth={1.8} />
          <p className="text-xs font-medium text-foreground/80">{t("skillPackageSelect")}</p>
          <p className="text-xs text-muted-foreground">{t("skillPackageDragHint")}</p>
          {pickedName ? (
            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <FileArchive className="size-3.5" strokeWidth={1.6} />
              <span className="max-w-56 truncate">{pickedName}</span>
              {previewing ? <Loader2 className="size-3.5 animate-spin" /> : null}
            </p>
          ) : null}
          {invalidFile ? <p className="text-xs text-destructive">{t("skillPackageInvalidFile")}</p> : null}
          <input
            ref={inputRef}
            type="file"
            accept={ZIP_ACCEPT}
            className="sr-only"
            onChange={(event) => handleFile(event.target.files?.[0])}
          />
        </div>
      )}
    </div>
  );
}
