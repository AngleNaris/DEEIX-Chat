package memory

import (
	"context"
	"fmt"
	"testing"

	domainmemory "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/memory"
)

// fakeMemoryRepo 模拟 MemoryRepository，内存维护条目以支持 List/计数。
type fakeMemoryRepo struct {
	items []domainmemory.UserMemory
}

func (f *fakeMemoryRepo) UpsertUserMemory(ctx context.Context, item *domainmemory.UserMemory) error {
	for i := range f.items {
		if f.items[i].MemoryKey == item.MemoryKey {
			f.items[i] = *item
			return nil
		}
	}
	f.items = append(f.items, *item)
	return nil
}

func (f *fakeMemoryRepo) DeleteUserMemory(ctx context.Context, userID uint, memoryKey string) error {
	out := f.items[:0]
	for _, m := range f.items {
		if m.MemoryKey != memoryKey {
			out = append(out, m)
		}
	}
	f.items = out
	return nil
}

func (f *fakeMemoryRepo) ListUserMemories(ctx context.Context, userID uint) ([]domainmemory.UserMemory, error) {
	return f.items, nil
}

func (f *fakeMemoryRepo) SearchUserMemoriesByEmbedding(ctx context.Context, userID uint, queryEmbedding []float32, embeddingSignature string, topK int, minSimilarity float64) ([]domainmemory.UserMemory, error) {
	return nil, nil
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
