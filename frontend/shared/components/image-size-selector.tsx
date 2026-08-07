"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { RectangleHorizontal, RectangleVertical, Square } from "lucide-react";

import { OptionSelect, type OptionSelectOption } from "@/shared/components/model-select";

// gpt-image-2 尺寸档位（分辨率级 × 宽高比）。
// 官方约束：长边 ≤3840px、边长 16 的倍数、长/短边 ≤3:1、总像素 655360~8294400；
// 档位取官方 popular sizes 并对称补齐合法尺寸，比例组内按分辨率升序。
export type ImageSizeTier = "standard" | "2k" | "4k";

export type ImageSizeOption = {
  width: number;
  height: number;
  tier: ImageSizeTier;
};

// 按 比例（方形 → 横向 → 纵向）分组排列的尺寸矩阵。
export const IMAGE_SIZE_GROUPS: readonly { ratio: string; sizes: readonly ImageSizeOption[] }[] = [
  {
    ratio: "1:1",
    sizes: [
      { width: 1024, height: 1024, tier: "standard" },
      { width: 2048, height: 2048, tier: "2k" },
    ],
  },
  {
    ratio: "3:2",
    sizes: [{ width: 1536, height: 1024, tier: "standard" }],
  },
  {
    ratio: "16:9",
    sizes: [
      { width: 2048, height: 1152, tier: "2k" },
      { width: 3840, height: 2160, tier: "4k" },
    ],
  },
  {
    ratio: "2:3",
    sizes: [{ width: 1024, height: 1536, tier: "standard" }],
  },
  {
    ratio: "9:16",
    sizes: [
      { width: 1152, height: 2048, tier: "2k" },
      { width: 2160, height: 3840, tier: "4k" },
    ],
  },
];

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

// 生图尺寸选择器：选项按「档位 比例（宽×高）」排列（1:1 → 3:2 → 16:9 → 2:3 → 9:16，组内分辨率升序）；
// 触发器为 ghost 样式（非 hover 无边框无背景），与模型选择组件一致。
export function ImageSizeSelector({ value, disabled, className, onChange }: ImageSizeSelectorProps) {
  const t = useTranslations("chat.imageSize");
  const options = React.useMemo<OptionSelectOption[]>(() => {
    const list: OptionSelectOption[] = [];
    for (const group of IMAGE_SIZE_GROUPS) {
      for (const size of group.sizes) {
        list.push({
          value: `${size.width}x${size.height}`,
          label: `${t(size.tier)} ${group.ratio}（${size.width}×${size.height}）`,
        });
      }
    }
    list.push({ value: "auto", label: t("auto") });
    return [{ value: "", label: t("default") }, ...list];
  }, [t]);
  return (
    <OptionSelect
      value={value}
      options={options}
      disabled={disabled}
      placeholder={t("default")}
      contentClassName="min-w-[220px]"
      triggerClassName={className}
      renderIcon={(option) => (option?.value ? <ImageSizeIcon size={option.value} /> : undefined)}
      onChange={onChange}
    />
  );
}
