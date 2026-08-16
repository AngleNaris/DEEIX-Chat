package objectstorage

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
)

func TestPurgePathsDeduplicatesAndIgnoresNotFound(t *testing.T) {
	store := &purgeTestStore{deleteErrors: map[string]error{
		"missing": objectstore.ErrNotFound,
		"failed":  errors.New("delete failed"),
	}}
	result := PurgePaths(context.Background(), purgeTestProvider{store: store}, []string{
		" kept ", "kept", "missing", "failed", " ",
	})

	if result.Attempted != 3 || result.Failed != 1 {
		t.Fatalf("PurgePaths() = %+v, want attempted=3 failed=1", result)
	}
	if len(store.deleted) != 3 || store.deleted[0] != "kept" || store.deleted[1] != "missing" || store.deleted[2] != "failed" {
		t.Fatalf("deleted keys = %v", store.deleted)
	}
}

func TestPurgePathsCountsDistinctFailuresWithoutProvider(t *testing.T) {
	result := PurgePaths(context.Background(), nil, []string{" first ", "first", "", "second"})
	if result.Attempted != 2 || result.Failed != 2 {
		t.Fatalf("PurgePaths() = %+v, want attempted=2 failed=2", result)
	}
}

type purgeTestProvider struct {
	store objectstore.Store
	err   error
}

func (p purgeTestProvider) Open(context.Context) (objectstore.Store, error) {
	return p.store, p.err
}

type purgeTestStore struct {
	deleted      []string
	deleteErrors map[string]error
}

func (*purgeTestStore) Put(context.Context, string, io.Reader, objectstore.PutOptions) (objectstore.ObjectInfo, error) {
	return objectstore.ObjectInfo{}, errors.New("not implemented")
}

func (*purgeTestStore) Open(context.Context, string) (io.ReadCloser, objectstore.ObjectInfo, error) {
	return nil, objectstore.ObjectInfo{}, errors.New("not implemented")
}

func (s *purgeTestStore) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return s.deleteErrors[key]
}

func (*purgeTestStore) Materialize(context.Context, string) (string, func(), error) {
	return "", nil, errors.New("not implemented")
}
