package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domainmemory "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/memory"
)

// fakeMemoryRepo 模拟 MemoryRepository，内存维护条目以支持 List/计数。
type fakeMemoryRepo struct {
	mu        sync.RWMutex
	items     []domainmemory.UserMemory
	listDelay time.Duration
}

func (f *fakeMemoryRepo) UpsertUserMemory(ctx context.Context, item *domainmemory.UserMemory) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.items {
		if f.items[i].UserID == item.UserID && f.items[i].MemoryKey == item.MemoryKey {
			f.items[i] = *item
			return nil
		}
	}
	f.items = append(f.items, *item)
	return nil
}

func (f *fakeMemoryRepo) DeleteUserMemory(ctx context.Context, userID uint, memoryKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.items[:0]
	for _, m := range f.items {
		if m.UserID != userID || m.MemoryKey != memoryKey {
			out = append(out, m)
		}
	}
	f.items = out
	return nil
}

func (f *fakeMemoryRepo) ListUserMemories(ctx context.Context, userID uint) ([]domainmemory.UserMemory, error) {
	f.mu.RLock()
	items := make([]domainmemory.UserMemory, 0, len(f.items))
	for _, item := range f.items {
		if item.UserID == userID {
			items = append(items, item)
		}
	}
	f.mu.RUnlock()
	if f.listDelay > 0 {
		time.Sleep(f.listDelay)
	}
	return items, nil
}

func (f *fakeMemoryRepo) seed(userID uint, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := 0; i < count; i++ {
		f.items = append(f.items, domainmemory.UserMemory{UserID: userID, MemoryKey: fmt.Sprintf("seed-%d", i), Value: "v"})
	}
}

func (f *fakeMemoryRepo) SearchUserMemoriesByEmbedding(ctx context.Context, userID uint, queryEmbedding []float32, embeddingSignature string, topK int, minSimilarity float64) ([]domainmemory.UserMemory, error) {
	return nil, nil
}

func TestUpsertUserMemoryConcurrentQuotaIsScopedPerUser(t *testing.T) {
	repo := &fakeMemoryRepo{listDelay: 5 * time.Millisecond}
	repo.seed(1, 180)
	svc := NewService(repo)

	var successes atomic.Int32
	var failures atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := svc.UpsertUserMemory(t.Context(), 1, fmt.Sprintf("concurrent-%d", i), "v", "custom", "user")
			if err != nil {
				if !errors.Is(err, ErrMemoryLimitReached) {
					t.Errorf("unexpected concurrent upsert error: %v", err)
				}
				failures.Add(1)
				return
			}
			successes.Add(1)
		}(i)
	}
	wg.Wait()

	items, err := repo.ListUserMemories(t.Context(), 1)
	if err != nil {
		t.Fatalf("list user 1: %v", err)
	}
	if len(items) != maxUserMemoriesPerUser || successes.Load() != 20 || failures.Load() != 20 {
		t.Fatalf("quota outcome: entries=%d successes=%d failures=%d, want 200/20/20", len(items), successes.Load(), failures.Load())
	}
	if err := svc.UpsertUserMemory(t.Context(), 1, "seed-0", "updated", "custom", "user"); err != nil {
		t.Fatalf("update at full quota failed: %v", err)
	}

	for i := 0; i < 25; i++ {
		if err := svc.UpsertUserMemory(t.Context(), 2, fmt.Sprintf("other-%d", i), "v", "custom", "user"); err != nil {
			t.Fatalf("user 2 upsert %d failed: %v", i, err)
		}
	}
	otherItems, err := repo.ListUserMemories(t.Context(), 2)
	if err != nil {
		t.Fatalf("list user 2: %v", err)
	}
	if len(otherItems) != 25 {
		t.Fatalf("user 2 entries = %d, want 25", len(otherItems))
	}
}

func (f *fakeMemoryRepo) UpsertUserMemoryEmbedding(ctx context.Context, userID uint, memoryKey string, expectedValue string, embedding []float32, embeddingSignature string) error {
	return nil
}

func TestUpsertUserMemoryLimit(t *testing.T) {
	repo := &fakeMemoryRepo{}
	svc := NewService(repo)
	ctx := context.Background()

	// 填满上限。
	for i := 0; i < maxUserMemoriesPerUser; i++ {
		if err := svc.UpsertUserMemory(ctx, 1, fmt.Sprintf("k%d", i), "v", "custom", "user"); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}
	// 新增被拒绝。
	if err := svc.UpsertUserMemory(ctx, 1, "overflow", "v", "custom", "user"); err == nil {
		t.Fatalf("expected limit error for new entry")
	}
	// 更新已有条目不受限。
	if err := svc.UpsertUserMemory(ctx, 1, "k0", "updated", "preference", "user"); err != nil {
		t.Fatalf("update must be allowed: %v", err)
	}
	// 删除后可继续新增。
	if err := svc.DeleteUserMemory(ctx, 1, "k0"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := svc.UpsertUserMemory(ctx, 1, "new1", "v", "custom", "user"); err != nil {
		t.Fatalf("new entry after delete must be allowed: %v", err)
	}
}
