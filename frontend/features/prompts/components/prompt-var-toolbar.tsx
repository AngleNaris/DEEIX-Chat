"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { Braces, Terminal } from "lucide-react";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { listDynamicPrompts } from "@/shared/api/dynamic-prompts";

// 系统内置提示词变量（每次发送时展开）。
const BUILTIN_VARS = ["date", "time", "datetime", "weekday", "language", "username"] as const;

/**
 * PromptVarToolbar 提示词编辑器标签工具栏：
 * 系统变量以 chip 形式点击插入；动态提示词（{{script: name}}）以下拉选单选择插入。
 * 插入位置由父组件（Textarea 光标处）处理，通过 onInsert 回调。
 */
export function PromptVarToolbar({ onInsert }: { onInsert: (text: string) => void }) {
  const t = useTranslations("recent.projects");
  const [prompts, setPrompts] = React.useState<{ name: string; kind: string }[]>([]);

  React.useEffect(() => {
    void (async () => {
      try {
        const token = await resolveAccessToken();
        if (!token) return;
        setPrompts(await listDynamicPrompts(token));
      } catch {
        // ignore
      }
    })();
  }, []);

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
        <Braces className="size-3" strokeWidth={1.8} />
        {t("promptVarsLabel")}
      </span>
      {BUILTIN_VARS.map((name) => (
        <button
          key={name}
          type="button"
          className="rounded-sm bg-muted/60 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          onClick={() => onInsert(`{{${name}}}`)}
          title={t("promptVarsInsertHint", { variable: `{{${name}}}` })}
        >
          {`{{${name}}}`}
        </button>
      ))}
      {prompts.length > 0 && (
        <Select
          onValueChange={(name) => {
            const prompt = prompts.find((item) => item.name === name);
            if (prompt) {
              onInsert(`{{script: ${prompt.name}}}`);
            }
          }}
        >
          <SelectTrigger className="flex h-6 w-auto gap-1 rounded-sm bg-muted/60 px-1.5 text-[10px] text-muted-foreground [&>svg]:size-3">
            <Terminal className="size-3" strokeWidth={1.8} />
            <SelectValue placeholder={t("promptVarsDynamic")} />
          </SelectTrigger>
          <SelectContent>
            {prompts.map((prompt) => (
              <SelectItem key={prompt.name} value={prompt.name} className="text-xs">
                {prompt.name}
                <span className="ml-1 text-[10px] uppercase text-muted-foreground">{prompt.kind}</span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    </div>
  );
}
