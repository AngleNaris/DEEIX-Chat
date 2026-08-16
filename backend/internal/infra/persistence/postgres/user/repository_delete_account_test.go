package user

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	appartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/artifact"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	artifactrepo "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/postgres/artifact"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/schema"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDeleteAccountHardRemovesNewUserDomainsAndRetainsFinancialAudit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:delete_user_new_domains?mode=memory&cache=shared"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err = db.AutoMigrate(schema.Models()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	users := []model.User{
		{PublicID: "u_delete_target", Username: "delete-target", Email: "delete-target@example.com", Role: "user", Status: "active"},
		{PublicID: "u_delete_control", Username: "delete-control", Email: "delete-control@example.com", Role: "user", Status: "active"},
	}
	if err = db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	targetID, controlID := users[0].ID, users[1].ID

	roles := []model.ConversationRole{
		{UserID: targetID, PublicID: "role_delete_target", Name: "Target role"},
		{UserID: controlID, PublicID: "role_delete_control", Name: "Control role"},
	}
	if err = db.Create(&roles).Error; err != nil {
		t.Fatalf("seed roles: %v", err)
	}
	roleTools := []model.ConversationRoleMCPTool{{RoleID: roles[0].ID, ToolID: 70}, {RoleID: roles[1].ID, ToolID: 71}}
	roleSkills := []model.ConversationRoleSkill{{RoleID: roles[0].ID, SkillID: 80}, {RoleID: roles[1].ID, SkillID: 81}}
	if err = db.Create(&roleTools).Error; err != nil {
		t.Fatalf("seed role tools: %v", err)
	}
	if err = db.Create(&roleSkills).Error; err != nil {
		t.Fatalf("seed role skills: %v", err)
	}

	groups := []model.AgentGroup{
		{UserID: targetID, PublicID: "group_delete_target", Name: "Target group"},
		{UserID: controlID, PublicID: "group_delete_control", Name: "Control group"},
	}
	if err = db.Create(&groups).Error; err != nil {
		t.Fatalf("seed agent groups: %v", err)
	}
	members := []model.AgentGroupMember{
		{PublicID: "member_delete_target", GroupID: groups[0].ID, RoleID: roles[0].ID},
		{PublicID: "member_delete_control", GroupID: groups[1].ID, RoleID: roles[1].ID},
	}
	if err = db.Create(&members).Error; err != nil {
		t.Fatalf("seed agent group members: %v", err)
	}
	conversations := []model.Conversation{
		{UserID: targetID, PublicID: "conversation_delete_target", SessionKey: "session_delete_target"},
		{UserID: controlID, PublicID: "conversation_delete_control", SessionKey: "session_delete_control"},
	}
	if err = db.Create(&conversations).Error; err != nil {
		t.Fatalf("seed conversations: %v", err)
	}
	fileObjects := []model.FileObject{
		{FileID: "file_delete_exclusive", UserID: targetID, StoragePath: "objects/delete-exclusive.bin", ExtractStoragePath: ".extracts/delete-exclusive.txt", Status: "active"},
		{FileID: "file_delete_soft_deleted", UserID: targetID, StoragePath: "objects/delete-soft.bin", ExtractStoragePath: ".extracts/delete-soft.txt", Status: "active"},
		{FileID: "file_delete_shared", UserID: targetID, StoragePath: "objects/delete-shared.bin", ExtractStoragePath: ".extracts/delete-shared.txt", Status: "active"},
		{FileID: "file_control_shared_main", UserID: controlID, StoragePath: "objects/delete-shared.bin", Status: "active"},
		{FileID: "file_control_shared_extract", UserID: controlID, StoragePath: "objects/control-exclusive.bin", ExtractStoragePath: ".extracts/delete-shared.txt", Status: "active"},
	}
	if err = db.Create(&fileObjects).Error; err != nil {
		t.Fatalf("seed file objects: %v", err)
	}
	if err = db.Delete(&fileObjects[1]).Error; err != nil {
		t.Fatalf("soft delete target file object: %v", err)
	}
	if err = db.Delete(&fileObjects[3]).Error; err != nil {
		t.Fatalf("soft delete control shared file object: %v", err)
	}
	attachments := []model.Attachment{
		{ConversationID: conversations[0].ID, UserID: targetID, FileID: "file_target_attachment", StoragePath: "objects/target-attachment.bin", Status: "active"},
		{ConversationID: conversations[1].ID, UserID: controlID, FileID: "file_control_shared_main", StoragePath: "objects/delete-shared.bin", Status: "active"},
	}
	if err = db.Create(&attachments).Error; err != nil {
		t.Fatalf("seed attachments: %v", err)
	}
	now := time.Now().UTC()
	groupRuns := []model.AgentGroupRun{
		{PublicID: "group_run_delete_target", ClientRunID: "client_run_delete_target", UserID: targetID, ConversationID: conversations[0].ID, GroupID: groups[0].ID, StartedAt: now},
		{PublicID: "group_run_delete_control", ClientRunID: "client_run_delete_control", UserID: controlID, ConversationID: conversations[1].ID, GroupID: groups[1].ID, StartedAt: now},
	}
	if err = db.Create(&groupRuns).Error; err != nil {
		t.Fatalf("seed agent group runs: %v", err)
	}
	steps := []model.AgentGroupStep{
		{PublicID: "group_step_delete_target", GroupRunID: groupRuns[0].ID, Sequence: 1},
		{PublicID: "group_step_delete_control", GroupRunID: groupRuns[1].ID, Sequence: 1},
	}
	if err = db.Create(&steps).Error; err != nil {
		t.Fatalf("seed agent group steps: %v", err)
	}
	attempts := []model.AgentGroupStepAttempt{
		{PublicID: "group_attempt_delete_target", StepID: steps[0].ID, AttemptNo: 1, RetryRequestID: "retry_delete_target", StartedAt: now},
		{PublicID: "group_attempt_delete_control", StepID: steps[1].ID, AttemptNo: 1, RetryRequestID: "retry_delete_control", StartedAt: now},
	}
	if err = db.Create(&attempts).Error; err != nil {
		t.Fatalf("seed agent group attempts: %v", err)
	}

	artifacts := []model.Artifact{
		{ArtifactPublicID: "artifact_delete_target", UserID: targetID, Kind: "html", Title: "Target artifact", Code: "<p>target</p>"},
		{ArtifactPublicID: "artifact_delete_control", UserID: controlID, Kind: "html", Title: "Control artifact", Code: "<p>control</p>"},
	}
	if err = db.Create(&artifacts).Error; err != nil {
		t.Fatalf("seed artifacts: %v", err)
	}
	artifactShares := []model.ArtifactShare{
		{ShareID: "artifact_share_delete_target", ArtifactID: artifacts[0].ID, UserID: targetID, TitleSnapshot: "Target artifact", Status: "active"},
		{ShareID: "artifact_share_delete_control", ArtifactID: artifacts[1].ID, UserID: controlID, TitleSnapshot: "Control artifact", Status: "active"},
	}
	if err = db.Create(&artifactShares).Error; err != nil {
		t.Fatalf("seed artifact shares: %v", err)
	}
	conversationShares := []model.ConversationShare{
		{ShareID: "conversation_share_delete_target", ConversationID: conversations[0].ID, UserID: targetID, Status: "active"},
		{ShareID: "conversation_share_delete_control", ConversationID: conversations[1].ID, UserID: controlID, Status: "active"},
	}
	if err = db.Create(&conversationShares).Error; err != nil {
		t.Fatalf("seed conversation shares: %v", err)
	}

	if err = db.Create(&[]model.Credential{
		{UserID: targetID, PublicID: "credential_delete_target", Name: "credential-target", SecretEnc: "ciphertext-target"},
		{UserID: controlID, PublicID: "credential_delete_control", Name: "credential-control", SecretEnc: "ciphertext-control"},
	}).Error; err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	if err = db.Create(&[]model.DocCard{
		{CardPublicID: "card_delete_target", UserID: targetID, Title: "Target card"},
		{CardPublicID: "card_delete_control", UserID: controlID, Title: "Control card"},
	}).Error; err != nil {
		t.Fatalf("seed doc cards: %v", err)
	}
	if err = db.Create(&[]model.DynamicPrompt{
		{PublicID: "prompt_delete_target", UserID: targetID, Name: "dynamic-target"},
		{PublicID: "prompt_delete_control", UserID: controlID, Name: "dynamic-control"},
	}).Error; err != nil {
		t.Fatalf("seed dynamic prompts: %v", err)
	}
	if err = db.Create(&[]model.PromptPreset{
		{Scope: "user", OwnerUserID: targetID, Title: "Target preset", Trigger: "preset-target"},
		{Scope: "user", OwnerUserID: controlID, Title: "Control preset", Trigger: "preset-control"},
		{Scope: "builtin", OwnerUserID: 0, Title: "Builtin preset", Trigger: "preset-builtin"},
	}).Error; err != nil {
		t.Fatalf("seed prompt presets: %v", err)
	}
	skills := []model.Skill{
		{
			Scope:            "user",
			OwnerUserID:      targetID,
			Title:            "Target package",
			Trigger:          "skill-target",
			PackageType:      "package",
			PackageFilesJSON: `[{"path":"SKILL.md","size":12,"kind":"text"},{"path":"references/guide.md","size":8,"kind":"text","object_key":"skills/user/PLACEHOLDER/generation-target/references/guide.md"}]`,
			Enabled:          true,
			CreatedByUserID:  targetID,
			UpdatedByUserID:  targetID,
		},
		{
			Scope:            "user",
			OwnerUserID:      controlID,
			Title:            "Control package",
			Trigger:          "skill-control",
			PackageType:      "package",
			PackageFilesJSON: `[{"path":"SKILL.md","size":12,"kind":"text"}]`,
			Enabled:          true,
			CreatedByUserID:  controlID,
			UpdatedByUserID:  controlID,
		},
	}
	if err = db.Create(&skills).Error; err != nil {
		t.Fatalf("seed skills: %v", err)
	}
	targetGenerationKey := "skills/user/" + strconv.FormatUint(uint64(skills[0].ID), 10) + "/generation-target/references/guide.md"
	targetManifest := `[{"path":"SKILL.md","size":12,"kind":"text"},{"path":"references/guide.md","size":8,"kind":"text","object_key":"` + targetGenerationKey + `"}]`
	if err = db.Model(&model.Skill{}).Where("id = ?", skills[0].ID).Update("package_files_json", targetManifest).Error; err != nil {
		t.Fatalf("set target generation manifest: %v", err)
	}
	announcement := model.Announcement{Title: "System announcement", Status: "active"}
	if err = db.Create(&announcement).Error; err != nil {
		t.Fatalf("seed announcement: %v", err)
	}
	if err = db.Create(&[]model.AnnouncementUserState{
		{AnnouncementID: announcement.ID, UserID: targetID, AnnouncementUpdatedAt: now},
		{AnnouncementID: announcement.ID, UserID: controlID, AnnouncementUpdatedAt: now},
	}).Error; err != nil {
		t.Fatalf("seed announcement states: %v", err)
	}

	billingAccount := model.BillingAccount{UserID: targetID, Currency: "USD", Status: "active"}
	if err = db.Create(&billingAccount).Error; err != nil {
		t.Fatalf("seed billing account: %v", err)
	}
	redemptionCode := model.RedemptionCode{CodeHash: "redemption_hash_delete_target", Status: "active"}
	if err = db.Create(&redemptionCode).Error; err != nil {
		t.Fatalf("seed redemption code: %v", err)
	}
	financialRows := []struct {
		label string
		item  interface{}
	}{
		{"payment order", &model.PaymentOrder{OrderNo: "order_delete_target", UserID: targetID, Status: "paid"}},
		{"balance transaction", &model.BalanceTransaction{AccountID: billingAccount.ID, UserID: targetID, Type: "credit"}},
		{"usage reservation", &model.UsageReservation{UserID: targetID, RefNo: "reservation_delete_target", Mode: "usage", Status: "settled", ExpiresAt: now.Add(time.Hour)}},
		{"redemption", &model.Redemption{CodeID: redemptionCode.ID, UserID: targetID, RefNo: "redemption_delete_target"}},
		{"usage ledger", &model.UsageLedger{UserID: targetID, UsageDate: now, BillingAt: now}},
	}
	for _, row := range financialRows {
		if err = db.Create(row.item).Error; err != nil {
			t.Fatalf("seed %s: %v", row.label, err)
		}
	}

	artifactService := appartifact.NewService(artifactrepo.NewRepo(db))
	if _, err = artifactService.GetPublicShare(context.Background(), artifactShares[0].ShareID); err != nil {
		t.Fatalf("public artifact share before deletion: %v", err)
	}

	for _, item := range []interface{}{&roles[0], &groups[0], &steps[0], &attempts[0]} {
		if err = db.Delete(item).Error; err != nil {
			t.Fatalf("soft delete %T: %v", item, err)
		}
	}

	storagePaths, err := NewRepo(db).DeleteAccountHardWithStoragePaths(context.Background(), targetID)
	if err != nil {
		t.Fatalf("DeleteAccountHardWithStoragePaths() error = %v", err)
	}
	wantStoragePaths := []string{
		".extracts/delete-exclusive.txt",
		".extracts/delete-soft.txt",
		"objects/delete-exclusive.bin",
		"objects/delete-soft.bin",
		"objects/target-attachment.bin",
		"skills/user/" + strconv.FormatUint(uint64(skills[0].ID), 10) + "/SKILL.md",
		targetGenerationKey,
	}
	if !reflect.DeepEqual(storagePaths, wantStoragePaths) {
		t.Fatalf("storage paths = %v, want %v", storagePaths, wantStoragePaths)
	}

	if _, err = artifactService.GetPublicShare(context.Background(), artifactShares[0].ShareID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("public artifact share after deletion error = %v, want not found", err)
	}

	assertUnscopedCount(t, db, &model.User{}, "id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.AgentGroup{}, "user_id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.AgentGroupRun{}, "user_id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.AgentGroupStep{}, "group_run_id = ?", groupRuns[0].ID, 0)
	assertUnscopedCount(t, db, &model.AgentGroupStepAttempt{}, "step_id = ?", steps[0].ID, 0)
	assertUnscopedCount(t, db, &model.AgentGroupMember{}, "group_id = ?", groups[0].ID, 0)
	assertUnscopedCount(t, db, &model.ConversationRole{}, "user_id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.ConversationRoleMCPTool{}, "role_id = ?", roles[0].ID, 0)
	assertUnscopedCount(t, db, &model.ConversationRoleSkill{}, "role_id = ?", roles[0].ID, 0)
	assertUnscopedCount(t, db, &model.Artifact{}, "user_id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.ArtifactShare{}, "user_id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.ConversationShare{}, "user_id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.Credential{}, "user_id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.DocCard{}, "user_id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.DynamicPrompt{}, "user_id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.PromptPreset{}, "owner_user_id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.Skill{}, "owner_user_id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.Attachment{}, "user_id = ?", targetID, 0)
	assertUnscopedCount(t, db, &model.AnnouncementUserState{}, "user_id = ?", targetID, 0)

	assertUnscopedCount(t, db, &model.User{}, "id = ?", controlID, 1)
	assertUnscopedCount(t, db, &model.AgentGroup{}, "user_id = ?", controlID, 1)
	assertUnscopedCount(t, db, &model.AgentGroupRun{}, "user_id = ?", controlID, 1)
	assertUnscopedCount(t, db, &model.AgentGroupStep{}, "group_run_id = ?", groupRuns[1].ID, 1)
	assertUnscopedCount(t, db, &model.AgentGroupStepAttempt{}, "step_id = ?", steps[1].ID, 1)
	assertUnscopedCount(t, db, &model.AgentGroupMember{}, "group_id = ?", groups[1].ID, 1)
	assertUnscopedCount(t, db, &model.ConversationRole{}, "user_id = ?", controlID, 1)
	assertUnscopedCount(t, db, &model.ArtifactShare{}, "user_id = ?", controlID, 1)
	assertUnscopedCount(t, db, &model.ConversationShare{}, "user_id = ?", controlID, 1)
	assertUnscopedCount(t, db, &model.Credential{}, "user_id = ?", controlID, 1)
	assertUnscopedCount(t, db, &model.DocCard{}, "user_id = ?", controlID, 1)
	assertUnscopedCount(t, db, &model.DynamicPrompt{}, "user_id = ?", controlID, 1)
	assertUnscopedCount(t, db, &model.PromptPreset{}, "owner_user_id = ?", controlID, 1)
	assertUnscopedCount(t, db, &model.PromptPreset{}, "scope = ?", "builtin", 1)
	assertUnscopedCount(t, db, &model.Skill{}, "owner_user_id = ?", controlID, 1)
	assertUnscopedCount(t, db, &model.Attachment{}, "user_id = ?", controlID, 1)
	assertUnscopedCount(t, db, &model.AnnouncementUserState{}, "user_id = ?", controlID, 1)

	assertUnscopedCount(t, db, &model.PaymentOrder{}, "user_id = ?", targetID, 1)
	assertUnscopedCount(t, db, &model.BalanceTransaction{}, "user_id = ?", targetID, 1)
	assertUnscopedCount(t, db, &model.UsageReservation{}, "user_id = ?", targetID, 1)
	assertUnscopedCount(t, db, &model.Redemption{}, "user_id = ?", targetID, 1)
	assertUnscopedCount(t, db, &model.UsageLedger{}, "user_id = ?", targetID, 1)
}

func assertUnscopedCount(t *testing.T, db *gorm.DB, item interface{}, query string, argument interface{}, want int64) {
	t.Helper()
	var count int64
	if err := db.Unscoped().Model(item).Where(query, argument).Count(&count).Error; err != nil {
		t.Fatalf("count %T: %v", item, err)
	}
	if count != want {
		t.Fatalf("count %T where %q = %d, want %d", item, query, count, want)
	}
}

func TestDeleteAccountHardPreservesObjectsReferencedByOtherTenantStatusDeletedRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:delete_user_status_deleted_refs?mode=memory&cache=shared"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err = db.AutoMigrate(schema.Models()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	users := []model.User{
		{PublicID: "u_status_deleted_target", Username: "status-deleted-target", Email: "status-deleted-target@example.com", Role: "user", Status: "active"},
		{PublicID: "u_status_deleted_control", Username: "status-deleted-control", Email: "status-deleted-control@example.com", Role: "user", Status: "active"},
	}
	if err = db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	targetID, controlID := users[0].ID, users[1].ID

	// 目标专属路径（应被清理）与跨租户共享路径（必须保护）。
	sharedMain := "objects/status-deleted-shared.bin"
	sharedExtract := ".extracts/status-deleted-shared.txt"
	files := []model.FileObject{
		{FileID: "file_status_deleted_target_exclusive", UserID: targetID, StoragePath: "objects/status-deleted-exclusive.bin", ExtractStoragePath: ".extracts/status-deleted-exclusive.txt", Status: "active"},
		{FileID: "file_status_deleted_target_shared", UserID: targetID, StoragePath: sharedMain, ExtractStoragePath: sharedExtract, Status: "active"},
		// 另一租户的 status=deleted 行仍保留记录并引用同一对象：物理删除必须被阻止。
		{FileID: "file_status_deleted_control_main", UserID: controlID, StoragePath: sharedMain, Status: "deleted"},
		{FileID: "file_status_deleted_control_extract", UserID: controlID, StoragePath: "objects/control-exclusive.bin", ExtractStoragePath: sharedExtract, Status: "deleted"},
	}
	if err = db.Create(&files).Error; err != nil {
		t.Fatalf("seed file objects: %v", err)
	}

	storagePaths, err := NewRepo(db).DeleteAccountHardWithStoragePaths(context.Background(), targetID)
	if err != nil {
		t.Fatalf("DeleteAccountHardWithStoragePaths() error = %v", err)
	}
	want := []string{".extracts/status-deleted-exclusive.txt", "objects/status-deleted-exclusive.bin"}
	if !reflect.DeepEqual(storagePaths, want) {
		t.Fatalf("storage paths = %v, want %v", storagePaths, want)
	}
	assertUnscopedCount(t, db, &model.FileObject{}, "user_id = ?", controlID, 2)
}
