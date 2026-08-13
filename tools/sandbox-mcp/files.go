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

// pyRealpathGuard 容器内 python 断言片段：校验路径（含符号链接解析后）仍位于 r 根内。
// 调用脚本约定变量 p=目标路径、r=允许根（两者都经 shellQuote 作为 argv 传入）。
func pyRealpathGuard() string {
	return `rp=os.path.realpath(p); assert rp==r or rp.startswith(r+'/'), 'path escapes allowed root';`
}

const exportFilePythonScript = `import os, secrets, stat, sys

p, r, d, name = sys.argv[1:5]


def open_dir_no_symlinks(path):
    current = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
    try:
        for part in os.path.abspath(path).split(os.sep)[1:]:
            if not part:
                continue
            next_fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=current)
            os.close(current)
            current = next_fd
        return current
    except Exception:
        os.close(current)
        raise


def open_file_beneath(root, target):
    root = os.path.abspath(root)
    target = os.path.abspath(target)
    rel = os.path.relpath(target, root)
    assert rel != os.pardir and not rel.startswith(os.pardir + os.sep), 'path escapes allowed root'
    parts = rel.split(os.sep)
    assert parts and all(part not in ('', '.', '..') for part in parts), 'invalid source path'
    parent = open_dir_no_symlinks(root)
    try:
        for part in parts[:-1]:
            next_fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
            os.close(parent)
            parent = next_fd
        return os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW, dir_fd=parent)
    finally:
        os.close(parent)


source_fd = open_file_beneath(r, p)
try:
    source_stat = os.fstat(source_fd)
    assert stat.S_ISREG(source_stat.st_mode), 'not a regular file'
    size = source_stat.st_size
    assert size > 0, 'file is empty'
    assert size <= 20 * 1024 * 1024, 'file exceeds 20MB limit'

    dest_dir_fd = open_dir_no_symlinks(d)
    temp_name = '.deeix-export-' + secrets.token_hex(16)
    temp_fd = None
    published = False
    try:
        try:
            existing = os.stat(name, dir_fd=dest_dir_fd, follow_symlinks=False)
        except FileNotFoundError:
            existing = None
        assert existing is None or not stat.S_ISLNK(existing.st_mode), 'refusing symlink target'

        temp_fd = os.open(
            temp_name,
            os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
            0o644,
            dir_fd=dest_dir_fd,
        )
        copied = 0
        while True:
            chunk = os.read(source_fd, 1024 * 1024)
            if not chunk:
                break
            copied += len(chunk)
            assert copied <= 20 * 1024 * 1024, 'file exceeds 20MB limit'
            offset = 0
            while offset < len(chunk):
                offset += os.write(temp_fd, chunk[offset:])
        assert copied == size, 'source changed during export'
        os.fsync(temp_fd)
        temp_stat = os.fstat(temp_fd)
        os.replace(temp_name, name, src_dir_fd=dest_dir_fd, dst_dir_fd=dest_dir_fd)
        published = True

        verify_fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW, dir_fd=dest_dir_fd)
        try:
            verify_stat = os.fstat(verify_fd)
            assert (verify_stat.st_dev, verify_stat.st_ino) == (temp_stat.st_dev, temp_stat.st_ino), 'destination changed during export'
        finally:
            os.close(verify_fd)
    finally:
        if temp_fd is not None:
            os.close(temp_fd)
        if not published:
            try:
                os.unlink(temp_name, dir_fd=dest_dir_fd)
            except FileNotFoundError:
                pass
        os.close(dest_dir_fd)
    print(size)
finally:
    os.close(source_fd)
`

func exportFileCopyScript(workspacePath, workspaceRoot, sharedDir, name string) string {
	return fmt.Sprintf("python3 -c %s %s %s %s %s", shellQuote(exportFilePythonScript), shellQuote(workspacePath), shellQuote(workspaceRoot), shellQuote(sharedDir), shellQuote(name))
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
	if err := s.writeWorkspaceBytes(ctx, scope, path, data, 60*time.Second); err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	kind := "text"
	if isBinary {
		kind = "binary"
	}
	return resultJSON(map[string]any{"ok": true, "path": path, "bytes": len(data), "kind": kind}), nil
}

