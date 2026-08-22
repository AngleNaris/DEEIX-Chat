package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCloneActiveFileObjectAndConsumeQuotaCopiesStorageArtifactsAtomically(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.User{}, &model.UserStorageQuota{}); err != nil {
		t.Fatalf("migrate users and quota: %v", err)
	}
	users := []model.User{
		{PublicID: "clone_source_user", Username: "clone-source", Role: "user", Status: "active"},
		{PublicID: "clone_target_user", Username: "clone-target", Role: "user", Status: "active"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	source := model.FileObject{
		FileID:                "file_source",
		UserID:                users[0].ID,
		FileName:              "source.pdf",
		SizeBytes:             128,
		StoragePath:           "objects/source.pdf",
		Status:                "active",
		ProcessingStatus:      "ready",
		ProcessingReady:       true,
		ExtractStatus:         "ready",
		ExtractStoragePath:    ".extracts/source.txt",
		ProcessingPayloadJSON: `{"processor":"test"}`,
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("seed source file: %v", err)
	}

	repo := NewRepo(db)
	clonedSource, target, err := repo.CloneActiveFileObjectAndConsumeQuota(
		context.Background(),
		users[0].ID,
		"file_source",
		users[1].ID,
		"file_target",
		1024,
	)
	if err != nil {
		t.Fatalf("CloneActiveFileObjectAndConsumeQuota() error = %v", err)
	}
	if clonedSource.ID != source.ID || target.ID == 0 || target.UserID != users[1].ID || target.FileID != "file_target" {
		t.Fatalf("source/target identity mismatch: source=%+v target=%+v", clonedSource, target)
	}
	if target.StoragePath != source.StoragePath || target.ExtractStoragePath != source.ExtractStoragePath {
		t.Fatalf("target storage paths = %q / %q, want %q / %q", target.StoragePath, target.ExtractStoragePath, source.StoragePath, source.ExtractStoragePath)
	}
	if target.ProcessingStatus != source.ProcessingStatus || target.ProcessingPayloadJSON != source.ProcessingPayloadJSON {
		t.Fatalf("target processing state was not copied: %+v", target)
	}
	if target.EmbedStatus != "none" || target.ChunkCount != 0 {
		t.Fatalf("target embedding state = %q / %d, want none / 0", target.EmbedStatus, target.ChunkCount)
	}

	var quota model.UserStorageQuota
	if err = db.Where("user_id = ?", users[1].ID).First(&quota).Error; err != nil {
		t.Fatalf("load target quota: %v", err)
	}
	if quota.UsedBytes != source.SizeBytes {
		t.Fatalf("target quota used bytes = %d, want %d", quota.UsedBytes, source.SizeBytes)
	}
}

func TestCloneActiveFileObjectAndConsumeQuotaRejectsDeletedSource(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.User{}, &model.UserStorageQuota{}); err != nil {
		t.Fatalf("migrate users and quota: %v", err)
	}
	users := []model.User{
		{PublicID: "deleted_clone_source_user", Username: "deleted-clone-source", Role: "user", Status: "active"},
		{PublicID: "deleted_clone_target_user", Username: "deleted-clone-target", Role: "user", Status: "active"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	source := model.FileObject{FileID: "file_deleted_source", UserID: users[0].ID, SizeBytes: 128, StoragePath: "objects/deleted.pdf", Status: "deleted"}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("seed source file: %v", err)
	}

	_, _, err := NewRepo(db).CloneActiveFileObjectAndConsumeQuota(
		context.Background(),
		users[0].ID,
		"file_deleted_source",
		users[1].ID,
		"file_target",
		1024,
	)
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("CloneActiveFileObjectAndConsumeQuota() error = %v, want ErrFileNotFound", err)
	}
	var targetCount int64
	if countErr := db.Model(&model.FileObject{}).Where("user_id = ?", users[1].ID).Count(&targetCount).Error; countErr != nil {
		t.Fatalf("count target files: %v", countErr)
	}
	if targetCount != 0 {
		t.Fatalf("target file count = %d, want 0", targetCount)
	}
}

