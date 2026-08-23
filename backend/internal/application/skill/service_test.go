package skill

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	appstorage "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/objectstorage"
	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

type fakeSkillRepo struct {
	mu       sync.Mutex
	items    map[uint]domainskill.Skill
	next     uint
	patchErr error
}

func (r *fakeSkillRepo) ListSkills(context.Context, repository.SkillListFilter, int, int) ([]domainskill.Skill, int64, error) {
	return nil, 0, nil
}

func (r *fakeSkillRepo) GetSkill(_ context.Context, id uint) (*domainskill.Skill, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &item, nil
}

func (r *fakeSkillRepo) CreateSkill(_ context.Context, item *domainskill.Skill) (*domainskill.Skill, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.next == 0 {
		r.next = 1
	}
	item.ID = r.next
	item.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	item.UpdatedAt = item.CreatedAt
	r.next++
	r.items[item.ID] = *item
	result := r.items[item.ID]
	return &result, nil
}

func (r *fakeSkillRepo) PatchSkill(_ context.Context, id uint, patch repository.SkillPatch) (*domainskill.Skill, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.patchErr != nil {
		return nil, r.patchErr
	}
	item, ok := r.items[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if patch.ExpectedUpdatedAt != nil && !item.UpdatedAt.Equal(*patch.ExpectedUpdatedAt) {
		return nil, repository.ErrConflict
	}
	if patch.Title != nil {
		item.Title = *patch.Title
	}
	if patch.Trigger != nil {
		item.Trigger = *patch.Trigger
	}
	if patch.Description != nil {
		item.Description = *patch.Description
	}
	if patch.Markdown != nil {
		item.Markdown = *patch.Markdown
	}
	if patch.PackageRootDir != nil {
		item.PackageRootDir = *patch.PackageRootDir
	}
	if patch.PackageFilesJSON != nil {
		item.PackageFiles = decodePackageFilesJSON(*patch.PackageFilesJSON)
	}
	item.UpdatedAt = item.UpdatedAt.Add(time.Microsecond)
	r.items[id] = item
	return &item, nil
}

func (r *fakeSkillRepo) DeleteSkill(_ context.Context, id uint, expectedUpdatedAt *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[id]
	if !ok {
		return repository.ErrNotFound
	}
	if expectedUpdatedAt != nil && !item.UpdatedAt.Equal(*expectedUpdatedAt) {
		return repository.ErrConflict
	}
	delete(r.items, id)
	return nil
}

func TestReplacePackageConflictLeavesOldGenerationUntouched(t *testing.T) {
	item := domainskill.Skill{
		ID:              7,
		Scope:           domainskill.ScopeUser,
		OwnerUserID:     9,
		Title:           "Old skill",
		Trigger:         "old-skill",
		PackageType:     domainskill.PackageTypePackage,
		PackageFiles:    []domainskill.PackageFile{{Path: "references/guide.md", Kind: domainskill.FileKindText}},
		UpdatedByUserID: 9,
		UpdatedAt:       time.Now().UTC().Truncate(time.Microsecond),
	}
	repo := &fakeSkillRepo{items: map[uint]domainskill.Skill{item.ID: item}, patchErr: repository.ErrConflict}
	store := newSkillLifecycleStore()
	oldKey := packageObjectKey(item.Scope, item.ID, "references/guide.md")
	store.objects[oldKey] = []byte("old guide")
	service := NewService(repo)
	service.SetObjectStoreProvider(skillLifecycleStoreProvider{store: store})

	zipData := buildZip(t, map[string]string{
		"SKILL.md":            "---\nname: New skill\n---\nNew instructions.",
		"references/guide.md": "new guide",
		"references/new.md":   "new file",
	})
	if _, err := service.replacePackage(t.Context(), item, zipData); !errors.Is(err, ErrSkillVersionConflict) {
		t.Fatalf("replacePackage() error = %v, want ErrSkillVersionConflict", err)
	}
	objects := store.snapshotObjects()
	if got := string(objects[oldKey]); got != "old guide" {
		t.Fatalf("old object = %q, want old guide", got)
	}
	if len(objects) != 1 {
		t.Fatalf("losing generation survived conflict: %v", objects)
	}
}

func TestReplacePackageMissingSkillCleansOnlyStagedGeneration(t *testing.T) {
	item := domainskill.Skill{
		ID:              8,
		Scope:           domainskill.ScopeUser,
		OwnerUserID:     10,
		Title:           "Deleted skill",
		Trigger:         "deleted-skill",
		PackageType:     domainskill.PackageTypePackage,
		PackageFiles:    []domainskill.PackageFile{{Path: "references/old.md", Kind: domainskill.FileKindText}},
		UpdatedByUserID: 10,
		UpdatedAt:       time.Now().UTC().Truncate(time.Microsecond),
	}
	repo := &fakeSkillRepo{items: map[uint]domainskill.Skill{item.ID: item}, patchErr: repository.ErrNotFound}
	store := newSkillLifecycleStore()
	oldKey := packageObjectKey(item.Scope, item.ID, "references/old.md")
	store.objects[oldKey] = []byte("old")
	service := NewService(repo)
	service.SetObjectStoreProvider(skillLifecycleStoreProvider{store: store})

	zipData := buildZip(t, map[string]string{
		"SKILL.md":            "---\nname: Deleted skill\n---\nNew instructions.",
		"references/new.md":   "new",
		"references/other.md": "other",
	})
	if _, err := service.replacePackage(t.Context(), item, zipData); !errors.Is(err, ErrSkillNotFound) {
		t.Fatalf("replacePackage() error = %v, want ErrSkillNotFound", err)
	}
	objects := store.snapshotObjects()
	if len(objects) != 1 || string(objects[oldKey]) != "old" {
		t.Fatalf("missing skill cleanup touched old generation: %v", objects)
	}
}

func TestReplacePackageSwitchesManifestBeforeDeletingOldGeneration(t *testing.T) {
	item := domainskill.Skill{
		ID:              9,
		Scope:           domainskill.ScopeUser,
		OwnerUserID:     12,
		Title:           "Old skill",
		Trigger:         "old-skill-success",
		PackageType:     domainskill.PackageTypePackage,
		PackageFiles:    []domainskill.PackageFile{{Path: "references/old.md", Kind: domainskill.FileKindText}},
		UpdatedByUserID: 12,
		UpdatedAt:       time.Now().UTC().Truncate(time.Microsecond),
	}
	repo := &fakeSkillRepo{items: map[uint]domainskill.Skill{item.ID: item}}
	store := newSkillLifecycleStore()
	oldKey := packageObjectKey(item.Scope, item.ID, "references/old.md")
	store.objects[oldKey] = []byte("old")
	service := NewService(repo)
	service.SetObjectStoreProvider(skillLifecycleStoreProvider{store: store})

	result, err := service.replacePackage(t.Context(), item, buildZip(t, map[string]string{
		"SKILL.md":            "---\nname: New skill\n---\nNew instructions.",
		"references/fresh.md": "fresh",
	}))
	if err != nil {
		t.Fatalf("replacePackage() error = %v", err)
	}
	if len(result.PackageFiles) != 1 {
		t.Fatalf("package manifest = %+v", result.PackageFiles)
	}
	objects := store.snapshotObjects()
	if _, exists := objects[oldKey]; exists {
		t.Fatalf("old generation survived successful replacement")
	}
	for _, file := range result.PackageFiles {
		if file.ObjectKey == "" {
			t.Fatalf("manifest file has no object key: %+v", file)
		}
		if _, exists := objects[file.ObjectKey]; !exists {
			t.Fatalf("manifest object %q is missing", file.ObjectKey)
		}
	}
}

func TestConcurrentPackageReplacementsKeepOnlyWinningGeneration(t *testing.T) {
	item := domainskill.Skill{
		ID:              10,
		Scope:           domainskill.ScopeUser,
		OwnerUserID:     13,
		Title:           "Concurrent skill",
		Trigger:         "concurrent-skill",
		PackageType:     domainskill.PackageTypePackage,
		PackageFiles:    []domainskill.PackageFile{{Path: "references/old.md", Kind: domainskill.FileKindText}},
		UpdatedByUserID: 13,
		UpdatedAt:       time.Now().UTC().Truncate(time.Microsecond),
	}
	repo := &fakeSkillRepo{items: map[uint]domainskill.Skill{item.ID: item}}
	store := newSkillLifecycleStore()
	oldKey := packageObjectKey(item.Scope, item.ID, "references/old.md")
	store.objects[oldKey] = []byte("old")
	service := NewService(repo)
	service.SetObjectStoreProvider(skillLifecycleStoreProvider{store: store})

	zips := [][]byte{
		buildZip(t, map[string]string{"SKILL.md": "---\nname: Winner A\n---\nA", "references/a.md": "a"}),
		buildZip(t, map[string]string{"SKILL.md": "---\nname: Winner B\n---\nB", "references/b.md": "b"}),
	}
	type outcome struct {
		item *domainskill.Skill
		err  error
	}
	outcomes := make(chan outcome, len(zips))
	var wait sync.WaitGroup
	for _, zipData := range zips {
		wait.Add(1)
		go func(data []byte) {
			defer wait.Done()
			result, err := service.replacePackage(t.Context(), item, data)
			outcomes <- outcome{item: result, err: err}
		}(zipData)
	}
	wait.Wait()
	close(outcomes)

	var winner *domainskill.Skill
	conflicts := 0
	for result := range outcomes {
		if result.err == nil {
			winner = result.item
			continue
		}
		if errors.Is(result.err, ErrSkillVersionConflict) {
			conflicts++
			continue
		}
		t.Fatalf("replacePackage() unexpected error = %v", result.err)
	}
	if winner == nil || conflicts != 1 {
		t.Fatalf("winner=%+v conflicts=%d, want one each", winner, conflicts)
	}
	objects := store.snapshotObjects()
	if _, exists := objects[oldKey]; exists {
		t.Fatalf("old generation survived concurrent replacement")
	}
	if len(objects) != len(winner.PackageFiles) {
		t.Fatalf("objects = %v, winner manifest = %+v", objects, winner.PackageFiles)
	}
	for _, file := range winner.PackageFiles {
		if _, exists := objects[file.ObjectKey]; !exists {
			t.Fatalf("winning object %q is missing", file.ObjectKey)
		}
	}
}

func TestImportPackageCleansPartialWrites(t *testing.T) {
	repo := &fakeSkillRepo{items: map[uint]domainskill.Skill{}}
	store := newSkillLifecycleStore()
	store.failPutAt = 2
	service := NewService(repo)
	service.SetObjectStoreProvider(skillLifecycleStoreProvider{store: store})
	item := &domainskill.Skill{
		Scope:           domainskill.ScopeUser,
		OwnerUserID:     11,
		Title:           "Partial package",
		Trigger:         "partial-package",
		PackageType:     domainskill.PackageTypePackage,
		PackageFiles:    []domainskill.PackageFile{{Path: "one.md"}, {Path: "two.md"}},
		CreatedByUserID: 11,
		UpdatedByUserID: 11,
	}

	if _, err := service.importPackage(t.Context(), item, map[string][]byte{"one.md": []byte("one"), "two.md": []byte("two")}); err == nil {
		t.Fatal("importPackage() error = nil, want injected put error")
	}
	if len(store.objects) != 0 {
		t.Fatalf("partial package objects survived: %v", store.objects)
	}
	if len(repo.items) != 0 {
		t.Fatalf("created skill survived partial import: %v", repo.items)
	}
}

type skillLifecycleStoreProvider struct {
	store objectstore.Store
}

func (p skillLifecycleStoreProvider) Open(context.Context) (objectstore.Store, error) {
	return p.store, nil
}

var _ appstorage.Provider = skillLifecycleStoreProvider{}

type skillLifecycleStore struct {
	mu        sync.Mutex
	objects   map[string][]byte
	putCount  int
	failPutAt int
}

func newSkillLifecycleStore() *skillLifecycleStore {
	return &skillLifecycleStore{objects: map[string][]byte{}}
}

func (s *skillLifecycleStore) Put(_ context.Context, key string, body io.Reader, opts objectstore.PutOptions) (objectstore.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putCount++
	if s.failPutAt > 0 && s.putCount == s.failPutAt {
		return objectstore.ObjectInfo{}, errors.New("injected put failure")
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return objectstore.ObjectInfo{}, err
	}
	s.objects[key] = append([]byte(nil), data...)
	return objectstore.ObjectInfo{Key: key, SizeBytes: int64(len(data)), ContentType: opts.ContentType, ModTime: time.Now()}, nil
}

func (s *skillLifecycleStore) Open(_ context.Context, key string) (io.ReadCloser, objectstore.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, exists := s.objects[key]
	if !exists {
		return nil, objectstore.ObjectInfo{}, objectstore.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), objectstore.ObjectInfo{Key: key, SizeBytes: int64(len(data))}, nil
}

func (s *skillLifecycleStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *skillLifecycleStore) snapshotObjects() map[string][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string][]byte, len(s.objects))
	for key, value := range s.objects {
		result[key] = append([]byte(nil), value...)
	}
	return result
}

