package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Meta 是 DEEIX 后端注入的请求元数据（client.go 将 _meta 写入 MCP 请求顶层 params）。
type Meta struct {
	UserID         uint
	ConversationID uint
	RequestID      string
}

// ParseMeta 从原始请求体提取 DEEIX 注入的 _meta。
// 缺失 user_id 视为非法调用（本服务仅面向 DEEIX 后端）。
func ParseMeta(raw []byte) (*Meta, error) {
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
	meta := &Meta{UserID: userID, RequestID: fmt.Sprintf("%v", m["request_id"])}
	if cid, ok := toUint(m["conversation_id"]); ok {
		meta.ConversationID = cid
	}
	return meta, nil
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
	Scope       string    // deeix-<uid>-<cid>
	Image       string    // 会话当前使用的镜像（sandbox_spawn 可更换）
	Container   string    // Docker 容器名
	CreatedAt   time.Time
	LastUsedAt  time.Time
	Env         []string  // 创建容器时的环境变量（镜像拉取所需）
	CacheMount  bool      // 是否挂用户级缓存卷
	mu          sync.Mutex
	tasks       map[string]*BackgroundTask // 后台任务
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

// SessionScope 生成 (user, conversation) 会话标识。
func SessionScope(meta *Meta) string {
	scope := fmt.Sprintf("deeix-%d", meta.UserID)
	if meta.ConversationID > 0 {
		scope += fmt.Sprintf("-%d", meta.ConversationID)
	}
	return scope
}

// GetOrCreate 获取会话；不存在则懒创建（create_if_missing 语义）。
// 返回会话与是否为新创建（重建时供工具回传 session_recreated 标志）。
func (m *SessionManager) GetOrCreate(ctx context.Context, scope string) (*Session, bool, error) {
	m.mu.Lock()
	if s, ok := m.live[scope]; ok {
		s.LastUsedAt = time.Now()
		m.mu.Unlock()
		return s, false, nil
	}
	m.mu.Unlock()

	// 创建过程较慢，先建对象占位避免并发重复创建。
	m.mu.Lock()
	if s, ok := m.live[scope]; ok {
		s.LastUsedAt = time.Now()
		m.mu.Unlock()
		return s, false, nil
	}
	s := &Session{
		Scope:      scope,
		Image:      m.cfg.BaseImage,
		Container:  scope,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
		Env:        []string{"WORKSPACE=" + m.cfg.WorkspaceDir},
		CacheMount: true,
		tasks:      make(map[string]*BackgroundTask),
	}
	m.live[scope] = s
	m.mu.Unlock()

	exists, err := m.d.containerExists(ctx, s.Container)
	if err != nil {
		m.drop(scope)
		return nil, false, err
	}
	if !exists {
		if err := m.createSessionContainer(ctx, s); err != nil {
			m.drop(scope)
			return nil, false, err
		}
	}
	return s, true, nil
}

// Get 仅读取已有会话（spawn/ps 等需要区分"新会话"语义时用）。
func (m *SessionManager) Get(scope string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.live[scope]
	if ok {
		s.LastUsedAt = time.Now()
	}
	return s, ok
}

func (m *SessionManager) createSessionContainer(ctx context.Context, s *Session) error {
	if err := m.d.createContainer(ctx, containerSpec{
		Name:       s.Container,
		Image:      s.Image,
		Env:        s.Env,
		Memory:     m.cfg.MemoryLimit,
		PidsLimit:  m.cfg.PidsLimit,
		CPUs:       m.cfg.CPUsLimit,
		Workspace:  m.cfg.WorkspaceDir,
		CacheMount: s.CacheMount,
	}, "deeix-sandbox-ws-"+s.Scope); err != nil {
		return err
	}
	return nil
}

// Spawn 为会话更换/新建镜像容器（sandbox_spawn 语义）：销毁旧容器后按新镜像重建。
func (m *SessionManager) Spawn(ctx context.Context, scope, image string) (bool, error) {
	s, created, err := m.GetOrCreate(ctx, scope)
	if err != nil {
		return false, err
	}
	if created {
		// 刚创建时已用默认镜像；若指定镜像不同则重建一次。
		if image == "" || image == s.Image {
			return true, nil
		}
	}
	if image == "" {
		return created, nil
	}
	if err := m.d.removeContainer(ctx, s.Container); err != nil {
		return created, err
	}
	s.Image = image
	if err := m.createSessionContainer(ctx, s); err != nil {
		return created, err
	}
	return created, nil
}

// Kill 销毁会话容器（保留工作区卷与缓存卷，环境不丢）。
func (m *SessionManager) Kill(ctx context.Context, scope string) error {
	s, ok := m.Get(scope)
	if !ok {
		return nil
	}
	if err := m.d.removeContainer(ctx, s.Container); err != nil {
		return err
	}
	return nil
}

// Reset 销毁会话容器并清空其工作区卷。
func (m *SessionManager) Reset(ctx context.Context, scope string) error {
	s, ok := m.Get(scope)
	if !ok {
		return nil
	}
	s.mu.Lock()
	s.tasks = make(map[string]*BackgroundTask)
	s.mu.Unlock()
	if err := m.d.removeContainer(ctx, s.Container); err != nil {
		return err
	}
	return m.removeWorkspaceVolume(scope)
}

func (m *SessionManager) removeWorkspaceVolume(scope string) error {
	vol := "deeix-sandbox-ws-" + scope
	if err := m.d.removeVolume(vol); err != nil {
		slog.Warn("remove workspace volume", "volume", vol, "err", err)
	}
	return nil
}

func (m *SessionManager) drop(scope string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.live, scope)
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

func (m *SessionManager) reclaimOnce(ctx context.Context) {
	now := time.Now()
	m.mu.Lock()
	expired := make([]*Session, 0)
	for scope, s := range m.live {
		if now.Sub(s.LastUsedAt) > m.cfg.LeaseTTL {
			expired = append(expired, s)
			delete(m.live, scope)
		}
	}
	m.mu.Unlock()
	for _, s := range expired {
		slog.Info("reclaim idle session container", "container", s.Container)
		if err := m.d.removeContainer(ctx, s.Container); err != nil {
			slog.Warn("reclaim container", "container", s.Container, "err", err)
		}
	}
}

// List 列出全部存活会话（sandbox_ps 用）。
func (m *SessionManager) List() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, len(m.live))
	for _, s := range m.live {
		out = append(out, s)
	}
	return out
}

// touch 更新会话最近使用时间（后台任务轮询时调用）。
func (m *SessionManager) touch(scope string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.live[scope]; ok {
		s.LastUsedAt = time.Now()
	}
}
