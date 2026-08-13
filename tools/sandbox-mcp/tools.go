package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// sandboxServer 持有全部工具 handler 所需的依赖。
type sandboxServer struct {
	cfg    *Config
	mgr    *SessionManager
	d      *dockerClient
	policy OutboundPolicy // sandbox_download 出站策略（SSRF 防护）
}

func newSandboxServer(cfg *Config, mgr *SessionManager) (*sandboxServer, error) {
	policy, err := cfg.DownloadPolicy()
	if err != nil {
		return nil, err
	}
	return &sandboxServer{cfg: cfg, mgr: mgr, d: mgr.d, policy: policy}, nil
}

// scopeFromRequest 从请求上下文解析会话 scope；无 _meta 时拒绝（仅服务 DEEIX 注入的调用）。
func (s *sandboxServer) scopeFromRequest(ctx context.Context) (string, error) {
	meta, ok := MetaFromContext(ctx)
	if !ok || meta == nil {
		return "", fmt.Errorf("missing DEEIX _meta.user_id: this MCP server only serves DEEIX-injected tool calls")
	}
	return SessionScope(meta), nil
}

// registerTools 注册全部沙箱工具。
func registerTools(mcpServer *server.MCPServer, s *sandboxServer) {
	execTool := mcp.NewTool("sandbox_exec",
		mcp.WithDescription(`在用户隔离的沙箱容器内执行 shell 命令（默认 /bin/sh）。
工作目录为 /workspace，会话按 (user, conversation) 隔离，闲置自动回收、重建后环境保留（pip 缓存命中）。
	可在容器内使用 apt、pip、npm、uv 安装所需环境。

输出截断到 64KB。长任务建议使用 sandbox_task_start 后台执行。
【共享目录】每次结果会返回 shared_dir（形如 /shared/deeix-<uid>-<cid>，本会话专属）。
需要多模态工具（如 transcribe_audio / ocr / read_image）处理文件时，先把文件复制到 shared_dir 下，
再把这些工具的 file_path/image_path 参数填为 /shared/<scope>/<文件名>。`),
		mcp.WithString("command", mcp.Required(), mcp.Description("要执行的命令（一行或多行 shell）")),
		mcp.WithString("cwd", mcp.Description("工作目录，默认 /workspace")),
		mcp.WithNumber("timeout", mcp.Description("超时秒数（默认 120，最大 600）")),
	)
	taskTool := mcp.NewTool("sandbox_task_start",
		mcp.WithDescription(`在沙箱内以后台任务方式启动长命令（如长时间训练/下载），立即返回任务 ID；用 sandbox_task_poll 查询结果。`),
		mcp.WithString("command", mcp.Required(), mcp.Description("要后台执行的命令")),
	)
	pollTool := mcp.NewTool("sandbox_task_poll",
		mcp.WithDescription("查询后台任务状态与已产出的输出。任务结束后返回完整 stdout/stderr 与退出码。"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("sandbox_task_start 返回的任务 ID")),
	)
	cancelTool := mcp.NewTool("sandbox_task_cancel",
		mcp.WithDescription("终止一个运行中的后台任务。"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("任务 ID")),
	)
	writeTool := mcp.NewTool("sandbox_write_file",
		mcp.WithDescription(`将内容写入沙箱工作区文件（路径必须位于 /workspace 内，禁止 .. 逃逸）。
音频/图片等二进制内容用 content_base64 传入；文本用 content_text。
需要多模态工具处理时，先写到 /workspace 再复制到 shared_dir（见 sandbox_exec 的返回）。`),
		mcp.WithString("path", mcp.Required(), mcp.Description("工作区相对或绝对路径，如 /workspace/input.mp3 或 input.mp3")),
		mcp.WithString("content_base64", mcp.Description("base64 编码的二进制内容（与 content_text 二选一）")),
		mcp.WithString("content_text", mcp.Description("纯文本内容（与 content_base64 二选一）")),
	)
	readTool := mcp.NewTool("sandbox_read_file",
		mcp.WithDescription(`读取沙箱工作区文件。默认返回文本；二进制文件返回 base64 + mime 类型。`),
		mcp.WithString("path", mcp.Required(), mcp.Description("工作区内文件路径")),
		mcp.WithNumber("max_bytes", mcp.Description("返回内容上限（默认 65536）")),
	)
	listTool := mcp.NewTool("sandbox_list_files",
		mcp.WithDescription("列出沙箱工作区目录内容。"),
		mcp.WithString("path", mcp.Description("目录路径，默认 /workspace")),
	)
	downloadTool := mcp.NewTool("sandbox_download",
		mcp.WithDescription(`从网络抓取 URL：save_path 为空时返回正文（截断），否则保存为工作区文件并返回文件信息。
用于获取网页/文件供后续分析（如抓取音频、文档）。`),
		mcp.WithString("url", mcp.Required(), mcp.Description("http(s) URL")),
		mcp.WithString("save_path", mcp.Description("保存为工作区文件（如 input.mp3）；为空则直接返回正文")),
		mcp.WithNumber("max_bytes", mcp.Description("返回正文上限（默认 65536；保存到文件时忽略）")),
	)
	exportTool := mcp.NewTool("sandbox_export_file",
		mcp.WithDescription(`把 /workspace 中的沙箱文件发送给用户。工具会把文件复制到当前会话的共享 scope，并通过 DEEIX __export__ 附件链交付。
	文件必须位于 /workspace，禁止路径逃逸或 symlink 越界；无需先手工复制到 shared_dir。
	可选 name 指定用户看到的文件名（保留扩展名）。上限 20MB。`),
		mcp.WithString("path", mcp.Required(), mcp.Description("工作区内文件路径，如 /workspace/report.pdf")),

		mcp.WithString("name", mcp.Description("用户可见文件名（默认取原文件名）")),
	)
	spawnTool := mcp.NewTool("sandbox_spawn",
		mcp.WithDescription(`为当前会话按需拉取镜像重建沙箱容器（环境拉取）。
默认镜像已含 python3 + ffmpeg + 常用数据分析包；如需要 Node/其它运行时，用此工具指定镜像（如 node:22-slim）。
重建后工作区卷保留，pip 缓存秒级命中。`),
		mcp.WithString("image", mcp.Description("Docker 镜像名，如 node:22-slim；为空使用默认镜像")),
	)
	psTool := mcp.NewTool("sandbox_ps",
		mcp.WithDescription("列出当前用户的存活沙箱会话容器（含 scope/镜像/最近使用时间）。"),
	)
	killTool := mcp.NewTool("sandbox_kill",
		mcp.WithDescription("销毁当前会话容器（工作区与已装环境保留，下次调用自动重建）。"),
	)
	resetTool := mcp.NewTool("sandbox_reset",
		mcp.WithDescription("销毁当前会话容器并清空工作区（回到全新状态）。"),
	)

	mcpServer.AddTool(execTool, s.handleExec)
	mcpServer.AddTool(taskTool, s.handleTaskStart)
	mcpServer.AddTool(pollTool, s.handleTaskPoll)
	mcpServer.AddTool(cancelTool, s.handleTaskCancel)
	mcpServer.AddTool(writeTool, s.handleWriteFile)
	mcpServer.AddTool(readTool, s.handleReadFile)
	mcpServer.AddTool(listTool, s.handleListFiles)
	mcpServer.AddTool(downloadTool, s.handleDownload)
	mcpServer.AddTool(exportTool, s.handleExportFile)
	mcpServer.AddTool(spawnTool, s.handleSpawn)
	mcpServer.AddTool(psTool, s.handlePS)
	mcpServer.AddTool(killTool, s.handleKill)
	mcpServer.AddTool(resetTool, s.handleReset)
}

