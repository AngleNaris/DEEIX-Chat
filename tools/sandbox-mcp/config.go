package main

import (
	"os"
	"strconv"
	"time"
)

// Config 沙箱 MCP 服务的运行配置（全部来自环境变量，生产通过 .env / compose 注入）。
type Config struct {
	// ListenAddr HTTP 监听地址，生产固定 127.0.0.1:8081，仅 DEEIX 后端回环访问。
	ListenAddr string
	// APIKey 管理端注册 MCP 服务时填写的 Bearer Token。为空时禁用鉴权（仅本地调试）。
	APIKey string
	// BaseImage 默认会话容器镜像。
	BaseImage string
	// WorkspaceDir 容器内工作目录。
	WorkspaceDir string
	// LeaseTTL 会话闲置回收时间（LobeHub 默认 900s）。
	LeaseTTL time.Duration
	// ReclaimInterval 闲置扫描间隔。
	ReclaimInterval time.Duration
	// ExecTimeout 单条命令默认超时。
	ExecTimeout time.Duration
	// MaxExecTimeout 单条命令允许的最大超时（防止占用线程过久）。
	MaxExecTimeout time.Duration
	// OutputLimitBytes 命令输出/读文件返回上限。
	OutputLimitBytes int
	// MemoryLimit 容器内存上限（docker --memory）。
	MemoryLimit string
	// PidsLimit 容器进程数上限（docker --pids-limit）。
	PidsLimit int64
	// CPUsLimit 容器 CPU 上限（docker --cpus）。
	CPUsLimit float64
	// CacheVolume 用户级共享包缓存卷名（pip/npm 缓存，跨会话保留，加速环境重建）。
	CacheVolume string
	// SharedVolume 沙箱与多模态 MCP（mm-core/mm-omni-av）共享的文件卷名。
	// 会话容器挂载到 /shared，按 /shared/<scope> 子目录隔离；mm 工具可读取该目录文件。
	SharedVolume string
	// NetworkMode 会话容器网络模式：空串 = Docker 默认 bridge（容器可出公网，
	// 但不能按服务名访问 DEEIX 内部容器）；设为 "1panel-network" 等外部网络名时，
	// 会话容器与 DEEIX 后端同网，可直接 http://deeix-chat-app:8080 访问本平台服务
	//（供"AI 维护 DEEIX 所在服务器"类任务使用）。
	NetworkMode string
	// MaxTasksPerSession 单会话并行后台任务上限。
	MaxTasksPerSession int
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDurationSeconds(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return def
}

// Load 读取环境变量构建配置。
func Load() *Config {
	return &Config{
		ListenAddr:         envStr("SANDBOX_MCP_ADDR", "127.0.0.1:8081"),
		APIKey:             os.Getenv("SANDBOX_MCP_API_KEY"),
		BaseImage:          envStr("SANDBOX_BASE_IMAGE", "deeix-sandbox-base:latest"),
		WorkspaceDir:       envStr("SANDBOX_WORKSPACE_DIR", "/workspace"),
		LeaseTTL:           envDurationSeconds("SANDBOX_LEASE_TTL_SEC", 15*time.Minute),
		ReclaimInterval:    envDurationSeconds("SANDBOX_RECLAIM_INTERVAL_SEC", 60*time.Second),
		ExecTimeout:        envDurationSeconds("SANDBOX_EXEC_TIMEOUT_SEC", 120*time.Second),
		MaxExecTimeout:     envDurationSeconds("SANDBOX_MAX_EXEC_TIMEOUT_SEC", 600*time.Second),
		OutputLimitBytes:   envInt("SANDBOX_OUTPUT_LIMIT_BYTES", 64*1024),
		MemoryLimit:        envStr("SANDBOX_MEMORY_LIMIT", "1g"),
		PidsLimit:          int64(envInt("SANDBOX_PIDS_LIMIT", 256)),
		CPUsLimit:          envFloat("SANDBOX_CPUS_LIMIT", 0.5),
		CacheVolume:        envStr("SANDBOX_CACHE_VOLUME", "deeix-sandbox-cache"),
		SharedVolume:       envStr("SANDBOX_SHARED_VOLUME", "deeix-mcp-shared"),
		NetworkMode:        envStr("SANDBOX_NETWORK_MODE", ""),
		MaxTasksPerSession: envInt("SANDBOX_MAX_TASKS_PER_SESSION", 4),
	}
}
