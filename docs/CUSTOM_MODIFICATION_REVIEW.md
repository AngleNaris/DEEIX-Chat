# Custom 自定义修改代码审查

## 1. 文档状态

- 审查日期：2026-08-12
- 审查对象：`custom` 分支
- Custom 提交：`f18c9be5b5e4f57e094ba9746a8b502dda7caba0`
- 对比上游：`upstream/dev`，提交 `2753b98e6a61c351e66e65c6e5f4323c753a1e37`
- Merge Base：`026c87718576526fb111c947e240d7db3897ced7`
- 分支差异：Custom 领先 93 个提交，落后 5 个提交
- 工作树状态：审查开始及结束时均干净
- 文档目的：记录当前定制修改的健壮性、安全性、性能、上游兼容性和多模态 MCP 演进结论
- 当前阶段：阶段 1（P0 全部 + sandbox 顺手 P1）修复完成，见第 14 节；阶段 2（Agent Group 一致性）与阶段 3（上游同步）未开始

本结论是上述提交快照的审查基线。后续修复、同步上游或重新生成代码后，应更新本文档中的状态和验证结果。

## 2. 总体结论

当前 `custom` 分支包含完整且有价值的产品能力，包括 Agent Group、平台工具、凭据、动态提示词、制品、文档卡、角色扩展、Sandbox MCP 和多模态 MCP 部署等。但当前版本仍存在发布阻断、安全隔离、一致性和部署可重建性问题。

结论如下：

1. 当前版本不适合直接发布到生产环境。
2. 根 Docker Compose 配置错误会阻断标准部署。
3. Sandbox MCP 存在命令注入、SSRF、跨会话文件读取和身份信任边界问题，应视为发布前必须修复的安全问题。
4. Agent Group 的单进程串行逻辑基本成形，但跨实例互斥、事务原子性、重试幂等和租约恢复仍不满足可靠生产运行要求。
5. 当前性能风险主要来自资源生命周期和无界内存状态，尚无负载测试数据，不能给出吞吐或延迟结论。
6. 最新上游可以同步，但消息发送、媒体生成、Agent Group 和 Omni Moderation 存在语义重叠，必须手工合并。
7. 将现有 Qwen 多模态 MCP 改造成 DEEIX 专用、Provider Neutral 的多模态 MCP Gateway 是可行且推荐的方案。
8. 新 Gateway 可以支持 Qwen、豆包和 Gemini，并通过后台配置切换 Provider 与模型，不需要为每个厂商改造 DEEIX 前端工具协议。

## 3. 风险分级

| 等级 | 定义 | 处理要求 |
| --- | --- | --- |
| P0 | 发布阻断、可利用安全问题、部署不可重建 | 发布或开放服务前必须修复 |
| P1 | 高概率一致性故障、资源泄漏、多实例错误 | 上游同步和功能扩展前优先修复 |
| P2 | 局部正确性、用户体验、维护性和工具链问题 | 在主风险收敛后修复 |

## 4. P0 发布阻断与安全问题

### P0-01 根 Docker Compose 无法解析

证据：

- `docker-compose.yml:20-24`
- `deeix-mcp-shared` 下重复定义两个 `name` 字段。
- `docker compose config` 实测解析失败。

影响：

- 标准 Compose 部署无法启动。
- 新环境、CI 和灾难恢复无法依赖仓库配置重建服务。

建议：

1. 将 `app_storage`、`deeix-mcp-shared` 和其他卷拆成独立定义。
2. 修复后执行 `docker compose config`。
3. 同时验证 `docker-compose.full.yml`、`docker-compose.sqlite.yml` 和 Sandbox 部署 Compose。

### P0-02 Sandbox 下载工具存在 Shell 命令注入

证据：

- `tools/sandbox-mcp/download.go:46-48`
- `tools/sandbox-mcp/download.go:59-61`

外部传入的 URL 被 `fmt.Sprintf("%q", target)` 拼入 `/bin/sh -c`。Go `%q` 生成的双引号字符串仍允许 Shell 展开命令替换、变量和部分特殊字符，因此不能作为 Shell 参数转义函数。

影响：

- 攻击者可能构造 URL，在会话容器中执行额外命令。
- 该问题与 Sandbox 本身允许执行命令叠加后，会扩大文件和共享卷读取风险。

建议：

1. 不经 Shell 拼接 URL。
2. 使用参数数组直接调用 `curl`，或在 Go 中使用受控 HTTP Client。
3. 如必须经 Shell，统一使用经过测试的 POSIX 单引号转义函数。
4. 增加 `$()`、反引号、换行、引号和重定向注入测试。

### P0-03 Sandbox 下载工具允许 SSRF

证据：

- `tools/sandbox-mcp/download.go`
- 当前实现允许任意 HTTP/HTTPS 目标，并跟随重定向。

当前未阻止：

- 回环地址
- RFC1918 私网地址
- Link-local 地址
- 云厂商 metadata 地址
- IPv6 本地地址
- DNS Rebinding
- 重定向后进入受限地址

影响：

- 可探测内部服务。
- 可读取宿主、容器网络或云环境中的敏感端点。

建议：

1. 复用后端已有的 Outbound Policy 和安全拨号逻辑。
2. 对每次 DNS 解析和每次重定向重新验证目标地址。
3. 默认拒绝私网、回环、Link-local、metadata 和非 HTTP/HTTPS 协议。
4. 设置响应大小、连接、读取、重定向次数和总时长上限。