// resultJSON 构造 JSON 文本结果。
func resultJSON(v any) *mcp.CallToolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultText(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return mcp.NewToolResultText(string(data))
}

// withRecreatedFlag 在结果中附加 session_recreated 标志（重建会话时）。
func withRecreatedFlag(v map[string]any, created bool) map[string]any {
	if created {
		v["session_recreated"] = true
	}
	return v
}

// execRequest 统一包装同步执行（供 exec / 文件 / 下载工具复用）。
type execRequest struct {
	scope       string
	cmd         []string
	cwd         string // 容器内工作目录（经 Exec WorkingDir 传递，必须已通过路径校验）
	stdin       []byte
	timeout     time.Duration
	outputLimit int
}

func (s *sandboxServer) exec(ctx context.Context, req execRequest) (*execResult, error) {
	session, created, release, err := s.mgr.GetOrCreate(ctx, req.scope)
	if err != nil {
		return nil, err
	}
	defer release()
	if req.timeout <= 0 {
		req.timeout = s.cfg.ExecTimeout
	}
	if req.timeout > s.cfg.MaxExecTimeout {
		req.timeout = s.cfg.MaxExecTimeout
	}
	limit := req.outputLimit
	if limit <= 0 {
		limit = s.cfg.OutputLimitBytes
	}
	res, err := s.d.execInContainer(ctx, session.Container, req.cmd, req.cwd, req.stdin, req.timeout)
	if err != nil {
		return nil, err
	}
	res.Stdout = truncateUTF8(res.Stdout, limit)
	res.Stderr = truncateUTF8(res.Stderr, limit)
	_ = created
	return res, nil
}

