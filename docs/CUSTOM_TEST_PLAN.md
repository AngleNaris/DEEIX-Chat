# Custom 分支全面测试计划

## 1. 目标与基线

本计划用于证明 DEEIX Chat `custom` 分支的自定义能力在受支持环境中可用、可恢复、可隔离并可发布。发布证据只对应已提交的 exact SHA；未提交改动只能用于本地预检，不能形成发布候选。

每轮候选必须在 `verification.md` 和 release `manifest.env` 中记录 Custom HEAD、刷新后的 `upstream/dev`、Merge Base、分叉规模、镜像 ID 和制品校验和，避免测试计划因提交推进而保存过期 SHA。

- 发布基线：`verification.md` 与 release `manifest.env`
- 产品验收来源：`docs/CUSTOM_FEATURES_PRODUCT_SPEC.zh-CN.md`
- 安全与历史风险来源：`docs/CUSTOM_MODIFICATION_REVIEW.md`

本计划不把“进程启动”“端口监听”或“单元测试通过”单独视为功能正常。发布结论必须同时具备接口、数据、浏览器行为和运行环境证据。

本轮支持边界固定为：**一个 DEEIX 应用实例 + 多个并发用户**。覆盖同进程并发、A/B/admin 数据与权限隔离、应用进程重启、断线恢复和资源回收；明确不覆盖多个应用实例之间的分布式锁、跨实例路由、跨实例审批恢复或多 Worker 协调。后续若部署拓扑改为多实例，必须重新启用对应门槛，不能沿用本轮结论。

## 2. 测试层级

| 层级 | 目的 | 主要环境 | 发布要求 |
| --- | --- | --- | --- |
| T0 静态门槛 | 格式、类型、生成物、Compose、依赖一致性 | Windows 或 Linux CI | 必须通过 |
| T1 单元与契约 | 纯逻辑、错误边界、前后端载荷契约 | Node 24、Go 1.26.5 | 必须通过 |
| T2 数据与接口集成 | Repository、事务、权限、HTTP/SSE | 单应用实例，SQLite 和 PostgreSQL | 必须通过 |
| T3 Docker 组件集成 | Sandbox、MM、容器/文件/网络生命周期 | Linux Docker | 必须通过 |
| T4 浏览器端到端 | 真实用户旅程、状态、刷新和响应式 | Chromium，桌面和移动视口 | 必须通过 |
| T5 Provider 验收 | 模型、OCR、多模态、对象存储和外部服务 | 受控测试账号 | 发布相关能力时必须通过 |
| T6 故障与性能 | 单进程并发、应用重启、资源回收、P95/P99 | 隔离压测环境 | 生产发布前必须通过 |

## 3. 功能覆盖矩阵

