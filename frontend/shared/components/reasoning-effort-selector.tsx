"use client";

import * as React from "react";
import { useTranslations } from "next-intl";

import { OptionSelect, type OptionSelectOption } from "@/shared/components/model-select";
import {
  resolveReasoningEffortForProtocols,
  reasoningEffortLevelsForMapping,
} from "@/shared/lib/reasoning-effort";

export type ReasoningEffortSelectorProps = {
  /** 当前生效模型的协议列表（模型协议不支持思考强度时整个选择器隐藏）。 */
  protocols: readonly string[];
  value: string;
  disabled?: boolean;
  className?: string;
  onChange: (value: string) => void;
};

// 思考强度选择器：无支持协议（如 anthropic/图片类端点）时返回 null 隐藏。
export function ReasoningEffortSelector({
  protocols,
  value,
  disabled,
  className,
  onChange,
}: ReasoningEffortSelectorProps) {
  const t = useTranslations("chat.reasoningEffort");
  const mapping = resolveReasoningEffortForProtocols(protocols);

  const options = React.useMemo<OptionSelectOption[]>(() => {
    if (!mapping) {
      return [];
    }
    const levels = reasoningEffortLevelsForMapping(mapping);
    return [
      { value: "", label: t("levels.default") },
      ...levels.map((level) => ({ value: level, label: t(`levels.${level}`) })),
    ];
  }, [mapping, t]);

  if (!mapping) {
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
      onChange={onChange}
    />
  );
}