func TestReplaceFileObjectContentReturnsReferenceSafeCleanupDecisions(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	users := []model.User{
		{PublicID: "replace_owner", Username: "replace-owner", Role: "user", Status: "active"},
		{PublicID: "replace_control", Username: "replace-control", Role: "user", Status: "active"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	files := []model.FileObject{
		{
			FileID:             "file_replace_target",
			UserID:             users[0].ID,
			StoragePath:        "objects/shared-old.txt",
			ExtractStoragePath: ".extracts/exclusive-old.txt",
			Status:             "active",
			ProcessingStatus:   "ready",
			ProcessingReady:    true,
			ExtractStatus:      "ready",
			EmbedStatus:        "ready",
			ChunkCount:         3,
		},
		{FileID: "file_replace_shared", UserID: users[1].ID, StoragePath: "objects/shared-old.txt", Status: "active"},
		{FileID: "file_replace_attachment", UserID: users[0].ID, StoragePath: "objects/attachment-old.txt", Status: "active"},
		// 软删除行仍保留记录，必须继续保护其引用的对象（跨租户引用语义）。
		{FileID: "file_replace_soft_deleted_shared", UserID: users[1].ID, StoragePath: "objects/soft-deleted-shared.txt", Status: "deleted"},
	}
	if err := db.Create(&files).Error; err != nil {
		t.Fatalf("seed files: %v", err)
	}
	if err := db.Create(&model.Attachment{
		UserID:      users[1].ID,
		FileID:      "file_attachment_snapshot",
		StoragePath: "objects/attachment-old.txt",
		Status:      "active",
	}).Error; err != nil {
		t.Fatalf("seed attachment: %v", err)
	}

	repo := NewRepo(db)
	cleanup, err := repo.ReplaceFileObjectContent(
		context.Background(),
		users[0].ID,
		files[0].FileID,
		"objects/replaced.txt",
		"sha-replaced",
		42,
	)
	if err != nil {
		t.Fatalf("ReplaceFileObjectContent() error = %v", err)
	}
	if cleanup.OldStoragePath != "objects/shared-old.txt" || cleanup.RemoveOldStorageObject {
		t.Fatalf("old shared storage cleanup = %+v, want retained", cleanup)
	}
	if cleanup.OldExtractStoragePath != ".extracts/exclusive-old.txt" || !cleanup.RemoveOldExtractObject {
		t.Fatalf("old extract cleanup = %+v, want removable", cleanup)
	}

	softDeletedFile := model.FileObject{
		FileID:      "file_replace_soft_deleted_target",
		UserID:      users[0].ID,
		StoragePath: "objects/soft-deleted-shared.txt",
		Status:      "active",
	}
	if err := db.Create(&softDeletedFile).Error; err != nil {
		t.Fatalf("seed soft-shared target: %v", err)
	}
	softCleanup, err := repo.ReplaceFileObjectContent(
		context.Background(),
		users[0].ID,
		softDeletedFile.FileID,
		"objects/soft-deleted-replaced.txt",
		"sha-soft-replaced",
		9,
	)
	if err != nil {
		t.Fatalf("ReplaceFileObjectContent() soft-shared error = %v", err)
	}
	if softCleanup.RemoveOldStorageObject {
		t.Fatalf("soft-deleted tenant row did not pin old storage: %+v", softCleanup)
	}

	var updated model.FileObject
	if err = db.Where("id = ?", files[0].ID).First(&updated).Error; err != nil {
		t.Fatalf("load updated file: %v", err)
	}
	if updated.StoragePath != "objects/replaced.txt" || updated.SHA256 != "sha-replaced" || updated.SizeBytes != 42 {
		t.Fatalf("updated content metadata = %+v", updated)
	}
	if updated.ExtractStoragePath != "" || updated.ProcessingStatus != "pending" || updated.ProcessingReady || updated.EmbedStatus != "none" || updated.ChunkCount != 0 {
		t.Fatalf("updated processing state was not reset: %+v", updated)
	}

	pendingCutoff := updated.UpdatedAt.Add(-time.Second)
	queuedCutoff := updated.UpdatedAt.Add(-time.Second)
	extractingCutoff := updated.UpdatedAt.Add(-time.Second)
	claimed, err := repo.ClaimRecoverableFilesForProcessing(
		context.Background(), pendingCutoff, queuedCutoff, extractingCutoff, 10,
	)
	if err != nil {
		t.Fatalf("claim fresh pending files: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("fresh pending file was recovered before debounce: %+v", claimed)
	}

	claimed, err = repo.ClaimRecoverableFilesForProcessing(
		context.Background(), updated.UpdatedAt.Add(time.Second), queuedCutoff, extractingCutoff, 10,
	)
	if err != nil {
		t.Fatalf("claim stale pending files: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed pending file count = %d, want 2: %+v", len(claimed), claimed)
	}
	var claimedTarget *domainconversation.FileObject
	for i := range claimed {
		if claimed[i].FileID == files[0].FileID {
			claimedTarget = &claimed[i]
			break
		}
	}
	if claimedTarget == nil || claimedTarget.ProcessingStatus != "queued" {
		t.Fatalf("target pending file was not queued: %+v", claimed)
	}
	claimedAgain, err := repo.ClaimRecoverableFilesForProcessing(
		context.Background(), time.Now().Add(time.Hour), time.Now().Add(-time.Hour), time.Now().Add(-time.Hour), 10,
	)
	if err != nil {
		t.Fatalf("claim queued files again: %v", err)
	}
	if len(claimedAgain) != 0 {
		t.Fatalf("fresh queued file was claimed twice: %+v", claimedAgain)
	}

	executing, err := repo.ClaimFileProcessingExecution(
		context.Background(), users[0].ID, files[0].FileID, "objects/replaced.txt", "test-v1",
		updated.UpdatedAt.Add(time.Second),
	)
	if err != nil || !executing {
		t.Fatalf("claim processing execution: claimed=%v err=%v", executing, err)
	}
	executingAgain, err := repo.ClaimFileProcessingExecution(
		context.Background(), users[0].ID, files[0].FileID, "objects/replaced.txt", "test-v1",
		time.Now(),
	)
	if err != nil || executingAgain {
		t.Fatalf("duplicate processing execution claim: claimed=%v err=%v", executingAgain, err)
	}

	var extractingRow model.FileObject
	if err = db.Where("id = ?", files[0].ID).First(&extractingRow).Error; err != nil {
		t.Fatalf("load extracting row: %v", err)
	}
	staleToken := extractingRow.ProcessingStartedAt.Add(-time.Minute)
	failureStatus := "failed"
	failureReady := false
	if err = repo.UpdateFileObjectProcessing(context.Background(), users[0].ID, files[0].FileID, repository.UpdateFileObjectProcessingInput{
		ExpectedStoragePath:         "objects/replaced.txt",
		ExpectedProcessingStartedAt: &staleToken,
		ProcessingStatus:            &failureStatus,
		ProcessingReady:             &failureReady,
	}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale worker commit was accepted: err=%v", err)
	}

	attachmentCleanup, err := repo.ReplaceFileObjectContent(
		context.Background(),
		users[0].ID,
		files[2].FileID,
		"objects/attachment-replaced.txt",
		"sha-attachment-replaced",
		21,
	)
	if err != nil {
		t.Fatalf("ReplaceFileObjectContent() attachment error = %v", err)
	}
	if attachmentCleanup.RemoveOldStorageObject {
		t.Fatalf("attachment-pinned old storage marked removable: %+v", attachmentCleanup)
	}
}

func TestCanRemoveExtractStoragePathHonorsRetainedReferences(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	users := []model.User{
		{PublicID: "extract_owner", Username: "extract-owner", Role: "user", Status: "active"},
		{PublicID: "extract_clone", Username: "extract-clone", Role: "user", Status: "active"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	exclusive := model.FileObject{
		FileID:             "file_extract_exclusive",
		UserID:             users[0].ID,
		StoragePath:        "objects/exclusive.pdf",
		ExtractStoragePath: ".extracts/uid_1/exclusive.txt",
		Status:             "active",
	}
	cloneShared := model.FileObject{
		FileID:             "file_extract_clone",
		UserID:             users[1].ID,
		StoragePath:        "objects/shared.pdf",
		ExtractStoragePath: ".extracts/uid_2/shared.txt",
		Status:             "active",
	}
	softShared := model.FileObject{
		FileID:             "file_extract_soft",
		UserID:             users[1].ID,
		StoragePath:        "objects/soft.pdf",
		ExtractStoragePath: ".extracts/uid_2/soft.txt",
		Status:             "deleted",
	}
	if err := db.Create(&exclusive).Error; err != nil {
		t.Fatalf("seed exclusive file: %v", err)
	}
	if err := db.Create(&cloneShared).Error; err != nil {
		t.Fatalf("seed clone file: %v", err)
	}
	if err := db.Create(&softShared).Error; err != nil {
		t.Fatalf("seed soft-deleted file: %v", err)
	}

	repo := NewRepo(db)
	removable, err := repo.CanRemoveExtractStoragePath(context.Background(), exclusive.ID, users[0].ID, exclusive.ExtractStoragePath)
	if err != nil {
		t.Fatalf("CanRemoveExtractStoragePath() exclusive error = %v", err)
	}
	if !removable {
		t.Fatal("exclusive extract should be removable")
	}

	removable, err = repo.CanRemoveExtractStoragePath(context.Background(), exclusive.ID, users[0].ID, cloneShared.ExtractStoragePath)
	if err != nil {
		t.Fatalf("CanRemoveExtractStoragePath() clone error = %v", err)
	}
	if removable {
		t.Fatal("extract still referenced by another active file must not be removed")
	}

	removable, err = repo.CanRemoveExtractStoragePath(context.Background(), exclusive.ID, users[0].ID, softShared.ExtractStoragePath)
	if err != nil {
		t.Fatalf("CanRemoveExtractStoragePath() soft error = %v", err)
	}
	if removable {
		t.Fatal("extract still referenced by a soft-deleted file must not be removed")
	}

	if _, err := repo.CanRemoveExtractStoragePath(context.Background(), exclusive.ID, users[0].ID, " "); err != nil {
		t.Fatalf("CanRemoveExtractStoragePath() empty error = %v", err)
	}
}

func TestReplaceFileObjectContentRejectsMissingOwnerWithoutMutation(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	orphan := model.FileObject{
		FileID:      "file_orphan_replace",
		UserID:      404,
		StoragePath: "objects/orphan-old.txt",
		SHA256:      "sha-old",
		SizeBytes:   12,
		Status:      "active",
	}
	if err := db.Create(&orphan).Error; err != nil {
		t.Fatalf("seed orphan file: %v", err)
	}

	_, err := NewRepo(db).ReplaceFileObjectContent(
		context.Background(),
		orphan.UserID,
		orphan.FileID,
		"objects/orphan-new.txt",
		"sha-new",
		24,
	)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("ReplaceFileObjectContent() error = %v, want ErrNotFound", err)
	}
	var unchanged model.FileObject
	if loadErr := db.Where("id = ?", orphan.ID).First(&unchanged).Error; loadErr != nil {
		t.Fatalf("load orphan file: %v", loadErr)
	}
	if unchanged.StoragePath != orphan.StoragePath || unchanged.SHA256 != orphan.SHA256 || unchanged.SizeBytes != orphan.SizeBytes {
		t.Fatalf("orphan file was mutated: %+v", unchanged)
	}
}

func TestStaleFileVersionCannotRestoreProcessingOrEmbeddingState(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	file := model.FileObject{
		FileID:           "file_version_guard",
		UserID:           71,
		StoragePath:      "objects/current-version.txt",
		Status:           "active",
		ProcessingStatus: "pending",
		EmbedStatus:      "none",
		ChunkCount:       1,
	}
	if err := db.Create(&file).Error; err != nil {
		t.Fatalf("seed versioned file: %v", err)
	}
	existingChunk := model.FileChunk{
		FileObjID:  file.ID,
		UserID:     file.UserID,
		ChunkIndex: 0,
		Content:    "current chunk",
		TokenCount: 2,
	}
	if err := db.Create(&existingChunk).Error; err != nil {
		t.Fatalf("seed current chunk: %v", err)
	}

	repo := NewRepo(db)
	ctx := context.Background()
	stalePath := "objects/stale-version.txt"
	ready := "ready"
	conflictUpdates := map[string]func() error{
		"processing state": func() error {
			return repo.UpdateFileObjectProcessingState(ctx, &domainconversation.FileObjectProcessing{
				FileObjectID:        file.ID,
				UserID:              file.UserID,
				ExpectedStoragePath: stalePath,
				ProcessingStatus:    ready,
			})
		},
		"processing fields": func() error {
			return repo.UpdateFileObjectProcessing(ctx, file.UserID, file.FileID, repository.UpdateFileObjectProcessingInput{
				ExpectedStoragePath: stalePath,
				ProcessingStatus:    &ready,
			})
		},
	}
	for name, update := range conflictUpdates {
		t.Run(name, func(t *testing.T) {
			if err := update(); !errors.Is(err, repository.ErrConflict) {
				t.Fatalf("stale update error = %v, want ErrConflict", err)
			}
		})
	}

	publishUpdates := map[string]func() (bool, error){
		"embedding status": func() (bool, error) {
			return repo.UpdateFileObjectEmbedStatus(ctx, file.UserID, file.FileID, stalePath, ready, "")
		},
		"chunk count": func() (bool, error) {
			return repo.UpdateFileObjectChunkCount(ctx, file.ID, stalePath, 99)
		},
		"chunks": func() (bool, error) {
			return repo.ReplaceFileChunks(ctx, file.ID, stalePath, []domainconversation.FileChunk{{
				FileObjID:  file.ID,
				UserID:     file.UserID,
				ChunkIndex: 0,
				Content:    "stale chunk",
				TokenCount: 2,
			}}, [][]float32{{1, 0, 0}})
		},
	}
	for name, update := range publishUpdates {
		t.Run(name, func(t *testing.T) {
			updated, err := update()
			if err != nil {
				t.Fatalf("stale publish error = %v, want nil", err)
			}
			if updated {
				t.Fatal("stale publish unexpectedly updated the current file version")
			}
		})
	}

	var unchanged model.FileObject
	if err := db.Where("id = ?", file.ID).First(&unchanged).Error; err != nil {
		t.Fatalf("load current file: %v", err)
	}
	if unchanged.ProcessingStatus != "pending" || unchanged.EmbedStatus != "none" || unchanged.ChunkCount != 1 {
		t.Fatalf("stale task mutated current file state: %+v", unchanged)
	}
	var chunks []model.FileChunk
	if err := db.Where("file_obj_id = ?", file.ID).Find(&chunks).Error; err != nil {
		t.Fatalf("load current chunks: %v", err)
	}
	if len(chunks) != 1 || chunks[0].Content != "current chunk" {
		t.Fatalf("stale task replaced current chunks: %+v", chunks)
	}
}

func TestCloneFileObjectProcessingStateUsesTargetContentVersion(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	files := []model.FileObject{
		{
			FileID:           "file_processing_source",
			UserID:           81,
			StoragePath:      "objects/source-version.txt",
			Status:           "active",
			ProcessingStatus: "ready",
			ExtractStatus:    "ready",
		},
		{
			FileID:           "file_processing_target",
			UserID:           82,
			StoragePath:      "objects/target-version.txt",
			Status:           "active",
			ProcessingStatus: "pending",
		},
	}
	if err := db.Create(&files).Error; err != nil {
		t.Fatalf("seed processing files: %v", err)
	}

	if err := NewRepo(db).CloneFileObjectProcessingState(context.Background(), files[0].ID, files[1].ID, files[1].UserID); err != nil {
		t.Fatalf("CloneFileObjectProcessingState() error = %v", err)
	}
	var target model.FileObject
	if err := db.Where("id = ?", files[1].ID).First(&target).Error; err != nil {
		t.Fatalf("load target file: %v", err)
	}
	if target.ProcessingStatus != "ready" || target.ExtractStatus != "ready" || target.StoragePath != files[1].StoragePath {
		t.Fatalf("target processing clone = %+v", target)
	}
}

func TestAttachmentWritesRejectBlankFileIDWithoutPartialMutation(t *testing.T) {
	tests := map[string]func(*Repo, *gorm.DB, model.User, model.Conversation, model.Message, model.Message) error{
		"create attachments": func(repo *Repo, _ *gorm.DB, user model.User, conversation model.Conversation, _ model.Message, assistant model.Message) error {
			return repo.CreateAttachments(context.Background(), []domainconversation.Attachment{{
				ConversationID: conversation.ID,
				MessageID:      assistant.ID,
				UserID:         user.ID,
				StoragePath:    "objects/unvalidated.txt",
				Status:         "active",
			}})
		},
		"create message pair": func(repo *Repo, _ *gorm.DB, user model.User, conversation model.Conversation, _ model.Message, _ model.Message) error {
			userMessage := &domainconversation.Message{ConversationID: conversation.ID, UserID: user.ID, PublicID: "blank_pair_user", Role: "user", Status: "pending"}
			assistantMessage := &domainconversation.Message{ConversationID: conversation.ID, UserID: user.ID, PublicID: "blank_pair_assistant", Role: "assistant", Status: "pending"}
			return repo.CreateMessagePairWithUserAttachments(context.Background(), userMessage, assistantMessage, []domainconversation.Attachment{{
				UserID:      user.ID,
				StoragePath: "objects/unvalidated.txt",
				Status:      "active",
			}})
		},
		"complete assistant": func(repo *Repo, _ *gorm.DB, user model.User, conversation model.Conversation, parent model.Message, assistant model.Message) error {
			return repo.CompleteAssistantMessageWithAttachments(
				context.Background(),
				parent.ID,
				repository.MessageUsageUpdate{},
				assistant.ID,
				repository.AssistantMessageCompletionUpdate{Content: "must rollback", Status: "success"},
				[]domainconversation.Attachment{{
					ConversationID: conversation.ID,
					UserID:         user.ID,
					StoragePath:    "objects/unvalidated.txt",
					Status:         "active",
				}},
			)
		},
		"complete generated assistant": func(repo *Repo, _ *gorm.DB, user model.User, conversation model.Conversation, _ model.Message, assistant model.Message) error {
			return repo.CompleteAssistantMessageWithGeneratedAttachments(
				context.Background(),
				assistant.ID,
				repository.AssistantMessageCompletionUpdate{Content: "must rollback", Status: "success"},
				[]domainconversation.Attachment{{
					ConversationID: conversation.ID,
					UserID:         user.ID,
					StoragePath:    "objects/unvalidated.txt",
					Status:         "active",
				}},
			)
		},
	}

	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			db := openConversationRepositoryTestDB(t)
			if err := db.AutoMigrate(&model.User{}); err != nil {
				t.Fatalf("migrate users: %v", err)
			}
			user := model.User{PublicID: "blank_attachment_owner", Username: "blank-attachment-owner", Role: "user", Status: "active"}
			if err := db.Create(&user).Error; err != nil {
				t.Fatalf("seed user: %v", err)
			}
			conversation := model.Conversation{UserID: user.ID, PublicID: "conversation_blank_attachment", SessionKey: "session_blank_attachment"}
			if err := db.Create(&conversation).Error; err != nil {
				t.Fatalf("seed conversation: %v", err)
			}
			messages := []model.Message{
				{ConversationID: conversation.ID, UserID: user.ID, PublicID: "blank_parent", Role: "user", Status: "pending"},
				{ConversationID: conversation.ID, UserID: user.ID, PublicID: "blank_assistant", Role: "assistant", Status: "pending"},
			}
			if err := db.Create(&messages).Error; err != nil {
				t.Fatalf("seed messages: %v", err)
			}

			if err := run(NewRepo(db), db, user, conversation, messages[0], messages[1]); !errors.Is(err, repository.ErrInvalidInput) {
				t.Fatalf("attachment write error = %v, want ErrInvalidInput", err)
			}
			var attachmentCount int64
			if err := db.Model(&model.Attachment{}).Count(&attachmentCount).Error; err != nil {
				t.Fatalf("count attachments: %v", err)
			}
			if attachmentCount != 0 {
				t.Fatalf("attachment count = %d, want 0", attachmentCount)
			}
			var storedAssistant model.Message
			if err := db.Where("id = ?", messages[1].ID).First(&storedAssistant).Error; err != nil {
				t.Fatalf("load assistant: %v", err)
			}
			if storedAssistant.Status != "pending" || storedAssistant.Content != "" {
				t.Fatalf("assistant was partially completed: %+v", storedAssistant)
			}
			var seededMessageCount int64
			if err := db.Model(&model.Message{}).Where("conversation_id = ?", conversation.ID).Count(&seededMessageCount).Error; err != nil {
				t.Fatalf("count messages: %v", err)
			}
			if seededMessageCount != 2 {
				t.Fatalf("message count = %d, want only two seeded rows", seededMessageCount)
			}
		})
	}
}

func TestCreateMessagePairRejectsStaleAttachmentStorageSnapshot(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	user := model.User{PublicID: "stale_attachment_owner", Username: "stale-attachment-owner", Role: "user", Status: "active"}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	conversation := model.Conversation{UserID: user.ID, PublicID: "conversation_stale_attachment", SessionKey: "session_stale_attachment"}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	file := model.FileObject{FileID: "file_stale_attachment", UserID: user.ID, StoragePath: "objects/current.txt", Status: "active"}
	if err := db.Create(&file).Error; err != nil {
		t.Fatalf("seed file: %v", err)
	}

	userMessage := &domainconversation.Message{ConversationID: conversation.ID, UserID: user.ID, PublicID: "message_stale_user", Role: "user", Status: "pending"}
	assistantMessage := &domainconversation.Message{ConversationID: conversation.ID, UserID: user.ID, PublicID: "message_stale_assistant", Role: "assistant", Status: "pending"}
	err := NewRepo(db).CreateMessagePairWithUserAttachments(
		context.Background(),
		userMessage,
		assistantMessage,
		[]domainconversation.Attachment{{
			UserID:      user.ID,
			FileID:      file.FileID,
			StoragePath: "objects/stale.txt",
			Status:      "active",
		}},
	)
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("CreateMessagePairWithUserAttachments() error = %v, want ErrConflict", err)
	}
	var messageCount int64
	if countErr := db.Model(&model.Message{}).Where("conversation_id = ?", conversation.ID).Count(&messageCount).Error; countErr != nil {
		t.Fatalf("count messages: %v", countErr)
	}
	if messageCount != 0 {
		t.Fatalf("message count = %d, want transaction rollback", messageCount)
	}
}

