"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { Monitor } from "lucide-react";

import { OptionSelect, type OptionSelectOption } from "@/shared/components/model-select";
import {
  IMAGE_RESOLUTION_LEVELS,
  type ImageResolutionLevel,
  inferResolutionLevel,
  resolveImageSize,
} from "@/shared/lib/image-size";

// 分辨率档位标签（1K/2K/4K 为通用单位，不参与翻译）。
const LEVEL_LABELS: Record<ImageResolutionLevel, string> = {
  "1k": "1K",
  "2k": "2K",
  "4k": "4K",
};

export type ImageResolutionSelectorProps = {
  /** 当前 options.size（"WxH" 或空串；选择器从其反推当前档位）。 */
  value: string;
  /** 当前比例（用于实时计算各档位合法尺寸；null 时选项不带尺寸后缀）。 */
  ratio: string | null;
  disabled?: boolean;
  className?: string;
  /** 选中档位后回调（"1k"/"2k"/"4k"）。 */
  onChange: (level: ImageResolutionLevel) => void;
};

// 生图分辨率选择器：1K/2K/4K 档位，选项实时显示按当前比例计算出的合法尺寸；
// 触发器为 ghost 样式（非 hover 无边框无背景），与模型选择组件一致。
export function ImageResolutionSelector({
  value,
  ratio,
  disabled,
  className,
  onChange,
}: ImageResolutionSelectorProps) {
  const t = useTranslations("chat.imageResolution");
  const resolvedValue = inferResolutionLevel(value);
  const options = React.useMemo<OptionSelectOption[]>(
    () =>
      IMAGE_RESOLUTION_LEVELS.map((level) => {
        const size = ratio ? resolveImageSize(ratio, level) : null;
        return {
          value: level,
          label: size ? `${LEVEL_LABELS[level]}（${size.replace("x", "×")}）` : LEVEL_LABELS[level],
        };
      }),
    [ratio],
  );

  return (
    <OptionSelect
      value={resolvedValue}
      options={options}
      disabled={disabled}
      placeholder={t("placeholder")}
      contentClassName="min-w-[180px]"
      triggerClassName={className}
      renderIcon={() => <Monitor className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.7} />}
      onChange={onChange}
    />
  );
}