### P0-04 Sandbox `cwd` 使用错误的 Shell 转义

证据：

- `tools/sandbox-mcp/tools.go:198-201`

`cwd` 被 `%q` 拼入 `cd %q && ...`，存在与下载 URL 相同的 Shell 解释风险。

影响：

- 可通过异常目录字符串改变命令结构。
- 没有保证 `cwd` 位于当前会话工作区。

建议：

1. 使用 `sanitizeWorkspacePath` 验证目录。
2. 使用正确的 POSIX Shell 参数转义。
3. 最好通过 Docker Exec 的工作目录能力传递 `cwd`，避免执行 `cd`。

### P0-05 共享卷无法提供真实租户隔离

证据：

- `tools/sandbox-mcp/docker.go:84-89`
- 每个会话容器都把同一个 `deeix-mcp-shared` 卷完整挂载为 `/shared`。
- `SessionManager.SharedDir` 只返回约定路径 `/shared/<scope>`，没有挂载级隔离。

影响：

- 当前会话可以通过 Shell 枚举并读取其他用户或会话目录。
- README 中“互不可见”和“本会话专属”的描述与实际挂载边界不一致。

建议：

1. 每个 scope 使用独立 volume，或只将当前 scope 子目录 bind mount 到容器。
2. 多模态 Gateway 通过受控文件 Broker 读取文件，不直接挂载所有租户目录。
3. 为容器设置独立 UID/GID，并收紧目录权限。
4. 增加跨用户和跨会话读取的拒绝测试。

### P0-06 文件 API 可通过符号链接逃逸

证据：

- `tools/sandbox-mcp/files.go:173-188`
- 当前仅使用 `filepath.Clean` 和字符串前缀检查。

影响：

- 位于允许目录内的符号链接可以指向其他目录。
- 导出或读取操作可能访问当前 scope 外的文件。

建议：

1. 解析真实路径并逐级拒绝符号链接。
2. 使用 `openat`、`O_NOFOLLOW` 或等价的目录句柄约束。
3. 最终打开文件后再次验证实际 inode/path 仍位于允许根目录。

### P0-07 Sandbox 身份与鉴权边界不可信

证据：

- `tools/sandbox-mcp/server.go:27-36`
- `SANDBOX_MCP_API_KEY` 为空时鉴权完全关闭。
- `tools/sandbox-mcp/sessions.go:23-42` 直接信任请求 `_meta.user_id` 和 `_meta.conversation_id`。
- `tools/sandbox-mcp/tools.go:99-101` 明确将 `sandbox_ps` 定义为全部会话聚合视图。

影响：

- 能访问 MCP 地址的调用方可以伪造任意用户和会话身份。
- 任意已认证用户可获得其他租户的 scope、镜像和最近使用时间。

建议：

1. API Key 为空时拒绝启动，而不是关闭鉴权。
2. 使用服务到服务签名或短期 JWT，身份必须来自已验证声明。
3. 将 `_meta` 视为业务上下文，不作为独立身份凭据。
4. `sandbox_ps` 默认只返回当前用户或当前 scope；全局视图仅提供给管理员端点。

### P0-08 Qwen 多模态部署不可稳定重建

证据：

- `tools/sandbox-mcp/deploy/docker-compose.yml:45-91`
- 依赖直接固定为 GitHub `@main`。
- 部署引用 `qwen-mm-plugins[omni-av]` 和 `qwen-mm-plugins-omni-av`。
- 审查时 Qwen-MM-Plugins 官方 `main` 已提供 `qwen-mm-plugins-core` 和 `qwen-mm-plugins-api` 入口，不再包含当前部署引用的 `omni-av` 入口。

影响：

- 相同仓库提交在不同日期可能构建出不同镜像。
- 当前全新构建可能因不存在的 extra 或入口命令失败。

建议：

1. 将依赖固定到不可变 tag 或 commit SHA。
2. 按官方当前结构迁移为 `core` 和 `api`。
3. 在 CI 中真实构建并启动 Gateway 镜像。
4. 运行 `tools/list` 冒烟测试，确认工具数量和关键工具存在。

官方参考：

- Qwen-MM-Plugins：<https://github.com/QwenLM/Qwen-MM-Plugins>
- 当前项目元数据：<https://raw.githubusercontent.com/QwenLM/Qwen-MM-Plugins/main/pyproject.toml>

## 5. P1 健壮性与一致性问题

### P1-01 Sandbox Session 创建存在半初始化竞态

证据：

- `tools/sandbox-mcp/sessions.go:101-141`
- `GetOrCreate` 在 Docker 容器创建前就把 Session 放入 `m.live`。

影响：

- 并发调用可能拿到容器尚未创建完成的 Session。
- 一个请求可能在另一个请求初始化失败前开始执行。

建议：

- 使用 per-scope singleflight、创建状态和 ready channel。
- 只有创建成功后才发布 Session，或让等待者显式等待创建结果。

### P1-02 Kill 和 Reset 后 Session 仍保留在 `live`

证据：

- `tools/sandbox-mcp/sessions.go:203-227`

影响：

- 后续调用会命中已不存在容器的旧 Session。
- `sandbox_ps` 返回与 Docker 实际状态不一致的会话。
- Session 与其任务映射可能持续占用内存。

建议：

- 容器删除成功后原子地从 `live` 移除。
- Reset、Kill、创建失败、服务退出和空闲回收使用统一生命周期函数。

### P1-03 Sandbox 异常路径存在 panic

证据：

