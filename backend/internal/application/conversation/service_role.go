package conversation

import (
	"context"
	"errors"
	"strings"

	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/google/uuid"
)

const (
	conversationRoleNameMaxChars         = 80
	conversationRoleDescriptionMaxChars  = 255
	conversationRoleSystemPromptMaxChars = 12000
	conversationRoleModelMaxChars        = 128
	conversationRoleProviderMaxChars     = 32
	conversationRoleMetaMaxChars         = 32
	conversationRoleGroupNameMaxChars    = 80
)

// ConversationRoleInput 定义新建角色输入。
type ConversationRoleInput struct {
	Name              string
	Description       string
	SystemPrompt      string
	Model             string
	Provider          string
	MCPDefaultMode    string
	DefaultMCPToolIDs []uint
	DefaultSkillIDs   []uint
	Color             string
	Icon              string
	GroupName         string
}

// ConversationRolePatchInput 定义角色局部更新输入。
type ConversationRolePatchInput struct {
	Name              *string
	Description       *string
	SystemPrompt      *string
	Model             *string
	Provider          *string
	MCPDefaultMode    *string
	DefaultMCPToolIDs *[]uint
	DefaultSkillIDs   *[]uint
	Color             *string
	Icon              *string
	GroupName         *string
	Status            *string
}

// CreateConversationRole 创建当前用户的角色。
func (s *Service) CreateConversationRole(ctx context.Context, userID uint, input ConversationRoleInput) (*model.ConversationRole, error) {
	normalized, err := normalizeConversationRoleInput(input)
	if err != nil {
		return nil, err
	}
	item := &model.ConversationRole{
		UserID:            userID,
		PublicID:          normalizePublicID(uuid.NewString()),
		Name:              normalized.Name,
		Description:       normalized.Description,
		SystemPrompt:      normalized.SystemPrompt,
		Model:             normalized.Model,
		Provider:          normalized.Provider,
		MCPDefaultMode:    normalized.MCPDefaultMode,
		DefaultMCPToolIDs: normalized.DefaultMCPToolIDs,
		DefaultSkillIDs:   normalized.DefaultSkillIDs,
		Color:             normalized.Color,
		Icon:              normalized.Icon,
		GroupName:         normalized.GroupName,
		Status:            "active",
	}
	if err = s.repo.CreateConversationRole(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

// ListConversationRoles 查询当前用户角色。
func (s *Service) ListConversationRoles(ctx context.Context, userID uint, statusFilter string) ([]model.ConversationRole, error) {
	return s.repo.ListConversationRoles(ctx, userID, normalizeConversationProjectStatusFilter(statusFilter))
}

// GetConversationRole 查询当前用户单个角色。
func (s *Service) GetConversationRole(ctx context.Context, userID uint, publicID string) (*model.ConversationRole, error) {
	return s.repo.GetConversationRoleByPublicID(ctx, userID, strings.TrimSpace(publicID))
}

// UpdateConversationRole 更新当前用户角色。
func (s *Service) UpdateConversationRole(ctx context.Context, userID uint, publicID string, patch ConversationRolePatchInput) (*model.ConversationRole, error) {
	normalized, err := normalizeConversationRolePatchInput(patch)
	if err != nil {
		return nil, err
	}
	domainPatch := model.ConversationRolePatch{
		Name:              normalized.Name,
		Description:       normalized.Description,
		SystemPrompt:      normalized.SystemPrompt,
		Model:             normalized.Model,
		Provider:          normalized.Provider,
		MCPDefaultMode:    normalized.MCPDefaultMode,
		DefaultMCPToolIDs: normalized.DefaultMCPToolIDs,
		DefaultSkillIDs:   normalized.DefaultSkillIDs,
		Color:             normalized.Color,
		Icon:              normalized.Icon,
		GroupName:         normalized.GroupName,
		Status:            normalized.Status,
	}
	return s.repo.UpdateConversationRoleByPublicID(ctx, userID, strings.TrimSpace(publicID), domainPatch)
}

// DeleteConversationRole 删除当前用户角色。
// 角色仍被未移除的群组成员引用时拒绝删除，避免群组运行快照引用悬空（§18 删除保护）。
func (s *Service) DeleteConversationRole(ctx context.Context, userID uint, publicID string) error {
	role, err := s.repo.GetConversationRoleByPublicID(ctx, userID, strings.TrimSpace(publicID))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrConversationProjectNotFound
		}
		return err
	}
	if s.agentGroupRepo != nil {
		count, err := s.agentGroupRepo.CountAgentGroupReferencesByRole(ctx, role.ID)
		if err != nil {
			return err
		}
		if count > 0 {
			return ErrConversationRoleInUseByAgentGroup
		}
	}
	return s.repo.DeleteConversationRoleByPublicID(ctx, userID, strings.TrimSpace(publicID))
}

