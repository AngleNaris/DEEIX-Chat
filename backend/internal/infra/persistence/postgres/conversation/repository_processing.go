package conversation

import (
	"context"
	"strings"
	"time"

	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	models "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/sqlitevec"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repo) UpdateFileObjectProcessingState(ctx context.Context, item *domainconversation.FileObjectProcessing) error {
	if item == nil {
		return nil
	}
	query := r.db.WithContext(ctx).
		Model(&models.FileObject{}).
		Where("id = ? AND user_id = ?", item.FileObjectID, item.UserID)
	if item.ExpectedStoragePath != "" {
		query = query.Where("storage_path = ?", item.ExpectedStoragePath)
	}
	if item.ExpectedProcessingStartedAt != nil {
		query = query.Where("processing_started_at = ?", *item.ExpectedProcessingStartedAt)
	}
	result := query.Updates(fileObjectProcessingStateUpdates(item))
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return fileObjectVersionConflictOrNotFound(r.db.WithContext(ctx), item.FileObjectID, item.UserID, item.ExpectedStoragePath)
	}
	return nil
}

func (r *Repo) GetFileObjectProcessingByObjectID(ctx context.Context, fileObjID uint) (*domainconversation.FileObjectProcessing, error) {
	var item models.FileObject
	if err := r.db.WithContext(ctx).
		Where("id = ?", fileObjID).
		First(&item).Error; err != nil {
		return nil, err
	}
	result := toFileObjectProcessingStateDomain(item)
	return &result, nil
}

func (r *Repo) CloneFileObjectProcessingState(ctx context.Context, sourceFileObjID uint, targetFileObjID uint, userID uint) error {
	if sourceFileObjID == 0 || targetFileObjID == 0 {
		return nil
	}
	source, err := r.GetFileObjectProcessingByObjectID(ctx, sourceFileObjID)
	if err != nil {
		return nil
	}
	target, err := r.GetFileObjectProcessingByObjectID(ctx, targetFileObjID)
	if err != nil {
		return err
	}
	now := time.Now()
	copyItem := *source
	copyItem.ID = 0
	copyItem.FileObjectID = targetFileObjID
	copyItem.ExpectedStoragePath = target.ExpectedStoragePath
	copyItem.UserID = userID
	copyItem.CreatedAt = now
	copyItem.UpdatedAt = now
	return r.UpdateFileObjectProcessingState(ctx, &copyItem)
}

func (r *Repo) UpdateFileObjectProcessing(
	ctx context.Context,
	userID uint,
	fileID string,
	input repository.UpdateFileObjectProcessingInput,
) error {
	updates := fileObjectProcessingUpdates(input)
	if len(updates) == 0 {
		return nil
	}
	updates["updated_at"] = time.Now()
	query := r.db.WithContext(ctx).
		Model(&models.FileObject{}).
		Where("user_id = ? AND file_id = ?", userID, fileID)
	if input.ExpectedStoragePath != "" {
		query = query.Where("storage_path = ?", input.ExpectedStoragePath)
	}
	if input.ExpectedProcessingStartedAt != nil {
		query = query.Where("processing_started_at = ?", *input.ExpectedProcessingStartedAt)
	}
	result := query.Updates(updates)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return fileObjectVersionConflictOrNotFoundByFileID(r.db.WithContext(ctx), userID, fileID, input.ExpectedStoragePath)
	}
	return nil
}

// CanRemoveExtractStoragePath 在锁定用户行后判断提取产物是否已无任何保留引用。
// 供处理流水线的 stale worker 在版本冲突（文件内容已被覆盖）时安全清理本次未提交的
// 唯一提取对象；克隆文件仍共享 extract 路径，因此必须按全量保留行判定，并排除
// 调用者自身的文件行（冲突时该行已不再指向本次 extract）。
func (r *Repo) CanRemoveExtractStoragePath(ctx context.Context, fileObjID uint, userID uint, extractPath string) (bool, error) {
	if strings.TrimSpace(extractPath) == "" {
		return false, nil
	}
	var removable bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !r.sqliteDialect() {
			var lockedUser models.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ?", userID).
				First(&lockedUser).Error; err != nil {
				return err
			}
		}
		var fileReferences int64
		if err := tx.Unscoped().
			Model(&models.FileObject{}).
			Where("id <> ? AND (storage_path = ? OR extract_storage_path = ?)", fileObjID, extractPath, extractPath).
			Count(&fileReferences).Error; err != nil {
			return err
		}
		if fileReferences > 0 {
			return nil
		}
		var attachmentReferences int64
		if err := tx.Model(&models.Attachment{}).
			Where("status <> ? AND storage_path = ?", "deleted", extractPath).
			Count(&attachmentReferences).Error; err != nil {
			return err
		}
		removable = attachmentReferences == 0
		return nil
	})
	if err != nil {
		return false, translateError(err)
	}
	return removable, nil
}

