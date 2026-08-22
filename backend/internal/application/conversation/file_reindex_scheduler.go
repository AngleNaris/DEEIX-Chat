package conversation

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

const fileProcessingRecoveryInterval = time.Minute

// fileReindexScheduler 延迟提交文件处理任务，在缓冲窗口内合并重复写入。
type fileReindexScheduler struct {
	mu          sync.Mutex
	pending     map[string]reindexPendingItem
	interval    time.Duration
	delayFn     func(context.Context) time.Duration
	process     func(ctx context.Context, userID uint, fileID string) error
	recover     func(ctx context.Context, delay time.Duration) error
	logger      *zap.Logger
	started     sync.Once
	stopCh      chan struct{}
	stopOnce    sync.Once
	nowFn       func() time.Time
	lastRecover time.Time
}

type reindexPendingItem struct {
	userID    uint
	lastWrite time.Time
}

func newFileReindexScheduler(
	interval time.Duration,
	delayFn func(context.Context) time.Duration,
	process func(ctx context.Context, userID uint, fileID string) error,
	recover func(ctx context.Context, delay time.Duration) error,
	logger *zap.Logger,
) *fileReindexScheduler {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &fileReindexScheduler{
		pending:  map[string]reindexPendingItem{},
		interval: interval,
		delayFn:  delayFn,
		process:  process,
		recover:  recover,
		logger:   logger,
		stopCh:   make(chan struct{}),
		nowFn:    time.Now,
	}
}

// Start 将调度器绑定到应用生命周期。重复调用无效。
func (s *fileReindexScheduler) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.started.Do(func() { go s.run(ctx) })
}

// MarkDirty 刷新文件最后写入时间，缓冲窗口随最后一次写入顺延。
func (s *fileReindexScheduler) MarkDirty(userID uint, fileID string) {
	if s == nil || fileID == "" {
		return
	}
	s.mu.Lock()
	s.pending[fileID] = reindexPendingItem{userID: userID, lastWrite: s.nowFn()}
	s.mu.Unlock()
}

func (s *fileReindexScheduler) run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.tick(ctx, s.nowFn())
	for {
		select {
		case <-ticker.C:
			s.tick(ctx, s.nowFn())
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		}
	}
}

type reindexDueItem struct {
	userID uint
	fileID string
}

func (s *fileReindexScheduler) tick(ctx context.Context, now time.Time) {
	s.mu.Lock()
	hasPending := len(s.pending) > 0
	s.mu.Unlock()
	shouldRecover := s.recover != nil && (s.lastRecover.IsZero() || now.Sub(s.lastRecover) >= fileProcessingRecoveryInterval)
	if !hasPending && !shouldRecover {
		return
	}
	delay := s.resolveDelay(ctx)
	if shouldRecover {
		s.lastRecover = now
		if err := s.recover(ctx, delay); err != nil && s.logger != nil {
			s.logger.Warn("platform_tools_reindex_recovery_failed", zap.Error(err))
		}
	}
	for _, item := range s.dueItemsWithDelay(now, delay) {
		if err := s.trigger(ctx, item.userID, item.fileID); err != nil {
			s.mu.Lock()
			if _, exists := s.pending[item.fileID]; !exists {
				s.pending[item.fileID] = reindexPendingItem{userID: item.userID, lastWrite: now}
			}
			s.mu.Unlock()
			if s.logger != nil {
				s.logger.Warn("platform_tools_reindex_enqueue_failed",
					zap.Uint("user_id", item.userID),
					zap.String("file_id", item.fileID),
					zap.Error(err),
				)
			}
		}
	}
}

func (s *fileReindexScheduler) dueItems(now time.Time) []reindexDueItem {
	return s.dueItemsWithDelay(now, s.resolveDelay(context.Background()))
}

func (s *fileReindexScheduler) dueItemsWithDelay(now time.Time, delay time.Duration) []reindexDueItem {
	due := make([]reindexDueItem, 0, 1)
	s.mu.Lock()
	for fileID, item := range s.pending {
		if now.Sub(item.lastWrite) >= delay {
			delete(s.pending, fileID)
			due = append(due, reindexDueItem{userID: item.userID, fileID: fileID})
		}
	}
	s.mu.Unlock()
	return due
}

func (s *fileReindexScheduler) resolveDelay(ctx context.Context) time.Duration {
	if s.delayFn == nil {
		return 60 * time.Second
	}
	delay := s.delayFn(ctx)
	if delay <= 0 {
		return 60 * time.Second
	}
	return delay
}

func (s *fileReindexScheduler) trigger(ctx context.Context, userID uint, fileID string) error {
	if s.process == nil {
		return nil
	}
	triggerCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.process(triggerCtx, userID, fileID)
}

func (s *fileReindexScheduler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
}
