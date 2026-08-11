# Qwen-MM-Plugins stdio MCP -> Streamable HTTP 桥接镜像（supergateway）
# 构建参数（包名必须带 git URL，qwen-mm-plugins 未发布到 PyPI）：
#   MM_PACKAGE: 如 "qwen-mm-plugins[core] @ git+https://github.com/QwenLM/Qwen-MM-Plugins.git@main"
#   MM_ENTRY:   入口命令，如 "qwen-mm-plugins-core"
# 预装（uv tool install）避免每次启动下载依赖；supergateway 以 stateful 模式桥接。
ARG MM_PACKAGE
ARG MM_ENTRY

FROM node:22-slim AS uv-install
ARG MM_PACKAGE
ARG MM_ENTRY
# 安装 uv（官方脚本）
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates git \
    && curl -LsSf https://astral.sh/uv/install.sh | sh \
    && rm -rf /var/lib/apt/lists/*
ENV PATH="/root/.local/bin:${PATH}"
# 预装 mm-plugins 包到 uv 工具缓存（构建期即可用，首次启动零下载）。
# 位置参数直接带完整 git 引用；运行时 uvx --from <同包> <entry> 会复用该缓存。
RUN uv tool install "${MM_PACKAGE}"

FROM node:22-slim
ARG MM_PACKAGE
ARG MM_ENTRY
ENV MM_PACKAGE=${MM_PACKAGE} MM_ENTRY=${MM_ENTRY} \
    PATH="/root/.local/bin:${PATH}" \
    SUPER_GATEWAY_PORT=8082
COPY --from=uv-install /root/.local /root/.local
RUN npm install -g supergateway --silent
EXPOSE 8082 8083
# 启动：supergateway 桥接 mm-plugins stdio MCP 进程。
# 注意 --stdio 必须直接指向 uv tool 安装的可执行文件：supergateway 按空格拆分命令，
# uvx --from "pkg @ git+url" 中的空格会导致 "@" 被当成可执行名而崩溃。
CMD sh -c 'exec npx supergateway --stdio /root/.local/bin/${MM_ENTRY} \
  --outputTransport streamableHttp --port ${SUPER_GATEWAY_PORT} --stateful'