func TestTranslateErrorAllowsNil(t *testing.T) {
	if err := translateError(nil); err != nil {
		t.Fatalf("translateError(nil) = %v, want nil", err)
	}
}

func TestAttachmentDurationSecondsFromMetaJSON(t *testing.T) {
	if got := attachmentDurationSecondsFromMetaJSON(`{"duration_seconds":6}`); got != 6 {
		t.Fatalf("expected attachment duration 6, got %d", got)
	}
	for _, raw := range []string{"", `{}`, `{"duration_seconds":0}`, `{"duration_seconds":"6"}`} {
		if got := attachmentDurationSecondsFromMetaJSON(raw); got != 0 {
			t.Fatalf("expected invalid attachment duration for %q, got %d", raw, got)
		}
	}
}

func TestCreateContextArtifactsRejectsIncompleteOwnerScope(t *testing.T) {
	repo := NewRepo(openConversationRepositoryTestDB(t))
	valid := domainconversation.ContextArtifact{
		ConversationID: 7,
		MessageID:      11,
		UserID:         1,
		RunID:          "run_1",
		Kind:           domainconversation.ContextArtifactToolResult,
		Content:        "evidence",
	}
	tests := map[string]func(*domainconversation.ContextArtifact){
		"conversation": func(item *domainconversation.ContextArtifact) { item.ConversationID = 0 },
		"message":      func(item *domainconversation.ContextArtifact) { item.MessageID = 0 },
		"user":         func(item *domainconversation.ContextArtifact) { item.UserID = 0 },
		"run":          func(item *domainconversation.ContextArtifact) { item.RunID = "  " },
	}
	for name, invalidate := range tests {
		t.Run(name, func(t *testing.T) {
			item := valid
			invalidate(&item)
			err := repo.CreateContextArtifacts(context.Background(), []domainconversation.ContextArtifact{item})
			if !errors.Is(err, repository.ErrInvalidInput) {
				t.Fatalf("CreateContextArtifacts() error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestCreateContextArtifactsNormalizesRunOwner(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.ChatContextRecord{}); err != nil {
		t.Fatalf("migrate context records: %v", err)
	}
	repo := NewRepo(db)
	items := []domainconversation.ContextArtifact{{
		ConversationID: 7,
		MessageID:      11,
		UserID:         1,
		RunID:          "  run_1  ",
		Kind:           domainconversation.ContextArtifactToolResult,
		Content:        "normalized evidence",
	}}

	if err := repo.CreateContextArtifacts(context.Background(), items); err != nil {
		t.Fatalf("CreateContextArtifacts() error = %v", err)
	}
	if items[0].RunID != "run_1" {
		t.Fatalf("artifact run id = %q, want normalized run_1", items[0].RunID)
	}
}

func TestListRecentContextArtifactsFiltersBranchBeforeLimit(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.Message{}, &model.ChatContextRecord{}); err != nil {
		t.Fatalf("migrate context records: %v", err)
	}
	repo := NewRepo(db)
	ctx := context.Background()
	rootMessageID := uint(1)
	activeOwnerID := uint(10)
	leafMessageID := uint(12)
	branchMessages := []model.Message{
		{
			BaseModel:      model.BaseModel{ID: rootMessageID},
			ConversationID: 7,
			UserID:         1,
			PublicID:       "msg_branch_root",
			Role:           "user",
			Status:         "success",
		},
		{
			BaseModel:       model.BaseModel{ID: activeOwnerID},
			ConversationID:  7,
			UserID:          1,
			PublicID:        "msg_artifact_owner",
			ParentMessageID: &rootMessageID,
			RunID:           "run_active",
			Role:            "assistant",
			Status:          "success",
		},
		{
			BaseModel:       model.BaseModel{ID: leafMessageID},
			ConversationID:  7,
			UserID:          1,
			PublicID:        "msg_branch_leaf",
			ParentMessageID: &activeOwnerID,
			Role:            "user",
			Status:          "pending",
		},
	}
	for index := 0; index < 31; index++ {
		branchMessages = append(branchMessages, model.Message{
			BaseModel:       model.BaseModel{ID: uint(100 + index)},
			ConversationID:  7,
			UserID:          1,
			PublicID:        fmt.Sprintf("msg_sibling_%d", index),
			ParentMessageID: &rootMessageID,
			Role:            "assistant",
			Status:          "success",
		})
	}
	if err := db.Create(&branchMessages).Error; err != nil {
		t.Fatalf("create branch messages: %v", err)
	}

	items := []model.ChatContextRecord{
		{
			RecordType:     chatContextRecordArtifact,
			ConversationID: 7,
			MessageID:      activeOwnerID,
			UserID:         1,
			RunID:          "run_active",
			Kind:           string(domainconversation.ContextArtifactToolResult),
			SourceType:     "tool_call",
			SourceID:       "active",
			Content:        "active branch evidence",
		},
		{
			RecordType:     chatContextRecordArtifact,
			ConversationID: 7,
			MessageID:      rootMessageID,
			UserID:         1,
			Kind:           string(domainconversation.ContextArtifactToolResult),
			SourceType:     "tool_call",
			SourceID:       "user-owned",
			Content:        "legacy evidence with ambiguous branch ownership",
		},
		{
			RecordType:     chatContextRecordArtifact,
			ConversationID: 7,
			MessageID:      activeOwnerID,
			UserID:         1,
			RunID:          "run_wrong_owner",
			Kind:           string(domainconversation.ContextArtifactToolResult),
			SourceType:     "tool_call",
			SourceID:       "mismatched-run",
			Content:        "evidence must not borrow an unrelated assistant owner",
		},
	}
	for index := 0; index < 31; index++ {
		items = append(items, model.ChatContextRecord{
			RecordType:     chatContextRecordArtifact,
			ConversationID: 7,
			MessageID:      uint(100 + index),
			UserID:         1,
			Kind:           string(domainconversation.ContextArtifactToolResult),
			SourceType:     "tool_call",
			SourceID:       fmt.Sprintf("sibling-%d", index),
			Content:        "sibling branch evidence",
		})
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatalf("create context records: %v", err)
	}

	artifacts, err := repo.ListRecentContextArtifacts(ctx, repository.ContextArtifactListFilter{
		Scope: repository.HistoricalMessageScope{
			ConversationID: 7,
			UserID:         1,
			LeafMessageID:  leafMessageID,
		},
		Kinds: []domainconversation.ContextArtifactKind{domainconversation.ContextArtifactToolResult},
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListRecentContextArtifacts() error = %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].MessageID != activeOwnerID {
		t.Fatalf("expected active branch evidence before limit, got %#v", artifacts)
	}
}

func TestConversationProjectDefaultsRoundTripAndDelete(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()
	knowledgeBases := []model.KnowledgeBase{
		{PublicID: "kb_default_one", Scope: "builtin", Name: "Default one", Enabled: true},
		{PublicID: "kb_default_two", Scope: "user", OwnerUserID: 1, Name: "Default two", Enabled: true},
	}
	if err := db.Create(&knowledgeBases).Error; err != nil {
		t.Fatalf("seed knowledge bases: %v", err)
	}
	project := domainconversation.ConversationProject{
		UserID:                  1,
		PublicID:                "project_defaults",
		Name:                    "Project defaults",
		MCPDefaultMode:          domainconversation.ConversationProjectMCPDefaultModeCustom,
		DefaultMCPToolIDs:       []uint{7, 3},
		DefaultSkillIDs:         []uint{11, 5},
		DefaultKnowledgeBaseIDs: []string{"kb_default_two", "kb_default_one"},
		Status:                  "active",
	}
	if err := repo.CreateConversationProject(ctx, &project); err != nil {
		t.Fatalf("CreateConversationProject() error = %v", err)
	}
	if !reflect.DeepEqual(project.DefaultMCPToolIDs, []uint{7, 3}) ||
		!reflect.DeepEqual(project.DefaultSkillIDs, []uint{11, 5}) ||
		!reflect.DeepEqual(project.DefaultKnowledgeBaseIDs, []string{"kb_default_two", "kb_default_one"}) {
		t.Fatalf("created defaults = MCP %v Skills %v KnowledgeBases %v", project.DefaultMCPToolIDs, project.DefaultSkillIDs, project.DefaultKnowledgeBaseIDs)
	}

	loaded, err := repo.GetConversationProjectByPublicID(ctx, 1, project.PublicID)
	if err != nil {
		t.Fatalf("GetConversationProjectByPublicID() error = %v", err)
	}
	if loaded.MCPDefaultMode != domainconversation.ConversationProjectMCPDefaultModeCustom ||
		!reflect.DeepEqual(loaded.DefaultMCPToolIDs, []uint{7, 3}) ||
		!reflect.DeepEqual(loaded.DefaultSkillIDs, []uint{11, 5}) ||
		!reflect.DeepEqual(loaded.DefaultKnowledgeBaseIDs, []string{"kb_default_two", "kb_default_one"}) {
		t.Fatalf("loaded project defaults = %#v", loaded)
	}
	badProject := domainconversation.ConversationProject{
		UserID:                  1,
		PublicID:                "project_other_user_base",
		Name:                    "Invalid project defaults",
		DefaultKnowledgeBaseIDs: []string{"kb_other_user"},
		Status:                  "active",
	}
	if err = db.Create(&model.KnowledgeBase{PublicID: "kb_other_user", Scope: "user", OwnerUserID: 2, Name: "Other user", Enabled: true}).Error; err != nil {
		t.Fatalf("seed other user's knowledge base: %v", err)
	}
	if err = repo.CreateConversationProject(ctx, &badProject); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("CreateConversationProject(other user's knowledge base) error = %v, want ErrNotFound", err)
	}
	var badProjectCount int64
	if err = db.Model(&model.ConversationProject{}).Where("public_id = ?", badProject.PublicID).Count(&badProjectCount).Error; err != nil {
		t.Fatalf("count rolled back project: %v", err)
	}
	if badProjectCount != 0 {
		t.Fatalf("rolled back project count = %d, want 0", badProjectCount)
	}

	nextMCPToolIDs := []uint{}
	nextSkillIDs := []uint{5}
	nextKnowledgeBaseIDs := []string{"kb_default_one"}
	inheritMode := domainconversation.ConversationProjectMCPDefaultModeInherit
	updated, err := repo.UpdateConversationProjectMetadataByPublicID(ctx, 1, project.PublicID, domainconversation.ConversationProjectPatch{
		MCPDefaultMode:          &inheritMode,
		DefaultMCPToolIDs:       &nextMCPToolIDs,
		DefaultSkillIDs:         &nextSkillIDs,
		DefaultKnowledgeBaseIDs: &nextKnowledgeBaseIDs,
	})
	if err != nil {
		t.Fatalf("UpdateConversationProjectMetadataByPublicID() error = %v", err)
	}
	if updated.MCPDefaultMode != inheritMode || len(updated.DefaultMCPToolIDs) != 0 ||
		!reflect.DeepEqual(updated.DefaultSkillIDs, nextSkillIDs) ||
		!reflect.DeepEqual(updated.DefaultKnowledgeBaseIDs, nextKnowledgeBaseIDs) {
		t.Fatalf("updated project defaults = %#v", updated)
	}

	if _, err = repo.DeleteConversationProjectByPublicID(ctx, 1, project.PublicID, false, false); err != nil {
		t.Fatalf("DeleteConversationProjectByPublicID() error = %v", err)
	}
	var associationCount int64
	if err = db.Model(&model.ConversationProjectMCPTool{}).Where("project_id = ?", project.ID).Count(&associationCount).Error; err != nil {
		t.Fatalf("count project MCP associations: %v", err)
	}
	if associationCount != 0 {
		t.Fatalf("project MCP association count = %d, want 0", associationCount)
	}
	if err = db.Model(&model.ConversationProjectSkill{}).Where("project_id = ?", project.ID).Count(&associationCount).Error; err != nil {
		t.Fatalf("count project Skill associations: %v", err)
	}
	if associationCount != 0 {
		t.Fatalf("project Skill association count = %d, want 0", associationCount)
	}
	if err = db.Model(&model.ConversationProjectKnowledgeBase{}).Where("project_id = ?", project.ID).Count(&associationCount).Error; err != nil {
		t.Fatalf("count project knowledge base associations: %v", err)
	}
	if associationCount != 0 {
		t.Fatalf("project knowledge base association count = %d, want 0", associationCount)
	}
}

func TestListConversationEventLogsHydratesRunRouteSnapshot(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()
	now := time.Now()

	run := model.ConversationRun{
		RunID:             "run_with_route",
		UserID:            1,
		ConversationID:    2,
		ProviderProtocol:  "openai_responses",
		UpstreamName:      "OpenAI Official",
		PlatformModelName: "gpt-5.5",
		RoutedBindingCode: "binding_openai",
		UpstreamModelName: "gpt-5.5-pro",
		Status:            "error",
		StartedAt:         now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create conversation run: %v", err)
	}

	events := []model.ChatRunEvent{
		{
			ConversationID: 2,
			UserID:         1,
			RunID:          run.RunID,
			EventScope:     "trace_event",
			EventID:        "event_with_route",
			EventType:      "error",
			Status:         "error",
			StartedAt:      now,
		},
		{
			ConversationID: 2,
			UserID:         1,
			RunID:          "run_before_route",
			EventScope:     "trace_event",
			EventID:        "event_without_route",
			EventType:      "error",
			Status:         "error",
			StartedAt:      now,
		},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("create conversation events: %v", err)
	}

	items, total, err := repo.ListConversationEventLogs(ctx, repository.ConversationEventLogListFilter{}, 0, 10)
	if err != nil {
		t.Fatalf("ListConversationEventLogs() error = %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("got total=%d len=%d, want 2", total, len(items))
	}
	itemsByRunID := make(map[string]domainconversation.EventLog, len(items))
	for _, item := range items {
		itemsByRunID[item.RunID] = item
	}
	withRoute := itemsByRunID[run.RunID]
	if withRoute.UpstreamName != run.UpstreamName ||
		withRoute.ProviderProtocol != run.ProviderProtocol ||
		withRoute.PlatformModelName != run.PlatformModelName ||
		withRoute.RoutedBindingCode != run.RoutedBindingCode ||
		withRoute.UpstreamModelName != run.UpstreamModelName {
		t.Fatalf("route snapshot = %#v, want run snapshot %#v", withRoute, run)
	}
	withoutRoute := itemsByRunID["run_before_route"]
	if withoutRoute.UpstreamName != "" || withoutRoute.ProviderProtocol != "" || withoutRoute.UpstreamModelName != "" {
		t.Fatalf("unexpected route snapshot for unmatched run: %#v", withoutRoute)
	}
}

func TestConversationEventLogListAndDetailBoundPayloads(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()
	now := time.Now()
	largePayload := strings.Repeat("x", maxConversationEventDetailPayloadBytes+1)
	events := []model.ChatRunEvent{
		{
			ConversationID:  1,
			UserID:          1,
			RunID:           "run_normal_payload",
			EventScope:      "trace_event",
			EventID:         "event_normal_payload",
			EventType:       "error",
			Status:          "error",
			ContentMarkdown: "request failed after upload",
			PayloadJSON:     `{"error":"上游不可用"}`,
			InputJSON:       `{"input":true}`,
			OutputJSON:      `{"output":true}`,
			ErrorJSON:       `{"code":"upstream_unavailable"}`,
			StartedAt:       now,
		},
		{
			ConversationID: 1,
			UserID:         1,
			RunID:          "run_large_payload",
			EventScope:     "trace_event",
			EventID:        "event_large_payload",
			EventType:      "error",
			Status:         "error",
			PayloadJSON:    largePayload,
			StartedAt:      now,
		},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("create conversation events: %v", err)
	}

	items, total, err := repo.ListConversationEventLogs(ctx, repository.ConversationEventLogListFilter{}, 0, 10)
	if err != nil {
		t.Fatalf("ListConversationEventLogs() error = %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("got total=%d len=%d, want 2", total, len(items))
	}
	itemsByRunID := make(map[string]domainconversation.EventLog, len(items))
	for _, item := range items {
		itemsByRunID[item.RunID] = item
		if item.ContentMarkdown != "" || item.PayloadJSON != "" || item.InputJSON != "" || item.OutputJSON != "" || item.ErrorJSON != "" {
			t.Fatalf("list item contains detail payloads: %#v", item)
		}
	}
	if got := itemsByRunID["run_normal_payload"].PayloadSizeBytes; got != int64(len(events[0].PayloadJSON)) {
		t.Fatalf("normal payload size = %d, want %d", got, len(events[0].PayloadJSON))
	}
	if itemsByRunID["run_normal_payload"].PayloadOmitted {
		t.Fatal("normal list payload should not be marked omitted")
	}
	if got := itemsByRunID["run_large_payload"].PayloadSizeBytes; got != int64(len(largePayload)) {
		t.Fatalf("large payload size = %d, want %d", got, len(largePayload))
	}
	if !itemsByRunID["run_large_payload"].PayloadOmitted {
		t.Fatal("large list payload should be marked omitted")
	}

	normalDetail, err := repo.GetConversationEventLog(ctx, events[0].ID)
	if err != nil {
		t.Fatalf("GetConversationEventLog(normal) error = %v", err)
	}
	if normalDetail.ContentMarkdown != events[0].ContentMarkdown ||
		normalDetail.PayloadJSON != events[0].PayloadJSON ||
		normalDetail.InputJSON != events[0].InputJSON ||
		normalDetail.OutputJSON != events[0].OutputJSON ||
		normalDetail.ErrorJSON != events[0].ErrorJSON ||
		normalDetail.PayloadOmitted {
		t.Fatalf("normal detail = %#v", normalDetail)
	}

	largeDetail, err := repo.GetConversationEventLog(ctx, events[1].ID)
	if err != nil {
		t.Fatalf("GetConversationEventLog(large) error = %v", err)
	}
	if largeDetail.PayloadJSON != "" || !largeDetail.PayloadOmitted {
		t.Fatalf("large detail should omit payload, got %#v", largeDetail)
	}
	if largeDetail.PayloadSizeBytes != int64(len(largePayload)) {
		t.Fatalf("large detail payload size = %d, want %d", largeDetail.PayloadSizeBytes, len(largePayload))
	}
}

func TestConversationMessageTraceReadsBoundPayloads(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()
	now := time.Now()
	largePayload := strings.Repeat("x", maxConversationEventDetailPayloadBytes+1)
	items := []model.ChatRunEvent{
		{
			MessageID:       11,
			RunID:           "run_trace_block_large",
			EventScope:      "trace_block",
			EventID:         "trace_block_large",
			EventType:       "process",
			ContentMarkdown: "处理失败",
			PayloadJSON:     largePayload,
			StartedAt:       now,
		},
		{
			MessageID:   11,
			RunID:       "run_trace_event_large",
			EventScope:  "trace_event",
			EventID:     "trace_event_large",
			EventType:   "error",
			PayloadJSON: largePayload,
			StartedAt:   now,
		},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatalf("create trace events: %v", err)
	}

	blocks, err := repo.ListConversationMessageTracesByMessageIDs(ctx, []uint{11})
	if err != nil {
		t.Fatalf("list message traces: %v", err)
	}
	if len(blocks) != 1 || blocks[0].PayloadJSON != "" || blocks[0].ContentMarkdown != "处理失败" {
		t.Fatalf("large trace block was not safely loaded: %#v", blocks)
	}

	events, err := repo.ListConversationMessageTraceEventsByMessageIDs(ctx, []uint{11})
	if err != nil {
		t.Fatalf("list trace events: %v", err)
	}
	if len(events) != 1 || events[0].PayloadJSON != "" {
		t.Fatalf("large trace event was not safely loaded: %#v", events)
	}
}

func TestListMessagesBeforeIDReturnsPreviousWindowAscending(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	conversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_before",
		Title:      "before window",
		LabelsJSON: "[]",
		SessionKey: "session_before",
		Status:     "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	messages := make([]model.Message, 0, 5)
	var parentID *uint
	for index := 1; index <= 5; index++ {
		message := model.Message{
			ConversationID:  conversation.ID,
			UserID:          1,
			PublicID:        fmt.Sprintf("msg_%d", index),
			ParentMessageID: parentID,
			Role:            "user",
			ContentType:     "text",
			Content:         fmt.Sprintf("message %d", index),
			BranchReason:    "default",
			Status:          "success",
		}
		if err := db.Create(&message).Error; err != nil {
			t.Fatalf("create message %d: %v", index, err)
		}
		messages = append(messages, message)
		nextParentID := message.ID
		parentID = &nextParentID
	}

	got, total, err := repo.ListMessagesBeforeID(ctx, conversation.ID, messages[4].ID, 2)
	if err != nil {
		t.Fatalf("ListMessagesBeforeID() error = %v", err)
	}
	if total != int64(len(messages)) {
		t.Fatalf("total = %d, want %d", total, len(messages))
	}
	if len(got) != 2 || got[0].PublicID != "msg_3" || got[1].PublicID != "msg_4" {
		t.Fatalf("unexpected previous window: %#v", got)
	}
	if got[1].ParentPublicID != "msg_3" {
		t.Fatalf("expected parent public id hydrated, got %q", got[1].ParentPublicID)
	}
}

func TestListMessageAncestorsUntilStopsAtBoundary(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	conversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_ancestors_until",
		Title:      "ancestors until",
		LabelsJSON: "[]",
		SessionKey: "session_ancestors_until",
		Status:     "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	messages := make([]model.Message, 0, 6)
	var parentID *uint
	for index := 1; index <= 6; index++ {
		message := model.Message{
			ConversationID:  conversation.ID,
			UserID:          1,
			PublicID:        fmt.Sprintf("msg_%d", index),
			ParentMessageID: parentID,
			Role:            "user",
			ContentType:     "text",
			Content:         fmt.Sprintf("message %d", index),
			BranchReason:    "default",
			Status:          "success",
		}
		if err := db.Create(&message).Error; err != nil {
			t.Fatalf("create message %d: %v", index, err)
		}
		messages = append(messages, message)
		nextParentID := message.ID
		parentID = &nextParentID
	}

	got, found, err := repo.ListMessageAncestorsUntil(ctx, conversation.ID, messages[5].ID, messages[2].ID, 10)
	if err != nil {
		t.Fatalf("ListMessageAncestorsUntil() error = %v", err)
	}
	if !found {
		t.Fatal("expected boundary to be found")
	}
	if len(got) != 4 {
		t.Fatalf("expected boundary through leaf, got %#v", got)
	}
	if got[0].PublicID != "msg_3" || got[len(got)-1].PublicID != "msg_6" {
		t.Fatalf("expected msg_3..msg_6, got %#v", got)
	}
	if got[0].ParentPublicID != "msg_2" {
		t.Fatalf("expected boundary parent public id hydrated, got %q", got[0].ParentPublicID)
	}
}

func TestListMessageAncestorsUntilReportsMissingBoundary(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	conversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_missing_boundary",
		Title:      "missing boundary",
		LabelsJSON: "[]",
		SessionKey: "session_missing_boundary",
		Status:     "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	message := model.Message{
		ConversationID: conversation.ID,
		UserID:         1,
		PublicID:       "msg_1",
		Role:           "user",
		ContentType:    "text",
		Content:        "message 1",
		BranchReason:   "default",
		Status:         "success",
	}
	if err := db.Create(&message).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	got, found, err := repo.ListMessageAncestorsUntil(ctx, conversation.ID, message.ID, message.ID+100, 10)
	if err != nil {
		t.Fatalf("ListMessageAncestorsUntil() error = %v", err)
	}
	if found {
		t.Fatal("expected boundary to be missing")
	}
	if len(got) != 1 || got[0].PublicID != "msg_1" {
		t.Fatalf("expected available ancestor path, got %#v", got)
	}
}

// 祖先链走的是手写 CTE，与 GetMessageByID 的常规 GORM 查询是两条取数路径。
// 这里逐字段比对两者结果，确保 CTE 不会丢列——曾因漏掉 reasoning_content 导致推理回传失效。
// 注意覆盖边界：比对的是 domain.Message，因此只能守住会映射进领域模型的列；
// 未进入领域模型的列（如 is_compacted）不在此测试范围内。
func TestListMessageAncestorsMatchesFullColumnLoad(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	conversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_ancestors_columns",
		Title:      "ancestors columns",
		LabelsJSON: "[]",
		SessionKey: "session_ancestors_columns",
		Status:     "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	root := model.Message{
		ConversationID: conversation.ID,
		UserID:         1,
		PublicID:       "msg_columns_root",
		Role:           "user",
		ContentType:    "text",
		Content:        "root",
		BranchReason:   "default",
		Status:         "success",
	}
	if err := db.Create(&root).Error; err != nil {
		t.Fatalf("create root message: %v", err)
	}

	editedAt := time.Now().UTC().Truncate(time.Second)
	sourceID := root.ID
	// 所有可空/可选列都填非零值，任何一列被 CTE 丢弃都会在比对中暴露。
	leaf := model.Message{
		ConversationID:   conversation.ID,
		UserID:           1,
		PublicID:         "msg_columns_leaf",
		ParentMessageID:  &root.ID,
		RunID:            "run_columns",
		Role:             "assistant",
		ContentType:      "text",
		Content:          "leaf",
		ReasoningContent: "historical reasoning",
		BranchReason:     "retry",
		SourceMessageID:  &sourceID,
		TokenUsage:       321,
		InputTokens:      111,
		OutputTokens:     222,
		CacheReadTokens:  33,
		CacheWriteTokens: 44,
		ReasoningTokens:  125,
		LatencyMS:        987,
		BilledCurrency:   "USD",
		BilledNanousd:    654,
		PricingSnapshot:  `{"in":1}`,
		Status:           "success",
		ErrorCode:        "none",
		ErrorMessage:     "no error",
		IsCompacted:      true,
		EditedAt:         &editedAt,
	}
	if err := db.Create(&leaf).Error; err != nil {
		t.Fatalf("create leaf message: %v", err)
	}

	want, err := repo.GetMessageByID(ctx, conversation.ID, leaf.ID)
	if err != nil {
		t.Fatalf("GetMessageByID() error = %v", err)
	}
	if want.ReasoningContent == "" {
		t.Fatal("baseline load lost reasoning content")
	}

	ancestors, err := repo.ListMessageAncestors(ctx, conversation.ID, leaf.ID, 10)
	if err != nil {
		t.Fatalf("ListMessageAncestors() error = %v", err)
	}
	if len(ancestors) != 2 {
		t.Fatalf("expected root and leaf, got %d", len(ancestors))
	}
	if !reflect.DeepEqual(ancestors[1], *want) {
		t.Fatalf("ListMessageAncestors dropped columns:\n cte = %#v\nfull = %#v", ancestors[1], *want)
	}

	until, found, err := repo.ListMessageAncestorsUntil(ctx, conversation.ID, leaf.ID, root.ID, 10)
	if err != nil {
		t.Fatalf("ListMessageAncestorsUntil() error = %v", err)
	}
	if !found {
		t.Fatal("expected boundary to be found")
	}
	if len(until) != 2 {
		t.Fatalf("expected root and leaf, got %d", len(until))
	}
	if !reflect.DeepEqual(until[1], *want) {
		t.Fatalf("ListMessageAncestorsUntil dropped columns:\n cte = %#v\nfull = %#v", until[1], *want)
	}
}

// 祖先链加载必须保留 reasoning_content，否则「回传推理上下文」在后续轮次拿不到历史推理。
func TestListMessageAncestorsPreservesReasoningContent(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	conversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_ancestors_reasoning",
		Title:      "ancestors reasoning",
		LabelsJSON: "[]",
		SessionKey: "session_ancestors_reasoning",
		Status:     "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	var parentID *uint
	messages := make([]model.Message, 0, 4)
	for index := 1; index <= 4; index++ {
		role := "user"
		reasoning := ""
		if index%2 == 0 {
			role = "assistant"
			reasoning = fmt.Sprintf("reasoning %d", index)
		}
		message := model.Message{
			ConversationID:   conversation.ID,
			UserID:           1,
			PublicID:         fmt.Sprintf("msg_reasoning_%d", index),
			ParentMessageID:  parentID,
			Role:             role,
			ContentType:      "text",
			Content:          fmt.Sprintf("message %d", index),
			ReasoningContent: reasoning,
			BranchReason:     "default",
			Status:           "success",
		}
		if err := db.Create(&message).Error; err != nil {
			t.Fatalf("create message %d: %v", index, err)
		}
		messages = append(messages, message)
		nextParentID := message.ID
		parentID = &nextParentID
	}

	leafID := messages[len(messages)-1].ID
	assertReasoning := func(t *testing.T, method string, got []domainconversation.Message) {
		t.Helper()
		if len(got) != len(messages) {
			t.Fatalf("%s: expected %d ancestors, got %d", method, len(messages), len(got))
		}
		for index, item := range got {
			want := messages[index].ReasoningContent
			if item.ReasoningContent != want {
				t.Fatalf("%s: ancestor %d reasoning content = %q, want %q", method, index, item.ReasoningContent, want)
			}
		}
	}

	ancestors, err := repo.ListMessageAncestors(ctx, conversation.ID, leafID, 10)
	if err != nil {
		t.Fatalf("ListMessageAncestors() error = %v", err)
	}
	assertReasoning(t, "ListMessageAncestors", ancestors)

	until, found, err := repo.ListMessageAncestorsUntil(ctx, conversation.ID, leafID, messages[0].ID, 10)
	if err != nil {
		t.Fatalf("ListMessageAncestorsUntil() error = %v", err)
	}
	if !found {
		t.Fatal("expected boundary to be found")
	}
	assertReasoning(t, "ListMessageAncestorsUntil", until)
}

func TestUpdateAssistantMessageCompletionPersistsReasoningAndKnowledgeSources(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	conversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_reasoning_completion",
		Title:      "reasoning completion",
		LabelsJSON: "[]",
		SessionKey: "session_reasoning_completion",
		Status:     "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	message := model.Message{
		ConversationID: conversation.ID,
		UserID:         1,
		PublicID:       "msg_reasoning_completion",
		Role:           "assistant",
		ContentType:    "text",
		Content:        "",
		BranchReason:   "default",
		Status:         "pending",
	}
	if err := db.Create(&message).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	err := repo.UpdateAssistantMessageCompletion(ctx, message.ID, repository.AssistantMessageCompletionUpdate{
		ContentType:      "text",
		Content:          "final answer",
		ReasoningContent: "stored reasoning",
		KnowledgeSources: []domainconversation.MessageKnowledgeSource{{
			FileName:   "handbook.md",
			FileID:     "file_handbook",
			ChunkIndex: 2,
			Score:      0.91,
			Preview:    "relevant policy",
		}},
		Status: "success",
	})
	if err != nil {
		t.Fatalf("UpdateAssistantMessageCompletion() error = %v", err)
	}

	got, err := repo.GetMessageByID(ctx, conversation.ID, message.ID)
	if err != nil {
		t.Fatalf("GetMessageByID() error = %v", err)
	}
	if got.Content != "final answer" || got.ReasoningContent != "stored reasoning" {
		t.Fatalf("unexpected completed message: %#v", got)
	}
	if len(got.KnowledgeSources) != 1 || got.KnowledgeSources[0].FileID != "file_handbook" {
		t.Fatalf("unexpected knowledge sources: %#v", got.KnowledgeSources)
	}
}

func TestUpdateConversationMetadataSQLiteUsesPortableTrim(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	conversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_metadata_sqlite",
		Title:      " 新对话 ",
		LabelsJSON: "[]",
		SessionKey: "session_metadata_sqlite",
		Status:     "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	updated, err := repo.UpdateConversationMetadata(ctx, conversation.ID, repository.ConversationMetadataPatch{
		Title: "SQLite 标题",
	})
	if err != nil {
		t.Fatalf("UpdateConversationMetadata() error = %v", err)
	}
	if updated.Title != "SQLite 标题" {
		t.Fatalf("updated title = %q, want %q", updated.Title, "SQLite 标题")
	}
}

