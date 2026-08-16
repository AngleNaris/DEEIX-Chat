package conversation

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	appprocessing "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/processing"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	persistconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/postgres/conversation"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOverwriteFileContentUsesUniqueKeysAndRemovesReplacedObjects(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:platform_write_file?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err = db.AutoMigrate(&model.User{}, &model.FileObject{}, &model.Attachment{}, &model.FileChunk{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	user := model.User{PublicID: "platform_write_owner", Username: "platform-write-owner", Role: "user", Status: "active"}
	if err = db.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	file := model.FileObject{
		FileID:       "file_platform_write",
		UserID:       user.ID,
		FileName:     "notes.txt",
		MimeType:     "text/plain",
		FileCategory: "text",
		StoragePath:  "objects/initial-notes.txt",
		Status:       "active",
	}
	if err = db.Create(&file).Error; err != nil {
		t.Fatalf("seed file: %v", err)
	}

	store := objectstore.NewLocal(t.TempDir())
	if _, err = store.Put(t.Context(), file.StoragePath, bytes.NewBufferString("initial"), objectstore.PutOptions{SizeBytes: 7, ContentType: "text/plain"}); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	service := &Service{
		repo:          persistconversation.NewRepo(db),
		storeProvider: &conversationTestStoreProvider{store: store},
		processingSvc: &appprocessing.Service{},
	}

	if err = service.overwriteFileContent(t.Context(), user.ID, file.FileID, "first"); err != nil {
		t.Fatalf("first overwrite: %v", err)
	}
	var first model.FileObject
	if err = db.Where("id = ?", file.ID).First(&first).Error; err != nil {
		t.Fatalf("load first overwrite: %v", err)
	}
	if first.StoragePath == file.StoragePath || !strings.Contains(first.StoragePath, file.FileID) {
		t.Fatalf("first storage path = %q, want unique file-scoped key", first.StoragePath)
	}
	assertObjectMissing(t, store, file.StoragePath)

	if err = service.overwriteFileContent(context.Background(), user.ID, file.FileID, "second"); err != nil {
		t.Fatalf("second overwrite: %v", err)
	}
	var second model.FileObject
	if err = db.Where("id = ?", file.ID).First(&second).Error; err != nil {
		t.Fatalf("load second overwrite: %v", err)
	}
	if second.StoragePath == first.StoragePath {
		t.Fatalf("consecutive writes reused storage path %q", second.StoragePath)
	}
	assertObjectMissing(t, store, first.StoragePath)

	reader, _, err := store.Open(t.Context(), second.StoragePath)
	if err != nil {
		t.Fatalf("open current object: %v", err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read current object: %v", err)
	}
	if string(content) != "second" {
		t.Fatalf("current object content = %q, want second", content)
	}
}

func assertObjectMissing(t *testing.T, store objectstore.Store, key string) {
	t.Helper()
	reader, _, err := store.Open(t.Context(), key)
	if reader != nil {
		_ = reader.Close()
	}
	if !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("open removed object %q error = %v, want ErrNotFound", key, err)
	}
}
