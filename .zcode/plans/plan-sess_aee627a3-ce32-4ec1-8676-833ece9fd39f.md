## 生图参数选择器 UI 修订：推理强度图标/ghost 样式 + 比例/分辨率/质量三选择器

### 需求（逐字）
1. 推理强度：任何选择都给图标；UI 与模型选择控件一致（无 hover 无边框/填充）
2. 生图模型的比例/分辨率/质量都有图标
3. 比例与分辨率**分别选择**：比例提供 1:1、4:3、3:2、16:9、2.35:1、2:3、9:16 + 自定义输入（已确认：横向为主，长边优先）
4. 分辨率档位 1K/2K/4K；最终传入的 size = 档位长边 × 比例自动计算合法值

### 一、新文件：`frontend/shared/lib/image-size.ts`（纯函数）
- 常量：`IMAGE_RESOLUTION_LEVELS = ["1k","2k","4k"]`、目标长边 `{1k:1024, 2k:2048, 4k:3840}`、`IMAGE_ASPECT_RATIO_PRESETS = ["1:1","4:3","3:2","16:9","2.35:1","2:3","9:16"]`
- `parseImageSize(value)`：解析存储值 "WxH"（兼容 ASCII x 与 U+00D7 ×）；"auto" 等无法解析返回 null
- `resolveImageSize(ratioStr, level): string`（**核心算法，长边优先**）：
  - 解析 "w:h"（w/h 可为小数）→ r = 长短比（≥1，纵向归一为横向计算后交换）
  - 目标长边 E0（1K=1024/2K=2048/4K=3840）；从 E0 向上步进 16（≤3840）：短边 S = round(E/r/16)*16，校验：像素 ∈ [655360, 8294400]、长边 ≤3840、比例偏差 ≤2% → 命中即返回；向上无解则向下枚举（处理 4K 4:3 超像素上限 → 3296×2480、4K 1:1 → 2880² 命中上限）
  - 输出 "WxH"（ASCII x，与 OpenAI size 字段格式一致）
- `inferAspectRatio(sizeValue)`：反推比例 → 命中预设返回预设值，否则 "custom"
- `inferResolutionLevel(sizeValue)`：用 `resolveImageSize(ratio, level)` 三档逐一比对存储值，精确命中返回对应档；无匹配（含旧值 "auto"/非法）返回 null（UI 显示占位）
- 关键输出已手算验证（像素/16 倍数/≤3840/偏差≤2%）：见上方输出表；2.35:1 2K = 2048×864（长边优先，偏差 0.87%）

### 二、新组件
**`frontend/shared/components/image-aspect-ratio-selector.tsx`**（基于 OptionSelect，样式与模型选择一致）
- `renderIcon={() => <Frame …/>}`（lucide Frame，size-3.5 text-muted-foreground，strokeWidth 1.7）
- 选项：7 个预设（值即比例串）+ 末尾「自定义…」（值 "custom"）
- 选中自定义时（value="custom" 或用户点自定义项）→ 打开 Dialog（components/ui/dialog）：
  - 输入框 placeholder「例如 16:9 或 2.35:1」，解析 "w:h"（无冒号视为 N:1）
  - 校验：格式非法 → 错误提示；比例 > 3:1 → 提示超支持范围（gpt-image-2 限制）
  - 确认 → `resolveImageSize(customRatio, 当前档位)` → onChange("WxH")
- value 由 `inferAspectRatio` 反推；自定义当前值显示推导出的比例串（如 "1.85:1"）
- props：`value/disabled/className/onChange`（透传）

**`frontend/shared/components/image-resolution-selector.tsx`**
- `renderIcon={() => <Monitor …/>}`
- 选项：1K/2K/4K，label 动态显示 `2K · 2048×1152`（随当前比例实时重算，useMemo）
- value = `inferResolutionLevel`；onChange(level) → `resolveImageSize(当前比例, level)` → onChange("WxH")

**`frontend/shared/components/image-quality-selector.tsx`**（改）：新增 `renderIcon={() => <Gem …/>}`

### 三、修改
- **`frontend/shared/components/reasoning-effort-selector.tsx`**：L57-59 `option?.value ? undefined : <Zap/>` → 恒返回 `<Zap/>`（任何选择都显示图标）
- **`frontend/features/chat/components/sections/chat-input.tsx`**：
  - L1154 推理强度 className 追加 ghost 类：`border-transparent bg-transparent hover:bg-accent/60 dark:border-transparent dark:bg-transparent dark:hover:bg-accent/40`（与生图选择器/模型选择一致）
  - L446-475：`imageSizeValue` 保留（存储原样 "WxH"），新增 `imageAspectRatioValue = inferAspectRatio(options.size)`、`imageResolutionLevelValue = inferResolutionLevel(options.size)`；两个 onChange：读当前另一维度（缺省：比例默认 "1:1"、档位默认 "2k"）→ resolveImageSize → `setModelOptionNestedValue(options, "size", value)`；删除键逻辑保留（仅清空值分支）
  - L1164-1197：ImageSizeSelector 替换为 ImageAspectRatioSelector + ImageResolutionSelector（三者各自 Tooltip，保留 ghost className 与 max-w），ImageQualitySelector 不动（仅组件内加图标）
- **`frontend/i18n/messages/{zh-CN,en-US}/chat.json`**：删除 `imageSize.*`（无其他引用），新增 `imageAspectRatio`（title/description/custom.title/custom.description/custom.placeholder/custom.invalid/custom.tooWide）、`imageResolution`（title/description）；取消/确认复用公共键

### 四、删除
- `frontend/shared/components/image-size-selector.tsx`（先 grep 确认仅 chat-input 引用）

### 五、验证（按既定约束）
1. 临时 node 脚本断言算法全组合输出（21 组预设 + 自定义 1.85:1/21:9/1:2.35/3:1），跑完即删
2. `frontend/`：`./node_modules/.bin/biome lint <改动文件>`、`npx tsc --noEmit`
3. `NEXT_PUBLIC_API_BASE_URL=http://localhost:18080 pnpm build` → 复制 out 到 backend/frontend/out
4. 浏览器回归（IAB/Playwright，class 断言）：推理强度任何档位均见 Zap、无 hover 无边框/填充；生图三选择器均有图标（Frame/Monitor/Gem）；比例列表 7 预设 + 自定义；分辨率列表 1K/2K/4K 带计算尺寸；选 16:9 + 2K → options.size="2048x1152"；自定义 2.35:1 + 1K → "1280x544"；非法输入报错；发送消息 payload 含 size
5. 提交 custom 分支（**不 push**、不含 .zcode/）；VPS 部署管线（docker build --build-arg GIT_COMMIT → tag deeix-chat-custom → save/load → compose up -d → curl http://127.0.0.1:8088/api/v1/version 验证），**绝不回滚**

### 不做的事
- 后端零改动（size/quality 透传链路已通）；不动 chat-model-config.tsx 预设；不加 auto 档位（按需求仅 1K/2K/4K；旧值 "auto" 显示占位不覆盖，直到用户重新选择）