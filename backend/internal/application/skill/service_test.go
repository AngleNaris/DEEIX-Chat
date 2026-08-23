package skill

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	appstorage "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/objectstorage"
	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

type fakeSkillRepo struct {
	items        map[uint]domainskill.Skill
	next         uint
	patchErr     error
	deleteErr    error
	beforeDelete func(*fakeSkillRepo, uint)
}

func (r *fakeSkillRepo) ListSkills(context.Context, repository.SkillListFilter, int, int) ([]domainskill.Skill, int64, error) {
	return nil, 0, nil
}

func (r *fakeSkillRepo) GetSkill(_ context.Context, id uint) (*domainskill.Skill, error) {
	item, ok := r.items[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &item, nil
}

func (r *fakeSkillRepo) CreateSkill(_ context.Context, item *domainskill.Skill) (*domainskill.Skill, error) {
	if r.next == 0 {
		r.next = 1
	}
	item.ID = r.next
	r.next++
	r.items[item.ID] = *item
	result := r.items[item.ID]
	return &result, nil
}

func (r *fakeSkillRepo) PatchSkill(_ context.Context, id uint, patch repository.SkillPatch) (*domainskill.Skill, error) {
	if r.patchErr != nil {
		return nil, r.patchErr
	}
	item, ok := r.items[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if patch.ExpectedPackageStorageVersion != nil &&
		item.PackageStorageVersion != *patch.ExpectedPackageStorageVersion {
		return nil, repository.ErrConflict
	}
	if patch.PackageStorageVersion != nil {
		item.PackageStorageVersion = *patch.PackageStorageVersion
	}
	r.items[id] = item
	return &item, nil
}

func (r *fakeSkillRepo) DeleteSkill(_ context.Context, id uint) (*domainskill.Skill, error) {
	if r.deleteErr != nil {
		return nil, r.deleteErr
	}
	if r.beforeDelete != nil {
		r.beforeDelete(r, id)
	}
	item, ok := r.items[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	delete(r.items, id)
	return &item, nil
}

type skillTestStoreProvider struct {
	store objectstore.Store
}

func (p skillTestStoreProvider) Open(context.Context) (objectstore.Store, error) {
	return p.store, nil
}

var _ appstorage.Provider = skillTestStoreProvider{}

type skillTestStore struct {
	objects    map[string][]byte
	putCount   int
	failPutAt  int
	failDelete bool
}

func newSkillTestStore() *skillTestStore {
	return &skillTestStore{objects: map[string][]byte{}}
}

func (s *skillTestStore) Put(_ context.Context, key string, body io.Reader, opts objectstore.PutOptions) (objectstore.ObjectInfo, error) {
	s.putCount++
	if s.failPutAt > 0 && s.putCount == s.failPutAt {
		return objectstore.ObjectInfo{}, errors.New("injected put failure")
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return objectstore.ObjectInfo{}, err
	}
	s.objects[key] = data
	return objectstore.ObjectInfo{Key: key, SizeBytes: int64(len(data)), ContentType: opts.ContentType}, nil
}

func (s *skillTestStore) Open(_ context.Context, key string) (io.ReadCloser, objectstore.ObjectInfo, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, objectstore.ObjectInfo{}, objectstore.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), objectstore.ObjectInfo{Key: key, SizeBytes: int64(len(data))}, nil
}

func (s *skillTestStore) Delete(_ context.Context, key string) error {
	if s.failDelete {
		return errors.New("injected delete failure")
	}
	delete(s.objects, key)
	return nil
}

func (s *skillTestStore) Materialize(context.Context, string) (string, func(), error) {
	return "", func() {}, errors.New("not implemented")
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

func TestImportPackageRollsBackWrittenObjectsWhenPutFails(t *testing.T) {
	repo := &fakeSkillRepo{items: map[uint]domainskill.Skill{}}
	store := newSkillTestStore()
	store.failPutAt = 2
	service := NewService(repo)
	service.SetObjectStoreProvider(skillTestStoreProvider{store: store})
	item := &domainskill.Skill{
		Scope:                 domainskill.ScopeUser,
		OwnerUserID:           7,
		PackageType:           domainskill.PackageTypePackage,
		PackageStorageVersion: "import-v1",
		PackageFiles: []domainskill.PackageFile{
			{Path: "a.txt", Kind: domainskill.FileKindText},
			{Path: "b.txt", Kind: domainskill.FileKindText},
		},
	}

	if _, err := service.importPackage(context.Background(), item, map[string][]byte{
		"a.txt": []byte("a"),
		"b.txt": []byte("b"),
	}); err == nil {
		t.Fatal("expected import failure")
	}
	if len(store.objects) != 0 {
		t.Fatalf("expected written objects to be rolled back, got %#v", store.objects)
	}
	if len(repo.items) != 0 {
		t.Fatalf("expected skill row to be rolled back, got %#v", repo.items)
	}
}

func TestReplacePackagePatchFailurePreservesOldPackage(t *testing.T) {
	oldItem := domainskill.Skill{
		ID:                    1,
		Scope:                 domainskill.ScopeUser,
		OwnerUserID:           7,
		PackageType:           domainskill.PackageTypePackage,
		PackageStorageVersion: "old-v1",
		PackageFiles: []domainskill.PackageFile{
			{Path: "scripts/run.py", Kind: domainskill.FileKindText},
		},
	}
	repo := &fakeSkillRepo{
		items:    map[uint]domainskill.Skill{oldItem.ID: oldItem},
		patchErr: errors.New("injected patch failure"),
	}
	store := newSkillTestStore()
	oldKey := packageObjectKey(oldItem, "scripts/run.py")
	store.objects[oldKey] = []byte("old")
	service := NewService(repo)
	service.SetObjectStoreProvider(skillTestStoreProvider{store: store})

	_, err := service.replacePackage(context.Background(), oldItem, buildZip(t, map[string]string{
		"SKILL.md":       "replacement",
		"scripts/run.py": "new",
	}))
	if err == nil {
		t.Fatal("expected replace failure")
	}
	if got := string(store.objects[oldKey]); got != "old" {
		t.Fatalf("expected old object to survive, got %q", got)
	}
	if len(store.objects) != 1 {
		t.Fatalf("expected staged objects to be cleaned, got %#v", store.objects)
	}
}

func TestDeleteUserDatabaseFailurePreservesPackageObjects(t *testing.T) {
	item := domainskill.Skill{
		ID:                    1,
		Scope:                 domainskill.ScopeUser,
		OwnerUserID:           7,
		PackageType:           domainskill.PackageTypePackage,
		PackageStorageVersion: "delete-v1",
		PackageFiles: []domainskill.PackageFile{
			{Path: "notes.txt", Kind: domainskill.FileKindText},
		},
	}
	repo := &fakeSkillRepo{
		items:     map[uint]domainskill.Skill{item.ID: item},
		deleteErr: errors.New("injected delete failure"),
	}
	store := newSkillTestStore()
	key := packageObjectKey(item, "notes.txt")
	store.objects[key] = []byte("keep")
	service := NewService(repo)
	service.SetObjectStoreProvider(skillTestStoreProvider{store: store})

	if err := service.DeleteUser(context.Background(), 7, item.ID); err == nil {
		t.Fatal("expected delete failure")
	}
	if got := string(store.objects[key]); got != "keep" {
		t.Fatalf("expected object to remain when database delete fails, got %q", got)
	}
}

func TestDeleteUserCleansStorageVersionDeletedByRepository(t *testing.T) {
	oldItem := domainskill.Skill{
		ID:                    1,
		Scope:                 domainskill.ScopeUser,
		OwnerUserID:           7,
		PackageType:           domainskill.PackageTypePackage,
		PackageStorageVersion: "delete-v1",
		PackageFiles:          []domainskill.PackageFile{{Path: "notes.txt", Kind: domainskill.FileKindText}},
	}
	currentItem := oldItem
	currentItem.PackageStorageVersion = "delete-v2"
	repo := &fakeSkillRepo{
		items: map[uint]domainskill.Skill{oldItem.ID: oldItem},
		beforeDelete: func(repo *fakeSkillRepo, id uint) {
			repo.items[id] = currentItem
		},
	}
	store := newSkillTestStore()
	oldKey := packageObjectKey(oldItem, "notes.txt")
	currentKey := packageObjectKey(currentItem, "notes.txt")
	store.objects[oldKey] = []byte("old")
	store.objects[currentKey] = []byte("current")
	service := NewService(repo)
	service.SetObjectStoreProvider(skillTestStoreProvider{store: store})

	if err := service.DeleteUser(context.Background(), 7, oldItem.ID); err != nil {
		t.Fatalf("DeleteUser() error = %v", err)
	}
	if _, exists := store.objects[currentKey]; exists {
		t.Fatalf("expected current storage generation %q to be deleted", currentKey)
	}
	if got := string(store.objects[oldKey]); got != "old" {
		t.Fatalf("expected stale generation to remain owned by its replacement cleanup, got %q", got)
	}
}

func TestDeleteUserReportsPackageCleanupFailure(t *testing.T) {
	item := domainskill.Skill{
		ID:                    1,
		Scope:                 domainskill.ScopeUser,
		OwnerUserID:           7,
		PackageType:           domainskill.PackageTypePackage,
		PackageStorageVersion: "delete-v1",
		PackageFiles:          []domainskill.PackageFile{{Path: "notes.txt", Kind: domainskill.FileKindText}},
	}
	repo := &fakeSkillRepo{items: map[uint]domainskill.Skill{item.ID: item}}
	store := newSkillTestStore()
	store.failDelete = true
	store.objects[packageObjectKey(item, "notes.txt")] = []byte("keep")
	service := NewService(repo)
	service.SetObjectStoreProvider(skillTestStoreProvider{store: store})

	if err := service.DeleteUser(context.Background(), 7, item.ID); err == nil {
		t.Fatal("expected package cleanup failure")
	}
}
