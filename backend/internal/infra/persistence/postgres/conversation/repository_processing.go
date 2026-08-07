package conversation

import (
	"context"
	"time"

	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	models "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

func (r *Repo) UpdateFileObjectProcessingState(ctx context.Context, item *domainconversation.FileObjectProcessing) error {
	if item == nil {
		return nil
	}
	result := r.db.WithContext(ctx).
		Model(&models.FileObject{}).
		Where("id = ? AND user_id = ?", item.FileObjectID, item.UserID).
		Updates(fileObjectProcessingStateUpdates(item))
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
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
	now := time.Now()
	copyItem := *source
	copyItem.ID = 0
	copyItem.FileObjectID = targetFileObjID
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
	result := r.db.WithContext(ctx).
		Model(&models.FileObject{}).
		Where("user_id = ? AND file_id = ?", userID, fileID).
		Updates(updates)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
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
func (r *Repo) ReplaceFileObjectContent(ctx context.Context, userID uint, fileID string, storagePath string, sha256 string, sizeBytes int64) error {
	updates := map[string]interface{}{
		"storage_path":          storagePath,
		"sha256":                sha256,
		"size_bytes":            sizeBytes,
		"processing_status":     "pending",
		"processing_ready":      false,
		"processing_error_code": "",
		"processing_error_message": "",
		"extract_status":        "",
		"extract_engine":        "",
		"extract_storage_path":  "",
		"extract_chars":         0,
		"extract_pages":         0,
		"preview_text":          "",
		"ocr_used":              false,
		"rag_ready":             false,
		"rag_reason":            "",
		"embed_status":          "",
		"embed_error":           "",
		"chunk_count":           0,
		"page_count":            0,
		"extractor_version":     "",
		"processing_started_at": nil,
		"processing_completed_at": nil,
		"extracted_at":          nil,
		"updated_at":            time.Now(),
	}
	result := r.db.WithContext(ctx).
		Model(&models.FileObject{}).
		Where("user_id = ? AND file_id = ?", userID, fileID).
		Updates(updates)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}