func (s *skillLifecycleStore) Materialize(_ context.Context, key string) (string, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.objects[key]; !exists {
		return "", nil, objectstore.ErrNotFound
	}
	return key, func() {}, nil
}

func TestCreateUserRequiresMarkdown(t *testing.T) {
	service := NewService(&fakeSkillRepo{items: map[uint]domainskill.Skill{}})
	_, err := service.CreateUser(context.Background(), 7, WriteInput{
		Title:   "Review",
		Trigger: "review",
		Enabled: true,
	})
	if !errors.Is(err, ErrInvalidSkill) {
		t.Fatalf("expected ErrInvalidSkill, got %v", err)
	}
}

func TestCreateUserStoresMarkdown(t *testing.T) {
	service := NewService(&fakeSkillRepo{items: map[uint]domainskill.Skill{}})
	item, err := service.CreateUser(context.Background(), 7, WriteInput{
		Title:       "Review",
		Trigger:     "/review",
		Description: "Review code",
		Markdown:    "Review the submitted code and return prioritized findings.",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("expected skill to be created, got %v", err)
	}
	if item.Trigger != "review" {
		t.Fatalf("expected normalized trigger, got %q", item.Trigger)
	}
	if item.Markdown != "Review the submitted code and return prioritized findings." {
		t.Fatalf("unexpected markdown: %q", item.Markdown)
	}
}

func TestResolveAvailableEnforcesVisibility(t *testing.T) {
	service := NewService(&fakeSkillRepo{items: map[uint]domainskill.Skill{
		1: {ID: 1, Scope: domainskill.ScopeUser, OwnerUserID: 8, Enabled: true, Title: "Private"},
		2: {ID: 2, Scope: domainskill.ScopeBuiltin, Enabled: true, Title: "Builtin"},
	}})

	if _, err := service.ResolveAvailable(context.Background(), 7, 1); !errors.Is(err, ErrSkillNotFound) {
		t.Fatalf("expected ErrSkillNotFound, got %v", err)
	}
	item, err := service.ResolveAvailable(context.Background(), 7, 2)
	if err != nil {
		t.Fatalf("expected builtin visible, got %v", err)
	}
	if item.ID != 2 {
		t.Fatalf("expected id 2, got %d", item.ID)
	}
}

func TestGetPackageFileRejectsBinaryFiles(t *testing.T) {
	service := NewService(&fakeSkillRepo{items: map[uint]domainskill.Skill{
		1: {
			ID:          1,
			Scope:       domainskill.ScopeUser,
			OwnerUserID: 7,
			Enabled:     true,
			PackageType: domainskill.PackageTypePackage,
			PackageFiles: []domainskill.PackageFile{{
				Path: "assets/icon.png",
				Kind: domainskill.FileKindBinary,
			}},
		},
	}})

	_, err := service.GetPackageFile(context.Background(), 7, 1, "assets/icon.png")
	if !errors.Is(err, ErrPackageFileUnreadable) {
		t.Fatalf("expected ErrPackageFileUnreadable, got %v", err)
	}
}
