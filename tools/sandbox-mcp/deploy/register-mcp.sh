#!/bin/bash
# DEEIX 配套 MCP 服务注册脚本（VPS 上执行）
# 用法：ADMIN_USER=<用户名> ADMIN_PASS=<密码> bash /opt/deeix-mcp/register-mcp.sh
# 完成：登录拿 token -> 开启 mcp_enable -> 调大工具超时 -> 注册 3 个 MCP server -> 同步工具
set -euo pipefail

API="http://127.0.0.1:8088/api/v1"
ADMIN_USER="${ADMIN_USER:?set ADMIN_USER}"
ADMIN_PASS="${ADMIN_PASS:?set ADMIN_PASS}"
SANDBOX_KEY=$(grep '^SANDBOX_MCP_API_KEY=' /opt/deeix-mcp/.env | cut -d= -f2)

echo "== 1/5 登录 =="
LOGIN=$(curl -s -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}")
TOKEN=$(echo "$LOGIN" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("accessToken",""))' 2>/dev/null)
if [ -z "$TOKEN" ]; then
  echo "登录失败，请检查账号密码。响应: $(echo "$LOGIN" | head -c 300)"
  exit 1
fi
AUTH="Authorization: Bearer $TOKEN"

echo "== 2/5 开启 mcp_enable + 工具超时 300s =="
curl -s -X PATCH "$API/admin/settings" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"items":[{"namespace":"mcp","key":"mcp_enable","value":"true"},{"namespace":"mcp","key":"mcp_tool_timeout_seconds","value":"300"}]}' > /dev/null || true

echo "== 3/5 注册 MCP server =="
register_server() { # name base_url token
  curl -s -X POST "$API/admin/mcp/servers" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"name\":\"$1\",\"baseURL\":\"$2\",\"authToken\":\"$3\",\"status\":\"active\"}"
  echo
}
echo "-- sandbox --"
register_server "deeix-sandbox" "http://127.0.0.1:8081/mcp" "$SANDBOX_KEY"
echo "-- mm-core --"
register_server "qwen-mm-core" "http://127.0.0.1:8082/mcp" ""
echo "-- mm-omni-av --"
register_server "qwen-mm-omni-av" "http://127.0.0.1:8083/mcp" ""

echo "== 4/5 同步工具（每个 server）=="
for SID in $(curl -s "$API/admin/mcp/servers" -H "$AUTH" | python3 -c 'import sys,json;print(" ".join(str(s["id"]) for s in json.load(sys.stdin).get("data",[])))' 2>/dev/null); do
  echo "-- sync server $SID --"
  curl -s -X POST "$API/admin/mcp/servers/$SID/sync" -H "$AUTH" > /dev/null || true
done

echo "== 5/5 完成 =="
echo "已注册。下一步在管理后台「工具」页确认工具状态，并在对话的工具选择器勾选沙箱工具。"
echo "注：mm-core/mm-omni-av 需先在 /opt/deeix-mcp/.env 填入 DASHSCOPE_API_KEY 并 docker compose up -d。"
