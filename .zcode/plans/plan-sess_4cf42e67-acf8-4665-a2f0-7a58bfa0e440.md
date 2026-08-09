# 制品体验修复与增强（tips 统一 / AI 保存增强 / 域名补齐 / 缩略图 / 宽度模式）

## A. 保存按钮 Tooltip 统一
- `save-artifact-button.tsx` 的裸 `<button title=...>` 改为 Tooltip 组件（`TooltipTrigger asChild` + `TooltipContent side="bottom"`），与制品面板其他操作按钮（ArtifactActionButton）一致

## B. AI 保存制品（save_artifact 工具已存在 → 增强）
探索确认 `save_artifact`/`list_artifacts`/`share_artifact`/`delete_artifact` 平台工具已注册（含审批/审计/引导提示段落）。本次增强：
- `platformSaveArtifact` 的 CreateInput 补 `ConversationID: call.ConversationID`（AI 保存的制品关联当前会话）
- `platformShareArtifact` 的 share_url 用 `s.cfg.Snapshot().PublicWebBaseURL` 拼完整域名（后端已有该配置访问通道），返回绝对链接

## C. 分享链接补域名
- `artifactShareUrl(shareId, previewWidth?)`：客户端拼 `${window.location.origin}`（SSR fallback 相对路径，参照 use-conversation-share-dialog 模式），支持可选 `preview_width` 参数
- 管理页链接文本 / toast / 保存弹窗链接全部显示绝对 URL

## D. 制品管理页缩略图
- 后端 `ArtifactView` 加 `Code string json:"code"`（分页 20 条可接受）；前端 `ArtifactListItemDTO` 加 `code`
- `ArtifactsSection` 由列表改为**卡片网格**（max-w-5xl）：
  - `kind === "html"`：iframe srcdoc 渲染缩略图（h-28、`pointer-events-none`，真实渲染效果）
  - 其他类型：kind 图标占位块

## E. 管理页按钮 Tooltip
- share / revoke / openLink / delete 按钮全部用 Tooltip 包裹（复用现有 aria-label 文案作 tooltip）

## F. 预览宽度切换（全宽 / 固定 768px 居中）
- **聊天制品面板**（ChatArtifactPanel）：头部操作区加宽度切换按钮（Maximize2 图标），本地 state：full → `w-full`；fixed → iframe 容器 `max-w-3xl mx-auto` 居中
- **分享页**（public-artifact-page）：同样加切换按钮；初始值读 URL 参数 `preview_width=full|fixed`（分享者设置的默认），用户仍可手动切换
- **分享链接区块**（保存弹窗 + 管理页分享弹窗共用小组件 `ArtifactShareLink`）：宽度选择（全宽/固定）→ 链接实时带 `preview_width` 参数

## G. 管理页分享弹窗
- 分享按钮改为创建 share 后打开**分享弹窗**（绝对链接 + 宽度选择 + 复制 + 打开），替代 toast 显示链接；撤销分享仍为列表按钮

## i18n
- chat.artifacts / share.artifact：previewWidth / previewWidthFull / previewWidthFixed / togglePreviewWidth
- settings.chatPage.artifacts：分享弹窗标题等键

## 验证与部署
- 后端 build/test（docker deeix-godev）+ 前端 tsc + 生产构建
- 提交 custom 分支 → docker build/save/scp/load/compose → VPS 版本接口验证 buildTime