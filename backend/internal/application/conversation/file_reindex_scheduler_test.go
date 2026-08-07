package conversation

import (
	"context"
	"testing"
	"time"
)

func newTestScheduler(delay time.Duration) *fileReindexScheduler {
	scheduler := newFileReindexScheduler(
		time.Hour, // 测试不依赖 ticker 循环，手动调用 dueItems
		func() time.Duration { return delay },
		func(ctx context.Context, userID uint, fileID string) error { return nil },
		nil,
	)
	return scheduler
}

func TestReindexSchedulerDebounceMergesRapidWrites(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	delay := time.Minute
	scheduler := newTestScheduler(delay)
	scheduler.nowFn = func() time.Time { return now }

	// 12:00:00 第一次写入。
	scheduler.MarkDirty(7, "file_a")
	// 12:00:30 第二次写入（缓冲窗口内，应顺延）。
	now = now.Add(30 * time.Second)
	scheduler.MarkDirty(7, "file_a")

	// 12:00:40 扫描：距最后写入仅 10s < 60s，不应触发。
	now = now.Add(10 * time.Second)
	if due := scheduler.dueItems(now); len(due) != 0 {
		t.Fatalf("expected no trigger inside buffer window, got %d", len(due))
	}

	// 12:01:40 扫描：距最后写入 70s >= 60s，触发一次（两次写入合并）。
	now = now.Add(60 * time.Second)
	due := scheduler.dueItems(now)
	if len(due) != 1 {
		t.Fatalf("expected exactly 1 trigger after debounce, got %d", len(due))
	}
	if due[0].fileID != "file_a" || due[0].userID != 7 {
		t.Fatalf("unexpected trigger payload: %+v", due[0])
	}

	// 再次扫描：已移除，不应重复触发。
	if due := scheduler.dueItems(now); len(due) != 0 {
		t.Fatalf("expected no duplicate trigger, got %d", len(due))
	}
}

func TestReindexSchedulerSeparateFiles(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	delay := time.Minute
	scheduler := newTestScheduler(delay)
	scheduler.nowFn = func() time.Time { return now }

	scheduler.MarkDirty(1, "file_a")
	now = now.Add(30 * time.Second)
	scheduler.MarkDirty(2, "file_b")

	// 距 file_a 90s、file_b 60s，两者都到期。
	now = now.Add(60 * time.Second)
	due := scheduler.dueItems(now)
	if len(due) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(due))
	}
}

func TestReindexSchedulerRespectsRuntimeDelay(t *testing.T) {
	// delayFn 返回 0 或负值时回落到 60s 默认。
	scheduler := newFileReindexScheduler(
		time.Hour,
		func() time.Duration { return 0 },
		func(ctx context.Context, userID uint, fileID string) error { return nil },
		nil,
	)
	if got := scheduler.resolveDelay(); got != 60*time.Second {
		t.Fatalf("expected fallback 60s, got %v", got)
	}
}