- `tools/sandbox-mcp/tools.go:271-275`
- 第二次 `execInContainer` 的错误被忽略，随后直接访问 `output.Stdout`。

影响：

- 容器消失、Docker 断开或 Exec 失败时可能 nil pointer panic，导致整个 MCP 服务退出。

建议：

- 检查 `err` 和 `output == nil`。
- 把容器不存在映射为任务中断或 Session 失效，不得 panic。

### P1-04 Sandbox 资源生命周期不完整

确认问题：

- 服务退出时没有遍历并回收本服务创建的会话容器。
- 后台任务限额在启动进程后检查，拒绝请求时可能留下进程。
- Kill/Reset 未完整处理任务进程组。
- 容器创建或启动失败可能留下部分资源。

建议：

1. SessionManager 实现 `Close(ctx)`，记录并清理本实例拥有的完整容器集合。
2. 后台任务先占用配额，再创建进程。
3. 保存进程组 ID，取消时终止完整进程组。
4. 所有创建流程使用补偿清理。

### P1-05 Docker ImagePull 响应未关闭和消费

证据：

- `tools/sandbox-mcp/docker.go:42-52`

影响：

- Docker Client HTTP 连接和响应资源泄漏。
- 镜像拉取结果未被消费，错误可能延迟暴露。

建议：

- 保存 `ImagePull` 返回的 reader，`defer Close()` 并完整消费响应。

### P1-06 配置的缓存卷名未生效

证据：

- `tools/sandbox-mcp/config.go` 提供 `CacheVolume`。
- `tools/sandbox-mcp/docker.go:77-82` 硬编码使用 `deeix-sandbox-cache-root`。

影响：

- 环境配置和实际运行不一致。
- 测试与部署可能写入非预期卷。

建议：

- 将 `CacheVolume` 传入 `containerSpec` 并只使用配置值。

### P1-07 Agent Group 的运行互斥只在单进程内有效

证据：

- `backend/internal/application/conversation/service.go:229`
- `backend/internal/application/conversation/agent_group_orchestrator.go:72-84`
- `backend/internal/application/conversation/agent_group_orchestrator.go:915-922`

当前先获取进程内 `sync.Map` Mutex，再查询 active run 并创建运行。不同后端实例拥有不同 Mutex，因此都可能通过 active 检查。

影响：

- 多实例、滚动发布或 Worker 并存时，同一会话可能创建多个运行。
- 串行执行约束失效，消息、计费和步骤状态可能相互覆盖。

建议：

1. 数据库增加“每个会话最多一个非终态运行”的约束或租约表。
2. 创建运行必须是单个事务中的原子抢占。
3. SQLite 和 PostgreSQL 都应具备确定行为，不依赖 PostgreSQL 专属锁。

### P1-08 Agent Group Step、Attempt 和 Run 检查点非原子

证据：

- `backend/internal/application/conversation/agent_group_orchestrator.go:384-462`
- 当前依次创建 Step、创建 Attempt、CAS 更新 Run。

影响：

- Attempt 创建失败会留下 running Step。
- Run CAS 失败会留下 Step 和 Attempt，但 Run 不指向它们。
- 恢复逻辑可能遇到孤儿或半完成检查点。

建议：

- 在 Repository 中提供事务方法，原子完成未完成步骤守卫、限额检查、Step、Attempt 和 Run CAS。
- CAS 无更新时回滚整个事务。

### P1-09 重试请求幂等键未生效

证据：

- HTTP 请求接收 `retryRequestID`。
- `backend/internal/application/conversation/agent_group_control.go:502-507` 创建 Attempt 时总是生成新 UUID。

影响：

- 客户端超时重放或跨实例双击可能创建重复重试。
- 数据库唯一索引无法阻止同一客户端请求的重复执行。

建议：

- 使用已验证并规范化的客户端 `retryRequestID`。
- 幂等键唯一范围应与业务语义一致，例如 `(step_id, retry_request_id)`。
- 重复请求返回原 Attempt，而不是通用冲突。

### P1-10 租约恢复可能选择错误步骤并报告错误数量

证据：

- `backend/internal/infra/persistence/postgres/agentgroup/repository_run.go:309-371`
- 查询步骤时没有按 `sequence` 排序。
- `stepByRun` 取数据库返回的第一个元素。
- 更新 Run 后没有检查 `RowsAffected`，但仍递增 `affected`。
- Attempt 被标记 interrupted 后，对应 Step 状态没有同步。

影响：

- 同一运行多个异常 Attempt 时可能恢复到非预期步骤。
- Worker 报告的恢复数量可能高于实际成功恢复数量。
- Run、Step 和 Attempt 状态不一致。

建议：

1. 明确恢复规则并按 `sequence`、AttemptNo 排序。
2. 同一事务中同步 Attempt、Step 和 Run。
3. 只在 CAS `RowsAffected == 1` 时计入 recovered。
4. 增加多个过期 Attempt 和并发恢复测试。

### P1-11 Agent Group 配置修改和删除存在一致性窗口

确认问题：

- 主管切换缺少强制“每组仅一个 supervisor”的数据库约束。
- Group Revision 更新没有完整事务或 CAS。
- 删除前检查历史再删除存在 TOCTOU。
- 模型中缺少足够的外键保护，可能产生孤儿引用。
- 部分 Raw `Table().Scan()` 查询可能绕过 GORM 软删除过滤。

建议：

- 将主管切换、Revision 更新和删除保护移入事务。
- 添加必要外键、唯一约束和明确的软删除条件。
- 为并发主管切换、创建历史与删除竞态增加集成测试。

