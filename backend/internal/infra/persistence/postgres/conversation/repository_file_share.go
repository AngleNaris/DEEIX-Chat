package conversation

import (
	"context"
	"time"

	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	models "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func toFileShareDomain(item models.FileShare) domainconversation.FileShare {
	return domainconversation.FileShare{
		ID:        item.ID,
		ShareID:   item.ShareID,
		FileID:    item.FileID,
		UserID:    item.UserID,
		Status:    item.Status,
		ExpiresAt: item.ExpiresAt,
		RevokedAt: item.RevokedAt,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

// ReplaceActiveFileShare validates ownership, revokes the previous link, and creates a new one atomically.
func (r *Repo) ReplaceActiveFileShare(ctx context.Context, item *domainconversation.FileShare) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var file models.FileObject
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND file_id = ? AND status = ?", item.UserID, item.FileID, "active").
			First(&file).Error; err != nil {
			return translateError(err)
		}
		if err := tx.Model(&models.FileShare{}).
			Where("user_id = ? AND file_id = ? AND status = ?", item.UserID, item.FileID, "active").
			Updates(map[string]interface{}{"status": "revoked", "revoked_at": time.Now().UTC()}).Error; err != nil {
			return translateError(err)
		}
		record := models.FileShare{
			ShareID:   item.ShareID,
			FileID:    item.FileID,
			UserID:    item.UserID,
			Status:    "active",
			ExpiresAt: item.ExpiresAt,
		}
		if err := tx.Create(&record).Error; err != nil {
			return translateError(err)
		}
		*item = toFileShareDomain(record)
		return nil
	})
}

func (r *Repo) GetLatestFileShare(ctx context.Context, userID uint, fileID string) (*domainconversation.FileShare, error) {
	var item models.FileShare
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND file_id = ?", userID, fileID).
		Order("id DESC").First(&item).Error; err != nil {
		return nil, translateError(err)
	}
	result := toFileShareDomain(item)
	return &result, nil
}

func (r *Repo) GetFileShareByShareID(ctx context.Context, shareID string) (*domainconversation.FileShare, *domainconversation.FileObject, error) {
	var share models.FileShare
	if err := r.db.WithContext(ctx).Where("share_id = ?", shareID).First(&share).Error; err != nil {
		return nil, nil, translateError(err)
	}
	var file models.FileObject
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND file_id = ? AND status = ?", share.UserID, share.FileID, "active").
		First(&file).Error; err != nil {
		return nil, nil, translateError(err)
	}
	shareDomain := toFileShareDomain(share)
	fileDomain := toFileObjectDomain(file)
	return &shareDomain, &fileDomain, nil
}

func (r *Repo) RevokeActiveFileShare(ctx context.Context, userID uint, fileID string) error {
	result := r.db.WithContext(ctx).Model(&models.FileShare{}).
		Where("user_id = ? AND file_id = ? AND status = ?", userID, fileID, "active").
		Updates(map[string]interface{}{"status": "revoked", "revoked_at": time.Now().UTC()})
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}
