# Qwen-MM-Plugins stdio MCP -> Streamable HTTP 桥接镜像（supergateway）
# 构建参数：
#   MM_PACKAGE: uvx 安装包，如 "qwen-mm-plugins[core]"
#   MM_ENTRY:   入口命令，如 "qwen-mm-plugins-core"
# 预装（uv tool install）避免每次启动下载依赖；supergateway 以 stateful 模式桥接。
ARG MM_PACKAGE
ARG MM_ENTRY

FROM node:22-slim AS uv-install
ARG MM_PACKAGE
ARG MM_ENTRY
# 安装 uv（官方脚本）
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
    && curl -LsSf https://astral.sh/uv/install.sh | sh \
    && rm -rf /var/lib/apt/lists/*
ENV PATH="/root/.local/bin:${PATH}"
# 预装 mm-plugins 包到 uv 工具缓存（构建期即可用，首次启动零下载）
RUN uv tool install --from "${MM_PACKAGE}" "${MM_ENTRY}" || true

FROM node:22-slim
ARG MM_PACKAGE
ARG MM_ENTRY
ENV MM_PACKAGE=${MM_PACKAGE} MM_ENTRY=${MM_ENTRY} \
    PATH="/root/.local/bin:${PATH}" \
    SUPER_GATEWAY_PORT=8082 SUPER_GATEWAY_MODE=stateful
COPY --from=uv-install /root/.local /root/.local
RUN npm install -g supergateway --silent
EXPOSE 8082 8083
# 启动：supergateway 桥接 uvx 拉起的 stdio MCP 进程
CMD sh -c 'exec npx supergateway --stdio "uvx --from ${MM_PACKAGE} ${MM_ENTRY}" \
  --outputTransport streamableHttp --port ${SUPER_GATEWAY_PORT} \
  ${SUPER_GATEWAY_MODE:+--mode ${SUPER_GATEWAY_MODE}}'