// truncateUTF8 按字节上限截断并保留合法 UTF-8 边界。
func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := s[:limit]
	for !utf8.ValidString(cut) && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return cut + "\n...[truncated]"
}

// ---- 工具 handlers ----

func (s *sandboxServer) handleExec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	command := strings.TrimSpace(req.GetString("command", ""))
	if command == "" {
		return resultJSON(map[string]any{"ok": false, "error": "command is required"}), nil
	}
	// P0-04：cwd 必须位于工作区内，且经 Exec WorkingDir 传递（不经 shell cd 拼接）。
	cwd, err := sanitizeWorkspacePath(s.cfg.WorkspaceDir, req.GetString("cwd", s.cfg.WorkspaceDir))
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": fmt.Sprintf("invalid cwd: %v", err)}), nil
	}
	timeoutSec := int(req.GetFloat("timeout", float64(s.cfg.ExecTimeout.Seconds())))
	res, err := s.exec(ctx, execRequest{scope: scope, cmd: []string{"/bin/sh", "-c", command}, cwd: cwd, timeout: time.Duration(timeoutSec) * time.Second})
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	return resultJSON(map[string]any{
		"ok":         res.ExitCode == 0,
		"exit_code":  res.ExitCode,
		"stdout":     res.Stdout,
		"stderr":     res.Stderr,
		"shared_dir": s.mgr.SharedDir(scope),
	}), nil
}

func (s *sandboxServer) handleTaskStart(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	command := strings.TrimSpace(req.GetString("command", ""))
	if command == "" {
		return resultJSON(map[string]any{"ok": false, "error": "command is required"}), nil
	}
	session, created, release, err := s.mgr.GetOrCreate(ctx, scope)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	defer release()
	// Reserve the slot before starting the process so concurrent callers cannot exceed quota.
	session.mu.Lock()
	if len(session.tasks) >= s.cfg.MaxTasksPerSession {
		session.mu.Unlock()
		return resultJSON(map[string]any{"ok": false, "error": fmt.Sprintf("task limit %d reached for this session", s.cfg.MaxTasksPerSession)}), nil
	}
	taskID := fmt.Sprintf("t%d", time.Now().UnixNano()/1e6)
	out := fmt.Sprintf("/tmp/%s.out", taskID)
	task := &BackgroundTask{ID: taskID, Output: out, StartedAt: time.Now()}
	session.tasks[taskID] = task
	session.mu.Unlock()

	cmd := fmt.Sprintf("nohup setsid /bin/sh -c %s > %s 2>&1 & echo $!", shellQuote(command), shellQuote(out))
	res, err := s.d.execInContainer(ctx, session.Container, []string{"/bin/sh", "-c", cmd}, "", nil, 30*time.Second)
	if err != nil || res.ExitCode != 0 {
		session.mu.Lock()
		delete(session.tasks, taskID)
		session.mu.Unlock()
		return resultJSON(map[string]any{"ok": false, "error": "failed to start task", "detail": safeErr(err, res)}), nil
	}
	pid := strings.TrimSpace(res.Stdout)
	if pid == "" || strings.Trim(pid, "0123456789") != "" {
		session.mu.Lock()
		delete(session.tasks, taskID)
		session.mu.Unlock()
		return resultJSON(map[string]any{"ok": false, "error": "failed to capture task pid"}), nil
	}
	session.mu.Lock()
	task.PID = pid
	session.mu.Unlock()
	return resultJSON(withRecreatedFlag(map[string]any{"ok": true, "task_id": taskID, "pid": pid}, created)), nil

}

