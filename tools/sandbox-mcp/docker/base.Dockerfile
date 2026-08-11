# deeix-sandbox-base —— 沙箱会话基础镜像
# 定位：文件处理（音频/图像/数据分析）+ 网络抓取的最小环境；容器内可再 pip/apt 自装。
# 安全权衡：容器以 root 运行以支持 pip/apt 安装（环境拉取），由 --memory/--pids-limit/--cpus 限额兜底，
# 仅挂载工作区卷与 pip 缓存卷，不暴露宿主机资源。部署见 /opt/deeix-mcp 或 docker-compose 服务。
FROM python:3.12-slim

ENV DEBIAN_FRONTEND=noninteractive \
    PIP_NO_CACHE_DIR=0 \
    PYTHONUNBUFFERED=1

# 系统工具：ffmpeg（音频/视频处理）、curl（网络抓取）、git、网络工具
RUN apt-get update && apt-get install -y --no-install-recommends \
        ffmpeg \
        curl \
        ca-certificates \
        git \
        procps \
        file \
    && rm -rf /var/lib/apt/lists/*

# 常用数据分析/音频处理/抓取依赖
RUN pip install --no-cache-dir \
        numpy \
        pandas \
        requests \
        httpx \
        librosa \
        pydub \
        ffmpeg-python \
        soundfile \
        mutagen \
        beautifulsoup4 \
        lxml

WORKDIR /workspace
CMD ["/bin/sh"]