func TestUpdateConversationLabelsAppliesGeneratedLabelsWhenEligible(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	conversation := model.Conversation{
		PublicID:   "generated-label-eligible",
		UserID:     1,
		Title:      "已有标题",
		LabelsJSON: `[]`,
		SessionKey: "generated-label-eligible-session",
		Status:     "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	updated, applied, err := repo.SetGeneratedConversationLabelsIfEligible(context.Background(), conversation.ID, `["自动标签"]`)
	if err != nil {
		t.Fatalf("SetGeneratedConversationLabelsIfEligible() error = %v", err)
	}
	if !applied || updated.LabelsJSON != `["自动标签"]` {
		t.Fatalf("generated labels were not applied: applied=%v labels=%q", applied, updated.LabelsJSON)
	}
}

func TestUpdateConversationLabelsByPublicIDIsUserScopedAndMarksManualManagement(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	conversation := model.Conversation{
		PublicID:   "manual-label-user-scope",
		UserID:     1,
		Title:      "已有标题",
		LabelsJSON: `[]`,
		SessionKey: "manual-label-user-scope-session",
		Status:     "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	if _, err := repo.UpdateConversationLabelsByPublicID(context.Background(), 2, conversation.PublicID, `["越权标签"]`); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected other user update to return not found, got %v", err)
	}
	updated, err := repo.UpdateConversationLabelsByPublicID(context.Background(), 1, conversation.PublicID, `["手动标签"]`)
	if err != nil {
		t.Fatalf("UpdateConversationLabelsByPublicID() error = %v", err)
	}
	if updated.LabelsJSON != `["手动标签"]` || !updated.LabelsManuallyManaged {
		t.Fatalf("manual labels were not persisted correctly: %#v", updated)
	}
}

