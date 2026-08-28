package conversation

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/conv"
	"github.com/google/uuid"
)

// platformWriteApproval 记录一条待用户批准的写操作（ask 批准模式）。
// 模型侧先收到 pending_approval 结果，前端展示确认卡片；用户批准后异步执行。
type platformWriteApproval struct {
	ID                   string
	UserID               uint
	ConversationID       uint
	MessageID            uint
	RequestID            string
	RunID                string
	ToolCallID           string
	ToolName             string
	ExecutionToolName    string
	ArgumentsJSON        string
	CreatedAt            time.Time
	Status               string // pending / executing / approved / rejected / failed / expired
	credentialSecrets    *credentialSecretRefStore
	credentialSecretRefs []string
}

const (
	platformApprovalStatusPending   = "pending"
	platformApprovalStatusExecuting = "executing"
	platformApprovalStatusApproved  = "approved"
	platformApprovalStatusRejected  = "rejected"
	platformApprovalStatusFailed    = "failed"
	platformApprovalStatusExpired   = "expired"

	// platformApprovalTTL 待批准记录的保留时长，超期自动清理。
	platformApprovalTTL = 30 * time.Minute
)

// platformWriteApprovalStore 进程内待批准写操作存储。
type platformWriteApprovalStore struct {
	mu       sync.Mutex
	items    map[string]*platformWriteApproval
	ttl      time.Duration
	started  sync.Once
	stopCh   chan struct{}
	stopOnce sync.Once
	nowFn    func() time.Time
}

func newPlatformWriteApprovalStore() *platformWriteApprovalStore {
	return &platformWriteApprovalStore{
		items:  map[string]*platformWriteApproval{},
		ttl:    platformApprovalTTL,
		stopCh: make(chan struct{}),
		nowFn:  time.Now,
	}
}

// create 创建待批准记录（ask 模式写工具调用）。
func (s *platformWriteApprovalStore) create(
	userID uint,
	conversationID uint,
	requestID string,
	entry platformToolEntry,
	executionToolName string,
	argumentsJSON string,
) *platformWriteApproval {
	return s.createProtected(userID, conversationID, 0, requestID, "", "", entry, executionToolName, argumentsJSON, nil, nil)
}

func (s *platformWriteApprovalStore) createProtected(
	userID uint,
	conversationID uint,
	messageID uint,
	requestID string,
	runID string,
	toolCallID string,
	entry platformToolEntry,
	executionToolName string,
	argumentsJSON string,
	credentialSecrets *credentialSecretRefStore,
	credentialSecretRefs []string,
) *platformWriteApproval {
	if s == nil {
		s = newPlatformWriteApprovalStore()
	}
	record := &platformWriteApproval{
		ID:                   conv.NormalizePublicID(uuid.NewString()),
		UserID:               userID,
		ConversationID:       conversationID,
		MessageID:            messageID,
		RequestID:            requestID,
		RunID:                runID,
		ToolCallID:           toolCallID,
		ToolName:             entry.definition.Name,
		ExecutionToolName:    executionToolName,
		ArgumentsJSON:        argumentsJSON,
		CreatedAt:            s.nowFn(),
		Status:               platformApprovalStatusPending,
		credentialSecrets:    credentialSecrets,
		credentialSecretRefs: append([]string(nil), credentialSecretRefs...),
	}
	s.mu.Lock()
	s.items[record.ID] = record
	s.mu.Unlock()
	s.started.Do(s.start)
	return record
}

// get 读取待批准记录（不含状态变更）。
func (s *platformWriteApprovalStore) get(id string) *platformWriteApproval {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[id]
}

// status 返回属主可见的记录快照，并在读取时立即处理到期记录。
func (s *platformWriteApprovalStore) status(id string, userID uint) *platformWriteApproval {
	if s == nil {
		return nil
	}
	var expired *platformWriteApproval
	s.mu.Lock()
	record := s.items[id]
	if record == nil || record.UserID != userID {
		s.mu.Unlock()
		return nil
	}
	if record.Status != platformApprovalStatusExecuting && s.nowFn().Sub(record.CreatedAt) > s.ttl {
		delete(s.items, id)
		if record.Status == platformApprovalStatusPending {
			record.Status = platformApprovalStatusExpired
			expired = record
		}
	}
	snapshot := *record
	s.mu.Unlock()
	if expired != nil {
		expired.destroyCredentialSecrets()
	}
	return &snapshot
}

// claim 原子地认领一条 pending 记录（批准/拒绝互斥）。
// 仅当记录存在、属主匹配且仍为 pending 时返回记录并更新状态；否则返回 nil。
func (s *platformWriteApprovalStore) claim(id string, userID uint, status string) *platformWriteApproval {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.items[id]
	if record == nil || record.UserID != userID || record.Status != platformApprovalStatusPending {
		return nil
	}
	record.Status = status
	return record
}

// finish 将执行中的批准记录收敛为最终状态。
func (s *platformWriteApprovalStore) finish(id string, userID uint, status string) *platformWriteApproval {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.items[id]
	if record == nil || record.UserID != userID || record.Status != platformApprovalStatusExecuting {
		return nil
	}
	record.Status = status
	return record
}

// start 启动过期清理循环。
func (s *platformWriteApprovalStore) start() {
	go s.run()
}

func (s *platformWriteApprovalStore) run() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.cleanup(s.nowFn())
		case <-s.stopCh:
			return
		}
	}
}

func (s *platformWriteApprovalStore) cleanup(now time.Time) {
	if s == nil {
		return
	}
	expired := make([]*platformWriteApproval, 0)
	s.mu.Lock()
	for id, record := range s.items {
		if record.Status == platformApprovalStatusExecuting {
			continue
		}
		if record.Status != platformApprovalStatusPending && now.Sub(record.CreatedAt) > s.ttl {
			delete(s.items, id)
			expired = append(expired, record)
			continue
		}
		if record.Status == platformApprovalStatusPending && now.Sub(record.CreatedAt) > s.ttl {
			record.Status = platformApprovalStatusExpired
			delete(s.items, id)
			expired = append(expired, record)
		}
	}
	s.mu.Unlock()
	for _, record := range expired {
		record.destroyCredentialSecrets()
	}
}

// Stop 停止清理循环（测试用）。
func (s *platformWriteApprovalStore) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
		s.mu.Lock()
		items := make([]*platformWriteApproval, 0, len(s.items))
		for id, record := range s.items {
			items = append(items, record)
			delete(s.items, id)
		}
		s.mu.Unlock()
		for _, record := range items {
			record.destroyCredentialSecrets()
		}
	})
}

func (r *platformWriteApproval) destroyCredentialSecrets() {
	if r == nil || r.credentialSecrets == nil {
		return
	}
	for _, ref := range r.credentialSecretRefs {
		r.credentialSecrets.destroy(ref)
	}
}

// marshalApprovalSummary 序列化待批准记录（供前端卡片与响应体使用）。
func marshalApprovalSummary(record *platformWriteApproval) (string, error) {
	if record == nil {
		return "", nil
	}
	argumentsJSON := record.ArgumentsJSON
	executionToolName := record.ExecutionToolName
	if executionToolName == "" {
		executionToolName = record.ToolName
	}
	argumentsJSON = maskCredentialToolInput(executionToolName, true, argumentsJSON)
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(argumentsJSON), &args)
	return marshalPlatformResult(map[string]interface{}{
		"approval_id":     record.ID,
		"tool":            record.ToolName,
		"arguments":       args,
		"status":          record.Status,
		"conversation_id": record.ConversationID,
		"created_at":      record.CreatedAt.Format(time.RFC3339),
	})
}