func fileObjectVersionConflictOrNotFound(db *gorm.DB, fileObjectID uint, userID uint, expectedStoragePath string) error {
	if expectedStoragePath == "" {
		return repository.ErrNotFound
	}
	var count int64
	query := db.Model(&models.FileObject{}).Where("id = ?", fileObjectID)
	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.Count(&count).Error; err != nil {
		return translateError(err)
	}
	if count > 0 {
		return repository.ErrConflict
	}
	return repository.ErrNotFound
}

func fileObjectVersionConflictOrNotFoundByFileID(db *gorm.DB, userID uint, fileID string, expectedStoragePath string) error {
	if expectedStoragePath == "" {
		return repository.ErrNotFound
	}
	var count int64
	if err := db.Model(&models.FileObject{}).Where("user_id = ? AND file_id = ?", userID, fileID).Count(&count).Error; err != nil {
		return translateError(err)
	}
	if count > 0 {
		return repository.ErrConflict
	}
	return repository.ErrNotFound
}

func fileObjectProcessingUpdates(input repository.UpdateFileObjectProcessingInput) map[string]interface{} {
	updates := make(map[string]interface{})
	if input.ProcessingStatus != nil {
		updates["processing_status"] = *input.ProcessingStatus
	}
	if input.ProcessingReady != nil {
		updates["processing_ready"] = *input.ProcessingReady
	}
	if input.ProcessingErrorCode != nil {
		updates["processing_error_code"] = *input.ProcessingErrorCode
	}
	if input.ProcessingErrorMessage != nil {
		updates["processing_error_message"] = *input.ProcessingErrorMessage
	}
	if input.ExtractStatus != nil {
		updates["extract_status"] = *input.ExtractStatus
	}
	if input.PageCount != nil {
		updates["page_count"] = *input.PageCount
	}
	if input.ExtractorVersion != nil {
		updates["extractor_version"] = *input.ExtractorVersion
	}
	if input.ExtractedAt != nil {
		updates["extracted_at"] = *input.ExtractedAt
	}
	return updates
}

// ReplaceFileObjectContent 覆盖文件对象内容元数据并重置处理/提取/向量状态。
// 平台工具 write_file 覆盖内容后调用；重建由 file_reindex_scheduler 延迟触发。
func (r *Repo) ReplaceFileObjectContent(
	ctx context.Context,
	userID uint,
	fileID string,
	storagePath string,
	sha256 string,
	sizeBytes int64,
) (repository.ReplaceFileObjectContentResult, error) {
	var cleanup repository.ReplaceFileObjectContentResult
	updates := map[string]interface{}{
		"storage_path":             storagePath,
		"sha256":                   sha256,
		"size_bytes":               sizeBytes,
		"processing_status":        "pending",
		"processing_ready":         false,
		"processing_error_code":    "",
		"processing_error_message": "",
		"extract_status":           "",
		"extract_engine":           "",
		"extract_storage_path":     "",
		"extract_chars":            0,
		"extract_pages":            0,
		"preview_text":             "",
		"ocr_used":                 false,
		"rag_ready":                false,
		"rag_reason":               "",
		"embed_status":             "none",
		"embed_error":              "",
		"chunk_count":              0,
		"page_count":               0,
		"extractor_version":        "",
		"processing_started_at":    nil,
		"processing_completed_at":  nil,
		"extracted_at":             nil,
		"updated_at":               time.Now(),
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockUsersForFileWrite(tx, userID); err != nil {
			return err
		}

		var current models.FileObject
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND file_id = ? AND status = ?", userID, fileID, "active").
			First(&current).Error; err != nil {
			return err
		}
		cleanup.OldStoragePath = current.StoragePath
		cleanup.OldExtractStoragePath = current.ExtractStoragePath

		result := tx.Model(&models.FileObject{}).
			Where("id = ? AND status = ?", current.ID, "active").
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		if r.sqliteDialect() {
			available, availabilityErr := sqlitevec.Available(ctx, tx)
			if availabilityErr != nil {
				return availabilityErr
			}
			if available {
				if err := deleteSQLiteFileChunkVectorsByFile(tx, current.ID); err != nil {
					return err
				}
			}
		}
		if err := tx.Where("file_obj_id = ?", current.ID).Delete(&models.FileChunk{}).Error; err != nil {
			return translateError(err)
		}

		var err error
		cleanup.RemoveOldStorageObject, err = fileStoragePathUnreferenced(tx, current.StoragePath)
		if err != nil {
			return err
		}
		cleanup.RemoveOldExtractObject, err = fileStoragePathUnreferenced(tx, current.ExtractStoragePath)
		return err
	})
	if err != nil {
		return repository.ReplaceFileObjectContentResult{}, translateError(err)
	}
	return cleanup, nil
}