func TestUpdateConversationLabelsGeneratedLabelsDoNotOverwriteManualLabels(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	conversation := model.Conversation{
		PublicID:              "generated-label-race",
		UserID:                1,
		Title:                 "已有标题",
		LabelsJSON:            `["手动标签"]`,
		LabelsManuallyManaged: true,
		SessionKey:            "generated-label-race-session",
		Status:                "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	updated, applied, err := repo.SetGeneratedConversationLabelsIfEligible(context.Background(), conversation.ID, `["自动标签"]`)
	if err != nil {
		t.Fatalf("SetGeneratedConversationLabelsIfEligible() error = %v", err)
	}
	if applied {
		t.Fatal("generated labels update unexpectedly applied")
	}
	if updated.LabelsJSON != `["手动标签"]` {
		t.Fatalf("generated labels overwrote manual labels: %q", updated.LabelsJSON)
	}
	if !updated.UpdatedAt.Equal(conversation.UpdatedAt) {
		t.Fatalf("skipped generated labels changed updated_at: got %v, want %v", updated.UpdatedAt, conversation.UpdatedAt)
	}
}

func TestUpdateConversationLabelsGeneratedLabelsDoNotRestoreManuallyClearedLabels(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	conversation := model.Conversation{
		PublicID:              "generated-label-manual-clear-race",
		UserID:                1,
		Title:                 "已有标题",
		LabelsJSON:            `[]`,
		LabelsManuallyManaged: true,
		SessionKey:            "generated-label-manual-clear-race-session",
		Status:                "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	updated, applied, err := repo.SetGeneratedConversationLabelsIfEligible(context.Background(), conversation.ID, `["自动标签"]`)
	if err != nil {
		t.Fatalf("SetGeneratedConversationLabelsIfEligible() error = %v", err)
	}
	if applied {
		t.Fatal("generated labels update unexpectedly applied")
	}
	if updated.LabelsJSON != `[]` {
		t.Fatalf("generated labels restored manually cleared labels: %q", updated.LabelsJSON)
	}
	if !updated.UpdatedAt.Equal(conversation.UpdatedAt) {
		t.Fatalf("skipped generated labels changed updated_at: got %v, want %v", updated.UpdatedAt, conversation.UpdatedAt)
	}
}

func TestUpdateConversationMetadataCanReplaceAutomaticFallbackTitle(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	conversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_metadata_fallback",
		Title:      "画一张城市夜景",
		LabelsJSON: "[]",
		SessionKey: "session_metadata_fallback",
		Status:     "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	updated, err := repo.UpdateConversationMetadata(ctx, conversation.ID, repository.ConversationMetadataPatch{
		Title:             "城市夜景图像生成",
		ReplaceableTitles: []string{"画一张城市夜景"},
	})
	if err != nil {
		t.Fatalf("UpdateConversationMetadata() error = %v", err)
	}
	if updated.Title != "城市夜景图像生成" {
		t.Fatalf("updated title = %q, want %q", updated.Title, "城市夜景图像生成")
	}

	if err := db.Model(&model.Conversation{}).Where("id = ?", conversation.ID).Update("title", "手动标题").Error; err != nil {
		t.Fatalf("set manual title: %v", err)
	}
	updated, err = repo.UpdateConversationMetadata(ctx, conversation.ID, repository.ConversationMetadataPatch{
		Title:             "不应覆盖",
		ReplaceableTitles: []string{"画一张城市夜景"},
	})
	if err != nil {
		t.Fatalf("UpdateConversationMetadata() error = %v", err)
	}
	if updated.Title != "手动标题" {
		t.Fatalf("manual title was overwritten: got %q", updated.Title)
	}
}