| 功能域 | 核心测试 ID | T1 | T2 | T3 | T4 | T5/T6 |
| --- | --- | --- | --- | --- | --- | --- |
| 角色与导航 | ROLE-01~05 | 配置归一化 | CRUD/权限 | - | 创建、分组、拖拽、直聊、刷新 | 大列表性能 |
| Agent Group | AG-01~12 | 模型/快照/状态机 | 事务、幂等、租约 | 单实例生命周期 | 创建、运行、停止、恢复、重试 | 同进程并发和故障注入 |
| 思考强度 | REASON-01~04 | 能力过滤 | 保存/继承 | - | 普通/角色/群组回显 | Provider 档位 |
| 图片生成与改图 | IMG-01~07 | 参数与附件合成 | 任务/计费/错误 | 文件交付 | 生成、连续改图、参考图排序 | 真实图片模型 |
| 技能与技能包 | SKILL-01~08 | ZIP/清单/限制 | 预览、导入、替换、回滚 | 对象存储补偿 | 上传到调用 | 大包与恶意包 |
| 动态提示词与脚本 | PROMPT-01~08 | 长度、类型、超时、输出上限 | CRUD/属主/缓存 | - | 插入、运行、失败反馈 | 并发与资源上限 |
| 记忆与卡片 | MEM-01~09 | 绑定和召回规则 | 用户隔离、配额、异步写入 | - | 创建、绑定、召回、不召回 | 并发配额 |
| 制品 | ART-01~10 | 类型、缩略图协议 | CRUD/分享/撤销/权限 | 对象存储 | 预览、源码编辑、分享页 | 大内容与缓存失效 |
| 文件与媒体 | FILE-01~09 | MIME/大小/渲染映射 | 读写/权限/下载 | 导出链路 | 图片/音频/视频/附件 | 大文件和断点 |
| 平台工具与确认 | TOOL-01~12 | Registry/参数/脱敏 | 批准、拒绝、失败、审计、权限 | pending 重启失效/终态恢复 | 确认卡和管理员开关 | 并发副作用 |
| 凭据 | CRED-01~09 | 引用展开/流脱敏 | CRUD/轮换/属主 | - | 设置页和调用结果 | 实际服务调用 |
| Sandbox 任务空间 | SB-01~16 | 路径/身份/策略 | MCP 协议 | 容器、网络、scope、回收 | 任务状态/文件交付 | 并发与资源压测 |
| MCP 与多模态 | MM-01~14 | 路由/能力/JSON | 授权/附件/超时 | Gateway/隔离 | 当前及历史附件分析 | Qwen/Gemini/豆包 |
| 轨迹与连续性 | STREAM-01~10 | 事件合并/去重 | SSE 恢复 | 断连/重启 | 自动跟随、刷新、续接 | 长连接压力 |
| 管理配置 | ADMIN-01~06 | 配置校验 | 权限/保存/失败补偿 | - | 开关与用户入口一致 | 单实例聚焦刷新 |

## 4. 发布阻断测试

### 4.1 Agent Group

- `AG-01`：群组新会话、已有会话和排队提交均不得携带请求级模型；普通会话保留所选模型。
- `AG-02`：成员模型解析顺序固定为成员覆盖、角色默认、会话快照默认、平台默认；重试复用运行快照。
- `AG-03`：单进程内同一 `retryRequestID` 顺序重放和并发重放只产生一个 Attempt、一个计费引用和一次副作用。
- `AG-04`：不同 `retryRequestID` 只能在运行仍为可重试状态时产生下一 Attempt。
- `AG-05`：`BeginAgentGroupStepRetry` 必须拒绝 run A 与 step B 的错配，并回滚 Run、Step、Attempt。
- `AG-06`：同一应用进程内，同一会话被多个 goroutine 并发发起时最多存在一个非终态 Run。
- `AG-07`：Step、Attempt、Run 检查点任一步失败时事务整体回滚。
- `AG-08`：租约过期、心跳边界和同进程并发恢复调用下只恢复一次，状态同步且计数准确。
- `AG-09`：配置更新与启动并发时，快照必须全旧或全新，不得混合 Revision。
- `AG-10`：删除群组与创建会话/Run 并发时不得产生孤儿引用。
- `AG-11`：浏览器刷新后恢复运行，已完成步骤不重复，失败步骤可以重试。
- `AG-12`：分享内容遵循权限和脱敏规则，不泄露凭据或内部不可见数据。

### 4.2 平台工具、凭据与审批

