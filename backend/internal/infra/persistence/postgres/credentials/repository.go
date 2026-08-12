package credentials

import (
	"context"
	"time"

	domaincredentials "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/credentials"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/dberror"
	models "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
)

// translateError 将 gorm 底层错误统一映射为仓储语义错误。
func translateError(err error) error {
	if err == nil {
		return nil
	}
	if dberror.IsRecordNotFound(err) {
		return repository.ErrNotFound
	}
	if dberror.IsUniqueConstraint(err) {
		return repository.ErrDuplicate
	}
	return err
}

// Repo 用户凭据数据访问。
type Repo struct {
	db *gorm.DB
}

// NewRepo 创建仓储。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func toCredentialModel(item *domaincredentials.Credential) models.Credential {
	return models.Credential{
		UserID:      item.UserID,
		PublicID:    item.PublicID,
		Name:        item.Name,
		Type:        item.Type,
		Description: item.Description,
		SecretEnc:   item.SecretEnc,
		MetaJSON:    item.MetaJSON,
	}
}

func toCredentialDomain(entity models.Credential) domaincredentials.Credential {
	return domaincredentials.Credential{
		ID:          entity.ID,
		UserID:      entity.UserID,
		PublicID:    entity.PublicID,
		Name:        entity.Name,
		Type:        entity.Type,
		Description: entity.Description,
		SecretEnc:   entity.SecretEnc,
		MetaJSON:    entity.MetaJSON,
		CreatedAt:   entity.CreatedAt,
		UpdatedAt:   entity.UpdatedAt,
	}
}

// CreateCredential 创建凭据。
func (r *Repo) CreateCredential(ctx context.Context, item *domaincredentials.Credential) error {
	entity := toCredentialModel(item)
	if err := r.db.WithContext(ctx).Create(&entity).Error; err != nil {
		return translateError(err)
	}
	item.ID = entity.ID
	item.CreatedAt = entity.CreatedAt
	item.UpdatedAt = entity.UpdatedAt
	return nil
}

// UpdateCredential 更新凭据的局部字段（patch 中 nil 字段不修改；SecretEnc 非 nil 且非空才更新）。
func (r *Repo) UpdateCredential(ctx context.Context, userID uint, publicID string, patch *domaincredentials.CredentialPatch) error {
	fields := map[string]interface{}{"updated_at": time.Now().UTC()}
	if patch.Name != nil {
		fields["name"] = *patch.Name
	}
	if patch.Type != nil {
		fields["type"] = *patch.Type
	}
	if patch.Description != nil {
		fields["description"] = *patch.Description
	}
	if patch.SecretEnc != nil && *patch.SecretEnc != "" {
		fields["secret_enc"] = *patch.SecretEnc
	}
	if patch.MetaJSON != nil {
		fields["meta_json"] = *patch.MetaJSON
	}
	result := r.db.WithContext(ctx).
		Model(&models.Credential{}).
		Where("user_id = ? AND public_id = ?", userID, publicID).
		Updates(fields)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// DeleteCredential 删除凭据（软删除）。
func (r *Repo) DeleteCredential(ctx context.Context, userID uint, publicID string) error {
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND public_id = ?", userID, publicID).
		Delete(&models.Credential{})
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// ListCredentials 列出用户全部凭据。
func (r *Repo) ListCredentials(ctx context.Context, userID uint) ([]domaincredentials.Credential, error) {
	var rows []models.Credential
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	items := make([]domaincredentials.Credential, 0, len(rows))
	for _, row := range rows {
		items = append(items, toCredentialDomain(row))
	}
	return items, nil
}

// GetCredentialByPublicID 查询单个凭据。
func (r *Repo) GetCredentialByPublicID(ctx context.Context, userID uint, publicID string) (*domaincredentials.Credential, error) {
	var row models.Credential
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND public_id = ?", userID, publicID).
		First(&row).Error; err != nil {
		return nil, translateError(err)
	}
	item := toCredentialDomain(row)
	return &item, nil
}

// GetCredentialByName 按名称查询凭据。
func (r *Repo) GetCredentialByName(ctx context.Context, userID uint, name string) (*domaincredentials.Credential, error) {
	var row models.Credential
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND name = ?", userID, name).
		First(&row).Error; err != nil {
		return nil, translateError(err)
	}
	item := toCredentialDomain(row)
	return &item, nil
}
