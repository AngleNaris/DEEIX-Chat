"use client";

import * as React from "react";
import { useTranslations } from "next-intl";

import { OptionSelect, type OptionSelectOption } from "@/shared/components/model-select";

// gpt-image-2 质量档位（官方：low/medium/high/auto，默认 auto；空值表示不指定，沿用模型默认）。
export const IMAGE_QUALITY_OPTIONS = ["low", "medium", "high", "auto"] as const;

export type ImageQualitySelectorProps = {
  /** 当前 options.quality（空串表示不指定，沿用模型默认）。 */
  value: string;
  disabled?: boolean;
  className?: string;
  onChange: (value: string) => void;
};

// 生图质量选择器：触发器为 ghost 样式（非 hover 无边框无背景），与模型选择组件一致。
export function ImageQualitySelector({ value, disabled, className, onChange }: ImageQualitySelectorProps) {
  const t = useTranslations("chat.imageQuality");
  const options = React.useMemo<OptionSelectOption[]>(
    () => [
      { value: "", label: t("default") },
      ...IMAGE_QUALITY_OPTIONS.map((quality) => ({ value: quality, label: t(quality) })),
    ],
    [t],
  );
  return (
    <OptionSelect
      value={value}
      options={options}
      disabled={disabled}
      placeholder={t("default")}
      contentClassName="min-w-[140px]"
      triggerClassName={className}
      onChange={onChange}
    />
  );
}
