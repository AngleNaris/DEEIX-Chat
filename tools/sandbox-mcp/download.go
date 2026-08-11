package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// validateFetchURL 仅允许 http/https 目标（防 file://、gopher 等协议滥用）。
func validateFetchURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("only http/https urls are allowed")
	}
	return u.String(), nil
}

func (s *sandboxServer) handleDownload(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	target, err := validateFetchURL(req.GetString("url", ""))
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	savePath := strings.TrimSpace(req.GetString("save_path", ""))
	maxBytes := int(req.GetFloat("max_bytes", float64(s.cfg.OutputLimitBytes)))
	if maxBytes <= 0 || maxBytes > 8<<20 {
		maxBytes = s.cfg.OutputLimitBytes
	}

	if savePath != "" {
		path, err := sanitizeWorkspacePath(s.cfg.WorkspaceDir, savePath)
		if err != nil {
			return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
		}
		// 容器内 curl 下载到工作区文件。
		script := fmt.Sprintf("mkdir -p %q && curl -sSL --max-time 120 -o %q %q && echo __OK__ && stat -c '%%s' %q",
			s.cfg.WorkspaceDir, path, target, path)
		res, err := s.exec(ctx, execRequest{scope: scope, cmd: []string{"/bin/sh", "-c", script}, timeout: 150 * time.Second})
		if err != nil {
			return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
		}
		if res.ExitCode != 0 || !strings.Contains(res.Stdout, "__OK__") {
			return resultJSON(map[string]any{"ok": false, "error": "download failed", "detail": truncateUTF8(res.Stderr, 2000)}), nil
		}
		size := strings.TrimSpace(strings.ReplaceAll(res.Stdout, "__OK__", ""))
		return resultJSON(map[string]any{"ok": true, "path": path, "size_bytes": strings.TrimSpace(size)}), nil
	}

	// 直接返回正文（截断）。
	script := fmt.Sprintf("curl -sSL --max-time 60 -L %q | head -c %d", target, maxBytes)
	res, err := s.exec(ctx, execRequest{scope: scope, cmd: []string{"/bin/sh", "-c", script}, timeout: 90 * time.Second})
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	return resultJSON(map[string]any{"ok": true, "content": res.Stdout}), nil
}
