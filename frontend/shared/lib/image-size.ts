// gpt-image-2 尺寸计算：按「分辨率档位（1K/2K/4K）× 宽高比」自动推导合法尺寸。
// 官方约束：长边 ≤3840px、边长 16 的倍数、长/短边 ≤3:1、总像素 655360~8294400。
// 策略（长边优先）：目标长边（1K=1024/2K=2048/4K=3840）向上枚举首个合法解（像素不足时放大），
// 无解再向下枚举（像素超限时缩小），比例偏差 ≤2% 视为合法。
// 存储格式与 OpenAI size 字段一致：`${width}x${height}`（ASCII x）。

export type ImageResolutionLevel = "1k" | "2k" | "4k";

export const IMAGE_RESOLUTION_LEVELS = ["1k", "2k", "4k"] as const satisfies readonly ImageResolutionLevel[];

export const IMAGE_RESOLUTION_TARGETS: Record<ImageResolutionLevel, number> = {
  "1k": 1024,
  "2k": 2048,
  "4k": 3840,
};

/** 常用宽高比（值即展示文案；横向为主 + 纵向常见项，其余走自定义输入）。 */
export const IMAGE_ASPECT_RATIO_PRESETS = ["1:1", "4:3", "3:2", "16:9", "2.35:1", "2:3", "9:16"] as const;

/** 自定义比例在比例选择器中的选项值。 */
export const IMAGE_CUSTOM_ASPECT_RATIO = "custom";

// gpt-image-2 官方约束常量。
export const IMAGE_MIN_PIXELS = 655360;
export const IMAGE_MAX_PIXELS = 8294400;
export const IMAGE_MAX_LONG_EDGE = 3840;
export const IMAGE_MAX_RATIO = 3;
export const IMAGE_RATIO_TOLERANCE = 0.02;
export const IMAGE_EDGE_STEP = 16;

/** 解析存储尺寸（兼容 ASCII x 与 U+00D7 ×）；无法解析（空值/auto/非法）返回 null。 */
export function parseImageSize(value: string | undefined): { width: number; height: number } | null {
  if (!value) {
    return null;
  }
  const parts = value.trim().split(/[x×]/);
  if (parts.length !== 2) {
    return null;
  }
  const width = Number(parts[0]);
  const height = Number(parts[1]);
  if (!Number.isInteger(width) || !Number.isInteger(height) || width <= 0 || height <= 0) {
    return null;
  }
  return { width, height };
}

function parseRatioInput(ratioInput: string): { width: number; height: number } | null {
  const parts = ratioInput.trim().split(":");
  if (parts.length > 2) {
    return null;
  }
  const width = Number.parseFloat(parts[0]);
  const height = parts.length === 2 ? Number.parseFloat(parts[1]) : 1;
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
    return null;
  }
  return { width, height };
}

/** 数字裁剪到 2 位小数并去除尾零（16 → "16"，2.35 → "2.35"）。 */
function trimNumber(value: number): string {
  return String(Math.round(value * 100) / 100);
}

/**
 * 按「分辨率档位 × 宽高比」计算合法尺寸，返回 `${width}x${height}`（长边优先）。
 * 比例解析失败时返回 null。
 */
export function resolveImageSize(ratioInput: string, level: ImageResolutionLevel): string | null {
  const parsed = parseRatioInput(ratioInput);
  if (!parsed) {
    return null;
  }
  const portrait = parsed.width < parsed.height;
  const r = Math.max(parsed.width, parsed.height) / Math.min(parsed.width, parsed.height);
  const target = IMAGE_RESOLUTION_TARGETS[level];

  // 扫描一段长边范围（步进 16）：命中合法解立即返回，否则记录偏差最小的候选兜底。
  const scanRange = (
    from: number,
    to: number,
    step: number,
  ): { size: string | null; done: boolean; best: string | null } => {
    let best: { dev: number; size: string } | null = null;
    for (let E = from; step > 0 ? E <= to : E >= to; E += step) {
      let S = Math.round(E / r / IMAGE_EDGE_STEP) * IMAGE_EDGE_STEP;
      if (S < IMAGE_EDGE_STEP) {
        S = IMAGE_EDGE_STEP;
      }
      const pixels = E * S;
      if (pixels < IMAGE_MIN_PIXELS || pixels > IMAGE_MAX_PIXELS) {
        continue;
      }
      const dev = Math.abs(E / S - r) / r;
      const size = portrait ? `${S}x${E}` : `${E}x${S}`;
      if (dev <= IMAGE_RATIO_TOLERANCE) {
        return { size, done: true, best: size };
      }
      if (!best || dev < best.dev) {
        best = { dev, size };
      }
    }
    return { size: null, done: false, best: best?.size ?? null };
  };

  // 长边优先：先向上（目标长边 → 3840），无合法解再向下（目标长边-16 → 16）。
  const upward = scanRange(target, IMAGE_MAX_LONG_EDGE, IMAGE_EDGE_STEP);
  if (upward.done) {
    return upward.size;
  }
  const downward = scanRange(target - IMAGE_EDGE_STEP, IMAGE_EDGE_STEP, -IMAGE_EDGE_STEP);
  if (downward.done) {
    return downward.size;
  }
  return upward.best ?? downward.best;
}