func (r *Repo) ClaimFileProcessingQueue(
	ctx context.Context,
	userID uint,
	fileID string,
	expectedStoragePath string,
) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&models.FileObject{}).
		Where("user_id = ? AND file_id = ? AND storage_path = ? AND status = ? AND processing_status = ?",
			userID, fileID, expectedStoragePath, "active", "pending").
		Updates(map[string]interface{}{
			"processing_status": "queued",
			"updated_at":        time.Now(),
		})
	if result.Error != nil {
		return false, translateError(result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (r *Repo) ReleaseFileProcessingQueueClaim(
	ctx context.Context,
	userID uint,
	fileID string,
	expectedStoragePath string,
) error {
	result := r.db.WithContext(ctx).
		Model(&models.FileObject{}).
		Where("user_id = ? AND file_id = ? AND storage_path = ? AND status = ? AND processing_status = ?",
			userID, fileID, expectedStoragePath, "active", "queued").
		Updates(map[string]interface{}{
			"processing_status": "pending",
			"updated_at":        time.Now(),
		})
	return translateError(result.Error)
}

func (r *Repo) ClaimFileProcessingExecution(
	ctx context.Context,
	userID uint,
	fileID string,
	expectedStoragePath string,
	extractorVersion string,
	processingStartedAt time.Time,
) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&models.FileObject{}).
		Where("user_id = ? AND file_id = ? AND storage_path = ? AND status = ? AND processing_status IN ?",
			userID, fileID, expectedStoragePath, "active", []string{"queued", "failed"}).
		Updates(map[string]interface{}{
			"processing_status":        "extracting",
			"processing_ready":         false,
			"processing_error_code":    "",
			"processing_error_message": "",
			"extract_status":           "processing",
			"extractor_version":        extractorVersion,
			"processing_started_at":    processingStartedAt,
			"processing_completed_at":  nil,
			"updated_at":               time.Now(),
		})
	if result.Error != nil {
		return false, translateError(result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (r *Repo) ClaimRecoverableFilesForProcessing(
	ctx context.Context,
	pendingCutoff time.Time,
	queuedCutoff time.Time,
	extractingCutoff time.Time,
	limit int,
) ([]domainconversation.FileObject, error) {
	if limit <= 0 {
		limit = 100
	}
	var claimed []domainconversation.FileObject
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entities []models.FileObject
		query := tx.Where("status = ? AND ((processing_status = ? AND updated_at < ?) OR (processing_status = ? AND updated_at < ?) OR (processing_status = ? AND updated_at < ?))",
			"active", "pending", pendingCutoff, "queued", queuedCutoff, "extracting", extractingCutoff).
			Order("id ASC").
			Limit(limit)
		if tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := query.Find(&entities).Error; err != nil {
			return translateError(err)
		}
		for i := range entities {
			entity := entities[i]
			result := tx.Model(&models.FileObject{}).
				Where("id = ? AND storage_path = ? AND processing_status = ?", entity.ID, entity.StoragePath, entity.ProcessingStatus).
				Updates(map[string]interface{}{
					"processing_status":     "queued",
					"processing_started_at": nil,
					"updated_at":            time.Now(),
				})
			if result.Error != nil {
				return translateError(result.Error)
			}
			if result.RowsAffected == 0 {
				continue
			}
			entity.ProcessingStatus = "queued"
			claimed = append(claimed, toFileObjectDomain(entity))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func fileStoragePathUnreferenced(tx *gorm.DB, storagePath string) (bool, error) {
	if storagePath == "" {
		return false, nil
	}
	// 任何保留的 file_object 行（含软删除行与克隆行）仍引用该对象；
	// 调用方在写入路径下已先移除了自身行的旧路径，因此此处统计其他保留行。
	var fileReferences int64
	if err := tx.Unscoped().
		Model(&models.FileObject{}).
		Where("storage_path = ? OR extract_storage_path = ?", storagePath, storagePath).
		Count(&fileReferences).Error; err != nil {
		return false, translateError(err)
	}
	if fileReferences > 0 {
		return false, nil
	}

	var attachmentReferences int64
	if err := tx.Model(&models.Attachment{}).
		Where("status <> ? AND storage_path = ?", "deleted", storagePath).
		Count(&attachmentReferences).Error; err != nil {
		return false, translateError(err)
	}
	return attachmentReferences == 0, nil
}
