# Custom 分支 VPS 发布与回滚

本文只覆盖单应用实例、多用户部署。多实例调度、跨实例审批和分布式锁不在当前支持范围。

## 1. 发布前提

- 所有发布门禁按 `docs/CUSTOM_TEST_PLAN.md` 通过，报告明确区分 Passed、Blocked 和 Not Run。
- 未完成真实 canary 的 Qwen、Gemini、豆包、OCR 或其他 Provider 保持关闭。
- PostgreSQL 已完成备份和恢复命令演练；磁盘空间足够同时保存数据库备份、旧镜像和新镜像。
- 记录当前 Compose 文件、环境文件、镜像引用、运行版本和数据库版本，作为回滚基线。
- 候选必须是已提交的 exact SHA；禁止从脏工作树直接构建。

## 2. 构建不可变制品

在 Linux 或 WSL 中执行，避免 Windows checkout 的 CRLF 影响 `VERSION` 检查：

```bash
set -euo pipefail
SHA="$(git rev-parse HEAD)"
VERSION="$(git show "$SHA:VERSION" | tr -d '\r\n')"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
SHORT_SHA="$(printf '%s' "$SHA" | cut -c1-12)"
WORK="$(mktemp -d)"
OUT="$(pwd)/release/$SHORT_SHA"
IMAGE="deeix-chat:$SHORT_SHA"
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$OUT"
git archive --format=tar "$SHA" | tar -x -C "$WORK"
test "$(tr -d '\r\n' < "$WORK/VERSION")" = "$VERSION"

docker buildx build \
  --platform linux/amd64 \
  --file "$WORK/Dockerfile" \
  --build-arg "GIT_COMMIT=$SHA" \
  --build-arg "BUILD_TIME=$STAMP" \
  --tag "$IMAGE" \
  --load \
  "$WORK"

docker save "$IMAGE" -o "$OUT/deeix-chat-$SHORT_SHA-linux-amd64.tar"
sha256sum "$OUT/deeix-chat-$SHORT_SHA-linux-amd64.tar" > "$OUT/SHA256SUMS"
IMAGE_ID="$(docker image inspect "$IMAGE" --format '{{.Id}}')"
printf 'commit=%s\nversion=%s\nimage=%s\nimage_id=%s\nplatform=linux/amd64\nbuild_time=%s\n' \
  "$SHA" "$VERSION" "$IMAGE" "$IMAGE_ID" "$STAMP" > "$OUT/manifest.env"
sha256sum -c "$OUT/SHA256SUMS"
```

提交 `tar`、`SHA256SUMS` 和 `manifest.env` 到 VPS 后，先在 VPS 再执行一次 `sha256sum -c SHA256SUMS`。制品目录和镜像标签都使用 SHA，不复用 `latest`。

## 3. VPS 预检与备份

以下变量由运维按实际环境设置，命令中的占位值不得原样执行：

```bash
set -euo pipefail
APP_DIR="/opt/deeix-chat"
RELEASE_DIR="/opt/deeix-chat/releases/<short-sha>"
BACKUP_DIR="/opt/backups/deeix-chat-<short-sha>-$(date -u +%Y%m%dT%H%M%SZ)"
APP_CONTAINER="deeix-chat-app"
umask 077
mkdir -p "$BACKUP_DIR"

cd "$APP_DIR"
docker compose config > "$BACKUP_DIR/compose.rendered.yaml"
docker inspect "$APP_CONTAINER" > "$BACKUP_DIR/app.inspect.json"
docker inspect "$APP_CONTAINER" --format 'DEEIX_CHAT_IMAGE={{.Config.Image}}' > "$BACKUP_DIR/current-image.env"
cp -a docker-compose.yml config.yaml "$BACKUP_DIR/"
test ! -f .env.release || cp -a .env.release "$BACKUP_DIR/"
docker compose config --images > "$BACKUP_DIR/images.txt"

# 使用当前 PostgreSQL 运维方式生成一致性备份，并立即校验文件非空。
<postgres-backup-command> > "$BACKUP_DIR/deeix_chat.sql"
test -s "$BACKUP_DIR/deeix_chat.sql"
sha256sum "$BACKUP_DIR"/* > "$BACKUP_DIR/SHA256SUMS"
```

预检还必须确认：数据库可连接、pgvector/扩展版本满足当前 Schema、旧数据可读、反向代理目标正确、`SANDBOX_META_HMAC_KEY` 两端一致、shared/imports 父目录只允许服务账号写入。

## 4. 原子切换

```bash
set -euo pipefail
cd "$RELEASE_DIR"
sha256sum -c SHA256SUMS
docker load -i deeix-chat-<short-sha>-linux-amd64.tar

cd "$APP_DIR"
printf 'DEEIX_CHAT_IMAGE=deeix-chat:<short-sha>\n' > .env.release.next
mv .env.release.next .env.release
docker compose --env-file .env.release up -d --no-deps app
```

单实例切换会有短暂重启窗口。迁移失败、健康失败或版本不匹配时不得继续浏览器验收，立即执行回滚。

## 5. 发布后门禁

```bash
set -euo pipefail
EXPECTED_SHA="<full-sha>"
EXPECTED_VERSION="<version>"

for attempt in $(seq 1 60); do
  curl -fsS http://127.0.0.1:8080/readyz && break
  sleep 2
done

curl -fsS http://127.0.0.1:8080/healthz
VERSION_JSON="$(curl -fsS http://127.0.0.1:8080/api/v1/version)"
printf '%s' "$VERSION_JSON" | jq -e --arg sha "$EXPECTED_SHA" --arg version "$EXPECTED_VERSION" \
  '.commit == $sha and .version == $version'
test "$(docker inspect "$APP_CONTAINER" --format '{{.RestartCount}}')" = "0"
if docker logs --since 15m "$APP_CONTAINER" 2>&1 | grep -Eai 'fatal|panic|segmentation|unhandled|(^|[^a-z])error([^a-z]|$)'; then
  echo "fatal/error log gate failed" >&2
  exit 1
fi
```

随后通过真实域名完成登录、普通消息、管理员开关、A/B 用户隔离、凭据 CRUD、Agent Group、审批终态、制品 CRUD/分享和刷新持久化验证。仅对计划启用的 Provider 使用受控凭据执行 canary；不得把用户敏感附件作为测试材料。

## 6. 回滚

```bash
set -euo pipefail
cd "$APP_DIR"
cp -a "$BACKUP_DIR/current-image.env" .env.release.next
mv .env.release.next .env.release
docker compose --env-file .env.release up -d --no-deps app
curl -fsS http://127.0.0.1:8080/readyz
curl -fsS http://127.0.0.1:8080/api/v1/version
```

若新版本已执行不兼容的数据迁移，仅回滚镜像不够；必须先停止应用，再按已演练流程恢复数据库备份。回滚后同样检查版本、RestartCount、最近日志和真实域名登录。
