package dynamicprompt

import (
	"context"

	domaindynamicprompt "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/dynamicprompt"
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

// Repo 聚合动态提示词域数据访问。
type Repo struct {
	db *gorm.DB
}

// NewRepo 创建仓储。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func toDomain(item model.DynamicPrompt) domaindynamicprompt.DynamicPrompt {
	return domaindynamicprompt.DynamicPrompt{
		ID:        item.ID,
		PublicID:  item.PublicID,
		UserID:    item.UserID,
		Name:      item.Name,
		Kind:      item.Kind,
		Content:   item.Content,
		Enabled:   item.Enabled,
		UpdatedBy: item.UpdatedBy,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

// UpsertDynamicPrompt 按 user_id + public_id 更新或插入。
func (r *Repo) UpsertDynamicPrompt(ctx context.Context, item *domaindynamicprompt.DynamicPrompt) error {
	if item == nil {
		return nil
	}
	var existing model.DynamicPrompt
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND public_id = ?", item.UserID, item.PublicID).
		First(&existing).Error
	if err == nil {
		return translateError(r.db.WithContext(ctx).Model(&existing).Updates(map[string]interface{}{
			"name":       item.Name,
			"kind":       item.Kind,
			"content":    item.Content,
			"enabled":    item.Enabled,
			"updated_by": item.UpdatedBy,
		}).Error)
	}
	if dberror.IsRecordNotFound(err) {
		record := model.DynamicPrompt{
			PublicID:  item.PublicID,
			UserID:    item.UserID,
			Name:      item.Name,
			Kind:      item.Kind,
			Content:   item.Content,
			Enabled:   item.Enabled,
			UpdatedBy: item.UpdatedBy,
		}
		return translateError(r.db.WithContext(ctx).Create(&record).Error)
	}
	return translateError(err)
}

func (r *Repo) DeleteDynamicPrompt(ctx context.Context, userID uint, publicID string) error {
	return translateError(r.db.WithContext(ctx).
		Where("user_id = ? AND public_id = ?", userID, publicID).
		Delete(&model.DynamicPrompt{}).Error)
}

func (r *Repo) ListDynamicPrompts(ctx context.Context, userID uint) ([]domaindynamicprompt.DynamicPrompt, error) {
	records := make([]model.DynamicPrompt, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Find(&records).Error; err != nil {
		return nil, translateError(err)
	}
	results := make([]domaindynamicprompt.DynamicPrompt, 0, len(records))
	for _, item := range records {
		results = append(results, toDomain(item))
	}
	return results, nil
}
