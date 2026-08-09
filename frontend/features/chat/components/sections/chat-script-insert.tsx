"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { FileCode2, Loader2 } from "lucide-react";
import { Terminal } from "@/components/animate-ui/icons/terminal";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { InputGroupButton } from "@/components/ui/input-group";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { listDynamicPrompts, type DynamicPromptDTO } from "@/shared/api/dynamic-prompts";

/**
 * ScriptInsertButton 聊天输入工具栏按钮：选择动态提示词（自定义 JS/文本脚本），
 * 在输入框光标处插入 {{script: name}} 标签（发送时由后端展开执行）。
 */
export function ScriptInsertButton({
  disabled,
  onInsert,
}: {
  disabled: boolean;
  onInsert: (text: string) => void;
}) {
  const t = useTranslations("chat.composer");
  const [open, setOpen] = React.useState(false);
  const [items, setItems] = React.useState<DynamicPromptDTO[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [hovered, setHovered] = React.useState(false);

  const openDialog = async () => {
    setOpen(true);
    if (items.length > 0) {
      return;
    }
    setLoading(true);
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      setItems(await listDynamicPrompts(token));
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  };

  const select = (item: DynamicPromptDTO) => {
    onInsert(`{{script: ${item.name}}}`);
    setOpen(false);
  };

  return (
    <>
      <Tooltip>
        <TooltipTrigger asChild>
          <InputGroupButton
            type="button"
            variant="ghost"
            size="icon-sm"
            className="size-7 rounded-md text-muted-foreground hover:text-foreground sm:size-8"
            disabled={disabled}
            aria-label={t("insertScript")}
            onClick={() => void openDialog()}
            onMouseEnter={() => setHovered(true)}
            onMouseLeave={() => setHovered(false)}
          >
            <Terminal size={20} strokeWidth={1.4} animate={hovered ? "default" : undefined} />
          </InputGroupButton>
        </TooltipTrigger>
        <TooltipContent side="top">{t("insertScript")}</TooltipContent>
      </Tooltip>

      <Dialog open={open} onOpenChange={(next) => { if (!loading) setOpen(next); }}>
        <DialogContent className="sm:max-w-[440px]">
          <DialogHeader>
            <DialogTitle>{t("insertScriptTitle")}</DialogTitle>
            <DialogDescription>{t("insertScriptDescription")}</DialogDescription>
          </DialogHeader>

          <div className="max-h-[40vh] min-h-0 space-y-1 overflow-y-auto">
            {loading ? (
              <div className="flex h-16 items-center justify-center">
                <Loader2 className="size-4 animate-spin text-muted-foreground" />
              </div>
            ) : items.length === 0 ? (
              <div className="flex h-16 items-center justify-center">
                <p className="text-xs text-muted-foreground">{t("insertScriptEmpty")}</p>
              </div>
            ) : (
              items.map((item) => (
                <button
                  key={item.prompt_id}
                  type="button"
                  disabled={!item.enabled}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
                  onClick={() => select(item)}
                >
                  <FileCode2 className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate text-xs font-medium text-foreground/80">{item.name}</span>
                  <span className="shrink-0 rounded-sm bg-muted/70 px-1 py-px text-[10px] font-medium uppercase text-muted-foreground">
                    {item.kind}
                  </span>
                </button>
              ))
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
