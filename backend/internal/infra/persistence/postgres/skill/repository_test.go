package skill

import (
	"context"
	"errors"
	"testing"

	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
	if err = db.AutoMigrate(&model.Skill{}, &model.ConversationProjectSkill{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	skill := model.Skill{
		Scope:       "user",
		OwnerUserID: 1,
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

	if _, err = NewRepo(db).DeleteSkill(context.Background(), skill.ID); err != nil {
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

func TestPatchSkillRejectsStalePackageStorageVersion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:skill_package_version_cas?mode=memory&cache=shared"), &gorm.Config{})
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
	if err = db.AutoMigrate(&model.Skill{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	repo := NewRepo(db)
	created, err := repo.CreateSkill(context.Background(), &domainskill.Skill{
		Scope:                 domainskill.ScopeUser,
		OwnerUserID:           1,
		Title:                 "Package",
		Trigger:               "package",
		PackageType:           domainskill.PackageTypePackage,
		PackageStorageVersion: "v1",
		Enabled:               true,
	})
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}

	expected := "stale"
	next := "v2"
	if _, err = repo.PatchSkill(context.Background(), created.ID, repository.SkillPatch{
		PackageStorageVersion:         &next,
		ExpectedPackageStorageVersion: &expected,
	}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	current, err := repo.GetSkill(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if current.PackageStorageVersion != "v1" {
		t.Fatalf("expected storage version v1 to remain, got %q", current.PackageStorageVersion)
	}
}
