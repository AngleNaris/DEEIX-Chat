package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)
// sanitizeWorkspacePath 校验并规范化工作区路径：只允许 /workspace 内（或相对路径），
// 拒绝 .. 逃逸、绝对路径越界、空路径。返回容器内绝对路径。
func sanitizeWorkspacePath(workspace, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strings.ContainsRune(raw, '\x00') {
		return "", fmt.Errorf("path contains NUL")
	}
	p := filepath.ToSlash(filepath.Clean(strings.ReplaceAll(raw, "\\", "/")))
	if p == "." || p == "/" {
		return "", fmt.Errorf("path is a directory root: %s", raw)
	}
	var abs string
	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		abs = filepath.Clean(filepath.Join(workspace, p))
	}
	ws := filepath.Clean(workspace)
	if abs != ws && !strings.HasPrefix(abs, ws+"/") {
		return "", fmt.Errorf("path escapes workspace: %s", raw)
	}
	if strings.HasPrefix(abs, "/proc/") || strings.HasPrefix(abs, "/sys/") || strings.HasPrefix(abs, "/etc/") || strings.HasPrefix(abs, "/tmp/") {
		return "", fmt.Errorf("path outside allowed area: %s", raw)
	}
	return abs, nil
}

func (s *sandboxServer) handleWriteFile(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	path, err := sanitizeWorkspacePath(s.cfg.WorkspaceDir, req.GetString("path", ""))
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	b64 := req.GetString("content_base64", "")
	text := req.GetString("content_text", "")
	if b64 == "" && text == "" {
		return resultJSON(map[string]any{"ok": false, "error": "content_base64 or content_text is required"}), nil
	}
	if b64 != "" {
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil {
			return resultJSON(map[string]any{"ok": false, "error": "invalid content_base64"}), nil
		}
		// 大载荷走 stdin 注入，避免 exec 参数过长。
		return s.writeBytes(ctx, scope, path, data, true)
	}
	return s.writeBytes(ctx, scope, path, []byte(text), false)
}

func (s *sandboxServer) writeBytes(ctx context.Context, scope, path string, data []byte, isBinary bool) (*mcp.CallToolResult, error) {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "/workspace"
	}
	// 先建目录再写文件：二进制用 base64 经 stdin 解码，文本用 cat heredoc 不可靠，统一 python3。
	script := fmt.Sprintf(`python3 -c "import base64,sys,os; d=os.path.dirname(sys.argv[1]); os.makedirs(d,exist_ok=True); open(sys.argv[1],'wb').write(base64.b64decode(sys.stdin.read()))" %s`, shellQuote(path))
	res, err := s.exec(ctx, execRequest{scope: scope, cmd: []string{"/bin/sh", "-c", script}, stdin: []byte(base64.StdEncoding.EncodeToString(data)), timeout: 60 * time.Second})
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	if res.ExitCode != 0 {
		return resultJSON(map[string]any{"ok": false, "error": strings.TrimSpace(res.Stderr), "exit_code": res.ExitCode}), nil
	}
	kind := "text"
	if isBinary {
		kind = "binary"
	}
	return resultJSON(map[string]any{"ok": true, "path": path, "bytes": len(data), "kind": kind}), nil
}

func (s *sandboxServer) handleReadFile(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	path, err := sanitizeWorkspacePath(s.cfg.WorkspaceDir, req.GetString("path", ""))
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	maxBytes := int(req.GetFloat("max_bytes", float64(s.cfg.OutputLimitBytes)))
	if maxBytes <= 0 || maxBytes > 8<<20 {
		maxBytes = s.cfg.OutputLimitBytes
	}
	// 文件大小 + 内容 base64（限制上限，超出时截断）。
	script := fmt.Sprintf(`python3 -c "import base64,sys,os; p=sys.argv[1]; n=os.path.getsize(p); d=open(p,'rb').read(int(sys.argv[2])); print('__SIZE__', n); print(base64.b64encode(d).decode())" %s %d`, shellQuote(path), maxBytes)
	res, err := s.exec(ctx, execRequest{scope: scope, cmd: []string{"/bin/sh", "-c", script}, timeout: 60 * time.Second})
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	if res.ExitCode != 0 {
		return resultJSON(map[string]any{"ok": false, "error": strings.TrimSpace(res.Stderr)}), nil
	}
	lines := strings.SplitN(res.Stdout, "\n", 3)
	if len(lines) < 2 {
		return resultJSON(map[string]any{"ok": false, "error": "unexpected read result"}), nil
	}
	var size int64
	_, _ = fmt.Sscanf(strings.TrimSpace(lines[0]), "__SIZE__ %d", &size)
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": "decode file content failed"}), nil
	}
	truncated := size > int64(len(data))
	// 探测 MIME：常见类型用扩展名粗判。
	return resultJSON(map[string]any{
		"ok":        true,
		"path":      path,
		"size":      size,
		"truncated": truncated,
		"mime":      guessMIME(path, data),
		"content":   base64.StdEncoding.EncodeToString(data), // 统一 base64；文本由模型自行解码判断
	}), nil
}

