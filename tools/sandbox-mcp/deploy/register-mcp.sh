#!/bin/bash
# DEEIX 配套 MCP 服务注册脚本（VPS 上执行）
# 用法：ADMIN_USER=<用户名> ADMIN_PASS=<密码> bash /opt/deeix-mcp/register-mcp.sh
# MCP URL 使用 1panel-network 内的 Docker 服务名；脚本从 VPS 宿主通过
# 127.0.0.1 health/readiness 端点确认服务就绪后再注册和同步。
set -euo pipefail

API="http://127.0.0.1:8088/api/v1"
ADMIN_USER="${ADMIN_USER:?set ADMIN_USER}"
ADMIN_PASS="${ADMIN_PASS:?set ADMIN_PASS}"
SANDBOX_KEY=$(grep '^SANDBOX_MCP_API_KEY=' /opt/deeix-mcp/.env | cut -d= -f2-)

wait_for_endpoint() { # name url
  local name="$1" url="$2" attempt
  for attempt in $(seq 1 60); do
    if curl -fsS --max-time 5 "$url" > /dev/null; then
      return 0
    fi
    sleep 2
  done
  echo "$name 未就绪: $url" >&2
  return 1
}

echo "== 1/6 等待 MCP 服务就绪 =="
wait_for_endpoint "sandbox" "http://127.0.0.1:8081/healthz"
wait_for_endpoint "mm-core isolation" "http://127.0.0.1:8082/readyz"
wait_for_endpoint "mm-omni-av isolation" "http://127.0.0.1:8083/readyz"

echo "== 2/6 登录 =="
LOGIN=$(curl -s -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}")
TOKEN=$(echo "$LOGIN" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("accessToken",""))' 2>/dev/null)
if [ -z "$TOKEN" ]; then
  echo "登录失败，请检查账号密码。响应: $(echo "$LOGIN" | head -c 300)"
  exit 1
fi
AUTH="Authorization: Bearer $TOKEN"

echo "== 3/6 开启 mcp_enable + 工具超时 600s =="
curl -fsS -X PATCH "$API/admin/settings" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"items":[{"namespace":"mcp","key":"mcp_enable","value":"true"},{"namespace":"mcp","key":"mcp_tool_timeout_seconds","value":"600"}]}' > /dev/null

echo "== 4/6 注册或更新 MCP server =="
SERVERS=$(curl -fsS "$API/admin/mcp/servers" -H "$AUTH")
server_id_by_name() { # name
  printf '%s' "$SERVERS" | python3 -c '
import json,sys
name=sys.argv[1]
items=json.load(sys.stdin).get("data",{}).get("results",[])
print(next((str(item["id"]) for item in items if item.get("name")==name), ""))
' "$1"
}
server_update_payload() { # name description base_url
  printf '%s' "$SERVERS" | python3 -c '
import json,sys
name,description,url=sys.argv[1:4]
items=json.load(sys.stdin).get("data",{}).get("results",[])
item=next(item for item in items if item.get("name")==name)
print(json.dumps({
  "name": item.get("name", name),
  "description": description,
  "baseURL": url,
  "headersJSON": item.get("headersJSON", "{}"),
  "status": "active",
}, ensure_ascii=False))
' "$1" "$2" "$3"
}
server_create_payload() { # name description base_url token
  python3 -c '
import json,sys
name,description,url,token=sys.argv[1:5]
print(json.dumps({"name":name,"description":description,"baseURL":url,"authToken":token,"status":"active"}, ensure_ascii=False))
' "$1" "$2" "$3" "$4"
}
upsert_server() { # name description base_url token
  local name="$1" description="$2" base_url="$3" token="$4" server_id payload
  server_id=$(server_id_by_name "$name")
  if [ -n "$server_id" ]; then
    payload=$(server_update_payload "$name" "$description" "$base_url")
    curl -fsS -X PATCH "$API/admin/mcp/servers/$server_id" -H "$AUTH" -H 'Content-Type: application/json' -d "$payload" > /dev/null
  else
    payload=$(server_create_payload "$name" "$description" "$base_url" "$token")
    curl -fsS -X POST "$API/admin/mcp/servers" -H "$AUTH" -H 'Content-Type: application/json' -d "$payload" > /dev/null
  fi
  echo
}
echo "-- sandbox --"
upsert_server "deeix-sandbox" "Isolated per-user conversation sandbox with package installation, public internet access, and file import/export." "http://deeix-sandbox-mcp:8081/mcp" "$SANDBOX_KEY"
echo "-- mm-core --"
upsert_server "qwen-mm-core" "Qwen multimodal core tools routed through a signed per-call file isolation wrapper." "http://deeix-mm-core-isolation:8082/mcp" ""
echo "-- mm-omni-av --"
upsert_server "qwen-mm-omni-av" "Qwen multimodal API tools routed through a signed per-call file isolation wrapper." "http://deeix-mm-omni-av-isolation:8083/mcp" ""

# 刷新列表：创建分支会产生新 ID，更新分支保留原 server/tool identity。
SERVERS=$(curl -fsS "$API/admin/mcp/servers" -H "$AUTH")
echo "== 5/6 同步目标 server 工具 =="
for NAME in deeix-sandbox qwen-mm-core qwen-mm-omni-av; do
  SID=$(server_id_by_name "$NAME")
  if [ -z "$SID" ]; then
    echo "MCP server 不存在: $NAME" >&2
    exit 1
  fi
  echo "-- sync $NAME (server $SID) --"
  curl -fsS -X POST "$API/admin/mcp/servers/$SID/sync" -H "$AUTH" > /dev/null
done

echo "== 6/6 完成 =="
echo "已注册。下一步在管理后台「工具」页确认工具状态，并在对话的工具选择器勾选沙箱工具。"
echo "注：mm-core/mm-omni-av 需先在 /opt/deeix-mcp/.env 填入 DASHSCOPE_API_KEY 并 docker compose up -d。"
