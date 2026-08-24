package artifact

import (
	"context"
	"testing"

	domainartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/artifact"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpdateArtifactAlsoUpdatesActiveShareTitle(t *testing.T) {
	db := openArtifactSQLiteTestDB(t)
	record := model.Artifact{
		ArtifactPublicID: "artifact-1",
		UserID:           7,
		Kind:             "html",
		Title:            "Old title",
		Code:             "<p>old</p>",
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	share := model.ArtifactShare{
		ShareID:       "share-1",
		ArtifactID:    record.ID,
		UserID:        record.UserID,
		TitleSnapshot: record.Title,
		Status:        "active",
	}
	if err := db.Create(&share).Error; err != nil {
		t.Fatalf("create artifact share: %v", err)
	}

	err := NewRepo(db).UpdateArtifact(context.Background(), &domainartifact.Artifact{
		ID:               record.ID,
		ArtifactPublicID: record.ArtifactPublicID,
		UserID:           record.UserID,
		Kind:             "text",
		Title:            "New title",
		Code:             "new code",
	})
	if err != nil {
		t.Fatalf("UpdateArtifact() error = %v", err)
	}

	var updatedArtifact model.Artifact
	if err := db.First(&updatedArtifact, record.ID).Error; err != nil {
		t.Fatalf("load updated artifact: %v", err)
	}
	if updatedArtifact.Title != "New title" || updatedArtifact.Kind != "text" || updatedArtifact.Code != "new code" {
		t.Fatalf("updated artifact = %#v", updatedArtifact)
	}

	var updatedShare model.ArtifactShare
	if err := db.First(&updatedShare, share.ID).Error; err != nil {
		t.Fatalf("load updated share: %v", err)
	}
	if updatedShare.TitleSnapshot != "New title" {
		t.Fatalf("share title snapshot = %q, want %q", updatedShare.TitleSnapshot, "New title")
	}
}

func openArtifactSQLiteTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:artifact_repository?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("resolve sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	if err := db.AutoMigrate(&model.Artifact{}, &model.ArtifactShare{}); err != nil {
		t.Fatalf("migrate artifact tables: %v", err)
	}
	return db
}