- `TOOL-01`：`ask` 模式下每个写工具都返回 pending，批准前目标数据不变化。
- `TOOL-02`：批准和拒绝只能由记录属主执行，并且同一记录只能被处理一次。
- `TOOL-02A`：状态机固定为 `pending -> executing -> approved`；执行失败必须转为 `failed` 并写回持久化 ToolCall 轨迹，不能显示为 approved。approved、rejected、failed 终态刷新和应用重启后都必须恢复，且前端不得再次请求仅用于 pending 的进程内审批接口。
- `TOOL-03`：待批准记录只在当前应用进程内保留 30 分钟。应用重启或记录超时后，属主状态查询返回 404，历史确认卡自动显示“已失效/需重新发起”，不得继续显示可操作 pending；跨用户查询同样返回 404。
- `TOOL-04`：确认记录只保存凭据引用占位符，批准执行时才展开；摘要、消息、审计和日志不得出现明文。
- `TOOL-05`：凭据创建、更新和删除必须遵循统一 `auto`/`ask` 契约；`ask` 下批准前不得写入，确认记录和卡片只保留密钥引用，不得出现明文。
- `TOOL-06`：管理员关闭写操作后，已有 pending 记录也不能继续执行。
- `TOOL-07`：一次逻辑 MCP 调用在网络响应丢失后的所有重试必须复用同一 `call_id`；Sandbox replay cache 对同一身份的顺序/并发重放只执行一次。SSE 恢复和群组重试不得重复写工具副作用。
- `CRED-01`：列表、详情、确认卡、轨迹、分享和导出均不返回密钥明文。
- `CRED-02`：轮换后只使用新值；删除后返回不可用，不回退到旧缓存。
- `CRED-03`：跨用户读取、更新、删除和引用全部拒绝。

### 4.3 Sandbox 与 MM 隔离

- `SB-01`：API Key 缺失时拒绝启动；Bearer、HMAC、时间窗和 replay 均必须生效。
- `SB-02`：每个 scope 的 shared/imports 宿主目录创建不得跟随 symlink，bind 源必须位于受控根目录。marker 只用于发现挂错目录；shared/imports 根目录及父目录必须由服务账号独占写入。若宿主其他用户可替换父目录，则必须升级为受保护 staging mount 或等价 inode 固定方案。
- `SB-03`：scope A 容器无法枚举或读取 scope B；导出端也要复验真实路径。
- `SB-04`：URL、重定向、DNS rebinding、metadata、回环、私网和 IPv6 本地地址按策略拒绝。
- `SB-05`：`cwd`、文件路径、符号链接和 shell 元字符不能逃离 workspace。
- `SB-06`：并发 GetOrCreate 只创建一个 ready Session；失败创建不会发布半初始化状态。
- `SB-07`：Kill、Reset、Spawn、Sweeper 和 Shutdown 等待 active operation，回收完整容器和任务状态。
- `SB-08`：Docker 删除失败、进程取消和 PID 复用下状态可重试且不误杀其他任务。
- `SB-09`：MM 输入和输出使用 no-follow/beneath 约束，临时目录清理不越界。
- `SB-10`：Qwen 依赖由不可变提交构建，`tools/list` 与关键工具契约符合清单。

## 5. 浏览器关键旅程

浏览器测试使用隔离 SQLite 数据库和 admin、普通用户 A、普通用户 B 三个独立 Browser Context，至少覆盖 1440x900、768x1024 和 390x844。涉及写入的关键链路必须执行“浏览器写入 -> 刷新 -> 服务端读取回显”；跨用户隔离必须同时验证 owner success、other-user forbidden/not-found 和 missing not-found。

1. `E2E-01` 注册/登录后看到 Custom 导航入口和正确术语。
2. `E2E-02` 创建角色，配置模型/技能/工具/思考强度，直接发起对话并在刷新后保持。
3. `E2E-03` 创建群组，运行任务，查看成员轨迹，停止、刷新、恢复和重试。
4. `E2E-04` 创建技能并导入技能包，预览清单、替换版本并在对话中调用。
5. `E2E-05` 创建卡片并覆盖项目、角色、双重绑定和不匹配不召回。
6. `E2E-06` 生成制品，保存缩略图，编辑名称/类型/源码，分享并撤销链接。
7. `E2E-07` `ask` 模式下批准、拒绝写工具并注入一次执行失败，验证目标数据、审计和确认卡状态；刷新后终态不回退为 pending，应用重启后 approved/rejected/failed 仍从历史轨迹恢复。
8. `E2E-08` 新建、轮换、删除凭据，验证界面和聊天过程无明文。
9. `E2E-09` 上传图片、音频、视频和普通附件，验证预览、播放、下载和错误态。
10. `E2E-10` 生成图片后仅发送文本继续改图，多参考图排序保持提交顺序。
11. `E2E-11` 模拟 SSE 断连和页面刷新，验证回复续接、事件去重和自动跟随。
12. `E2E-12` 管理员开关群组/平台工具/写操作后，普通用户入口和权限立即一致。

