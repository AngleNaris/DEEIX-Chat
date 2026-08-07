"use client";

import * as React from "react";
import { RectangleHorizontal, RectangleVertical, Square } from "lucide-react";
import { useTranslations } from "next-intl";

import { OptionSelect, type OptionSelectOption } from "@/shared/components/model-select";

// 生图尺寸选项（宽x高；auto 由端点自动决定）。
export const IMAGE_SIZE_OPTIONS = ["1024x1024", "1536x1024", "1024x1536", "auto"] as const;

export type ImageSizeSelectorProps = {
  /** 当前 options.size（空串表示不指定，沿用模型默认）。 */
  value: string;
  disabled?: boolean;
  className?: string;
  onChange: (value: string) => void;
};

function ImageSizeIcon({ size }: { size: string }) {
  const [widthText, heightText] = size.split("x");
  const width = Number(widthText);
  const height = Number(heightText);
  if (!Number.isFinite(width) || !Number.isFinite(height)) {
    return <Square className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.7} />;
  }
  const Icon = width === height ? Square : width > height ? RectangleHorizontal : RectangleVertical;
  return <Icon className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.7} />;
}

// 生图尺寸选择器：空值（默认）时触发器不显示图标，选择后按横/竖/方显示方向图标。
export function ImageSizeSelector({ value, disabled, className, onChange }: ImageSizeSelectorProps) {
  const t = useTranslations("chat.imageSize");

  const options = React.useMemo<OptionSelectOption[]>(
    () => [
      { value: "", label: t("default") },
      ...IMAGE_SIZE_OPTIONS.map((size) => ({
        value: size,
        label: size === "auto" ? t("auto") : size,
      })),
    ],
    [t],
  );

  return (
    <OptionSelect
      value={value}
      options={options}
      disabled={disabled}
      placeholder={t("default")}
      contentClassName="min-w-[200px]"
      triggerClassName={className}
      renderIcon={(option) => (option?.value ? <ImageSizeIcon size={option.value} /> : undefined)}
      onChange={onChange}
    />
  );
}