## 6. P1 性能风险

本轮没有进行负载测试，因此不对 QPS、P95、P99、CPU 或内存峰值作数值结论。以下是由资源生命周期直接确认的性能风险。

### P1-PERF-01 Agent Group 锁表无界增长

证据：

- `agentGroupRunLocks` 使用 `sync.Map`。
- `agentGroupRunMutex` 只有 `LoadOrStore`，没有删除路径。

影响：

- 服务运行期间每出现一个新 ConversationID 就永久保留一个 Mutex。

建议：

- 使用带引用计数的 keyed mutex，并在无人等待和运行结束后删除。
- 或将互斥完全下沉到数据库租约，避免长期进程内状态。

### P1-PERF-02 Sandbox Session Map 无界增长

证据：

- Kill/Reset 后 Session 不从 `live` 移除。
- List 遍历全部 `live` Session。

影响：

- 长期运行后内存、锁竞争和管理操作成本随历史会话增长。

建议：

- 修复生命周期，并增加 TTL/最大 Session 数量和定期回收指标。

### P1-PERF-03 MCP 超时配置层级冲突

证据：

- 后端默认 `mcp_tool_timeout_seconds=10`。
- 注册脚本将全局值写为 300 秒。
- Qwen 环境变量默认聊天超时为 600 秒。
- MCP Server 数据模型没有 server/tool 级超时字段。

影响：

- 默认环境可能在多模态模型完成前终止请求。
- 将全局超时提高到 300 或 600 秒又会让普通轻工具长期占用资源。

建议：

采用以下层级：

1. MCP discovery/list tools：10 秒。
2. Server 默认执行超时。
3. Tool 单独覆盖。
4. Provider connect/read/stream 超时。
5. 异步任务提交、轮询和总截止时间。

## 7. P2 正确性与维护性问题

### P2-01 Dynamic Prompt 局部更新会重置字段

证据：

- `backend/internal/application/dynamicprompt/service.go:74-107`
- 更新仍要求非空 `name`。
- 缺少 `kind` 时回退为 JS。
- 缺少 `content` 时写入空内容。
- 缺少 `enabled` 时写入默认 true。
- `MaxNameLen` 和 `MaxContentLen` 未在 Upsert 路径完整执行。

建议：

- 将创建与 Patch 更新拆分。
- 更新输入使用指针字段，仅修改调用方显式提交的字段。
- 执行名称和内容长度限制。

### P2-02 Credential 局部更新会清空 description

证据：

- `backend/internal/application/credentials/service.go:156-188`
- `Description` 无论是否提交都会 Trim 后写入 Patch。

影响：

- 只修改名称、类型或密钥时会意外删除描述。

建议：

- Patch DTO 使用 `*string` 区分未提交和显式清空。

### P2-03 Credential 密钥语义可能被 Trim 判断篡改

证据：

- 更新是否写入密钥通过 `strings.TrimSpace(value) != ""` 判断。
- 实际加密使用原始 `value`。

风险：

- 仅包含空格的合法值无法保存。
- 创建和更新对空白值的语义不够明确。

建议：

- 明确 secret 是否允许空白和首尾空格。
- 使用显式 `hasValue` 或指针字段表达“修改密钥”。

### P2-04 用户内唯一索引定义不一致

证据：

- `backend/internal/infra/persistence/models/credential.go:8-10`
- `backend/internal/infra/persistence/models/dynamic_prompt.go:8-9`

Credential 的 `UserID` 没有加入 `idx_chat_credentials_user_name`；Dynamic Prompt 的 `Name` 使用复合索引名，但 `UserID` 只定义了另一普通索引。

影响：

- 实际 Schema 可能变成全局名称唯一，而不是用户内唯一。

建议：

- `UserID` 与 `Name` 使用相同 `uniqueIndex` 名称和明确 priority。
- 在 SQLite 与 PostgreSQL 上检查真实索引 DDL。

### P2-05 首次生成不会进入侧栏“进行中”集合

证据：

- `frontend/features/chat/components/app-chat-area.tsx:313-320`
- `setConversationStreaming` 只根据 `resumingRunID` 更新。

影响：

- 正常首次发送的活跃流不会出现在 Recent/Starred 的进行中状态。

建议：

- 使用当前 active run 或 stream 状态，而不是只依赖恢复运行 ID。

### P2-06 Role 保存状态未传给 Dialog

证据：

- `frontend/features/roles/components/role-dialog.tsx:614`
- `frontend/features/layouts/components/navigation/nav-roles.tsx:465`
- `frontend/features/layouts/components/navigation/nav-roles.tsx:1249`

Dialog 内 `submitting` 绑定目录加载状态，父组件 API 提交状态未传入。

影响：

- 保存请求期间按钮可重复点击。
- 创建 Role 时可能产生重复请求和重复记录。

建议：

- RoleDialog 接收真实 `submitting` Prop，并统一禁用所有会触发 mutation 的入口。

### P2-07 本地 API Contract 生成脚本硬编码另一个 checkout

证据：

- `packages/api-contract/scripts/generate.local.mjs:252`
- Docker volume 固定为 `C:/_MY_WORK/DEEIX-Chat/backend:/app`。

影响：

- 在 `X-DEEIX-custom` 中执行本地生成脚本时，可能从另一个工作树生成契约。

建议：

- 使用 `import.meta.url` 或当前仓库根动态解析路径。
- 禁止脚本引用仓库外固定绝对路径。

