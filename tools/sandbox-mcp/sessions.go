package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Meta 是 DEEIX 后端注入的请求元数据（client.go 将 _meta 写入 MCP 请求顶层 params）。
// user_id/conversation_id 必须经 HMAC 签名（见 ParseMeta），不能作为独立身份凭据。
type Meta struct {
	UserID         uint
	ConversationID uint
	RequestID      string
	CallID         string
}

// metaTimestampWindow _meta 签名的允许时钟偏移（短期签名，防重放）。
const metaTimestampWindow = 5 * time.Minute

// metaSignature 计算 _meta 的 HMAC-SHA256 签名（后端与沙箱共享密钥，canonical 串保持一致）。
func metaSignature(secret string, userID, conversationID uint, requestID, callID string, ts int64) string {
	canonical := fmt.Sprintf("user_id=%d\nconversation_id=%d\nrequest_id=%s\ncall_id=%s\nts=%d", userID, conversationID, requestID, callID, ts)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

// ParseMeta 从原始请求体提取 DEEIX 注入的 _meta 并校验 HMAC 签名。
// 签名缺失、不匹配或时间戳过期均拒绝（身份必须来自已验证声明，P0-07）。
// 缺失 user_id 视为非法调用（本服务仅面向 DEEIX 后端）。
func ParseMeta(raw []byte, hmacKey string) (*Meta, error) {
	// 兼容两种注入位置：DEEIX 放顶层 params._meta；未来其它客户端可能放 arguments._meta。
	var envelope struct {
		Params struct {
			Meta map[string]any `json:"_meta"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Params.Meta == nil {
		return nil, fmt.Errorf("missing _meta: this MCP server only serves DEEIX-injected calls")
	}
	m := envelope.Params.Meta
	userID, ok := toUint(m["user_id"])
	if !ok || userID == 0 {
		return nil, fmt.Errorf("missing or invalid _meta.user_id")
	}
	meta := &Meta{UserID: userID}
	if cid, ok := toUint(m["conversation_id"]); ok {
		meta.ConversationID = cid
	}
	if meta.ConversationID == 0 {
		return nil, fmt.Errorf("missing or invalid _meta.conversation_id")
	}
	if rid, ok := m["request_id"].(string); ok {
		meta.RequestID = strings.TrimSpace(rid)
	}
	if meta.RequestID == "" {
		return nil, fmt.Errorf("missing or invalid _meta.request_id")
	}
	if callID, ok := m["call_id"].(string); ok {
		meta.CallID = strings.TrimSpace(callID)
	}
	if meta.CallID == "" {
		return nil, fmt.Errorf("missing or invalid _meta.call_id")
	}
	if hmacKey == "" {
		return nil, fmt.Errorf("meta hmac key not configured: refusing unsigned identity")
	}
	ts, ok := toInt64(m["ts"])
	if !ok {
		return nil, fmt.Errorf("missing or invalid _meta.ts (signed meta required)")
	}
	if drift := time.Since(time.Unix(ts, 0)); drift < -metaTimestampWindow || drift > metaTimestampWindow {
		return nil, fmt.Errorf("_meta timestamp expired")
	}
	sig, _ := m["sig"].(string)
	expected := metaSignature(hmacKey, meta.UserID, meta.ConversationID, meta.RequestID, meta.CallID, ts)
	if sig == "" || subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return nil, fmt.Errorf("_meta signature mismatch: identity must be signed by DEEIX backend")
	}
	return meta, nil
}

func toInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case float64:
		return int64(t), t == float64(int64(t))
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

func toUint(v any) (uint, bool) {
	switch t := v.(type) {
	case float64:
		return uint(t), t > 0
	case string:
		if n, err := strconv.ParseUint(strings.TrimSpace(t), 10, 64); err == nil && n > 0 {
			return uint(n), true
		}
	}
	return 0, false
}

// Session 代表一个 (user, conversation) 隔离的容器会话。
type Session struct {
	Scope      string // deeix-<uid>-<cid>
	Image      string // 会话当前使用的镜像（sandbox_spawn 可更换）
	Container  string // Docker 容器名
	CreatedAt  time.Time
	LastUsedAt time.Time
	Env        []string // 创建容器时的环境变量（镜像拉取所需）
	CacheMount bool     // 是否挂用户级缓存卷
	CacheVol   string   // 按 verified user scope 派生的缓存卷
	mu         sync.Mutex
	ready      chan struct{}
	createErr  error
	activeOps  int
	reclaiming bool
	reclaimed  chan struct{}
	tasks      map[string]*BackgroundTask // 后台任务
}

// BackgroundTask 容器内的后台任务（nohup + 输出文件轮询）。
type BackgroundTask struct {
	ID        string
	PID       string
	Output    string // 容器内输出文件路径
	StartedAt time.Time
}

// SessionManager 管理全部会话：按 scope 懒创建、租约回收。
type SessionManager struct {
	cfg  *Config
	d    *dockerClient
	mu   sync.Mutex
	live map[string]*Session // scope -> session
}

func NewSessionManager(cfg *Config, d *dockerClient) *SessionManager {
	return &SessionManager{cfg: cfg, d: d, live: make(map[string]*Session)}
}

// validScopePattern scope 只允许 deeix-<数字>[-<数字>] 形式。
// 身份已由 HMAC 签名校验（ParseMeta），此处为纵深防御：防止任何异常 scope
// 被拼入宿主 bind 挂载路径（P0-05/P0-07）。
var validScopePattern = regexp.MustCompile(`^deeix-[0-9]+(-[0-9]+)?$`)

// SessionScope 生成 (user, conversation) 会话标识。
func SessionScope(meta *Meta) string {
	scope := fmt.Sprintf("deeix-%d", meta.UserID)
	if meta.ConversationID > 0 {
		scope += fmt.Sprintf("-%d", meta.ConversationID)
	}
	return scope
}

// sessionBelongsToUser 判断 scope 是否属于指定用户（sandbox_ps 过滤用）。
func sessionBelongsToUser(scope string, userID uint) bool {
	prefix := fmt.Sprintf("deeix-%d", userID)
	return scope == prefix || strings.HasPrefix(scope, prefix+"-")
}

// cacheVolumeForScope derives a stable per-user cache volume from the verified scope.
func cacheVolumeForScope(scope, prefix string) (string, error) {
	if !validScopePattern.MatchString(scope) {
		return "", fmt.Errorf("invalid session scope %q", scope)
	}
	parts := strings.Split(scope, "-")
	return prefix + "-u" + parts[1], nil
}

// GetOrCreate 获取会话；不存在则懒创建（create_if_missing 语义）。
// 返回的 release 必须在当前 Docker 操作完成后调用，避免租约回收删除执行中的容器。
func (m *SessionManager) GetOrCreate(ctx context.Context, scope string) (*Session, bool, func(), error) {
	if !validScopePattern.MatchString(scope) {
		return nil, false, nil, fmt.Errorf("invalid session scope %q", scope)
	}
	for {
		m.mu.Lock()
		if s, ok := m.live[scope]; ok {
			s.mu.Lock()
			if s.reclaiming {
				reclaimed := s.reclaimed
				s.mu.Unlock()
				m.mu.Unlock()
				select {
				case <-reclaimed:
					continue
				case <-ctx.Done():
					return nil, false, nil, ctx.Err()
				}
			}
			s.activeOps++
			s.LastUsedAt = time.Now()
			ready := s.ready
			s.mu.Unlock()
			m.mu.Unlock()

			select {
			case <-ready:
				if s.createErr != nil {
					releaseSessionOperation(s)()
					return nil, false, nil, s.createErr
				}
				return s, false, releaseSessionOperation(s), nil
			case <-ctx.Done():
				releaseSessionOperation(s)()
				return nil, false, nil, ctx.Err()
			}
		}

		cacheVol, err := cacheVolumeForScope(scope, m.cfg.CacheVolume)
		if err != nil {
			m.mu.Unlock()
			return nil, false, nil, err
		}
		now := time.Now()
		s := &Session{
			Scope: scope, Image: m.cfg.BaseImage, Container: scope,
			CreatedAt: now, LastUsedAt: now,
			Env: []string{"WORKSPACE=" + m.cfg.WorkspaceDir}, CacheMount: true, CacheVol: cacheVol,
			ready: make(chan struct{}), activeOps: 1, tasks: make(map[string]*BackgroundTask),
		}
		m.live[scope] = s
		m.mu.Unlock()

		err = m.ensureSessionContainer(ctx, s)
		s.mu.Lock()
		s.createErr = err
		close(s.ready)
		s.mu.Unlock()
		if err != nil {
			releaseSessionOperation(s)()
			m.dropSession(scope, s)
			return nil, false, nil, err
		}
		return s, true, releaseSessionOperation(s), nil
	}
}

func releaseSessionOperation(s *Session) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			if s.activeOps > 0 {
				s.activeOps--
			}
			s.LastUsedAt = time.Now()
			s.mu.Unlock()
		})
	}
}

func (m *SessionManager) dropSession(scope string, target *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.live[scope] == target {
		delete(m.live, scope)
	}
}

func (m *SessionManager) acquireExisting(ctx context.Context, scope string) (*Session, func(), bool, error) {
	if !validScopePattern.MatchString(scope) {
		return nil, nil, false, fmt.Errorf("invalid session scope %q", scope)
	}
	for {
		m.mu.Lock()
		s, ok := m.live[scope]
		if !ok {
			m.mu.Unlock()
			return nil, nil, false, nil
		}
		s.mu.Lock()
		if s.reclaiming {
			reclaimed := s.reclaimed
			s.mu.Unlock()
			m.mu.Unlock()
			select {
			case <-reclaimed:
				continue
			case <-ctx.Done():
				return nil, nil, false, ctx.Err()
			}
		}
		s.activeOps++
		s.LastUsedAt = time.Now()
		ready := s.ready
		s.mu.Unlock()
		m.mu.Unlock()

		select {
		case <-ready:
			if s.createErr != nil {
				releaseSessionOperation(s)()
				return nil, nil, false, s.createErr
			}
			return s, releaseSessionOperation(s), true, nil
		case <-ctx.Done():
			releaseSessionOperation(s)()
			return nil, nil, false, ctx.Err()
		}
	}
}

func beginSessionMaintenance(s *Session) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reclaiming || s.activeOps != 1 {
		return nil, fmt.Errorf("sandbox session is busy")
	}
	s.reclaiming = true
	s.reclaimed = make(chan struct{})
	return func() {
		s.mu.Lock()
		reclaimed := s.reclaimed
		s.reclaiming = false
		s.reclaimed = nil
		if reclaimed != nil {
			close(reclaimed)
		}
		s.mu.Unlock()
	}, nil
}

func (m *SessionManager) ensureSessionContainer(ctx context.Context, s *Session) error {
	spec, volumeName, err := m.sessionContainerSpec(s)
	if err != nil {
		return err
	}
	actual, exists, err := m.d.inspectContainerConfig(ctx, s.Container)
	if err != nil {
		return err
	}
	if !exists {
		return m.d.createContainer(ctx, spec, volumeName)
	}
	if actual.Config != nil && strings.TrimSpace(actual.Config.Image) != "" {
		s.Image = actual.Config.Image
		spec.Image = actual.Config.Image
	}
	desiredConfig, desiredHost := buildContainerConfig(spec, volumeName)
	if containerConfigMatches(actual, desiredConfig, desiredHost) {
		return nil
	}
	slog.Info("recreate sandbox container with current isolation constraints", "container", s.Container)
	backupName := s.Container + "-migration-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := m.d.renameContainer(ctx, s.Container, backupName); err != nil {
		return err
	}
	if err := m.d.createContainer(ctx, spec, volumeName); err != nil {
		if restoreErr := m.d.renameContainer(ctx, backupName, s.Container); restoreErr != nil {
			return fmt.Errorf("recreate sandbox container: %w; restore previous container name: %v", err, restoreErr)
		}
		return fmt.Errorf("recreate sandbox container: %w; previous container restored", err)
	}
	if err := m.d.removeContainer(ctx, backupName); err != nil {
		slog.Warn("remove sandbox migration backup", "container", backupName, "err", err)
	}
	return nil
}

func (m *SessionManager) sessionContainerSpec(s *Session) (containerSpec, string, error) {
	if !validScopePattern.MatchString(s.Scope) {
		return containerSpec{}, "", fmt.Errorf("invalid session scope %q", s.Scope)
	}
	// 宿主侧预先创建本会话的共享子目录：容器只 bind 挂载这一份，
	// 其他租户目录在容器内物理不可见（P0-05）。0755 允许后端只读挂载读取导出文件。
	sharedHostSub := filepath.Join(m.cfg.SharedHostDir, s.Scope)
	if err := os.MkdirAll(sharedHostSub, 0o755); err != nil {
		return containerSpec{}, "", fmt.Errorf("create shared scope dir %s: %w", sharedHostSub, err)
	}
	importsHostSub := filepath.Join(m.cfg.ImportsHostDir, s.Scope)
	if err := os.MkdirAll(importsHostSub, 0o755); err != nil {
		return containerSpec{}, "", fmt.Errorf("create imports scope dir %s: %w", importsHostSub, err)
	}
	return containerSpec{
		Name:          s.Container,
		Image:         s.Image,
		Env:           s.Env,
		Memory:        m.cfg.MemoryLimit,
		PidsLimit:     m.cfg.PidsLimit,
		CPUs:          m.cfg.CPUsLimit,
		Workspace:     m.cfg.WorkspaceDir,
		CacheMount:    s.CacheMount,
		CacheVol:      s.CacheVol,
		SharedBind:    sharedHostSub,
		SharedTarget:  filepath.Join(m.cfg.SharedMountDir, s.Scope),
		ImportsBind:   importsHostSub,
		ImportsTarget: filepath.Clean(m.cfg.ImportsMountDir),
		Network:       m.cfg.NetworkMode,
	}, "deeix-sandbox-ws-" + s.Scope, nil
}

func (m *SessionManager) createSessionContainer(ctx context.Context, s *Session) error {
	spec, volumeName, err := m.sessionContainerSpec(s)
	if err != nil {
		return err
	}
	return m.d.createContainer(ctx, spec, volumeName)
}

// SharedDir 返回当前会话在共享目录中的专属子目录（mm 多模态工具可读取）。
func (m *SessionManager) SharedDir(scope string) string {
	return m.cfg.SharedMountDir + "/" + scope
}

// Spawn 为会话更换/新建镜像容器（sandbox_spawn 语义）：销毁旧容器后按新镜像重建。
func (m *SessionManager) Spawn(ctx context.Context, scope, image string) (bool, error) {
	s, created, release, err := m.GetOrCreate(ctx, scope)
	if err != nil {
		return false, err
	}
	defer release()
	finish, err := beginSessionMaintenance(s)
	if err != nil {
		return created, err
	}
	defer finish()

	s.mu.Lock()
	currentImage := s.Image
	s.mu.Unlock()
	if image == "" || image == currentImage {
		return created, nil
	}
	if err := m.d.removeContainer(ctx, s.Container); err != nil {
		return created, err
	}
	s.mu.Lock()
	s.tasks = make(map[string]*BackgroundTask)
	s.Image = image
	s.mu.Unlock()
	if err := m.createSessionContainer(ctx, s); err != nil {
		s.mu.Lock()
		s.Image = currentImage
		s.mu.Unlock()
		if restoreErr := m.createSessionContainer(ctx, s); restoreErr != nil {
			m.dropSession(scope, s)
			return created, fmt.Errorf("spawn image %q: %w; restore image %q: %v", image, err, currentImage, restoreErr)
		}
		return created, err
	}
	return created, nil
}

// Kill 销毁会话容器（保留工作区卷与缓存卷，环境不丢）。
func (m *SessionManager) Kill(ctx context.Context, scope string) error {
	s, release, ok, err := m.acquireExisting(ctx, scope)
	if err != nil || !ok {
		return err
	}
	defer release()
	finish, err := beginSessionMaintenance(s)
	if err != nil {
		return err
	}
	defer finish()
	if err := m.d.removeContainer(ctx, s.Container); err != nil {
		return err
	}
	m.dropSession(scope, s)
	return nil
}

// Reset 销毁会话容器并清空其工作区卷。
func (m *SessionManager) Reset(ctx context.Context, scope string) error {
	s, release, ok, err := m.acquireExisting(ctx, scope)
	if err != nil || !ok {
		return err
	}
	defer release()
	finish, err := beginSessionMaintenance(s)
	if err != nil {
		return err
	}
	defer finish()
	if err := m.d.removeContainer(ctx, s.Container); err != nil {
		return err
	}
	s.mu.Lock()
	s.tasks = make(map[string]*BackgroundTask)
	s.mu.Unlock()
	volumeErr := m.removeWorkspaceVolume(scope)
	m.dropSession(scope, s)
	return volumeErr
}

func (m *SessionManager) removeWorkspaceVolume(scope string) error {
	vol := "deeix-sandbox-ws-" + scope
	if err := m.d.removeVolume(vol); err != nil {
		slog.Warn("remove workspace volume", "volume", vol, "err", err)
		return err
	}
	return nil
}

// StartReclaimer 启动闲置会话回收 goroutine（租约制：超过 TTL 未使用即销毁容器）。
func (m *SessionManager) StartReclaimer(ctx context.Context) {
	if m.cfg.ReclaimInterval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(m.cfg.ReclaimInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.reclaimOnce(ctx)
			}
		}
	}()
}

func sessionReclaimable(s *Session, now time.Time, leaseTTL time.Duration) bool {
	return !s.reclaiming && s.activeOps == 0 && len(s.tasks) == 0 && now.Sub(s.LastUsedAt) > leaseTTL
}

// reapExpiredTasksLocked 杀掉超过 TTL 的后台任务并从会话移除（调用方持有 s.mu）。
// 永不退出的任务会把容器永久钉住（sessionReclaimable 要求 tasks 为空），
// 因此在闲置回收扫描的同一 tick 内做 TTL 清理。持锁段只做快照与移除，
// Docker exec 杀进程组在锁外执行，避免阻塞同会话请求。返回被清理的任务数。
func (m *SessionManager) reapExpiredTasksLocked(ctx context.Context, s *Session, now time.Time) int {
	ttl := m.cfg.TaskTTL
	if ttl <= 0 {
		return 0
	}
	type expiredTask struct {
		id  string
		pid string
	}
	expired := make([]expiredTask, 0)
	for id, task := range s.tasks {
		if task != nil && now.Sub(task.StartedAt) > ttl {
			expired = append(expired, expiredTask{id: id, pid: task.PID})
			delete(s.tasks, id)
		}
	}
	s.mu.Unlock()
	defer s.mu.Lock()
	for _, task := range expired {
		if task.pid != "" {
			_, _ = m.d.execInContainer(
				ctx,
				s.Container,
				[]string{"/bin/sh", "-c", fmt.Sprintf("kill -TERM -- -%s 2>/dev/null; sleep 1; kill -KILL -- -%s 2>/dev/null; rm -f %s", shellQuote(task.pid), shellQuote(task.pid), shellQuote(taskOutputPathFor(task.id)))},
				"",
				nil,
				20*time.Second,
				m.cfg.OutputLimitBytes,
			)
		}
		slog.Info("reap expired background task", "container", s.Container, "task_id", task.id)
	}
	return len(expired)
}

// taskOutputPathFor 复现 handleTaskStart 的后台任务输出文件命名。
func taskOutputPathFor(taskID string) string {
	return "/tmp/" + taskID + ".out"
}

func (m *SessionManager) reclaimOnce(ctx context.Context) {
	now := time.Now()
	m.mu.Lock()
	expired := make([]*Session, 0)
	for _, s := range m.live {
		s.mu.Lock()
		// 先做任务 TTL 清理：僵尸任务清掉后容器才可能满足闲置回收条件。
		m.reapExpiredTasksLocked(ctx, s, now)
		if sessionReclaimable(s, now, m.cfg.LeaseTTL) {
			s.reclaiming = true
			s.reclaimed = make(chan struct{})
			expired = append(expired, s)
		}
		s.mu.Unlock()
	}
	m.mu.Unlock()
	for _, s := range expired {
		slog.Info("reclaim idle session container", "container", s.Container)
		err := m.d.removeContainer(ctx, s.Container)
		if err != nil {
			slog.Warn("reclaim container", "container", s.Container, "err", err)
		}
		m.mu.Lock()
		s.mu.Lock()
		if err == nil && m.live[s.Scope] == s {
			delete(m.live, s.Scope)
		}
		reclaimed := s.reclaimed
		s.reclaiming = false
		s.reclaimed = nil
		if reclaimed != nil {
			close(reclaimed)
		}
		s.mu.Unlock()
		m.mu.Unlock()
	}
}

func (m *SessionManager) StartExportSweeper(ctx context.Context) {
	if m.cfg.ExportSweepInterval <= 0 || m.cfg.ExportTTL <= 0 || strings.TrimSpace(m.cfg.SharedHostDir) == "" {
		return
	}
	go func() {
		ticker := time.NewTicker(m.cfg.ExportSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.sweepExports()
			}
		}
	}()
}

func (m *SessionManager) sweepExports() {
	liveScopes := func(scope string) bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		_, ok := m.live[scope]
		return ok
	}
	if err := sweepSharedExports(m.cfg.SharedHostDir, time.Now(), m.cfg.ExportTTL, liveScopes); err != nil {
		slog.Warn("sweep sandbox exports", "err", err)
	}
}

// Shutdown removes live session containers while preserving workspace and cache volumes.
func (m *SessionManager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.live))
	for _, s := range m.live {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		if err := m.d.removeContainer(ctx, s.Container); err != nil {
			slog.Warn("shutdown container", "container", s.Container, "err", err)
			continue
		}
		m.dropSession(s.Scope, s)
	}
}

type SessionInfo struct {
	Scope      string
	Image      string
	LastUsedAt time.Time
}

func (m *SessionManager) List() []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SessionInfo, 0, len(m.live))
	for _, s := range m.live {
		s.mu.Lock()
		if !s.reclaiming {
			out = append(out, SessionInfo{Scope: s.Scope, Image: s.Image, LastUsedAt: s.LastUsedAt})
		}
		s.mu.Unlock()
	}
	return out
}