func TestListConversationsByUserSearchesMetadataProjectsAndMessages(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	project := model.ConversationProject{
		UserID:      1,
		PublicID:    "proj_research",
		Name:        "Research Notes",
		Description: "knowledge base",
		Status:      "active",
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}

	projectConversation := model.Conversation{
		UserID:     1,
		ProjectID:  &project.ID,
		PublicID:   "conv_project_search",
		Title:      "Project conversation",
		LabelsJSON: "[]",
		Model:      "gpt-test",
		Provider:   "openai",
		SessionKey: "session_project_search",
		Status:     "active",
	}
	titleConversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_title_search",
		Title:      "Quarterly Budget",
		LabelsJSON: `["finance"]`,
		Model:      "claude-test",
		Provider:   "anthropic",
		SessionKey: "session_title_search",
		Status:     "active",
	}
	messageConversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_message_search",
		Title:      "Ordinary chat",
		LabelsJSON: "[]",
		Model:      "gemini-test",
		Provider:   "gemini",
		SessionKey: "session_message_search",
		Status:     "active",
	}
	toolOnlyConversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_tool_only_search",
		Title:      "Tool output",
		LabelsJSON: "[]",
		Model:      "gpt-test",
		Provider:   "openai",
		SessionKey: "session_tool_only_search",
		Status:     "active",
	}
	wildcardConversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_literal_wildcard_search",
		Title:      "Progress 100%",
		LabelsJSON: "[]",
		Model:      "gpt-test",
		Provider:   "openai",
		SessionKey: "session_literal_wildcard_search",
		Status:     "active",
	}
	otherUserConversation := model.Conversation{
		UserID:     2,
		PublicID:   "conv_other_user",
		Title:      "Private Budget",
		LabelsJSON: "[]",
		Model:      "gpt-test",
		Provider:   "openai",
		SessionKey: "session_other_user",
		Status:     "active",
	}
	for _, conversation := range []model.Conversation{
		projectConversation,
		titleConversation,
		messageConversation,
		toolOnlyConversation,
		wildcardConversation,
		otherUserConversation,
	} {
		if err := db.Create(&conversation).Error; err != nil {
			t.Fatalf("create conversation %q: %v", conversation.PublicID, err)
		}
	}

	var messageTarget model.Conversation
	if err := db.Where("public_id = ?", "conv_message_search").First(&messageTarget).Error; err != nil {
		t.Fatalf("load message target: %v", err)
	}
	if err := db.Create(&model.Message{
		ConversationID: messageTarget.ID,
		UserID:         1,
		PublicID:       "msg_search",
		Role:           "user",
		ContentType:    "text",
		Content:        "The launch checklist mentions AuroraKeyword",
		BranchReason:   "default",
		Status:         "success",
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}
	var toolOnlyTarget model.Conversation
	if err := db.Where("public_id = ?", "conv_tool_only_search").First(&toolOnlyTarget).Error; err != nil {
		t.Fatalf("load tool-only target: %v", err)
	}
	if err := db.Create(&model.Message{
		ConversationID: toolOnlyTarget.ID,
		UserID:         1,
		PublicID:       "msg_tool_only_search",
		Role:           "tool",
		ContentType:    "text",
		Content:        "InternalToolOnlyKeyword",
		BranchReason:   "default",
		Status:         "success",
	}).Error; err != nil {
		t.Fatalf("create tool-only message: %v", err)
	}

	tests := []struct {
		name   string
		query  string
		wantID string
	}{
		{name: "title", query: "budget", wantID: "conv_title_search"},
		{name: "project", query: "research", wantID: "conv_project_search"},
		{name: "message", query: "aurorakeyword", wantID: "conv_message_search"},
		{name: "literal wildcard", query: "%", wantID: "conv_literal_wildcard_search"},
		{name: "tool messages are excluded", query: "internaltoolonlykeyword", wantID: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := repo.ListConversationsByUser(ctx, 1, 0, 10, "active", "all", "all", "all", tt.query)
			if err != nil {
				t.Fatalf("ListConversationsByUser() error = %v", err)
			}
			if tt.wantID == "" {
				if total != 0 || len(items) != 0 {
					t.Fatalf("items = %#v, total = %d, want no results", items, total)
				}
				return
			}
			if total != 1 || len(items) != 1 || items[0].PublicID != tt.wantID {
				t.Fatalf("items = %#v, want %q", items, tt.wantID)
			}
		})
	}
}