重启场景在同一个候选环境中执行：先由用户 A 产生 approved、rejected 和 pending 三条独立轨迹，记录审批 ID、会话 ID 和目标数据；只重启应用容器，不替换 SQLite 数据卷；重新登录/刷新后确认 approved/rejected 终态保持、历史 pending 卡片变为失效、旧 pending ID 无法执行、用户 B 无法查询，随后重新发起并完成一次批准。重启前后都检查健康、版本、致命日志和目标数据。

每条旅程都必须检查加载态、空态、成功反馈、失败反馈、浏览器控制台、网络错误和刷新后的持久化结果。

## 6. 数据、权限与兼容性组合

### 6.1 数据库

- SQLite：本地开发、事务回滚、唯一索引和基础 CRUD。
- PostgreSQL：单应用实例下的并发 admission、`SKIP LOCKED`/租约、唯一约束、外键、删除竞态和真实迁移。
- 每个 Schema 变更都执行向前迁移、旧数据读取和回滚评估。

### 6.2 身份与权限

- 普通用户 A、普通用户 B、管理员三个身份。
- 所有用户资产至少执行 owner success、other-user forbidden、missing not-found 三组用例。
- 分享链接单独验证 private、public、revoked 和过期缓存。

### 6.3 上游兼容

- 每次刷新 `upstream/dev` 后记录 HEAD、merge base、ahead/behind 和生成物差异。
- 普通消息、审核、媒体生成、Agent Group 最终消息、API Contract 和 i18n 必须做语义回归。
- 禁止只以 Git 无文本冲突作为兼容通过依据。

## 7. 可执行命令

```powershell
# Frontend
pnpm --filter @deeix/web lint
pnpm --filter @deeix/web typecheck
pnpm --filter @deeix/web test
pnpm --filter @deeix/web build

# Agent Group 前后端契约
pnpm test:agent-group-contract

# API Contract，要求 Go 1.26.5 可用
pnpm api:check
pnpm --filter @deeix/api-contract typecheck

# Backend，要求 CGO、C compiler 和 libsqlite3-dev
Set-Location backend
go test ./... -count=1
go vet ./...
go build ./...

# PostgreSQL 单实例门槛；DSN 只通过临时环境变量注入，不写入仓库或日志
$env:DEEIX_TEST_DATABASE_DSN = "<ephemeral-postgres-dsn>"
go test ./internal/infra/persistence/schema -run '^TestMigrateCredentialNameIndexPostgres$' -count=1
go test ./internal/infra/persistence/postgres/agentgroup -run '^TestPostgresAgentGroup' -count=1
go test ./internal/infra/persistence/postgres/conversation ./internal/infra/persistence/postgres/billing -count=1
go test ./internal/infra/persistence/postgres -run '^TestEnsurePostgresVectorColumnPreservesLegacyVectors$' -count=1
Remove-Item Env:DEEIX_TEST_DATABASE_DSN

# Sandbox / MM，必须在 Linux 上执行 race
Set-Location tools/sandbox-mcp
go test -race ./... -count=1
go vet ./...
Set-Location ../mm-isolation
go test -race ./... -count=1
go vet ./...

# Compose 只读配置验证
docker compose -f docker-compose.yml config --quiet
docker compose -f docker-compose.full.yml config --quiet
docker compose -f docker-compose.sqlite.yml config --quiet
docker compose -f tools/sandbox-mcp/deploy/docker-compose.yml config --quiet

# T6 单实例 + 双用户负载门槛；账号与密码均为隔离环境临时值
$env:DEEIX_LOAD_BASE_URL = "http://127.0.0.1:18080"
$env:DEEIX_LOAD_USER_A = "<load-user-a>"
$env:DEEIX_LOAD_PASSWORD_A = "<load-password-a>"
$env:DEEIX_LOAD_USER_B = "<load-user-b>"
$env:DEEIX_LOAD_PASSWORD_B = "<load-password-b>"
$env:DEEIX_LOAD_CONCURRENCY = "100"
$env:DEEIX_LOAD_TOTAL_REQUESTS = "10000"
pnpm test:load
Remove-Item Env:DEEIX_LOAD_BASE_URL,Env:DEEIX_LOAD_USER_A,Env:DEEIX_LOAD_PASSWORD_A,Env:DEEIX_LOAD_USER_B,Env:DEEIX_LOAD_PASSWORD_B,Env:DEEIX_LOAD_CONCURRENCY,Env:DEEIX_LOAD_TOTAL_REQUESTS

# 压测前后分别记录；容器数必须回到基线，RestartCount 不得增加
docker stats --no-stream <candidate-container>
docker inspect <candidate-container> --format '{{.RestartCount}}'
docker ps -a --filter label=deeix.sandbox.scope --format '{{.ID}} {{.Names}} {{.Status}}'
```