/**
 * 从存储尺寸反推可计算比例（`${width}:${height}`，归一化后与原尺寸互为恒等）。
 * 无法解析（空值/auto/非法）返回 null。
 */
export function deriveRatioString(sizeValue: string | undefined): string | null {
  const parsed = parseImageSize(sizeValue);
  if (!parsed) {
    return null;
  }
  return `${parsed.width}:${parsed.height}`;
}

/**
 * 从存储尺寸反推比例选择器当前值：命中预设返回预设值，否则返回 "custom"，
 * 无法解析（未设置/auto）返回空串（显示占位）。
 */
export function inferAspectRatio(sizeValue: string | undefined): string {
  const parsed = parseImageSize(sizeValue);
  if (!parsed) {
    return "";
  }
  const ratio = parsed.width / parsed.height;
  for (const preset of IMAGE_ASPECT_RATIO_PRESETS) {
    const [pw, ph] = preset.split(":").map(Number);
    const p = pw / ph;
    if (Math.abs(ratio - p) / p <= IMAGE_RATIO_TOLERANCE) {
      return preset;
    }
  }
  return IMAGE_CUSTOM_ASPECT_RATIO;
}

/** 从存储尺寸反推分辨率档位（用三档逐一重算比对，精确命中）；未知返回空串。 */
export function inferResolutionLevel(sizeValue: string | undefined): ImageResolutionLevel | "" {
  const parsed = parseImageSize(sizeValue);
  if (!parsed) {
    return "";
  }
  const canonical = `${parsed.width}x${parsed.height}`;
  const ratio = `${parsed.width}:${parsed.height}`;
  for (const level of IMAGE_RESOLUTION_LEVELS) {
    if (resolveImageSize(ratio, level) === canonical) {
      return level;
    }
  }
  return "";
}

/** 自定义比例展示文案（"2.35:1" / 纵向 "1:1.79"）；无法解析返回 null。 */
export function formatCustomRatioLabel(sizeValue: string | undefined): string | null {
  const parsed = parseImageSize(sizeValue);
  if (!parsed) {
    return null;
  }
  const { width, height } = parsed;
  const r = Math.max(width, height) / Math.min(width, height);
  return width >= height ? `${trimNumber(r)}:1` : `1:${trimNumber(r)}`;
}

export type CustomRatioResult = { ok: true; ratio: string } | { ok: false; error: "invalid" | "tooWide" };

/**
 * 解析自定义比例输入（"16:9"、"2.35:1"、"2.35" 均合法；无冒号视为 N:1）。
 * 格式非法或长/短边比超过 3:1（gpt-image-2 上限）时返回错误。
 */
export function normalizeCustomRatio(text: string): CustomRatioResult {
  const trimmed = text.trim().replace(/\s+/g, "");
  if (!trimmed) {
    return { ok: false, error: "invalid" };
  }
  const parts = trimmed.split(":");
  if (parts.length > 2) {
    return { ok: false, error: "invalid" };
  }
  const width = Number.parseFloat(parts[0]);
  const height = parts.length === 2 ? Number.parseFloat(parts[1]) : 1;
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
    return { ok: false, error: "invalid" };
  }
  if (Math.max(width, height) / Math.min(width, height) > IMAGE_MAX_RATIO) {
    return { ok: false, error: "tooWide" };
  }
  return { ok: true, ratio: `${trimNumber(width)}:${trimNumber(height)}` };
}