func TestListConversationsForSearchReturnsOrderedWindowWithoutStatusFiltering(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()
	now := time.Now()

	items := []model.Conversation{
		{
			BaseModel:  model.BaseModel{UpdatedAt: now.Add(-2 * time.Hour)},
			UserID:     1,
			PublicID:   "conv_search_oldest",
			Title:      "Needle oldest",
			LabelsJSON: "[]",
			Model:      "gpt-test",
			Provider:   "openai",
			SessionKey: "session_search_oldest",
			Status:     "active",
		},
		{
			BaseModel:  model.BaseModel{UpdatedAt: now.Add(-time.Hour)},
			UserID:     1,
			PublicID:   "conv_search_middle",
			Title:      "Needle middle",
			LabelsJSON: "[]",
			Model:      "gpt-test",
			Provider:   "openai",
			SessionKey: "session_search_middle",
			Status:     "archived",
		},
		{
			BaseModel:  model.BaseModel{UpdatedAt: now},
			UserID:     1,
			PublicID:   "conv_search_latest",
			Title:      "Needle latest",
			LabelsJSON: "[]",
			Model:      "gpt-test",
			Provider:   "openai",
			SessionKey: "session_search_latest",
			Status:     "active",
		},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatalf("create conversations: %v", err)
	}

	results, err := repo.ListConversationsForSearch(ctx, 1, 1, 2, "needle")
	if err != nil {
		t.Fatalf("ListConversationsForSearch() error = %v", err)
	}
	if len(results) != 2 || results[0].PublicID != "conv_search_middle" || results[1].PublicID != "conv_search_oldest" {
		t.Fatalf("results = %#v, want middle and oldest conversations", results)
	}
}

func TestListLatestBranchPreviewMessagesReturnsLatestVisibleWindow(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	conversation := model.Conversation{
		UserID:     1,
		PublicID:   "conv_latest_branch_preview",
		Title:      "Latest branch preview",
		LabelsJSON: "[]",
		Model:      "gpt-test",
		Provider:   "openai",
		SessionKey: "session_latest_branch_preview",
		Status:     "active",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	createMessage := func(publicID string, role string, parentID *uint) model.Message {
		t.Helper()
		item := model.Message{
			ConversationID:  conversation.ID,
			UserID:          1,
			PublicID:        publicID,
			ParentMessageID: parentID,
			Role:            role,
			ContentType:     "text",
			Content:         publicID + " content",
			BranchReason:    "default",
			Status:          "success",
		}
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("create message %q: %v", publicID, err)
		}
		return item
	}

	root := createMessage("msg_root", "user", nil)
	rootID := root.ID
	createMessage("msg_old_branch", "assistant", &rootID)

	latestBranch := createMessage("msg_latest_branch", "assistant", &rootID)
	latestVisibleIDs := []string{root.PublicID, latestBranch.PublicID}
	parentID := latestBranch.ID
	for i := 1; i <= 12; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		item := createMessage(fmt.Sprintf("msg_latest_%02d", i), role, &parentID)
		latestVisibleIDs = append(latestVisibleIDs, item.PublicID)
		parentID = item.ID
	}
	createMessage("msg_latest_tool", "tool", &parentID)

	items, err := repo.ListLatestBranchPreviewMessages(ctx, conversation.ID, 100, 10)
	if err != nil {
		t.Fatalf("ListLatestBranchPreviewMessages() error = %v", err)
	}
	if len(items) != 10 {
		t.Fatalf("len(items) = %d, want 10", len(items))
	}

	wantPublicIDs := latestVisibleIDs[len(latestVisibleIDs)-10:]
	for i, item := range items {
		if item.PublicID != wantPublicIDs[i] {
			t.Fatalf("items[%d].PublicID = %q, want %q", i, item.PublicID, wantPublicIDs[i])
		}
		if item.Role != "user" && item.Role != "assistant" {
			t.Fatalf("items[%d].Role = %q, want visible role", i, item.Role)
		}
	}
}

func openConversationRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.Conversation{}, &model.ConversationProject{}, &model.ConversationProjectMCPTool{}, &model.ConversationProjectSkill{}, &model.KnowledgeBase{}, &model.KnowledgeBaseFile{}, &model.ConversationProjectKnowledgeBase{}, &model.ConversationShare{}, &model.Message{}, &model.Attachment{}, &model.FileObject{}, &model.FileChunk{}, &model.ConversationRun{}, &model.ChatRunEvent{}); err != nil {
		t.Fatalf("migrate models: %v", err)
	}
	return db
}

func TestConversationToolCallPersistenceRedactsCredentialValues(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	create := domainconversation.ToolCall{
		UserID:         1,
		ConversationID: 2,
		RunID:          "run_redact_create",
		ToolCallID:     "call_redact_create",
		ToolType:       "platform",
		ToolName:       "credential_create",
		Status:         "success",
		InputJSON:      `{"name":"tokyo-vps","type":"ssh","value":"secret-create","options":{"password":"nested-secret"}}`,
		OutputJSON:     `{"status":"created","echo":"secret-create","token":"output-token"}`,
		ErrorJSON:      `failed to persist secret-create`,
	}
	if err := repo.CreateConversationToolCall(ctx, &create); err != nil {
		t.Fatalf("CreateConversationToolCall() error = %v", err)
	}
	assertPersistedCredentialValueRedacted(t, db, create.ID)
	assertPersistedCredentialPayloadsRedacted(t, db, create.ID, "secret-create", "nested-secret", "output-token")

	batch := []domainconversation.ToolCall{{
		UserID:         1,
		ConversationID: 2,
		RunID:          "run_redact_batch",
		ToolCallID:     "call_redact_batch",
		ToolType:       "platform",
		ToolName:       "credential_update",
		Status:         "success",
		InputJSON:      `{"name":"tokyo-vps","value":"secret-batch"}`,
	}}
	if err := repo.CreateConversationToolCalls(ctx, batch); err != nil {
		t.Fatalf("CreateConversationToolCalls() error = %v", err)
	}
	assertPersistedCredentialValueRedacted(t, db, batch[0].ID)

	update := domainconversation.ToolCall{
		ID:        create.ID,
		ToolName:  "credential_update",
		InputJSON: `{"name":"tokyo-vps","value":"secret-update"}`,
	}
	if err := repo.UpdateConversationToolCallPayload(ctx, create.UserID, create.ConversationID, create.RunID, update); err != nil {
		t.Fatalf("UpdateConversationToolCallPayload() error = %v", err)
	}
	assertPersistedCredentialValueRedacted(t, db, create.ID)
}

func TestConversationToolCallPersistencePreservesNonCredentialValue(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()
	row := domainconversation.ToolCall{
		UserID:         1,
		ConversationID: 2,
		RunID:          "run_preserve_value",
		ToolCallID:     "call_preserve_value",
		ToolType:       "mcp",
		ToolName:       "custom_tool",
		Status:         "success",
		InputJSON:      `{"value":"keep-me"}`,
	}
	if err := repo.CreateConversationToolCall(ctx, &row); err != nil {
		t.Fatalf("CreateConversationToolCall() error = %v", err)
	}
	var persisted model.ChatRunEvent
	if err := db.First(&persisted, row.ID).Error; err != nil {
		t.Fatalf("load persisted tool call: %v", err)
	}
	if persisted.InputJSON != row.InputJSON {
		t.Fatalf("non-credential input changed: got %q want %q", persisted.InputJSON, row.InputJSON)
	}
}