func (s *sandboxServer) handleListFiles(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	raw := req.GetString("path", s.cfg.WorkspaceDir)
	path, err := sanitizeWorkspacePath(s.cfg.WorkspaceDir, raw)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	script := fmt.Sprintf(`python3 -c "import os,sys,json; p=sys.argv[1]; items=[]; 
for e in sorted(os.listdir(p)): 
    fp=os.path.join(p,e); st=os.stat(fp); items.append({'name':e,'dir':os.path.isdir(fp),'size':st.st_size,'mtime':int(st.st_mtime)})
print(json.dumps(items))" %s`, shellQuote(path))
	res, err := s.exec(ctx, execRequest{scope: scope, cmd: []string{"/bin/sh", "-c", script}, timeout: 60 * 1e9})
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	if res.ExitCode != 0 {
		return resultJSON(map[string]any{"ok": false, "error": strings.TrimSpace(res.Stderr)}), nil
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &items); err != nil {
		return resultJSON(map[string]any{"ok": false, "error": "list failed", "raw": truncateUTF8(res.Stdout, 2000)}), nil
	}
	return resultJSON(map[string]any{"ok": true, "path": path, "items": items}), nil
}

// handleExportFile 将共享目录中的文件标记为导出：DEEIX 后端识别 __export__ 字段后
// 从共享卷读取文件并落库为用户文件（出现在用户文件列表 + 消息附件 + 下载链接）。
func (s *sandboxServer) handleExportFile(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	raw := strings.TrimSpace(req.GetString("path", ""))
	if raw == "" {
		return resultJSON(map[string]any{"ok": false, "error": "path is required"}), nil
	}
	// 只允许导出当前会话共享目录内的文件（防跨会话/跨用户读取）。
	sharedDir := s.mgr.SharedDir(scope)
	if !strings.HasPrefix(filepath.Clean(strings.ReplaceAll(raw, "\\", "/"))+"/", sharedDir+"/") &&
		filepath.Clean(strings.ReplaceAll(raw, "\\", "/")) != sharedDir {
		return resultJSON(map[string]any{"ok": false, "error": fmt.Sprintf("path must be inside %s (your session shared dir)", sharedDir)}), nil
	}
	name := strings.TrimSpace(req.GetString("name", ""))
	if name == "" {
		name = filepath.Base(raw)
	}
	if name == "" || name == "." || name == "/" {
		return resultJSON(map[string]any{"ok": false, "error": "invalid file name"}), nil
	}
	// 读取文件元数据（大小校验上限 20MB，与 DEEIX 上传上限一致）。
	script := fmt.Sprintf(`python3 -c "import os,sys; p=sys.argv[1]; print(os.path.getsize(p))" %s`, shellQuote(raw))
	res, err := s.exec(ctx, execRequest{scope: scope, cmd: []string{"/bin/sh", "-c", script}, timeout: 30 * time.Second})
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	if res.ExitCode != 0 {
		return resultJSON(map[string]any{"ok": false, "error": fmt.Sprintf("file not found or not readable: %s", raw)}), nil
	}
	var size int64
	_, _ = fmt.Sscanf(strings.TrimSpace(res.Stdout), "%d", &size)
	if size <= 0 {
		return resultJSON(map[string]any{"ok": false, "error": "file is empty"}), nil
	}
	if size > 20<<20 {
		return resultJSON(map[string]any{"ok": false, "error": fmt.Sprintf("file exceeds 20MB limit (%d bytes)", size)}), nil
	}
	return resultJSON(map[string]any{
		"ok":        true,
		"file_path": raw,
		"name":      name,
		"size":      size,
		"note":      "文件已导出，用户可在对话中下载；在最终回答中给出下载链接。",
		"__export__": []map[string]string{{
			"path": raw,
			"name": name,
		}},
	}), nil
}

// shellQuote 单引号包裹 shell 参数（容器内 python 路径参数）。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// guessMIME 按扩展名粗判 MIME（仅供 read_file 返回参考）。
func guessMIME(path string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".m4a", ".aac":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".json":
		return "application/json"
	case ".csv":
		return "text/csv"
	case ".txt", ".md", ".log":
		return "text/plain"
	}
	if len(data) > 0 {
		// 简单魔数探测
		if len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8 {
			return "image/jpeg"
		}
		if len(data) > 7 && string(data[:4]) == "\x89PNG" {
			return "image/png"
		}
		if len(data) > 2 && string(data[:3]) == "ID3" {
			return "audio/mpeg"
		}
		if len(data) > 11 && string(data[8:12]) == "ftyp" {
			return "video/mp4"
		}
	}
	return "application/octet-stream"
}
