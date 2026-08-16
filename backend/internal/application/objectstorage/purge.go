package objectstorage

import (
	"context"
	"errors"
	"strings"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
)

// PurgeResult reports object deletion counts without retaining storage keys.
type PurgeResult struct {
	Attempted int
	Failed    int
}

// PurgePaths deletes each distinct non-empty object path. Storage failures are returned as counts.
func PurgePaths(ctx context.Context, provider Provider, paths []string) PurgeResult {
	result := PurgeResult{}
	seen := make(map[string]struct{}, len(paths))
	countFailure := func(rawPath string) {
		normalized := strings.TrimSpace(rawPath)
		if normalized == "" {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		result.Attempted++
		result.Failed++
	}
	if provider == nil {
		for _, path := range paths {
			countFailure(path)
		}
		return result
	}

	store, err := provider.Open(ctx)
	if err != nil {
		for _, path := range paths {
			countFailure(path)
		}
		return result
	}

	for _, path := range paths {
		normalized := strings.TrimSpace(path)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result.Attempted++
		if err := store.Delete(ctx, normalized); err != nil && !errors.Is(err, objectstore.ErrNotFound) {
			result.Failed++
		}
	}
	return result
}
