package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

// metaKey 存放中间件从请求体提取的 DEEIX _meta。
type metaKey struct{}

// MetaFromContext 从请求上下文读取 DEEIX 注入的调用元数据。
func MetaFromContext(ctx context.Context) (*Meta, bool) {
	m, ok := ctx.Value(metaKey{}).(*Meta)
	return m, ok
}

// authMiddleware 校验 Bearer Token 并提取 _meta。
// _meta 从 POST body 的 params._meta 提取（不依赖 mcp-go 的 Meta 解析细节），
// 提取后恢复请求体供 mcp-go 正常解析。
func authMiddleware(cfg *Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg.APIKey != "" {
			auth := r.Header.Get("Authorization")
			expected := "Bearer " + cfg.APIKey
			if subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
				http.Error(w, `{"jsonrpc":"2.0","error":{"code":-32001,"message":"unauthorized"}}`, http.StatusUnauthorized)
				return
			}
		}
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
			if err != nil {
				http.Error(w, "read body", http.StatusBadRequest)
				return
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))
			if meta, err := ParseMeta(body); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), metaKey{}, meta))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// runServer 组装并启动 HTTP 服务。
func runServer(ctx context.Context, cfg *Config, mgr *SessionManager) error {
	s := newSandboxServer(cfg, mgr)
	mcpServer := server.NewMCPServer("deeix-sandbox-mcp", "1.0.0")
	registerTools(mcpServer, s)

	httpSrv := server.NewStreamableHTTPServer(mcpServer)
	handler := authMiddleware(cfg, httpSrv)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("sandbox mcp server listening", "addr", cfg.ListenAddr, "endpoint", "/mcp", "base_image", cfg.BaseImage)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