func (s *sandboxServer) handleTaskPoll(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	taskID := strings.TrimSpace(req.GetString("task_id", ""))
	if taskID == "" {
		return resultJSON(map[string]any{"ok": false, "error": "task_id is required"}), nil
	}
	session, _, release, err := s.mgr.GetOrCreate(ctx, scope)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	defer release()
	session.mu.Lock()
	task, ok := session.tasks[taskID]
	session.mu.Unlock()
	if !ok {
		return resultJSON(map[string]any{"ok": false, "error": "unknown task_id (session 可能已重建)"}), nil
	}
	// 判断任务是否结束：检查容器内进程是否仍存活。
	alive, err := s.d.execInContainer(ctx, session.Container, []string{"/bin/sh", "-c", fmt.Sprintf("kill -0 %s 2>/dev/null", task.PID)}, "", nil, 20*time.Second)
	done := err != nil || alive.ExitCode != 0
	output, outErr := s.d.execInContainer(ctx, session.Container, []string{"/bin/sh", "-c", fmt.Sprintf("cat %s 2>/dev/null", task.Output)}, "", nil, 20*time.Second)
	// P1-03：第二次 exec 的错误不得忽略（容器消失/Docker 断开时 output 为 nil，直接访问会 panic）。
	if outErr != nil || output == nil {
		result := map[string]any{"ok": false, "error": "task output unavailable (session container lost?)"}
		if outErr != nil {
			result["detail"] = outErr.Error()
		}
		session.mu.Lock()
		delete(session.tasks, taskID)
		session.mu.Unlock()
		return resultJSON(result), nil
	}
	text := truncateUTF8(output.Stdout, s.cfg.OutputLimitBytes*2)
	// 尝试解析退出码：容器内通过 wait 拿不到（跨 exec），仅返回输出与是否存活。
	result := map[string]any{"ok": true, "task_id": taskID, "running": !done, "output": text}
	if done {
		session.mu.Lock()
		delete(session.tasks, taskID)
		session.mu.Unlock()
		result["finished"] = true
	}
	return resultJSON(result), nil
}

func (s *sandboxServer) handleTaskCancel(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := s.scopeFromRequest(ctx)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	taskID := strings.TrimSpace(req.GetString("task_id", ""))
	if taskID == "" {
		return resultJSON(map[string]any{"ok": false, "error": "task_id is required"}), nil
	}
	session, _, release, err := s.mgr.GetOrCreate(ctx, scope)
	if err != nil {
		return resultJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	defer release()
	session.mu.Lock()
	task, ok := session.tasks[taskID]
	if ok {
		delete(session.tasks, taskID)
	}
	session.mu.Unlock()
	if !ok {
		return resultJSON(map[string]any{"ok": false, "error": "unknown task_id"}), nil
	}
	_, _ = s.d.execInContainer(ctx, session.Container, []string{"/bin/sh", "-c", fmt.Sprintf("kill -TERM -- -%s 2>/dev/null; sleep 1; kill -KILL -- -%s 2>/dev/null; rm -f %s", shellQuote(task.PID), shellQuote(task.PID), shellQuote(task.Output))}, "", nil, 20*time.Second)

	return resultJSON(map[string]any{"ok": true, "task_id": taskID, "cancelled": true}), nil
}

func safeErr(err error, res *execResult) string {
	if err != nil {
		return err.Error()
	}
	if res != nil && res.ExitCode != 0 {
		return strings.TrimSpace(res.Stderr)
	}
	return ""
}
