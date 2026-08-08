// Package dynamicprompt 提供动态提示词管理：用户创建的命名脚本/文本片段，
// 提示词中以 {{script: name}} 引用，发送时展开。
package dynamicprompt

import (
	"context"
	"errors"
	"strings"

	domaindynamicprompt "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/dynamicprompt"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/conv"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/google/uuid"
)

// ErrPromptNotFound 动态提示词不存在。
var ErrPromptNotFound = errors.New("dynamic prompt not found")

// 长度限制。
const (
	MaxNameLen    = 64
	MaxContentLen = 20000
)

// UpsertInput 创建/更新动态提示词的输入。
type UpsertInput struct {
	Name    string
	Kind    string
	Content string
	Enabled *bool // nil 保持原值（更新时）
}

// PromptView 动态提示词视图。
type PromptView struct {
	PublicID  string `json:"prompt_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Content   string `json:"content"`
	Enabled   bool   `json:"enabled"`
	UpdatedBy string `json:"updated_by"`
	UpdatedAt string `json:"updated_at"`
}

// Service 封装动态提示词业务能力。
type Service struct {
	repo             repository.DynamicPromptRepository
	cacheInvalidator func(userID uint)
}

// NewService 创建服务。
func NewService(repo repository.DynamicPromptRepository) *Service {
	return &Service{repo: repo}
}

// SetCacheInvalidator 注入缓存失效回调。
func (s *Service) SetCacheInvalidator(fn func(userID uint)) {
	s.cacheInvalidator = fn
}

func (s *Service) invalidateCache(userID uint) {
	if s.cacheInvalidator != nil && userID != 0 {
		s.cacheInvalidator(userID)
	}
}

// UpsertDynamicPrompt 创建或更新动态提示词。
// publicID 为空时新建；非空时更新已有（不存在返回 ErrPromptNotFound）。
func (s *Service) UpsertDynamicPrompt(ctx context.Context, userID uint, publicID string, input UpsertInput, updatedBy string) (*domaindynamicprompt.DynamicPrompt, error) {
	publicID = strings.TrimSpace(publicID)
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" || !domaindynamicprompt.ValidKind(kind) {
		kind = domaindynamicprompt.KindJS
	}
	item := &domaindynamicprompt.DynamicPrompt{
		UserID:    userID,
		Name:      name,
		Kind:      kind,
		Content:   strings.TrimSpace(input.Content),
		Enabled:   true,
		UpdatedBy: strings.TrimSpace(updatedBy),
	}
	if item.UpdatedBy == "" {
		item.UpdatedBy = "user"
	}
	if publicID == "" {
		item.PublicID = conv.NormalizePublicID(uuid.NewString())
	} else {
		item.PublicID = publicID
	}
	if input.Enabled != nil {
		item.Enabled = *input.Enabled
	}
	if err := s.repo.UpsertDynamicPrompt(ctx, item); err != nil {
		return nil, err
	}
	s.invalidateCache(userID)
	return item, nil
}

// ListDynamicPrompts 列出用户全部动态提示词。
func (s *Service) ListDynamicPrompts(ctx context.Context, userID uint) ([]PromptView, error) {
	items, err := s.repo.ListDynamicPrompts(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]PromptView, 0, len(items))
	for _, item := range items {
		views = append(views, PromptView{
			PublicID:  item.PublicID,
			Name:      item.Name,
			Kind:      item.Kind,
			Content:   item.Content,
			Enabled:   item.Enabled,
			UpdatedBy: item.UpdatedBy,
			UpdatedAt: item.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return views, nil
}

// DeleteDynamicPrompt 删除动态提示词。
func (s *Service) DeleteDynamicPrompt(ctx context.Context, userID uint, publicID string) error {
	if err := s.repo.DeleteDynamicPrompt(ctx, userID, publicID); err != nil {
		return err
	}
	s.invalidateCache(userID)
	return nil
}