// writeWorkspaceBytes 将字节写入工作区文件（容器内 python 解码，含 realpath/symlink 校验，P0-06）。
// 供 sandbox_write_file 与 sandbox_download 复用。
func (s *sandboxServer) writeWorkspaceBytes(ctx context.Context, scope, path string, data []byte, timeout time.Duration) error {
	ws := s.cfg.WorkspaceDir
	// 先校验目标（存在则拒绝 symlink），再建目录并校验目录 realpath，最后写入。
	script := fmt.Sprintf(`python3 -c "import base64,sys,os; p=sys.argv[1]; r=sys.argv[2]; %s assert not os.path.islink(p), 'refusing symlink target'; d=os.path.dirname(p); os.makedirs(d,exist_ok=True); rd=os.path.realpath(d); assert rd==r or rd.startswith(r+'/'), 'path escapes workspace'; open(p,'wb').write(base64.b64decode(sys.stdin.read()))" %s %s`, pyRealpathGuard(), shellQuote(path), shellQuote(ws))
	res, err := s.exec(ctx, execRequest{scope: scope, cmd: []string{"/bin/sh", "-c", script}, stdin: []byte(base64.StdEncoding.EncodeToString(data)), timeout: timeout})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s", strings.TrimSpace(res.Stderr))
	}
	return nil
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
	// 文件大小 + 内容 base64（限制上限，超出时截断）；realpath 校验防 symlink 逃逸（P0-06）。
	script := fmt.Sprintf(`python3 -c "import base64,sys,os; p=sys.argv[1]; r=sys.argv[2]; %s n=os.path.getsize(p); d=open(p,'rb').read(int(sys.argv[3])); print('__SIZE__', n); print(base64.b64encode(d).decode())" %s %s %d`, pyRealpathGuard(), shellQuote(path), shellQuote(s.cfg.WorkspaceDir), maxBytes)
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
	// lstat 不跟随 symlink（防止列目录时泄露外部文件大小/mtime）；realpath 校验防逃逸（P0-06）。
	script := fmt.Sprintf(`python3 -c "import os,sys,json; p=sys.argv[1]; r=sys.argv[2]; %s items=[];
for e in sorted(os.listdir(p)):
    fp=os.path.join(p,e); st=os.lstat(fp); items.append({'name':e,'dir':os.path.isdir(fp) and not os.path.islink(fp),'size':st.st_size,'mtime':int(st.st_mtime)})
print(json.dumps(items))" %s %s`, pyRealpathGuard(), shellQuote(path), shellQuote(s.cfg.WorkspaceDir))
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
	// Export accepts a workspace file and copies it into the current scope's shared directory.
	workspacePath, err := sanitizeWorkspacePath(s.cfg.WorkspaceDir, raw)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	name := strings.TrimSpace(req.GetString("name", ""))
	if name == "" {
		name = filepath.Base(workspacePath)
	}
	if filepath.Base(name) != name || name == "." || name == "/" || strings.ContainsRune(name, '\x00') {
		return resultJSON(map[string]any{"ok": false, "error": "invalid file name"}), nil
	}
	sharedDir := s.mgr.SharedDir(scope)
	dest := filepath.Join(sharedDir, name)
	script := exportFileCopyScript(workspacePath, s.cfg.WorkspaceDir, sharedDir, name)
	res, err := s.exec(ctx, execRequest{scope: scope, cmd: []string{"/bin/sh", "-c", script}, timeout: 30 * time.Second})
	if err != nil || res.ExitCode != 0 {
		return resultJSON(map[string]any{"ok": false, "error": "file not found, unsafe, or exceeds 20MB", "detail": safeErr(err, res)}), nil
	}
	var size int64
	_, _ = fmt.Sscanf(strings.TrimSpace(res.Stdout), "%d", &size)
	return resultJSON(map[string]any{"ok": true, "file_path": dest, "name": name, "size": size, "__export__": []map[string]string{{"path": dest, "name": name}}}), nil
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
