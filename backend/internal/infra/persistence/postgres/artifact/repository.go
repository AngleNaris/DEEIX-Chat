package artifact

import (
	"context"

	domainartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/artifact"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/dberror"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
)

// translateError 将 gorm 底层错误统一映射为仓储语义错误。
func translateError(err error) error {
	if dberror.IsRecordNotFound(err) {
		return repository.ErrNotFound
	}
	return err
}

// Repo 聚合制品域数据访问。
type Repo struct {
	db *gorm.DB
}

// NewRepo 创建仓储。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func toDomain(item model.Artifact) domainartifact.Artifact {
	return domainartifact.Artifact{
		ID:               item.ID,
		ArtifactPublicID: item.ArtifactPublicID,
		UserID:           item.UserID,
		ConversationID:   item.ConversationID,
		MessageID:        item.MessageID,
		Kind:             item.Kind,
		Title:            item.Title,
		Code:             item.Code,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
	}
}

func (r *Repo) CreateArtifact(ctx context.Context, item *domainartifact.Artifact) error {
	if item == nil {
		return nil
	}
	record := model.Artifact{
		ArtifactPublicID: item.ArtifactPublicID,
		UserID:           item.UserID,
		ConversationID:   item.ConversationID,
		MessageID:        item.MessageID,
		Kind:             item.Kind,
		Title:            item.Title,
		Code:             item.Code,
	}
	if err := r.db.WithContext(ctx).Create(&record).Error; err != nil {
		return translateError(err)
	}
	item.ID = record.ID
	return nil
}

func (r *Repo) UpdateArtifact(ctx context.Context, item *domainartifact.Artifact) error {
	if item == nil {
		return nil
	}
	return translateError(r.db.WithContext(ctx).Model(&model.Artifact{}).
		Where("id = ? AND user_id = ?", item.ID, item.UserID).
		Updates(map[string]interface{}{
			"title": item.Title,
			"kind":  item.Kind,
			"code":  item.Code,
		}).Error)
}

func (r *Repo) GetArtifactByPublicID(ctx context.Context, userID uint, publicID string) (*domainartifact.Artifact, error) {
	var record model.Artifact
	err := r.db.WithContext(ctx).
		Where("artifact_public_id = ? AND user_id = ?", publicID, userID).
		First(&record).Error
	if err != nil {
		return nil, translateError(err)
	}
	domain := toDomain(record)
	return &domain, nil
}

func (r *Repo) GetArtifactByID(ctx context.Context, artifactID uint) (*domainartifact.Artifact, error) {
	var record model.Artifact
	err := r.db.WithContext(ctx).Where("id = ?", artifactID).First(&record).Error
	if err != nil {
		return nil, translateError(err)
	}
	domain := toDomain(record)
	return &domain, nil
}

func (r *Repo) ListArtifacts(ctx context.Context, userID uint, page int, pageSize int) ([]domainartifact.Artifact, int64, error) {
	records := make([]model.Artifact, 0)
	var total int64
	query := r.db.WithContext(ctx).Model(&model.Artifact{}).Where("user_id = ?", userID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, translateError(err)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if err := query.Order("updated_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&records).Error; err != nil {
		return nil, 0, translateError(err)
	}
	results := make([]domainartifact.Artifact, 0, len(records))
	for _, item := range records {
		results = append(results, toDomain(item))
	}
	return results, total, nil
}

func (r *Repo) DeleteArtifact(ctx context.Context, userID uint, publicID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record model.Artifact
		if err := tx.Where("artifact_public_id = ? AND user_id = ?", publicID, userID).First(&record).Error; err != nil {
			return translateError(err)
		}
		if err := tx.Where("artifact_id = ? AND user_id = ?", record.ID, userID).
			Delete(&model.ArtifactShare{}).Error; err != nil {
			return translateError(err)
		}
		return translateError(tx.Delete(&model.Artifact{}, record.ID).Error)
	})
}

func (r *Repo) ReplaceActiveArtifactShare(ctx context.Context, userID uint, artifactID uint, share *domainartifact.ArtifactShare) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ArtifactShare{}).
			Where("artifact_id = ? AND user_id = ? AND status = 'active'", artifactID, userID).
			Updates(map[string]interface{}{"status": "revoked", "revoked_at": gorm.Expr("NOW()")}).Error; err != nil {
			return translateError(err)
		}
		record := model.ArtifactShare{
			ShareID:       share.ShareID,
			ArtifactID:    artifactID,
			UserID:        userID,
			TitleSnapshot: share.TitleSnapshot,
			Status:        "active",
		}
		if err := tx.Create(&record).Error; err != nil {
			return translateError(err)
		}
		share.ID = record.ID
		return nil
	})
}

func (r *Repo) GetActiveArtifactShare(ctx context.Context, userID uint, artifactID uint) (*domainartifact.ArtifactShare, error) {
	var record model.ArtifactShare
	err := r.db.WithContext(ctx).
		Where("artifact_id = ? AND user_id = ? AND status = 'active'", artifactID, userID).
		Order("id DESC").First(&record).Error
	if err != nil {
		return nil, translateError(err)
	}
	return &domainartifact.ArtifactShare{
		ID:            record.ID,
		ShareID:       record.ShareID,
		ArtifactID:    record.ArtifactID,
		UserID:        record.UserID,
		TitleSnapshot: record.TitleSnapshot,
		Status:        record.Status,
		RevokedAt:     record.RevokedAt,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
	}, nil
}

func (r *Repo) GetArtifactShareByShareID(ctx context.Context, shareID string) (*domainartifact.ArtifactShare, error) {
	var record model.ArtifactShare
	err := r.db.WithContext(ctx).Where("share_id = ?", shareID).First(&record).Error
	if err != nil {
		return nil, translateError(err)
	}
	return &domainartifact.ArtifactShare{
		ID:            record.ID,
		ShareID:       record.ShareID,
		ArtifactID:    record.ArtifactID,
		UserID:        record.UserID,
		TitleSnapshot: record.TitleSnapshot,
		Status:        record.Status,
		RevokedAt:     record.RevokedAt,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
	}, nil
}

func (r *Repo) RevokeArtifactShare(ctx context.Context, userID uint, shareID string) error {
	return translateError(r.db.WithContext(ctx).Model(&model.ArtifactShare{}).
		Where("share_id = ? AND user_id = ? AND status = 'active'", shareID, userID).
		Updates(map[string]interface{}{"status": "revoked", "revoked_at": gorm.Expr("NOW()")}).Error)
}
