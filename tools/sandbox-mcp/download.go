package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	// maxDownloadRedirects 下载允许的最大重定向次数。
	maxDownloadRedirects = 5
	// maxDownloadFileBytes 存文件硬上限（100MB），防下载撑爆工作区磁盘。
	maxDownloadFileBytes = 100 << 20
	// maxDownloadReadTimeout 单次下载的总超时。
	maxDownloadReadTimeout = 120 * time.Second
)

// validateFetchURL 校验下载目标：仅 http/https、无 userinfo，并经出站策略复验（SSRF 防护，P0-03）。
func validateFetchURL(raw string, policy OutboundPolicy) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("url is required")
	}
	if err := ValidateOutboundHTTPURL(value, policy); err != nil {
		return "", fmt.Errorf("url blocked: %w", err)
	}
	return value, nil
}

// fetchDownload 用策略保护的 HTTP client 在 Go 侧（宿主）下载目标 URL——不经 Shell/curl（P0-02）。
// 每次重定向重新校验目标地址；响应读取受 limit 上限约束。
func (s *sandboxServer) fetchDownload(ctx context.Context, target string, limit int64) ([]byte, error) {
	client := NewOutboundHTTPClient(s.policy, maxDownloadReadTimeout)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxDownloadRedirects {
			return fmt.Errorf("too many redirects (max %d)", maxDownloadRedirects)
		}
		return ValidateOutboundHTTPURL(req.URL.String(), s.policy)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("download request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail := make([]byte, 512)
		n, _ := resp.Body.Read(detail)
		return nil, fmt.Errorf("download failed: http status %d: %s", resp.StatusCode, truncateUTF8(string(detail[:n]), 512))
	}
	// 多读 1 字节区分"恰好等于上限"与"被截断"。
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download read failed: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download exceeds size limit (%d bytes)", limit)
	}
	return data, nil
}

func (s *sandboxServer) handleDownload(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	target, err := validateFetchURL(req.GetString("url", ""), s.policy)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	savePath := strings.TrimSpace(req.GetString("save_path", ""))
	maxBytes := int(req.GetFloat("max_bytes", float64(s.cfg.OutputLimitBytes)))
	if maxBytes <= 0 || maxBytes > 8<<20 {
		maxBytes = s.cfg.OutputLimitBytes
	}

	limit := int64(maxBytes)
	if savePath != "" {
		limit = maxDownloadFileBytes
	}
	body, err := s.fetchDownload(ctx, target, limit)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}

	if savePath != "" {
		path, err := sanitizeWorkspacePath(s.cfg.WorkspaceDir, savePath)
		if err != nil {
			return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
		}
		// 经容器 stdin 解码写入工作区（复用 writeBytes 管道，URL 不经过 Shell）。
		if err := s.writeWorkspaceBytes(ctx, scope, path, body, 10*time.Minute); err != nil {
			return resultJSON(map[string]any{"ok": false, "error": "save download failed", "detail": err.Error()}), nil
		}
		return resultJSON(map[string]any{"ok": true, "path": path, "size_bytes": len(body)}), nil
	}

	// 直接返回正文（已在 fetch 内按 maxBytes 截断）。
	return resultJSON(map[string]any{"ok": true, "content": string(body)}), nil
}
