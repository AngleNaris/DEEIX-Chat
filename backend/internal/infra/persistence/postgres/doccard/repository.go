package doccard

import (
	"context"
	"encoding/json"
	"strings"

	domaindoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/doccard"
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

// Repo 聚合文档卡片域数据访问。
type Repo struct {
	db *gorm.DB
}

// NewRepo 创建仓储。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func encodeKeywords(keywords []string) string {
	if len(keywords) == 0 {
		return "[]"
	}
	data, err := json.Marshal(keywords)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func decodeKeywords(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var keywords []string
	if err := json.Unmarshal([]byte(raw), &keywords); err != nil {
		return nil
	}
	return keywords
}

func toDomain(item model.DocCard) domaindoccard.DocCard {
	return domaindoccard.DocCard{
		ID:           item.ID,
		CardPublicID: item.CardPublicID,
		UserID:       item.UserID,
		Title:        item.Title,
		Content:      item.Content,
		Keywords:     decodeKeywords(item.KeywordsJSON),
		Enabled:      item.Enabled,
		UpdatedBy:    item.UpdatedBy,
		CreatedAt:    item.CreatedAt,
		UpdatedAt:    item.UpdatedAt,
	}
}

// UpsertDocCard 按 user_id + card_public_id 更新或插入。
func (r *Repo) UpsertDocCard(ctx context.Context, item *domaindoccard.DocCard) error {
	if item == nil {
		return nil
	}
	var existing model.DocCard
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND card_public_id = ?", item.UserID, item.CardPublicID).
		First(&existing).Error
	if err == nil {
		return translateError(r.db.WithContext(ctx).Model(&existing).Updates(map[string]interface{}{
			"title":         item.Title,
			"content":       item.Content,
			"keywords_json": encodeKeywords(item.Keywords),
			"enabled":       item.Enabled,
			"updated_by":    item.UpdatedBy,
		}).Error)
	}
	if dberror.IsRecordNotFound(err) {
		record := model.DocCard{
			CardPublicID: item.CardPublicID,
			UserID:       item.UserID,
			Title:        item.Title,
			Content:      item.Content,
			KeywordsJSON: encodeKeywords(item.Keywords),
			Enabled:      item.Enabled,
			UpdatedBy:    item.UpdatedBy,
		}
		return translateError(r.db.WithContext(ctx).Create(&record).Error)
	}
	return translateError(err)
}

func (r *Repo) DeleteDocCard(ctx context.Context, userID uint, publicID string) error {
	return translateError(r.db.WithContext(ctx).
		Where("user_id = ? AND card_public_id = ?", userID, publicID).
		Delete(&model.DocCard{}).Error)
}

func (r *Repo) ListDocCards(ctx context.Context, userID uint) ([]domaindoccard.DocCard, error) {
	records := make([]model.DocCard, 0)
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Find(&records).Error; err != nil {
		return nil, translateError(err)
	}
	results := make([]domaindoccard.DocCard, 0, len(records))
	for _, item := range records {
		results = append(results, toDomain(item))
	}
	return results, nil
}

func (r *Repo) GetDocCardByPublicID(ctx context.Context, userID uint, publicID string) (*domaindoccard.DocCard, error) {
	var record model.DocCard
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND card_public_id = ?", userID, publicID).
		First(&record).Error
	if err != nil {
		return nil, translateError(err)
	}
	domain := toDomain(record)
	return &domain, nil
}
