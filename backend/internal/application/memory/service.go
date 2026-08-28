package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	domainmemory "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/memory"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

type embeddingProvider interface {
	EmbedTextsWithSignature(ctx context.Context, texts []string) ([][]float32, string, error)
}

type auditWriter interface {
	Write(ctx context.Context, requestID string, actorUserID uint, action string, resource string, resourceID string, ip string, userAgent string, detail interface{})
}

// maxUserMemoriesPerUser 每用户记忆条数防御性上限（仅约束新增，更新不受限）。
// 前端 UI 对 preference 另有 20 条展示上限；此上限兜底 AI 工具批量写入导致的膨胀。
const maxUserMemoriesPerUser = 200

// ErrMemoryLimitReached 表示当前用户的新增长期记忆已达到上限。
var ErrMemoryLimitReached = errors.New("memory limit reached")

type userMemoryLock struct {
	mu   sync.Mutex
	refs int
}

// Service 封装记忆业务能力。
type Service struct {
	repo             repository.MemoryRepository
	cacheInvalidator func(userID uint)
	embedding        embeddingProvider
	auditWriter      auditWriter
	userLocksMu      sync.Mutex
	userLocks        map[uint]*userMemoryLock
}

// NewService 创建服务。
func NewService(repo repository.MemoryRepository) *Service {
	return &Service{repo: repo, userLocks: make(map[uint]*userMemoryLock)}
}

func (s *Service) lockUserMemory(userID uint) func() {
	s.userLocksMu.Lock()
	if s.userLocks == nil {
		s.userLocks = make(map[uint]*userMemoryLock)
	}
	entry := s.userLocks[userID]
	if entry == nil {
		entry = &userMemoryLock{}
		s.userLocks[userID] = entry
	}
	entry.refs++
	s.userLocksMu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		s.userLocksMu.Lock()
		entry.refs--
		if entry.refs == 0 && s.userLocks[userID] == entry {
			delete(s.userLocks, userID)
		}
		s.userLocksMu.Unlock()
	}
}

// SetCacheInvalidator 注入缓存失效回调。每当用户记忆写入成功后调用，通知上层清除本地缓存。
func (s *Service) SetCacheInvalidator(fn func(userID uint)) {
	s.cacheInvalidator = fn
}

// SetEmbeddingProvider 注入可选的向量化能力，用于用户长期记忆的语义检索。
func (s *Service) SetEmbeddingProvider(provider embeddingProvider) {
	s.embedding = provider
}

// SetAuditWriter 注入记忆域审计写入器。
func (s *Service) SetAuditWriter(writer auditWriter) {
	s.auditWriter = writer
}

// RecordAudit 记录记忆域审计日志。
func (s *Service) RecordAudit(ctx context.Context, input AuditInput) {
	if s.auditWriter == nil {
		return
	}
	s.auditWriter.Write(
		ctx,
		strings.TrimSpace(input.RequestID),
		input.UserID,
		strings.TrimSpace(input.Action),
		"memory",
		strings.TrimSpace(input.MemoryKey),
		strings.TrimSpace(input.ClientIP),
		strings.TrimSpace(input.UserAgent),
		input.Detail,
	)
}

// AuditInput 描述记忆域一次审计写入。
type AuditInput struct {
	UserID    uint
	RequestID string
	Action    string
	MemoryKey string
	ClientIP  string
	UserAgent string
	Detail    interface{}
}

// UpsertUserMemory 新增或更新用户长期记忆。
func (s *Service) UpsertUserMemory(ctx context.Context, userID uint, key string, value string, scope string, updatedBy string) error {
	key = strings.TrimSpace(key)
	item := &domainmemory.UserMemory{
		UserID:    userID,
		MemoryKey: key,
		Value:     strings.TrimSpace(value),
		Scope:     strings.TrimSpace(scope),
		UpdatedBy: strings.TrimSpace(updatedBy),
	}
	unlock := s.lockUserMemory(userID)
	// 防御性条数上限：一次查询同时判断 key 是否已存在（更新不受限）与当前条数。
	// 查询失败不阻断写入主路径（真实故障会由随后的 Upsert 报错暴露）。
	if existing, err := s.repo.ListUserMemories(ctx, userID); err == nil {
		isNew := true
		for _, m := range existing {
			if m.MemoryKey == key {
				isNew = false
				break
			}
		}
		if isNew && len(existing) >= maxUserMemoriesPerUser {
			unlock()
			return fmt.Errorf("%w: %d entries per user", ErrMemoryLimitReached, maxUserMemoriesPerUser)
		}
	}
	if err := s.repo.UpsertUserMemory(ctx, item); err != nil {
		unlock()
		return err
	}
	unlock()
	if s.cacheInvalidator != nil {
		s.cacheInvalidator(userID)
	}
	s.embedUserMemoryAsync(userID, item.MemoryKey, item.Value)
	return nil
}

// DeleteUserMemory 删除用户长期记忆，并失效会话缓存。
func (s *Service) DeleteUserMemory(ctx context.Context, userID uint, memoryKey string) error {
	unlock := s.lockUserMemory(userID)
	if err := s.repo.DeleteUserMemory(ctx, userID, strings.TrimSpace(memoryKey)); err != nil {
		unlock()
		return err
	}
	unlock()
	if s.cacheInvalidator != nil {
		s.cacheInvalidator(userID)
	}
	return nil
}

// ListUserMemories 返回用户长期记忆。
func (s *Service) ListUserMemories(ctx context.Context, userID uint) ([]domainmemory.UserMemory, error) {
	return s.repo.ListUserMemories(ctx, userID)
}

// SearchUserMemoriesByEmbedding 语义检索用户记忆（需向量存储支持）。
func (s *Service) SearchUserMemoriesByEmbedding(ctx context.Context, userID uint, queryEmbedding []float32, embeddingSignature string, topK int, minSimilarity float64) ([]domainmemory.UserMemory, error) {
	return s.repo.SearchUserMemoriesByEmbedding(ctx, userID, queryEmbedding, embeddingSignature, topK, minSimilarity)
}

// UpsertUserMemoryEmbedding 更新记忆向量（异步写入，失败静默）。
func (s *Service) UpsertUserMemoryEmbedding(ctx context.Context, userID uint, memoryKey string, expectedValue string, embedding []float32, embeddingSignature string) error {
	return s.repo.UpsertUserMemoryEmbedding(ctx, userID, memoryKey, expectedValue, embedding, embeddingSignature)
}

func (s *Service) embedUserMemoryAsync(userID uint, memoryKey string, value string) {
	if s.embedding == nil || strings.TrimSpace(memoryKey) == "" || strings.TrimSpace(value) == "" {
		return
	}
	go func() {
		// 记忆向量是检索增强，不属于写入主事务；失败时保留文本记忆并走关键词兜底。
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		embeddings, embeddingSignature, err := s.embedding.EmbedTextsWithSignature(ctx, []string{value})
		if err != nil || len(embeddings) == 0 {
			return
		}
		_ = s.repo.UpsertUserMemoryEmbedding(ctx, userID, memoryKey, strings.TrimSpace(value), embeddings[0], embeddingSignature)
	}()
}
