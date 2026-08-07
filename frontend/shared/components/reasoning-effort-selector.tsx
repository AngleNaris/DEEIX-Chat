"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { Zap } from "lucide-react";

import { OptionSelect, type OptionSelectOption } from "@/shared/components/model-select";
import {
  resolveReasoningEffortForProtocols,
  reasoningEffortLevelsForMapping,
} from "@/shared/lib/reasoning-effort";

export type ReasoningEffortSelectorProps = {
  /** 当前生效模型的协议列表（协议不支持思考强度时按协议截断档位；与 levels 均缺省时隐藏）。 */
  protocols: readonly string[];
  /** 显式档位列表（对话框等语义档位场景：不依赖协议映射，始终显示全部档位）。 */
  levels?: readonly string[];
  value: string;
  disabled?: boolean;
  className?: string;
  onChange: (value: string) => void;
};

// 思考强度选择器：无支持协议且未传显式档位时返回 null 隐藏；
// 空值（继承默认）时触发器显示 Zap 图标。
export function ReasoningEffortSelector({
  protocols,
  levels,
  value,
  disabled,
  className,
  onChange,
}: ReasoningEffortSelectorProps) {
  const t = useTranslations("chat.reasoningEffort");
  const mapping = resolveReasoningEffortForProtocols(protocols);

  const options = React.useMemo<OptionSelectOption[]>(() => {
    const levelList = levels ?? (mapping ? reasoningEffortLevelsForMapping(mapping) : []);
    return [
      { value: "", label: t("levels.default") },
      ...levelList.map((level) => ({ value: level, label: t(`levels.${level}`) })),
    ];
  }, [levels, mapping, t]);

  if (!mapping && !levels) {
    return null;
  }

  return (
    <OptionSelect
      value={value}
      options={options}
      disabled={disabled}
      placeholder={t("levels.default")}
      contentClassName="min-w-[200px]"
      triggerClassName={className}
      renderIcon={(option) =>
        option?.value ? undefined : <Zap className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.7} />
      }
      onChange={onChange}
    />
  );
}