// ReorderConversationRoles 更新当前用户角色展示顺序。
func (s *Service) ReorderConversationRoles(ctx context.Context, userID uint, publicIDs []string) error {
	return s.repo.ReorderConversationRoles(ctx, userID, publicIDs)
}

func normalizeConversationRoleInput(input ConversationRoleInput) (ConversationRoleInput, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return input, ErrInvalidConversationProject
	}
	if len([]rune(name)) > conversationRoleNameMaxChars {
		return input, ErrInvalidConversationProject
	}
	mcpDefaultMode := strings.TrimSpace(input.MCPDefaultMode)
	if mcpDefaultMode != model.ConversationProjectMCPDefaultModeCustom {
		mcpDefaultMode = model.ConversationProjectMCPDefaultModeInherit
	}
	return ConversationRoleInput{
		Name:              name,
		Description:       truncateRunes(strings.TrimSpace(input.Description), conversationRoleDescriptionMaxChars),
		SystemPrompt:      truncateRunes(input.SystemPrompt, conversationRoleSystemPromptMaxChars),
		Model:             truncateRunes(strings.TrimSpace(input.Model), conversationRoleModelMaxChars),
		Provider:          truncateRunes(strings.TrimSpace(input.Provider), conversationRoleProviderMaxChars),
		MCPDefaultMode:    mcpDefaultMode,
		DefaultMCPToolIDs: dedupeIDs(input.DefaultMCPToolIDs),
		DefaultSkillIDs:   dedupeIDs(input.DefaultSkillIDs),
		Color:             truncateRunes(strings.TrimSpace(input.Color), conversationRoleMetaMaxChars),
		Icon:              truncateRunes(strings.TrimSpace(input.Icon), conversationRoleMetaMaxChars),
		GroupName:         truncateRunes(strings.TrimSpace(input.GroupName), conversationRoleGroupNameMaxChars),
	}, nil
}

func normalizeConversationRolePatchInput(input ConversationRolePatchInput) (ConversationRolePatchInput, error) {
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len([]rune(name)) > conversationRoleNameMaxChars {
			return input, ErrInvalidConversationProject
		}
		input.Name = &name
	}
	if input.Description != nil {
		value := truncateRunes(strings.TrimSpace(*input.Description), conversationRoleDescriptionMaxChars)
		input.Description = &value
	}
	if input.SystemPrompt != nil {
		value := truncateRunes(*input.SystemPrompt, conversationRoleSystemPromptMaxChars)
		input.SystemPrompt = &value
	}
	if input.Model != nil {
		value := truncateRunes(strings.TrimSpace(*input.Model), conversationRoleModelMaxChars)
		input.Model = &value
	}
	if input.Provider != nil {
		value := truncateRunes(strings.TrimSpace(*input.Provider), conversationRoleProviderMaxChars)
		input.Provider = &value
	}
	if input.MCPDefaultMode != nil {
		mode := strings.TrimSpace(*input.MCPDefaultMode)
		if mode != model.ConversationProjectMCPDefaultModeCustom {
			mode = model.ConversationProjectMCPDefaultModeInherit
		}
		input.MCPDefaultMode = &mode
	}
	if input.Color != nil {
		value := truncateRunes(strings.TrimSpace(*input.Color), conversationRoleMetaMaxChars)
		input.Color = &value
	}
	if input.Icon != nil {
		value := truncateRunes(strings.TrimSpace(*input.Icon), conversationRoleMetaMaxChars)
		input.Icon = &value
	}
	if input.GroupName != nil {
		value := truncateRunes(strings.TrimSpace(*input.GroupName), conversationRoleGroupNameMaxChars)
		input.GroupName = &value
	}
	if input.DefaultMCPToolIDs != nil {
		value := dedupeIDs(*input.DefaultMCPToolIDs)
		input.DefaultMCPToolIDs = &value
	}
	if input.DefaultSkillIDs != nil {
		value := dedupeIDs(*input.DefaultSkillIDs)
		input.DefaultSkillIDs = &value
	}
	return input, nil
}

func dedupeIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	result := make([]uint, 0, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
