package conversation

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// fileReindexScheduler 延迟重建调度器（debounce）：
// write_file 修改文件内容后不立即触发提取/RAG 重建，而是在缓冲窗口内合并多次修改，
// 到期才触发一次重建，避免频繁修改反复重建。状态字段在写入时已重置（管理页可见），
// 重建任务延迟异步执行。调度器为进程内内存态，进程重启丢失未到期任务（后续可持久化）。
type fileReindexScheduler struct {
	mu       sync.Mutex
	pending  map[string]reindexPendingItem // fileID → 待重建项
	interval time.Duration
	delayFn  func() time.Duration
	process  func(ctx context.Context, userID uint, fileID string) error
	logger   *zap.Logger
	started  sync.Once
	stopCh   chan struct{}
	stopOnce sync.Once
	nowFn    func() time.Time
}

type reindexPendingItem struct {
	userID    uint
	lastWrite time.Time
}

// newFileReindexScheduler 创建延迟重建调度器。
// delayFn 每次扫描时读取最新缓冲窗口（来自运行时设置，缺省 60s）。
func newFileReindexScheduler(
	interval time.Duration,
	delayFn func() time.Duration,
	process func(ctx context.Context, userID uint, fileID string) error,
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
		logger:   logger,
		stopCh:   make(chan struct{}),
		nowFn:    time.Now,
	}
}

// MarkDirty 登记一次文件内容修改（刷新最后写入时间，缓冲窗口顺延）。
func (s *fileReindexScheduler) MarkDirty(userID uint, fileID string) {
	if s == nil || fileID == "" {
		return
	}
	s.mu.Lock()
	s.pending[fileID] = reindexPendingItem{userID: userID, lastWrite: s.nowFn()}
	s.mu.Unlock()
	s.started.Do(s.start)
}

// start 启动后台扫描循环。
func (s *fileReindexScheduler) start() {
	go s.run()
}

func (s *fileReindexScheduler) run() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.tick(s.nowFn())
		case <-s.stopCh:
			return
		}
	}
}

// reindexDueItem 到期待重建项。
type reindexDueItem struct {
	userID uint
	fileID string
}

// tick 扫描到期项并触发重建（到期项从待处理集合移除后异步执行）。
func (s *fileReindexScheduler) tick(now time.Time) {
	for _, item := range s.dueItems(now) {
		s.trigger(item.userID, item.fileID)
	}
}

// dueItems 返回到期待重建项并将其从待处理集合移除（同步，供 tick 与测试使用）。
func (s *fileReindexScheduler) dueItems(now time.Time) []reindexDueItem {
	delay := s.resolveDelay()
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

// resolveDelay 读取当前缓冲窗口（秒），缺省 60s。
func (s *fileReindexScheduler) resolveDelay() time.Duration {
	if s.delayFn == nil {
		return 60 * time.Second
	}
	seconds := s.delayFn()
	if seconds <= 0 {
		return 60 * time.Second
	}
	return seconds
}

// trigger 异步执行单个文件的重建（后台 context，不阻塞调用方）。
func (s *fileReindexScheduler) trigger(userID uint, fileID string) {
	if s.process == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.process(ctx, userID, fileID); err != nil && s.logger != nil {
			s.logger.Warn("platform_tools_reindex_failed",
				zap.Uint("user_id", userID),
				zap.String("file_id", fileID),
				zap.Error(err),
			)
		}
	}()
}

// Stop 停止调度器（测试与优雅退出使用）。
func (s *fileReindexScheduler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
}
