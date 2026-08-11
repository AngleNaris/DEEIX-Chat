# deeix-sandbox-mcp — DEEIX 多用户轻量沙箱 MCP 服务

DEEIX（X-DEEIX）配套的**多用户隔离沙箱**：Agent 可在远程沙箱容器内执行 shell/python 命令、处理文件（如用 ffmpeg/librosa 分析音频）、从网络抓取信息，并按需拉取所需环境（pip/apt 安装、指定镜像重建）。

## 设计要点

- **多用户隔离**：DEEIX 后端在每次 MCP 调用中注入 `_meta{user_id, conversation_id, request_id}`（`backend/internal/infra/mcp/client.go`），本服务按 `(user_id, conversation_id)` 建立独立 Docker 容器会话，互不可见。
- **会话租约制（参考 LobeHub Onlyboxes）**：懒创建（create_if_missing）、闲置超 `lease TTL`（默认 900s）自动回收；重建时工具结果回传 `session_recreated: true`，模型可感知工作区被重置。
- **环境拉取**：容器内可自由 `pip install` / `apt-get install`；用户级共享缓存卷（`/root/.cache`）保证容器重建后安装秒级命中；`sandbox_spawn` 可按需拉任意镜像（如 `node:22-slim`）重建会话。
- **网络**：默认允许出网（抓取信息需求）；仅接受 http/https URL。
- **传输**：Streamable HTTP（`/mcp`），Bearer Token 鉴权；仅建议 DEEIX 后端回环访问（VPS `127.0.0.1:8081`）。

## 工具

| 工具 | 说明 |
|---|---|
| `sandbox_exec` | 执行 shell 命令（默认超时 120s，最大 600s，输出截断 64KB） |
| `sandbox_task_start` / `sandbox_task_poll` / `sandbox_task_cancel` | 后台长任务（轮询/终止） |
| `sandbox_write_file` | 写文件（`content_base64` / `content_text`；DEEIX 附件注入路径） |
| `sandbox_read_file` / `sandbox_list_files` | 读文件（base64 + mime）/ 列目录 |
| `sandbox_download` | 抓取 URL 存文件或返回正文 |
| `sandbox_spawn` | 按需拉镜像重建会话容器（环境拉取） |
| `sandbox_ps` / `sandbox_kill` / `sandbox_reset` | 会话管理 |

## 安全权衡（有意为之）

- 容器以 **root** 运行以支持 pip/apt 自装（环境拉取），由 `--memory 1g --pids-limit 256 --cpus 0.5`（可配）限额兜底；仅挂载工作区卷与缓存卷，不暴露宿主机目录。
- 沙箱服务需挂载宿主 `/var/run/docker.sock`（唯一持该权限的服务，`cap_drop: ALL` + `no-new-privileges`）。
- 路径消毒：文件工具仅允许 `/workspace` 内路径，拒绝 `..` 逃逸与 `/etc /proc /sys /tmp`。
- 单租户 VPS 部署；多租户共享宿主需另行评估（建议每租户独立沙箱服务实例或加 seccomp 加固）。

## 目录

```
tools/sandbox-mcp/
├── main.go / server.go     入口 + Streamable HTTP + Bearer 鉴权 + _meta 提取
├── config.go               环境变量配置
├── sessions.go             会话管理（scope 路由、租约回收）
├── docker.go               Docker 容器/exec/卷封装
├── tools.go                MCP 工具注册 + exec/后台任务
├── files.go                文件工具 + 路径消毒
├── download.go             网络抓取
├── spawn.go                spawn/ps/kill/reset
├── main_test.go            单元测试（路由/消毒/解析）
├── integration_test.go     与 DEEIX 客户端协议对打（initialize/session/_meta/鉴权）
├── docker/base.Dockerfile  基础镜像（python3.12 + ffmpeg + 数据分析包）
└── deploy/                 VPS 部署（compose + 镜像 Dockerfile + .env.example）
```

## 部署

见 `deploy/docker-compose.yml`（含 Qwen-MM-Plugins 桥接服务）：

```bash
cp deploy/.env.example deploy/.env   # 填 SANDBOX_MCP_API_KEY / DASHSCOPE_API_KEY
# 构建基础镜像
docker build -t deeix-sandbox-base:latest -f docker/base.Dockerfile docker/
# 构建并启动
cd deploy && docker compose up -d --build
# 验证
curl -s http://127.0.0.1:8081/mcp -H 'Authorization: Bearer <key>' -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

DEEIX 管理后台 → MCP 服务：注册 `http://127.0.0.1:8081/mcp` + Bearer Token，同步工具后按需启用。

## 开发

本地无 Go 工具链时用容器（`deeix-build:1`）：

```bash
MSYS_NO_PATHCONV=1 docker run --rm -v "$(cygpath -w <repo>/tools/sandbox-mcp)":/app -w /app -v deeix-gomod-cache:/go/pkg/mod deeix-build:1 sh -c 'go mod tidy && go test ./...'
```
