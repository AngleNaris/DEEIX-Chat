package conversation

import (
	"context"
	"errors"
	"testing"
	"time"

	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	models "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFileShareLifecycleEnforcesOwnerAndSourceFile(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:fileshare?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.FileObject{}, &models.FileShare{}); err != nil {
		t.Fatal(err)
	}
	file := models.FileObject{FileID: "file-owner", UserID: 7, FileName: "report.pdf", MimeType: "application/pdf", Status: "active"}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewRepo(db)
	ctx := context.Background()
	expiresAt := time.Now().Add(time.Hour)
	first := &domainconversation.FileShare{ShareID: "share-first", FileID: file.FileID, UserID: 7, ExpiresAt: &expiresAt}
	if err := repo.ReplaceActiveFileShare(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceActiveFileShare(ctx, &domainconversation.FileShare{ShareID: "share-cross-user", FileID: file.FileID, UserID: 8, ExpiresAt: &expiresAt}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user create error = %v, want not found", err)
	}
	second := &domainconversation.FileShare{ShareID: "share-second", FileID: file.FileID, UserID: 7, ExpiresAt: &expiresAt}
	if err := repo.ReplaceActiveFileShare(ctx, second); err != nil {
		t.Fatal(err)
	}
	oldShare, _, err := repo.GetFileShareByShareID(ctx, first.ShareID)
	if err != nil || oldShare.Status != "revoked" {
		t.Fatalf("old share = %#v, err = %v", oldShare, err)
	}
	if err := repo.RevokeActiveFileShare(ctx, 8, file.FileID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user revoke error = %v, want not found", err)
	}
	if err := db.Delete(&file).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.GetFileShareByShareID(ctx, second.ShareID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("share after source delete error = %v, want not found", err)
	}
}

func TestFileShareAllowsOnlyOneActiveLinkPerOwnerAndFile(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:fileshare-unique?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.FileShare{}); err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().Add(time.Hour)
	first := models.FileShare{ShareID: "share-active-1", FileID: "file-1", UserID: 7, Status: "active", ExpiresAt: &expiresAt}
	second := models.FileShare{ShareID: "share-active-2", FileID: "file-1", UserID: 7, Status: "active", ExpiresAt: &expiresAt}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&second).Error; !errors.Is(translateError(err), repository.ErrDuplicate) {
		t.Fatalf("second active share error = %v, want duplicate", err)
	}
	second.Status = "revoked"
	if err := db.Create(&second).Error; err != nil {
		t.Fatalf("revoked share should be retained: %v", err)
	}
}