### P2-08 应用层 DTO 携带 JSON Tag

证据：

- `backend/internal/application/conversation/service_share.go:54-99`
- 后端分层测试会拒绝 Application 层协议 Tag。

影响：

- `go test ./...` 无法全绿。
- Application 与 HTTP/JSON 协议耦合。

建议：

- 将公开响应 DTO 移到 Transport 层，Application 返回协议无关 View。

## 8. 最新上游兼容性

### 8.1 当前差异

| 项目 | 结果 |
| --- | --- |
| Custom HEAD | `f18c9be5` |
| Latest upstream/dev | `2753b98e` |
| Merge Base | `026c8771` |
| Custom 独有提交 | 93 |
| 上游待同步提交 | 5 |
| Custom 相对 Merge Base | 358 files，约 `+54695/-3931` |
| 最新上游改动 | 103 files，约 `+12565/-489` |

最新上游主要加入异步 Omni Moderation 和内容审核边界加固：

- `7b584290`：异步 Omni Moderation
- `11c33e80`：审核边界和输入验证加固
- `69559c9f`：审核失败处理和 Provider 边界加固
- 其余为合并提交

### 8.2 预计文本冲突

预计至少涉及以下 7 组：

1. `.gitignore`
2. `backend/internal/app/app.go`
3. `backend/internal/application/conversation/service.go`
4. `backend/internal/transport/http/server.go`
5. `frontend/features/chat/hooks/use-chat-message-submit.ts`
6. `frontend/i18n/messages.ts`
7. `frontend/i18n/messages/en-US/admin-users.json` 与 `zh-CN/admin-users.json`

### 8.3 语义冲突

即使 Git 不报告文本冲突，也必须人工检查：

- 消息发送前后的审核状态和补偿逻辑
- 媒体生成结果的审核
- Custom 工具阶段结果合并
- `generateImagesForTool`
- Agent Group 内部 Actor 和最终主管消息
- Conversation、Message、DTO 和 API Contract 新字段
- 前端 blocked/moderation 状态
- i18n 错误映射

### 8.4 推荐同步顺序

1. 先修复 P0 Docker 和 Sandbox 安全问题。
2. 创建 Custom 备份分支并确认工作树干净。
3. `git fetch upstream --prune`。
4. 在隔离工作树预演合并。
5. 按 Domain/Model 和迁移开始合并。
6. 合并 Repository 和 Service。
7. 合并消息发送、媒体生成和审核补偿。
8. 合并 App、Server 和后台 Worker 装配。
9. 重新生成 DTO、Swagger 和 API Contract。
10. 合并前端发送流程、错误状态和 i18n。
11. 执行完整后端、前端、Compose 和浏览器验证。

禁止：

- 对核心冲突文件整文件选择 `ours` 或 `theirs`。
- 用上游 `service.go` 覆盖 Custom 工具阶段和 Agent Group 装配。
- 用 Custom 旧消息发送逻辑覆盖上游审核补偿。
- 未经验证直接 push。

## 9. 多模态 MCP 改造评估

### 9.1 可行性结论

可以将现有 Qwen 多模态 MCP 改造成 DEEIX 专用服务，并支持切换到豆包、Gemini 或其他模型。

原因：

- DEEIX MCP Client 已使用通用 Streamable HTTP JSON-RPC。
- 客户端核心只依赖 `initialize`、`tools/list`、`tools/call`、Base URL、Token 和 Timeout。
- Qwen 专属环境变量、包名和工具实现主要集中在部署层。
- DEEIX 前端选择的是 MCP 工具，不需要理解底层 Provider SDK。

相关代码：

- `backend/internal/infra/mcp/client.go:77-115`
- `backend/internal/application/mcp/service.go`
- `backend/internal/application/conversation/service_mcp_tools.go`
- `tools/sandbox-mcp/deploy/docker-compose.yml`

### 9.2 不推荐直接 Fork Qwen 插件作为长期主线

直接 Fork 的问题：

- 工具名和返回结构容易继续绑定 Qwen。
- 上游包结构变化会反复造成部署漂移。
- 豆包和 Gemini 的文件上传、媒体限制、异步任务和 usage 结构不同。
- 难以实现统一的能力探测、路由、熔断和审计。

推荐新建独立服务：

`deeix-mm-gateway`

该服务以 MCP 作为对 DEEIX 的稳定边界，以 Adapter 作为对模型厂商的变化边界。

### 9.3 推荐架构

```text
DEEIX Chat
    |
    | MCP Streamable HTTP
    v
deeix-mm-gateway
    |
    +-- Capability Registry
    +-- Media Resolver / File Broker
    +-- Request Normalizer
    +-- Response Normalizer
    +-- Provider Router
    +-- Timeout / Retry / Circuit Breaker
    +-- Audit / Metrics / Health
          |
          +-- Qwen Adapter
          +-- Doubao Ark Adapter
          +-- Gemini Adapter
```

### 9.4 稳定工具契约

建议优先提供少而稳定的 DEEIX 工具，而不是暴露每个厂商的全部原子 API：

- `mm_analyze_image`
- `mm_ocr`
- `mm_locate_objects`
- `mm_analyze_video`
- `mm_transcribe_audio`
- `mm_caption_media`
- `mm_analyze_document`
- `mm_capabilities`

每个工具都应接受：

- 当前 scope 下的文件引用
- 可选任务提示词
- 可选语言
- 管理员允许范围内的 Provider/模型覆盖
- 输出格式和最大输出限制

