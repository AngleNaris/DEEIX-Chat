"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { Frame } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { OptionSelect, type OptionSelectOption } from "@/shared/components/model-select";
import {
  IMAGE_ASPECT_RATIO_PRESETS,
  IMAGE_CUSTOM_ASPECT_RATIO,
  formatCustomRatioLabel,
  inferAspectRatio,
  normalizeCustomRatio,
} from "@/shared/lib/image-size";

export type ImageAspectRatioSelectorProps = {
  /** 当前 options.size（"WxH" 或空串；选择器从其反推当前比例）。 */
  value: string;
  disabled?: boolean;
  className?: string;
  /** 选中预设或确认自定义后回调，参数为宽高比字符串（如 "16:9" / "1.85:1"）。 */
  onChange: (ratio: string) => void;
};

// 生图比例选择器：预设（1:1/4:3/3:2/16:9/2.35:1/2:3/9:16）+ 自定义输入；
// 触发器为 ghost 样式（非 hover 无边框无背景），与模型选择组件一致。
export function ImageAspectRatioSelector({ value, disabled, className, onChange }: ImageAspectRatioSelectorProps) {
  const t = useTranslations("chat.imageAspectRatio");
  const tActions = useTranslations("common.actions");
  const [dialogOpen, setDialogOpen] = React.useState(false);
  const [ratioText, setRatioText] = React.useState("");
  const [errorText, setErrorText] = React.useState<string | null>(null);

  const resolvedValue = inferAspectRatio(value);
  const customLabel = resolvedValue === IMAGE_CUSTOM_ASPECT_RATIO ? formatCustomRatioLabel(value) : null;

  const options = React.useMemo<OptionSelectOption[]>(
    () => [
      ...IMAGE_ASPECT_RATIO_PRESETS.map((ratio) => ({ value: ratio, label: ratio })),
      { value: IMAGE_CUSTOM_ASPECT_RATIO, label: customLabel ?? t("custom.label") },
    ],
    [customLabel, t],
  );

  const openDialog = React.useCallback(() => {
    setRatioText(resolvedValue === IMAGE_CUSTOM_ASPECT_RATIO ? formatCustomRatioLabel(value) ?? "" : "");
    setErrorText(null);
    setDialogOpen(true);
  }, [resolvedValue, value]);

  const handleChange = React.useCallback(
    (next: string) => {
      if (next === IMAGE_CUSTOM_ASPECT_RATIO) {
        openDialog();
        return;
      }
      onChange(next);
    },
    [onChange, openDialog],
  );

  const handleConfirm = React.useCallback(() => {
    const result = normalizeCustomRatio(ratioText);
    // 项目 tsconfig 为 strict:false，布尔判别（ok:true/false）窄化不生效，
    // 因此用 "error" in result 区分错误分支（in 窄化不依赖 strictNullChecks）。
    if ("error" in result) {
      setErrorText(result.error === "tooWide" ? t("custom.tooWide") : t("custom.invalid"));
      return;
    }
    setDialogOpen(false);
    setErrorText(null);
    onChange(result.ratio);
  }, [onChange, ratioText, t]);

  return (
    <>
      <OptionSelect
        value={resolvedValue}
        options={options}
        disabled={disabled}
        placeholder={t("placeholder")}
        contentClassName="min-w-[180px]"
        triggerClassName={className}
        renderIcon={() => <Frame className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.7} />}
        onChange={handleChange}
      />
      <Dialog
        open={dialogOpen}
        onOpenChange={(open) => {
          setDialogOpen(open);
          if (!open) {
            setErrorText(null);
          }
        }}
      >
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>{t("custom.title")}</DialogTitle>
            <DialogDescription>{t("custom.description")}</DialogDescription>
          </DialogHeader>
          <Input
            value={ratioText}
            autoFocus
            placeholder={t("custom.placeholder")}
            onChange={(event) => setRatioText(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                handleConfirm();
              }
            }}
          />
          {errorText ? <p className="text-xs leading-5 text-destructive">{errorText}</p> : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setDialogOpen(false);
                setErrorText(null);
              }}
            >
              {tActions("cancel")}
            </Button>
            <Button type="button" onClick={handleConfirm}>
              {tActions("confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
