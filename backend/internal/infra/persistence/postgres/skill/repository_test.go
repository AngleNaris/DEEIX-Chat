package skill

import (
	"context"
	"errors"
	"testing"
	"time"

	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUserSkillWritesRequireExistingOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:skill_owner_lock?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err = db.AutoMigrate(&model.User{}, &model.Skill{}, &model.ConversationProjectSkill{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	repo := NewRepo(db)

	_, err = repo.CreateSkill(context.Background(), &domainskill.Skill{
		Scope:           domainskill.ScopeUser,
		OwnerUserID:     404,
		Title:           "Orphan skill",
		Trigger:         "orphan-skill",
		Markdown:        "Must not persist.",
		Enabled:         true,
		CreatedByUserID: 404,
		UpdatedByUserID: 404,
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("CreateSkill() error = %v, want ErrNotFound", err)
	}
	var skillCount int64
	if countErr := db.Model(&model.Skill{}).Count(&skillCount).Error; countErr != nil {
		t.Fatalf("count skills: %v", countErr)
	}
	if skillCount != 0 {
		t.Fatalf("skill count = %d, want 0", skillCount)
	}

	orphanRows := []model.Skill{
		{Scope: domainskill.ScopeUser, OwnerUserID: 404, Title: "Patch orphan", Trigger: "patch-orphan", Enabled: true},
		{Scope: domainskill.ScopeUser, OwnerUserID: 404, Title: "Delete orphan", Trigger: "delete-orphan", Enabled: true},
	}
	if err = db.Create(&orphanRows).Error; err != nil {
		t.Fatalf("seed orphan skills: %v", err)
	}
	updatedTitle := "Should not update"
	if _, err = repo.PatchSkill(context.Background(), orphanRows[0].ID, repository.SkillPatch{Title: &updatedTitle}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("PatchSkill() error = %v, want ErrNotFound", err)
	}
	if err = repo.DeleteSkill(context.Background(), orphanRows[1].ID, nil); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("DeleteSkill() error = %v, want ErrNotFound", err)
	}
	var unchanged model.Skill
	if err = db.Where("id = ?", orphanRows[0].ID).First(&unchanged).Error; err != nil {
		t.Fatalf("load patch orphan: %v", err)
	}
	if unchanged.Title != orphanRows[0].Title {
		t.Fatalf("orphan title = %q, want unchanged %q", unchanged.Title, orphanRows[0].Title)
	}
	unchanged = model.Skill{}
	if err = db.Where("id = ?", orphanRows[1].ID).First(&unchanged).Error; err != nil {
		t.Fatalf("load delete orphan: %v", err)
	}
}

func TestSkillPatchAndDeleteUseUpdatedAtCAS(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:skill_updated_at_cas?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err = db.AutoMigrate(&model.User{}, &model.Skill{}, &model.ConversationProjectSkill{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	owner := model.User{PublicID: "skill_cas_owner", Username: "skill-cas-owner", Role: "user", Status: "active"}
	if err = db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	repo := NewRepo(db)
	created, err := repo.CreateSkill(context.Background(), &domainskill.Skill{
		Scope:       domainskill.ScopeUser,
		OwnerUserID: owner.ID,
		Title:       "CAS skill",
		Trigger:     "cas-skill",
		Markdown:    "Initial",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("CreateSkill() error = %v", err)
	}

	stale := created.UpdatedAt.Add(-time.Second)
	staleTitle := "Stale title"
	if _, err = repo.PatchSkill(context.Background(), created.ID, repository.SkillPatch{
		ExpectedUpdatedAt: &stale,
		Title:             &staleTitle,
	}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale PatchSkill() error = %v, want ErrConflict", err)
	}
	freshTitle := "Fresh title"
	updated, err := repo.PatchSkill(context.Background(), created.ID, repository.SkillPatch{
		ExpectedUpdatedAt: &created.UpdatedAt,
		Title:             &freshTitle,
	})
	if err != nil {
		t.Fatalf("matching PatchSkill() error = %v", err)
	}
	if updated.Title != freshTitle || !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("updated skill = %+v, want fresh title and newer version", updated)
	}
	if err = repo.DeleteSkill(context.Background(), created.ID, &created.UpdatedAt); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale DeleteSkill() error = %v, want ErrConflict", err)
	}
	if err = repo.DeleteSkill(context.Background(), created.ID, &updated.UpdatedAt); err != nil {
		t.Fatalf("matching DeleteSkill() error = %v", err)
	}
}

func TestSkillPackageManifestRoundTripsObjectKeysAndLegacyRows(t *testing.T) {
	files := []domainskill.PackageFile{{
		Path:      "references/guide.md",
		Size:      42,
		Kind:      domainskill.FileKindText,
		ObjectKey: "skills/user/7/generation/references/guide.md",
	}}
	decoded := decodePackageFiles(encodePackageFiles(files))
	if len(decoded) != 1 || decoded[0] != files[0] {
		t.Fatalf("manifest round trip = %+v, want %+v", decoded, files)
	}
	legacy := decodePackageFiles(`[{"path":"SKILL.md","size":12,"kind":"text"}]`)
	if len(legacy) != 1 || legacy[0].Path != "SKILL.md" || legacy[0].ObjectKey != "" {
		t.Fatalf("legacy manifest = %+v", legacy)
	}
}

func TestDeleteSkillCleansConversationProjectAssociations(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:skill_project_cascade?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("open sqlite connection: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	if err = db.AutoMigrate(&model.User{}, &model.Skill{}, &model.ConversationProjectSkill{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	owner := model.User{PublicID: "skill_project_owner", Username: "skill-project-owner", Role: "user", Status: "active"}
	if err = db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}

	skill := model.Skill{
		Scope:       "user",
		OwnerUserID: owner.ID,
		Title:       "Project skill",
		Trigger:     "project-skill",
		Enabled:     true,
	}
	if err = db.Create(&skill).Error; err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if err = db.Create(&model.ConversationProjectSkill{ProjectID: 9, SkillID: skill.ID}).Error; err != nil {
		t.Fatalf("create project Skill association: %v", err)
	}

	if err = NewRepo(db).DeleteSkill(context.Background(), skill.ID, nil); err != nil {
		t.Fatalf("DeleteSkill() error = %v", err)
	}

	var associationCount int64
	if err = db.Model(&model.ConversationProjectSkill{}).Where("skill_id = ?", skill.ID).Count(&associationCount).Error; err != nil {
		t.Fatalf("count project Skill associations: %v", err)
	}
	if associationCount != 0 {
		t.Fatalf("project Skill association count = %d, want 0", associationCount)
	}
}