### 9.5 Provider Adapter 契约

建议内部接口至少包含：

```go
type ProviderAdapter interface {
    Capabilities(ctx context.Context) (Capabilities, error)
    AnalyzeImage(ctx context.Context, req ImageRequest) (MMResult, error)
    AnalyzeVideo(ctx context.Context, req VideoRequest) (MMResult, error)
    TranscribeAudio(ctx context.Context, req AudioRequest) (MMResult, error)
    AnalyzeDocument(ctx context.Context, req DocumentRequest) (MMResult, error)
    Health(ctx context.Context) error
}
```

Adapter 负责：

- 厂商鉴权
- 文件上传或 Files API
- MIME 和大小限制转换
- 同步与异步任务差异
- 模型请求格式
- usage 和错误归一化
- Provider Request ID

### 9.6 Capability Manifest

每个 Provider/模型必须通过 Manifest 声明真实能力，不能只根据厂商名称推断：

```yaml
provider: gemini
model: configured-model-id
modalities:
  image: true
  video: true
  audio: true
  document: true
features:
  function_calling: true
  timestamps: false
  grounding_boxes: false
execution:
  mode: sync
  max_input_bytes: 52428800
  timeout_seconds: 300
credential_ref: mm-gemini-primary
```

至少包含：

- Provider
- 模型 ID
- 支持模态
- 同步或异步模式
- 文件大小与数量限制
- 支持的 MIME
- 上下文限制
- 默认和最大超时
- 是否支持 Function Calling
- 是否支持时间戳、检测框或结构化输出
- Credential Reference
- Readiness 状态

### 9.7 模型切换方式

推荐支持三级路由：

1. 平台默认 Provider 和模型。
2. 按工具或能力配置默认路由。
3. 管理员允许的请求级覆盖。

示例：

```yaml
routes:
  mm_analyze_image:
    provider: qwen
    model: configured-qwen-vl-model
  mm_analyze_video:
    provider: gemini
    model: configured-gemini-model
  mm_transcribe_audio:
    provider: doubao
    model: configured-doubao-audio-model
```

限制：

- 不允许模型自由填写任意 Base URL。
- 不允许模型访问任意 Credential。
- Provider 和模型必须来自管理员允许列表。
- 切换前执行 Capability 与 Readiness 校验。

### 9.8 文件与租户隔离

Gateway 实施前必须先修复当前 `/shared` 隔离问题。

推荐：

1. DEEIX 或 File Broker 发放短期、scope-bound 文件令牌。
2. Gateway 通过令牌读取当前用户当前会话文件。
3. Provider Adapter 再上传到对应厂商。
4. 不让 Provider Gateway 直接挂载全部租户共享卷。
5. 临时上传文件必须有 TTL、删除任务和审计记录。

### 9.9 Qwen、Gemini 与豆包接入结论

#### Qwen

- 保留 Qwen 作为首个 Adapter，迁移当前工具行为。
- 本地媒体处理可以继续复用 `core` 能力。
- 云端视觉、音频和视频能力迁移到官方当前 `api` 入口。
- 依赖必须固定 tag 或 commit SHA。

#### Gemini

- 官方 Gemini API 提供图片、音频、视频、文档、Files API 和 Function Calling 能力。
- 适合作为首个第二 Provider，用于验证 Gateway 的文件上传和长媒体处理抽象。
- 不同模型能力与限制不同，必须通过 Manifest 和启动探测确认。

官方参考：

- <https://ai.google.dev/gemini-api/docs/image-understanding>
- <https://ai.google.dev/gemini-api/docs/video-understanding>
- <https://ai.google.dev/gemini-api/docs/audio>
- <https://ai.google.dev/gemini-api/docs/document-processing>
- <https://ai.google.dev/gemini-api/docs/function-calling>

#### 豆包

- 火山方舟当前模型体系可提供多模态理解和 Function Calling。
- 可作为 Adapter 接入统一 Gateway。
- 具体图片、视频、音频、文件接口和限制必须按实际启用的模型 ID 核验，不能把某个模型的能力推广到全部豆包模型。
- 建议在接入阶段通过官方 SDK/API 做 Capability Probe，并把结果写入 Manifest。

官方参考：

- <https://www.volcengine.com/docs/82379>

### 9.10 推荐实施顺序

1. 固化现有 Qwen 工具清单、参数、返回结果和附件行为。
2. 建立 Provider Neutral Gateway 骨架。
3. 实现安全文件 Broker 和 scope-bound 文件引用。
4. 实现 Qwen Adapter，作为行为兼容基线。
5. 实现 Gemini Adapter，验证多媒体与 Files API 抽象。
6. 实现豆包 Adapter，按实际开通模型做能力探测。
7. 增加后台 Provider、模型、Credential、路由和超时配置。
8. 增加契约测试、录制响应测试和三家 Provider 冒烟测试。
9. 灰度替换当前 `qwen-mm-core` 和 `qwen-mm-omni-av` 注册。
10. 稳定后移除旧部署入口。

## 10. 验证结果