`pnpm test:load` 默认门槛为 P95 <= 500ms、P99 <= 1000ms，可用 `DEEIX_LOAD_P95_LIMIT_MS` 和 `DEEIX_LOAD_P99_LIMIT_MS` 收紧。脚本只输出状态码、吞吐和延迟，不输出账号、密码或访问令牌。

## 8. CI 与报告要求

- `custom` push 和面向 `custom` 的 PR 必须触发前端、后端、API Contract、CodeQL 和 GHCR 镜像构建；当前 frontend-quality、CodeQL 和 GHCR 已覆盖 `custom`，GHCR manifest metadata 由 actionlint 1.7.7 验证。Docker Hub 不承担 custom 发布职责。
- 快速门槛运行 T0/T1；合并门槛增加 SQLite/PostgreSQL、race、Compose 和浏览器主链路。
- 夜间门槛运行恶意 ZIP、SSRF、同进程 Agent Group 并发、应用重启、Sandbox 生命周期、断线恢复和资源压测。
- Provider 测试使用受控凭据，仅记录 Provider Request ID、模型、耗时和脱敏错误，不保存用户敏感附件。
- 每次报告必须区分 Passed、Failed、Blocked、Not Run，不得把环境缺失记为代码通过或代码失败。

## 9. 发布退出条件

只有同时满足以下条件，才可以声明“自定义功能正常”：

1. T0/T1/T2/T3 全部通过，且没有未解释的跳过项。
2. 十二条浏览器旅程全部通过，桌面和移动视口无阻断问题。
3. Agent Group 幂等、事务、同进程并发，以及“重启后 pending 失效、已落库审批终态轨迹可恢复”的门槛全部通过。
4. 平台工具确认契约与产品文案一致，待批准操作具备明确的单实例重启失效语义。
5. Sandbox scope、网络、路径和生命周期隔离通过真实 Docker 验证。
6. 发布所启用的 Provider 均完成真实受控调用；未验证 Provider 必须保持关闭。
7. 至少完成一轮负载与资源回收测试，并记录 QPS、P95/P99、内存、容器数和泄漏结论。
8. 生成物、镜像 revision、健康接口和浏览器渲染对应同一候选 SHA。

VPS 制品构建、校验、切换和回滚按 `docs/CUSTOM_DEPLOYMENT.md` 执行。生产切换必须使用提交后的 exact SHA，不得从脏工作树直接构建。
