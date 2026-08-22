package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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
	// ExportTTL shared scope exports/staging retention time.
	ExportTTL time.Duration
	// ExportSweepInterval shared scope export sweep interval.
	ExportSweepInterval time.Duration
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
	// CacheVolume 用户级共享包缓存卷名前缀（pip/npm/uv 缓存，按 user_id 派生）。
	CacheVolume string
	// ImportsHostDir 宿主机按 scope 提供给沙箱的只读导入目录根路径。
	ImportsHostDir string
	// ImportsMountDir 沙箱容器内只读导入目录挂载点。
	ImportsMountDir string
	// SharedHostDir 共享目录的宿主路径（sandbox-mcp 进程与 Docker daemon 所在主机）。
	// 会话容器只 bind mount 自己的 scope 子目录（SharedHostDir/<scope> → /shared/<scope>），
	// 其他租户目录物理不可见；DEEIX 后端与 mm 网关挂载整目录只读。
	SharedHostDir string
	// SharedMountDir 会话容器内共享目录挂载点（默认 /shared）。
	SharedMountDir string
	// MetaHMACKey _meta 签名的 HMAC 密钥（与后端 SANDBOX_META_HMAC_KEY 一致）。
	// 为空时回退使用 APIKey 派生（APIKey 必填，见 Validate）。
	MetaHMACKey string
	// AllowedHosts 下载工具出站白名单主机（精确主机名，逗号分隔；默认空 = 拒绝全部私网/回环）。
	AllowedHosts []string
	// AllowedCIDRs 下载工具出站白名单网段（逗号分隔；默认空）。
	AllowedCIDRs []string
	// NetworkMode 会话容器网络模式：空串 = Docker 默认 bridge（容器可出公网，
	// 但不能按服务名访问 DEEIX 内部容器）；设为 "1panel-network" 等外部网络名时，
	// 会话容器与 DEEIX 后端同网，可直接 http://deeix-chat-app:8080 访问本平台服务
	//（供"AI 维护 DEEIX 所在服务器"类任务使用）。
	NetworkMode string
	// MaxTasksPerSession 单会话并行后台任务上限。
	MaxTasksPerSession int
	// TaskTTL 后台任务最长存活时间：超时的任务会被回收 goroutine 杀掉进程组
	// 并从会话移除（防止永不退出的任务把容器永久钉住）。<=0 表示禁用。
	TaskTTL time.Duration
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

// splitEnvList 解析逗号分隔的环境变量列表（去空白、去空项）。
func splitEnvList(key string) []string {
	var out []string
	for _, item := range strings.Split(os.Getenv(key), ",") {
		if v := strings.TrimSpace(item); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Load 读取环境变量构建配置。
func Load() *Config {
	return &Config{
		ListenAddr:          envStr("SANDBOX_MCP_ADDR", "127.0.0.1:8081"),
		APIKey:              os.Getenv("SANDBOX_MCP_API_KEY"),
		BaseImage:           envStr("SANDBOX_BASE_IMAGE", "deeix-sandbox-base:latest"),
		WorkspaceDir:        envStr("SANDBOX_WORKSPACE_DIR", "/workspace"),
		LeaseTTL:            envDurationSeconds("SANDBOX_LEASE_TTL_SEC", 15*time.Minute),
		ReclaimInterval:     envDurationSeconds("SANDBOX_RECLAIM_INTERVAL_SEC", 60*time.Second),
		ExportTTL:           envDurationSeconds("SANDBOX_EXPORT_TTL_SEC", 7*24*time.Hour),
		ExportSweepInterval: envDurationSeconds("SANDBOX_EXPORT_SWEEP_INTERVAL_SEC", time.Hour),
		ExecTimeout:         envDurationSeconds("SANDBOX_EXEC_TIMEOUT_SEC", 120*time.Second),
		MaxExecTimeout:      envDurationSeconds("SANDBOX_MAX_EXEC_TIMEOUT_SEC", 600*time.Second),
		OutputLimitBytes:    envInt("SANDBOX_OUTPUT_LIMIT_BYTES", 64*1024),
		MemoryLimit:         envStr("SANDBOX_MEMORY_LIMIT", "1g"),
		PidsLimit:           int64(envInt("SANDBOX_PIDS_LIMIT", 256)),
		CPUsLimit:           envFloat("SANDBOX_CPUS_LIMIT", 0.5),
		CacheVolume:         envStr("SANDBOX_CACHE_VOLUME", "deeix-sandbox-cache"),
		ImportsHostDir:      envStr("SANDBOX_IMPORTS_HOST_DIR", "/opt/deeix-mcp/imports"),
		ImportsMountDir:     envStr("SANDBOX_IMPORTS_DIR", "/imports"),
		SharedHostDir:       envStr("SANDBOX_SHARED_HOST_DIR", "/opt/deeix-mcp/shared"),
		SharedMountDir:      envStr("SANDBOX_SHARED_DIR", "/shared"),
		MetaHMACKey:         os.Getenv("SANDBOX_META_HMAC_KEY"),
		AllowedHosts:        splitEnvList("SANDBOX_ALLOWED_HOSTS"),
		AllowedCIDRs:        splitEnvList("SANDBOX_ALLOWED_CIDRS"),
		NetworkMode:         envStr("SANDBOX_NETWORK_MODE", "deeix-sandbox-egress"),
		MaxTasksPerSession:  envInt("SANDBOX_MAX_TASKS_PER_SESSION", 4),
		TaskTTL:             envDurationSeconds("SANDBOX_TASK_TTL_SEC", 24*time.Hour),
	}
}

// HmacKey 返回 _meta 签名密钥：优先专用密钥，回退 APIKey（必填）。
func (c *Config) HmacKey() string {
	if c.MetaHMACKey != "" {
		return c.MetaHMACKey
	}
	return c.APIKey
}

// DownloadPolicy 构建 sandbox_download 的出站策略（SSRF 防护）。
func (c *Config) DownloadPolicy() (OutboundPolicy, error) {
	return NewOutboundPolicy(true, c.AllowedHosts, c.AllowedCIDRs)
}

// Validate 启动前校验配置：API Key 缺失或出站策略非法时拒绝启动（发布阻断项 P0-07）。
func (c *Config) Validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("SANDBOX_MCP_API_KEY is required: refusing to run without auth (P0-07)")
	}
	if _, err := c.DownloadPolicy(); err != nil {
		return fmt.Errorf("invalid download outbound policy: %w", err)
	}
	return nil
}