| 验证项 | 结果 | 说明 |
| --- | --- | --- |
| Custom 工作树 | 通过 | 与 `origin/custom` 一致，审查时干净 |
| Frontend TypeScript | 通过 | `tsc --noEmit` 通过 |
| Frontend Build | 通过 | 构建成功，37 个静态路由生成 |
| Frontend Lint | 失败 | 包含条件调用 Hook、依赖缺失和字幕规则等问题 |
| Backend `go test ./...` | 失败 | 绝大多数通过，分层测试因 Application DTO JSON Tag 失败 |
| Backend `go vet ./...` | 通过 | 未发现 vet 问题 |
| Sandbox `go test ./...` | 通过 | 当前测试未覆盖上述安全和故障路径 |
| Sandbox Race | 通过 | 当前测试范围内通过 |
| Sandbox `go vet ./...` | 通过 | 未发现 vet 问题 |
| API Contract Typecheck | 通过 | 生成类型自身可检查 |
| `pnpm api:check` | 失败 | 版本同步脚本报告 3 个文件不同步 |
| `docker compose config` | 失败 | 根 Compose 重复 volume `name` |
| `git diff --check` | 失败 | 3 个空白问题 |
| 上游预冲突审查 | 完成 | 识别 7 组主要文本冲突和多处语义重叠 |
| 负载测试 | 未执行 | 不提供吞吐、延迟和资源峰值结论 |
| Qwen/豆包/Gemini 真实调用 | 未执行 | 需要有效凭据和受控测试环境 |

## 11. 修复优先级

### 阶段 0：冻结发布

- 保持 Agent Group Feature Flag 默认关闭。
- Sandbox MCP 不对不可信网络开放。
- 不使用当前根 Compose 执行生产发布。

### 阶段 1：修复 P0

1. ✅ 修复根 Compose。
2. ✅ 修复下载命令注入和 SSRF。
3. ✅ 修复 `cwd` 路径约束。
4. ✅ 重做共享文件隔离和符号链接防护。
5. ✅ 强制 Sandbox 服务鉴权和可信身份。
6. ✅ 固定 Qwen 依赖并迁移失效入口。

### 阶段 2：修复 Agent Group 一致性

1. 数据库级 active run 互斥。
2. Step、Attempt、Run 原子事务。
3. 重试幂等键。
4. 租约恢复状态同步。
5. 主管、Revision、删除和外键约束。
6. 多实例和故障注入测试。

### 阶段 3：同步最新上游

- 手工合并 Omni Moderation。
- 重新生成 Swagger 与 API Contract。
- 完整执行 Backend、Frontend 和浏览器回归。

### 阶段 4：多模型 MCP Gateway

- Qwen Adapter。
- Gemini Adapter。
- 豆包 Adapter。
- Capability、Provider 路由、Credential Reference 和分层超时。

### 阶段 5：性能与运维验证

- Agent Group 并发和恢复压测。
- Sandbox Session、任务、卷和容器生命周期压测。
- 大文件和长音视频调用。
- Provider 限流、超时、熔断和降级。
- 内存、容器数量、连接和文件 TTL 指标。

## 12. 发布验收门槛

在以下条件全部满足前，不建议发布：

- 所有 P0 已修复并有回归测试。
- `docker compose config` 全部通过。
- Backend、Frontend、Sandbox 测试和 Lint 达到项目门槛。
- Agent Group 多实例下不能为同一会话创建两个非终态运行。
- 故障注入后 Run、Step、Attempt 状态保持一致。
- Sandbox 无法访问其他 scope、私网目标和符号链接外部文件。
- Sandbox API Key 缺失时拒绝启动。
- Qwen 多模态镜像可从不可变依赖重建。
- 最新上游审核逻辑已在普通消息、媒体生成和 Agent Group 最终消息中验证。
- 完成代表性浏览器关键路径测试。
- 完成至少一轮负载和资源回收验证。

## 13. 当前未执行事项

- 未合并 `upstream/dev`（阶段 3）。
- 未执行生产部署（阶段 1 修复待上线验证）。
- 未使用真实 Qwen、豆包或 Gemini 凭据调用。
- 阶段 2 的 Agent Group 一致性（P1-07~11）、sandbox 生命周期（P1-01/02/04）、性能项（P1-PERF-*）与 P2 项未开始。

## 14. 阶段 1 修复记录（2026-08-13）

修复提交：`e6386ddc`（fix(sandbox,security,deploy): 审查阶段1 P0 全修复 + sandbox 顺手 P1）

### 14.1 P0-01 根 Compose —— 已修复

- 修改：`docker-compose.yml`（volumes 段去重 name、`app_storage` 补 name），`docker-compose.full.yml` / `docker-compose.sqlite.yml` 补 app 的 `/shared` 只读挂载与 sandbox env 透传。
- 验证：三份 `docker compose config --quiet` 全部通过。

### 14.2 P0-02 下载 Shell 注入 —— 已修复

- 修改：`tools/sandbox-mcp/download.go` 整体重写，删除全部 `curl` / `/bin/sh -c %q` 拼接；下载在 Go 侧执行，存文件复用 `writeWorkspaceBytes`（python stdin 解码）管道。
- 测试：`main_test.go` 新增 `$()`、反引号、换行、引号、重定向等注入载荷用例（非法 URL 拒绝；合法主机 + 路径内载荷安全通过——不再经过任何 Shell）。

### 14.3 P0-03 下载 SSRF —— 已修复

- 修改：`tools/sandbox-mcp/outbound.go`（拷贝自 `backend/internal/shared/security/outbound.go`，含 DNS rebinding 防护 dialer，注释标明 canonical 源）；`validateFetchURL` 接入策略校验；`fetchDownload` 用 `NewOutboundHTTPClient` + 每跳重定向复验 + 响应/重定向/超时上限。
- 配置：新增 `SANDBOX_ALLOWED_HOSTS` / `SANDBOX_ALLOWED_CIDRS`（默认空 = 拒绝全部私网/回环/link-local/metadata）。
- 测试：私网/回环/link-local/metadata/IPv6 本机/白名单放行用例。
- 剩余风险：出站策略与后端 canonical 源存在拷贝分叉风险（源更新时需同步，outbound.go 头部已注明）。

