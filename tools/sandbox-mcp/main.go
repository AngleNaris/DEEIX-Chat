// Package main 实现 DEEIX 配套的多用户轻量沙箱 MCP 服务。
//
// 设计要点（参考 LobeHub Onlyboxes 会话模型）：
//   - DEEIX 后端在每次 MCP 调用时注入 _meta{user_id, conversation_id, request_id}，
//     本服务按 (user_id, conversation_id) 建立隔离的 Docker 容器会话；
//   - 会话采用租约制：懒创建（create_if_missing）、闲置超过 lease TTL 即回收容器，
//     workspace 卷持续保留，重建时在工具结果中回传 session_recreated 标志；
//   - 容器内可自由 pip/apt 安装（环境拉取），用户级共享缓存卷保证重建后安装秒级命中；
//   - 仅暴露 Streamable HTTP（/mcp），Bearer Token 鉴权，仅供 DEEIX 后端回环访问。
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	cfg := Load()
	// P0-07：API Key 缺失或出站策略非法时拒绝启动（不再静默关闭鉴权）。
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid config, refusing to start", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := newDockerClient()
	if err != nil {
		slog.Error("docker client init failed", "err", err)
		os.Exit(1)
	}
	defer d.close()

	mgr := NewSessionManager(cfg, d)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		mgr.Shutdown(cleanupCtx)
	}()
	mgr.StartReclaimer(ctx)

	if err := runServer(ctx, cfg, mgr); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}
