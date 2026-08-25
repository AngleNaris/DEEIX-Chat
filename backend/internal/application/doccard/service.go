// Package doccard 提供文档卡片（lorebook 式）的管理：用户或 AI 创建/更新
// 关键字触发文档，发送消息时命中关键字即注入上下文。
package doccard

import (
	"context"
	"errors"
	"strings"

	domaindoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/doccard"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/conv"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/google/uuid"
)

// ErrCardNotFound 卡片不存在。
var ErrCardNotFound = errors.New("doc card not found")

// 长度限制。
const (
	MaxTitleLen   = 128
	MaxContentLen = 20000
	MaxKeywords   = 20
)

// UpsertInput 创建/更新卡片的输入。
type UpsertInput struct {
	Category  string
	ProjectID *uint
	RoleID    *uint
	Title     string
	Content   string
	Keywords  []string
	Enabled   *bool // nil 保持原值（更新时）
}

// CardView 卡片视图。
type CardView struct {
	CardPublicID string   `json:"card_id"`
	Category     string   `json:"category"`
	ProjectID    *uint    `json:"project_id,omitempty"`
	RoleID       *uint    `json:"role_id,omitempty"`
	Title        string   `json:"title"`
	Content      string   `json:"content"`
	Keywords     []string `json:"keywords"`
	Enabled      bool     `json:"enabled"`
	UpdatedBy    string   `json:"updated_by"`
	UpdatedAt    string   `json:"updated_at"`
}

// Service 封装文档卡片业务能力。
type Service struct {
	repo             repository.DocCardRepository
	cacheInvalidator func(userID uint)
}

// NewService 创建服务。
func NewService(repo repository.DocCardRepository) *Service {
	return &Service{repo: repo}
}

// SetCacheInvalidator 注入缓存失效回调。卡片写入/删除成功后调用，通知上层清除本地缓存。
func (s *Service) SetCacheInvalidator(fn func(userID uint)) {
	s.cacheInvalidator = fn
}

func (s *Service) invalidateCache(userID uint) {
	if s.cacheInvalidator != nil && userID != 0 {
		s.cacheInvalidator(userID)
	}
}

// normalizeKeywords 清洗关键字：去空白、去空项、去重、上限 20。
func normalizeKeywords(raw []string) []string {
	seen := make(map[string]bool, len(raw))
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		kw := strings.TrimSpace(item)
		if kw == "" || seen[kw] {
			continue
		}
		seen[kw] = true
		result = append(result, kw)
		if len(result) >= MaxKeywords {
			break
		}
	}
	return result
}

// UpsertDocCard 创建或更新卡片。
// publicID 为空时新建；非空时更新已有卡片（不存在返回 ErrCardNotFound）。
func (s *Service) UpsertDocCard(ctx context.Context, userID uint, publicID string, input UpsertInput, updatedBy string) (*domaindoccard.DocCard, error) {
	publicID = strings.TrimSpace(publicID)
	enabled := true
	if publicID != "" {
		existing, err := s.repo.GetDocCardByPublicID(ctx, userID, publicID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, ErrCardNotFound
			}
			return nil, err
		}
		enabled = existing.Enabled
	}
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if strings.TrimSpace(input.Title) == "" {
		input.Title = "untitled"
	}
	item := &domaindoccard.DocCard{
		UserID:    userID,
		Category:  strings.TrimSpace(input.Category),
		ProjectID: input.ProjectID,
		RoleID:    input.RoleID,
		Title:     strings.TrimSpace(input.Title),
		Content:   strings.TrimSpace(input.Content),
		Keywords:  normalizeKeywords(input.Keywords),
		UpdatedBy: strings.TrimSpace(updatedBy),
		Enabled:   enabled,
	}
	if item.UpdatedBy == "" {
		item.UpdatedBy = "user"
	}
	if publicID == "" {
		item.CardPublicID = conv.NormalizePublicID(uuid.NewString())
	} else {
		item.CardPublicID = publicID
	}
	if err := s.repo.UpsertDocCard(ctx, item); err != nil {
		return nil, err
	}
	s.invalidateCache(userID)
	return item, nil
}

// ListDocCards 列出用户全部卡片。
func (s *Service) ListDocCards(ctx context.Context, userID uint) ([]CardView, error) {
	items, err := s.repo.ListDocCards(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]CardView, 0, len(items))
	for _, item := range items {
		views = append(views, CardView{
			CardPublicID: item.CardPublicID,
			Category:     item.Category,
			ProjectID:    item.ProjectID,
			RoleID:       item.RoleID,
			Title:        item.Title,
			Content:      item.Content,
			Keywords:     item.Keywords,
			Enabled:      item.Enabled,
			UpdatedBy:    item.UpdatedBy,
			UpdatedAt:    item.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return views, nil
}

// DeleteDocCard 删除卡片。
func (s *Service) DeleteDocCard(ctx context.Context, userID uint, publicID string) error {
	if err := s.repo.DeleteDocCard(ctx, userID, publicID); err != nil {
		return err
	}
	s.invalidateCache(userID)
	return nil
}

// GetDocCard 返回单张卡片。
func (s *Service) GetDocCard(ctx context.Context, userID uint, publicID string) (*domaindoccard.DocCard, error) {
	return s.repo.GetDocCardByPublicID(ctx, userID, publicID)
}
