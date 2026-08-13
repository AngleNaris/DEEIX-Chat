# deeix-sandbox-mcp

DEEIX 的多用户沙箱 MCP。Agent 在按用户/会话隔离的 Docker 容器中执行命令、安装依赖、处理当前消息附件，并通过现有 `__export__` 链把生成文件交付给用户。

## Runtime contract

- DEEIX backend signs `_meta` fields `user_id`, `conversation_id`, `request_id`, `call_id`, and `ts`. Sandbox calls without a valid HMAC, nonzero user/conversation scope, or a fresh timestamp are rejected.
- One session container is created for each `deeix-<user>-<conversation>` scope. Other users and conversations are not mounted into that container.
- Session containers run as root, are non-privileged, and have configurable memory, PID, and CPU limits. The default image supports `apt`, `pip`, `npm`, and `uv`; package caches are mounted from a per-user Docker volume at `/root/.cache`.
- `/workspace` is a per-conversation Docker volume. `/imports` is a read-only bind of the current conversation scope. Current-message files are prepared by the backend in a per-run lane and their exact paths are added to the transient model prompt.
- `/shared/deeix-<user>-<conversation>` is the writable scope used for file export and sandbox-to-MM handoff. No complete tenant tree is mounted into a session container.
- Session containers join only the dedicated IPv4 `deeix-sandbox-egress` bridge. They do not join `1panel-network` or the application network.
- `sandbox_export_file` accepts a regular `/workspace` file, verifies the resolved path, copies it into the current shared scope, and returns `__export__` for the existing DEEIX attachment pipeline.

## Tools

| Tool | Purpose |
|---|---|
| `sandbox_exec` | Run shell commands in `/workspace` |
| `sandbox_task_start`, `sandbox_task_poll`, `sandbox_task_cancel` | Manage bounded background tasks |
| `sandbox_write_file`, `sandbox_read_file`, `sandbox_list_files` | Work with files inside `/workspace` |
| `sandbox_download` | Download a public HTTP(S) resource through SSRF checks |
| `sandbox_export_file` | Return a `/workspace` file through `__export__` |
| `sandbox_spawn` | Recreate the scope session with another image while retaining the workspace |
| `sandbox_ps`, `sandbox_kill`, `sandbox_reset` | Inspect or reset authorized sessions |

## Network isolation

`deploy/setup-sandbox-egress.sh` creates the dedicated bridge and installs idempotent host firewall rules:

- `INPUT` drops all traffic from the sandbox bridge to host-local addresses, including the bridge gateway and host public IPs.
- `DOCKER-USER` drops private, loopback, link-local, shared-address, documentation, multicast, and reserved IPv4 destination ranges.
- The network is required to be IPv4-only. If Docker exposes IPv6 filter chains, defense-in-depth rules drop traffic arriving from the sandbox bridge.
- Globally routed IPv4 remains available for package managers and normal public internet access.

Review the script on the target VPS before running it. The application never applies firewall changes automatically. Docker or 1Panel may recreate filter chains during reload; install `deploy/deeix-sandbox-egress.service` so the script runs after `docker.service`:

```bash
sudo install -m 0755 deploy/setup-sandbox-egress.sh /opt/deeix-mcp/setup-sandbox-egress.sh
sudo install -m 0644 deploy/deeix-sandbox-egress.service /etc/systemd/system/deeix-sandbox-egress.service
sudo systemctl daemon-reload
sudo systemctl enable --now deeix-sandbox-egress.service
```

After any Docker/1Panel firewall change, run `systemctl reload deeix-sandbox-egress.service` and repeat the egress smoke tests.

## Deployment

```bash
cp deploy/.env.example deploy/.env
# Set SANDBOX_MCP_API_KEY, SANDBOX_META_HMAC_KEY, and MM provider keys.
# Set DOCKER_SOCKET_GID to: stat -c '%g' /var/run/docker.sock
docker build -t deeix-sandbox-base:latest -f docker/base.Dockerfile docker/
cd deploy
docker compose config
docker compose up -d --build
```

The app compose must mount the same imports host directory read-write at its configured `SANDBOX_IMPORTS_DIR`. The sandbox MCP and MM wrappers mount that directory read-only. Shared and imports host paths must match across both compose projects.

Register or update the MCP endpoints with `deploy/register-mcp.sh`. It upserts by server name so existing server IDs, tool associations, and user/project/role selections remain intact.

## Required smoke tests

Run these on the deployment host before enabling the tools for users:

1. Confirm `apt`, `pip`, `npm`, and `uv` can install a small public package in a session.
2. Confirm public HTTPS and DNS work.
3. Confirm application, database, Redis, Docker gateway, host public IP, metadata, RFC1918, and sibling sandbox addresses are unreachable.
4. Attach a file to the current message and confirm only its provided `/imports/...` lane is usable.
5. Create a file in `/workspace`, call `sandbox_export_file`, and confirm it appears as a downloadable DEEIX attachment.
6. Reset or expire a session and confirm other user/conversation scopes remain inaccessible.

## Development

```bash
MSYS_NO_PATHCONV=1 docker run --rm \
  -v "$(cygpath -w <repo>/tools/sandbox-mcp)":/app \
  -w /app golang:1.26.5-bookworm \
  sh -lc '/usr/local/go/bin/go test ./... && /usr/local/go/bin/go test -race ./...'
```

MM input and generated-file isolation is implemented by `tools/mm-isolation`. The pinned Qwen core `crop`, `draw_bbox`, and `save_view` tools receive forced per-call output lanes; validated files are copied into the signed shared scope and returned through `__export__`.