### 14.4 P0-04 cwd Shell 转义 —— 已修复

- 修改：`handleExec` 的 cwd 先过 `sanitizeWorkspacePath`（强制位于 /workspace），经 `container.ExecOptions.WorkingDir` 原生传递，删除 `cd %q && ...` 拼接。
- 测试：注入/越界拒绝、合法 cwd 生效（单元层覆盖路径校验逻辑）。

### 14.5 P0-05 共享卷租户隔离 —— 已修复（宿主目录 per-scope bind mount）

- 修改：`SessionManager.createSessionContainer` 在宿主目录 `SANDBOX_SHARED_HOST_DIR/<scope>`（0o755，scope 白名单正则校验）预先建目录；会话容器只 bind 挂载自己 scope 子目录到 `/shared/<scope>`，其他租户目录物理不可见。mm 网关与后端挂整目录只读（`deploy/docker-compose.yml`、三份根 compose）。
- 部署变更：`deeix-mcp-shared` 命名卷废弃；旧数据迁移 `docker run --rm -v deeix-mcp-shared:/data -v <hostDir>:/out alpine cp -a /data/. /out/`。
- 剩余风险：未做容器内真实挂载隔离的集成验证（需 VPS 冒烟）；跨用户读取拒绝测试待补（下一轮随生命周期重做补上）。

### 14.6 P0-06 符号链接逃逸 —— 已修复（双端）

- 修改：sandbox 侧 write/read/list/export 的容器内 python 统一 `os.path.realpath` 根内断言（list 改用 lstat 不跟随 symlink）；后端 `exportFilePath` 增加 `filepath.EvalSymlinks` 后前缀复验。
- 剩余风险：容器内写路径的 TOCTOU 窗口极小（单用户容器内，实际风险来自共享卷 symlink，已由 bind 隔离 + 双端复验覆盖）。

### 14.7 P0-07 鉴权与身份 —— 已修复

- 修改：`Config.Validate()` 空 API Key 拒绝启动（main.go 强制校验，authMiddleware 纵深防御）；`_meta` 增加 HMAC-SHA256 短期签名（`ts` 5 分钟窗口，`SANDBOX_META_HMAC_KEY`，空则回退 APIKey 派生）；后端 `infra/mcp/client.go` 在 `tools/call` 的 `_meta` 注入 `ts`/`sig`（canonical 串与沙箱一致）；`sandbox_ps` 只返回当前用户会话。
- 测试：签名缺失/篡改/过期拒绝、空 key 启动拒绝、ps 跨用户不泄露、后端签名 canonical 一致性测试。
- 剩余风险：mm-core/mm-omni-av 网关（supergateway）无鉴权中间件，仅靠 127.0.0.1 回环绑定——与审查结论一致，留待多模型 Gateway 阶段统一。

### 14.8 P0-08 Qwen 部署可重建 —— 已修复

- 修改：`deploy/docker-compose.yml` 两个 mm 服务 git 依赖 pin 到 commit `114aaaefc3f23d132c6ecc9df50fae72703f14fa`（2026-08-12 main HEAD）；mm-omni-av 从已废弃的 `[omni-av]` 迁移到官方 `[api]` 入口（`qwen-mm-plugins-api`，经官方 pyproject.toml 确认）。
- 剩余风险：api 入口工具清单与旧 omni-av 的差异需 VPS 重建后 `tools/list` 冒烟比对（文档 10 节验证表待更新）。

### 14.9 顺手修复

- P1-03：`handleTaskPoll` 检查 `err`/`output == nil`，容器丢失返回任务中断而非 panic。
- P1-05：`ensureImage` 保存并消费 `ImagePull` reader。
- P1-06：`containerSpec.CacheVolume` 用配置值替换硬编码 `deeix-sandbox-cache-root`。
- `createContainer` 启动失败补偿删除容器。
- 3 处空白（`files.go:144-145`、`artifact-thumbnail-protocol.ts:3`）修复，`git diff --check` 干净。
- 移除误入库的编译产物 `tools/sandbox-mcp/sandbox-mcp`（加入 .gitignore）。

### 14.10 验证结果（本轮）

| 验证项 | 结果 |
| --- | --- |
| Sandbox `go vet` + `go test -race ./...` | 通过 |
| Backend `go build ./...` + `go vet ./...` | 通过 |
| Backend 受影响包（mcp/config/security/conversation）测试 | 通过 |
| 三份 `docker compose config` | 通过 |
| Deploy compose（`tools/sandbox-mcp/deploy`） | 通过（需注入必需 env） |
| `git diff --check` | 干净 |
| VPS 上线冒烟（mm tools/list、跨租户隔离、导出链路） | 待部署后执行 |
| Backend 全量 `go test ./...` | 仍失败（P2-08 分层测试，本轮未拆层） |

### 14.11 下一发布门槛评估

- 阶段 1（P0）已落地并有单测；发布阻断项在代码层解除。
- 在 VPS 完成 sandbox 栈重建 + mm 工具冒烟 + 沙箱/导出链路实测前，不建议视为生产发布；Agent Group 一致性（阶段 2）与上游同步（阶段 3）仍是发布门槛。
