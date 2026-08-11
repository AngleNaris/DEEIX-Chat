package main

import (
	"context"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *sandboxServer) handleSpawn(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	image := strings.TrimSpace(req.GetString("image", ""))
	if image == "" {
		image = s.cfg.BaseImage
	}
	created, err := s.mgr.Spawn(ctx, scope, image)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	return resultJSON(withRecreatedFlag(map[string]any{
		"ok":        true,
		"scope":     scope,
		"image":     image,
		"workspace": s.cfg.WorkspaceDir,
	}, created)), nil
}

func (s *sandboxServer) handlePS(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// 聚合视图：列出全部存活会话（管理用途，含所有用户 scope）。
	// 同样要求 DEEIX _meta（避免未授权调用探测会话信息）。
	if _, err := s.scopeFromRequest(ctx); err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	sessions := s.mgr.List()
	items := make([]map[string]any, 0, len(sessions))
	for _, sess := range sessions {
		items = append(items, map[string]any{
			"scope":        sess.Scope,
			"image":        sess.Image,
			"last_used_at": sess.LastUsedAt.Format(time.RFC3339),
		})
	}
	return resultJSON(map[string]any{"ok": true, "sessions": items}), nil
}

func (s *sandboxServer) handleKill(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	if err := s.mgr.Kill(ctx, scope); err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	return resultJSON(map[string]any{"ok": true, "scope": scope, "killed": true}), nil
}

func (s *sandboxServer) handleReset(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	if err := s.mgr.Reset(ctx, scope); err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	return resultJSON(map[string]any{"ok": true, "scope": scope, "reset": true}), nil
}