func assertPersistedCredentialValueRedacted(t *testing.T, db *gorm.DB, id uint) {
	t.Helper()
	var persisted model.ChatRunEvent
	if err := db.First(&persisted, id).Error; err != nil {
		t.Fatalf("load persisted credential tool call: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(persisted.InputJSON), &payload); err != nil {
		t.Fatalf("decode persisted input: %v", err)
	}
	if got := payload["value"]; got != "[REDACTED]" {
		t.Fatalf("persisted credential value = %#v, want [REDACTED]", got)
	}
	if strings.Contains(persisted.InputJSON, "secret-") {
		t.Fatalf("persisted credential input still contains a raw secret: %s", persisted.InputJSON)
	}
}

func assertPersistedCredentialPayloadsRedacted(t *testing.T, db *gorm.DB, id uint, secrets ...string) {
	t.Helper()
	var persisted model.ChatRunEvent
	if err := db.First(&persisted, id).Error; err != nil {
		t.Fatalf("load persisted credential tool call: %v", err)
	}
	serialized := persisted.InputJSON + persisted.OutputJSON + persisted.ErrorJSON
	for _, secret := range secrets {
		if strings.Contains(serialized, secret) {
			t.Fatalf("persisted credential payload still contains %q: %s", secret, serialized)
		}
	}
	if !strings.Contains(persisted.InputJSON, `"password":"[REDACTED]"`) {
		t.Fatalf("nested password was not redacted: %s", persisted.InputJSON)
	}
	if !strings.Contains(persisted.OutputJSON, `"token":"[REDACTED]"`) {
		t.Fatalf("output token was not redacted: %s", persisted.OutputJSON)
	}
}

func TestConversationToolCallReadRedactsHistoricalCredentialPayloads(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()
	historical := model.ChatRunEvent{
		UserID:         1,
		ConversationID: 2,
		RunID:          "run_historical_secret",
		EventScope:     chatRunEventScopeToolCall,
		EventID:        "call_historical_secret",
		ToolCallID:     "call_historical_secret",
		EventType:      "platform",
		ToolName:       "credential_create",
		Status:         "success",
		InputJSON:      `{"name":"legacy","value":"historical-secret"}`,
		OutputJSON:     `{"message":"stored historical-secret"}`,
	}
	if err := db.Create(&historical).Error; err != nil {
		t.Fatalf("create historical tool call: %v", err)
	}
	rows, err := repo.ListConversationToolCallsByRunID(ctx, 1, 2, historical.RunID)
	if err != nil {
		t.Fatalf("ListConversationToolCallsByRunID() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	serialized := rows[0].InputJSON + rows[0].OutputJSON + rows[0].ErrorJSON
	if strings.Contains(serialized, "historical-secret") {
		t.Fatalf("historical credential payload leaked on read: %s", serialized)
	}
}

// parent_message_id 上没有外键，「父消息同会话」只靠应用层保证。这里绕过应用层直接写入
// 一条跨会话的父指针，确认递归查询不会走出当前会话——否则外部内容会进入 prompt 并被
// 烤进压缩摘要反复重放。ListMessageAncestorsUntil 早已有此约束，两者需保持一致。
func TestListMessageAncestorsStopsAtConversationBoundary(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	makeConversation := func(publicID string) model.Conversation {
		conversation := model.Conversation{
			UserID: 1, PublicID: publicID, Title: publicID,
			LabelsJSON: "[]", SessionKey: "session_" + publicID, Status: "active",
		}
		if err := db.Create(&conversation).Error; err != nil {
			t.Fatalf("create conversation %s: %v", publicID, err)
		}
		return conversation
	}
	foreign := makeConversation("conv_foreign")
	own := makeConversation("conv_own")

	// 另一个会话中的消息，内容不应被泄漏到本会话的祖先链里。
	foreignMessage := model.Message{
		ConversationID: foreign.ID, UserID: 1, PublicID: "msg_foreign",
		Role: "assistant", ContentType: "text", Content: "FOREIGN_SECRET",
		ReasoningContent: "FOREIGN_REASONING", BranchReason: "default", Status: "success",
	}
	if err := db.Create(&foreignMessage).Error; err != nil {
		t.Fatalf("create foreign message: %v", err)
	}

	leaf := model.Message{
		ConversationID: own.ID, UserID: 1, PublicID: "msg_own_leaf",
		ParentMessageID: &foreignMessage.ID,
		Role:            "user", ContentType: "text", Content: "own leaf",
		BranchReason: "default", Status: "success",
	}
	if err := db.Create(&leaf).Error; err != nil {
		t.Fatalf("create leaf: %v", err)
	}

	got, err := repo.ListMessageAncestors(ctx, own.ID, leaf.ID, 10)
	if err != nil {
		t.Fatalf("ListMessageAncestors() error = %v", err)
	}
	for _, item := range got {
		if item.ConversationID != own.ID {
			t.Fatalf("ancestor walked into conversation %d: %#v", item.ConversationID, item)
		}
		if strings.Contains(item.Content, "FOREIGN_SECRET") {
			t.Fatalf("foreign content leaked into ancestor chain: %#v", item)
		}
	}
	if len(got) != 1 || got[0].PublicID != "msg_own_leaf" {
		t.Fatalf("expected only the in-conversation leaf, got %#v", got)
	}
}

func TestListRecentContextArtifactsUsesCTEForLongBranchAndSnapshotBoundary(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.Message{}, &model.ChatContextRecord{}); err != nil {
		t.Fatalf("migrate context records: %v", err)
	}
	repo := NewRepo(db)
	ctx := context.Background()
	conversationID := uint(77)

	const branchLength = 1205
	var parentMessageID *uint
	branchMessages := make([]model.Message, 0, branchLength)
	branchMessageIDs := make([]uint, 0, branchLength)
	for index := 0; index < branchLength; index++ {
		messageID := uint(10_000 + index)
		message := model.Message{
			BaseModel:       model.BaseModel{ID: messageID},
			ConversationID:  conversationID,
			UserID:          1,
			PublicID:        fmt.Sprintf("msg_context_long_%d", index),
			ParentMessageID: parentMessageID,
			Role:            []string{"user", "assistant"}[index%2],
			ContentType:     "text",
			Content:         fmt.Sprintf("message %d", index),
			BranchReason:    "default",
			Status:          "success",
		}
		branchMessages = append(branchMessages, message)
		branchMessageIDs = append(branchMessageIDs, messageID)
		parentMessageID = &messageID
	}
	if err := db.CreateInBatches(&branchMessages, 50).Error; err != nil {
		t.Fatalf("create %d branch messages: %v", branchLength, err)
	}
	sibling := model.Message{
		ConversationID:  conversationID,
		UserID:          1,
		PublicID:        "msg_context_long_sibling",
		ParentMessageID: &branchMessageIDs[10],
		Role:            "assistant",
		ContentType:     "text",
		Content:         "sibling",
		BranchReason:    "retry",
		Status:          "success",
	}
	if err := db.Create(&sibling).Error; err != nil {
		t.Fatalf("create sibling: %v", err)
	}
	artifacts := []model.ChatContextRecord{
		{
			RecordType: chatContextRecordArtifact, ConversationID: conversationID, MessageID: branchMessageIDs[1], UserID: 1,
			Kind: string(domainconversation.ContextArtifactToolResult), SourceType: "tool_call", SourceID: "covered", Content: "covered evidence",
		},
		{
			RecordType: chatContextRecordArtifact, ConversationID: conversationID, MessageID: branchMessageIDs[999], UserID: 1,
			Kind: string(domainconversation.ContextArtifactToolResult), SourceType: "tool_call", SourceID: "boundary", Content: "boundary evidence",
		},
		{
			RecordType: chatContextRecordArtifact, ConversationID: conversationID, MessageID: branchMessageIDs[branchLength-2], UserID: 1,
			Kind: string(domainconversation.ContextArtifactToolResult), SourceType: "tool_call", SourceID: "retained", Content: "retained evidence",
		},
		{
			RecordType: chatContextRecordArtifact, ConversationID: conversationID, MessageID: sibling.ID, UserID: 1,
			Kind: string(domainconversation.ContextArtifactToolResult), SourceType: "tool_call", SourceID: "sibling", Content: "sibling evidence",
		},
	}
	if err := db.Create(&artifacts).Error; err != nil {
		t.Fatalf("create context artifacts: %v", err)
	}

	items, err := repo.ListRecentContextArtifacts(ctx, repository.ContextArtifactListFilter{
		Scope: repository.HistoricalMessageScope{
			ConversationID:          conversationID,
			UserID:                  1,
			LeafMessageID:           branchMessageIDs[branchLength-1],
			ExcludeThroughMessageID: branchMessageIDs[999],
		},
		Kinds: []domainconversation.ContextArtifactKind{domainconversation.ContextArtifactToolResult},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListRecentContextArtifacts() error = %v", err)
	}
	if len(items) != 1 || items[0].SourceID != "retained" {
		t.Fatalf("expected only retained long-branch artifact, got %#v", items)
	}

	items, err = repo.ListRecentContextArtifacts(ctx, repository.ContextArtifactListFilter{
		Scope: repository.HistoricalMessageScope{
			ConversationID:          conversationID,
			UserID:                  1,
			LeafMessageID:           branchMessageIDs[branchLength-1],
			ExcludeThroughMessageID: sibling.ID,
		},
		Kinds: []domainconversation.ContextArtifactKind{domainconversation.ContextArtifactToolResult},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListRecentContextArtifacts(invalid boundary) error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected non-ancestor boundary to fail closed, got %#v", items)
	}
}

func TestListRecentContextArtifactsHistoricalScopeTerminatesCycle(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.Message{}, &model.ChatContextRecord{}); err != nil {
		t.Fatalf("migrate context records: %v", err)
	}
	repo := NewRepo(db)
	ctx := context.Background()
	conversationID := uint(88)
	first := model.Message{
		ConversationID: conversationID, UserID: 1, PublicID: "msg_scope_cycle_first",
		Role: "assistant", ContentType: "text", Content: "first", BranchReason: "default", Status: "success",
	}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create first message: %v", err)
	}
	second := model.Message{
		ConversationID: conversationID, UserID: 1, PublicID: "msg_scope_cycle_second",
		ParentMessageID: &first.ID,
		Role:            "user", ContentType: "text", Content: "second", BranchReason: "default", Status: "success",
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatalf("create second message: %v", err)
	}
	if err := db.Model(&first).Update("parent_message_id", second.ID).Error; err != nil {
		t.Fatalf("create cycle: %v", err)
	}
	artifact := model.ChatContextRecord{
		RecordType: chatContextRecordArtifact, ConversationID: conversationID, MessageID: first.ID, UserID: 1,
		Kind: string(domainconversation.ContextArtifactToolResult), SourceType: "tool_call", SourceID: "cycle", Content: "cycle evidence",
	}
	if err := db.Create(&artifact).Error; err != nil {
		t.Fatalf("create cycle artifact: %v", err)
	}

	items, err := repo.ListRecentContextArtifacts(ctx, repository.ContextArtifactListFilter{
		Scope: repository.HistoricalMessageScope{ConversationID: conversationID, UserID: 1, LeafMessageID: second.ID},
		Kinds: []domainconversation.ContextArtifactKind{domainconversation.ContextArtifactToolResult},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListRecentContextArtifacts() error = %v", err)
	}
	if len(items) != 1 || items[0].MessageID != first.ID {
		t.Fatalf("expected cycle to terminate with one historical artifact, got %#v", items)
	}
}

func TestHistoricalMessageScopeStopsAtUserBoundary(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.Message{}); err != nil {
		t.Fatalf("migrate messages: %v", err)
	}
	conversationID := uint(89)
	ownerAncestor := model.Message{
		ConversationID: conversationID, UserID: 1, PublicID: "msg_scope_owner_ancestor",
		Role: "assistant", ContentType: "text", Content: "owner ancestor", BranchReason: "default", Status: "success",
	}
	if err := db.Create(&ownerAncestor).Error; err != nil {
		t.Fatalf("create owner ancestor: %v", err)
	}
	foreignParent := model.Message{
		ConversationID: conversationID, UserID: 2, PublicID: "msg_scope_foreign_parent",
		ParentMessageID: &ownerAncestor.ID,
		Role:            "assistant", ContentType: "text", Content: "foreign parent", BranchReason: "default", Status: "success",
	}
	if err := db.Create(&foreignParent).Error; err != nil {
		t.Fatalf("create foreign parent: %v", err)
	}
	leaf := model.Message{
		ConversationID: conversationID, UserID: 1, PublicID: "msg_scope_owner_leaf",
		ParentMessageID: &foreignParent.ID,
		Role:            "user", ContentType: "text", Content: "owner leaf", BranchReason: "default", Status: "pending",
	}
	if err := db.Create(&leaf).Error; err != nil {
		t.Fatalf("create owner leaf: %v", err)
	}

	var messageIDs []uint
	if err := historicalMessageScopeSubquery(db, repository.HistoricalMessageScope{
		ConversationID: conversationID,
		UserID:         1,
		LeafMessageID:  leaf.ID,
	}).Scan(&messageIDs).Error; err != nil {
		t.Fatalf("query historical scope: %v", err)
	}
	if len(messageIDs) != 0 {
		t.Fatalf("expected traversal to stop at foreign-user parent, got message ids %v", messageIDs)
	}
}

func TestDeleteFileObjectAndReleaseQuotaPreservesObjectsReferencedBySoftDeletedRows(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.User{}, &model.UserStorageQuota{}, &model.Conversation{}, &model.Attachment{}); err != nil {
		t.Fatalf("migrate users, quota and attachments: %v", err)
	}
	users := []model.User{
		{PublicID: "delete_file_owner", Username: "delete-file-owner", Role: "user", Status: "active"},
		{PublicID: "delete_file_control", Username: "delete-file-control", Role: "user", Status: "active"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	owner := model.FileObject{
		FileID:      "file_delete_owner",
		UserID:      users[0].ID,
		StoragePath: "objects/delete-shared.bin",
		SizeBytes:   64,
		Status:      "active",
	}
	// 另一租户的软删除行仍保留记录并引用同一对象：物理删除必须被阻止。
	controlSoft := model.FileObject{
		FileID:      "file_delete_control_soft",
		UserID:      users[1].ID,
		StoragePath: "objects/delete-shared.bin",
		Status:      "deleted",
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("seed owner file: %v", err)
	}
	if err := db.Create(&controlSoft).Error; err != nil {
		t.Fatalf("seed control soft-deleted file: %v", err)
	}

	_, _, shouldRemovePhysical, err := NewRepo(db).DeleteFileObjectAndReleaseQuota(
		context.Background(),
		users[0].ID,
		owner.FileID,
		1024,
		repository.DeleteFileObjectOptions{},
	)
	if err != nil {
		t.Fatalf("DeleteFileObjectAndReleaseQuota() error = %v", err)
	}
	if shouldRemovePhysical {
		t.Fatal("soft-deleted tenant row did not pin the shared object")
	}

	var ownerRow model.FileObject
	if err = db.Where("id = ?", owner.ID).First(&ownerRow).Error; err != nil {
		t.Fatalf("load owner row: %v", err)
	}
	if ownerRow.Status != "deleted" {
		t.Fatalf("owner row status = %q, want deleted", ownerRow.Status)
	}
}

func TestDeleteFileObjectAndReleaseQuotaRemovesExclusiveObject(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	if err := db.AutoMigrate(&model.User{}, &model.UserStorageQuota{}, &model.Conversation{}, &model.Attachment{}); err != nil {
		t.Fatalf("migrate users, quota and attachments: %v", err)
	}
	users := []model.User{
		{PublicID: "delete_file_exclusive_owner", Username: "delete-file-exclusive-owner", Role: "user", Status: "active"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	owner := model.FileObject{
		FileID:      "file_delete_exclusive",
		UserID:      users[0].ID,
		StoragePath: "objects/delete-exclusive.bin",
		SizeBytes:   64,
		Status:      "active",
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("seed owner file: %v", err)
	}

	_, _, shouldRemovePhysical, err := NewRepo(db).DeleteFileObjectAndReleaseQuota(
		context.Background(),
		users[0].ID,
		owner.FileID,
		1024,
		repository.DeleteFileObjectOptions{},
	)
	if err != nil {
		t.Fatalf("DeleteFileObjectAndReleaseQuota() error = %v", err)
	}
	if !shouldRemovePhysical {
		t.Fatal("exclusive object should be removable after file deletion")
	}
}
