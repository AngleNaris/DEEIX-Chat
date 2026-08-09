package conversation

import (
	"context"
	"strings"
	"time"

	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	models "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
)

// CreateConversationRole 创建角色。
func (r *Repo) CreateConversationRole(ctx context.Context, item *domainconversation.ConversationRole) error {
	entity := toConversationRoleModel(item)
	mcpToolIDs := append([]uint(nil), item.DefaultMCPToolIDs...)
	skillIDs := append([]uint(nil), item.DefaultSkillIDs...)
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&entity).Error; err != nil {
			return err
		}
		if err := replaceConversationRoleMCPTools(tx, entity.ID, mcpToolIDs); err != nil {
			return err
		}
		return replaceConversationRoleSkills(tx, entity.ID, skillIDs)
	}); err != nil {
		return translateError(err)
	}
	*item = toConversationRoleDomain(entity)
	item.DefaultMCPToolIDs = mcpToolIDs
	item.DefaultSkillIDs = skillIDs
	return nil
}

// ListConversationRoles 查询用户角色。
func (r *Repo) ListConversationRoles(ctx context.Context, userID uint, statusFilter string) ([]domainconversation.ConversationRole, error) {
	items := make([]models.ConversationRole, 0)
	query := r.db.WithContext(ctx).
		Where("user_id = ?", userID)
	switch strings.TrimSpace(statusFilter) {
	case "archived":
		query = query.Where("status = ?", "archived")
	case "all":
		// 保留全部状态。
	default:
		query = query.Where("status = ?", "active")
	}
	if err := query.
		Order("pinned_at IS NULL ASC").
		Order("pinned_at ASC").
		Order("sort_order ASC").
		Order("id DESC").
		Find(&items).Error; err != nil {
		return nil, translateError(err)
	}
	results := toConversationRoleDomains(items)
	if err := r.hydrateConversationRoleDefaults(ctx, results); err != nil {
		return nil, err
	}
	return results, nil
}

// GetConversationRoleByPublicID 查询用户角色。
func (r *Repo) GetConversationRoleByPublicID(ctx context.Context, userID uint, publicID string) (*domainconversation.ConversationRole, error) {
	var item models.ConversationRole
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND public_id = ?", userID, strings.TrimSpace(publicID)).
		First(&item).Error; err != nil {
		return nil, translateError(err)
	}
	result := toConversationRoleDomain(item)
	roles := []domainconversation.ConversationRole{result}
	if err := r.hydrateConversationRoleDefaults(ctx, roles); err != nil {
		return nil, err
	}
	result = roles[0]
	return &result, nil
}

// UpdateConversationRoleByPublicID 更新角色元信息。
func (r *Repo) UpdateConversationRoleByPublicID(
	ctx context.Context,
	userID uint,
	publicID string,
	patch domainconversation.ConversationRolePatch,
) (*domainconversation.ConversationRole, error) {
	updates := make(map[string]interface{})
	if patch.Name != nil {
		updates["name"] = *patch.Name
	}
	if patch.Description != nil {
		updates["description"] = *patch.Description
	}
	if patch.SystemPrompt != nil {
		updates["system_prompt"] = *patch.SystemPrompt
	}
	if patch.Model != nil {
		updates["model"] = *patch.Model
	}
	if patch.Provider != nil {
		updates["provider"] = *patch.Provider
	}
	if patch.ReasoningEffort != nil {
		updates["reasoning_effort"] = *patch.ReasoningEffort
	}
	if patch.MCPDefaultMode != nil {
		updates["mcp_default_mode"] = *patch.MCPDefaultMode
	}
	if patch.Color != nil {
		updates["color"] = *patch.Color
	}
	if patch.Icon != nil {
		updates["icon"] = *patch.Icon
	}
	if patch.GroupName != nil {
		updates["group_name"] = *patch.GroupName
	}
	if patch.Pinned != nil {
		if *patch.Pinned {
			updates["pinned_at"] = time.Now()
		} else {
			updates["pinned_at"] = nil
		}
	}
	if patch.Status != nil {
		updates["status"] = *patch.Status
	}
	if len(updates) == 0 {
		return r.GetConversationRoleByPublicID(ctx, userID, publicID)
	}
	var entity models.ConversationRole
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND public_id = ?", userID, strings.TrimSpace(publicID)).First(&entity).Error; err != nil {
			return err
		}
		result := tx.Model(&models.ConversationRole{}).
			Where("user_id = ? AND public_id = ?", userID, strings.TrimSpace(publicID)).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return repository.ErrNotFound
		}
		if patch.DefaultMCPToolIDs != nil {
			if err := replaceConversationRoleMCPTools(tx, entity.ID, *patch.DefaultMCPToolIDs); err != nil {
				return err
			}
		}
		if patch.DefaultSkillIDs != nil {
			if err := replaceConversationRoleSkills(tx, entity.ID, *patch.DefaultSkillIDs); err != nil {
				return err
			}
		}
		return tx.Where("user_id = ? AND public_id = ?", userID, strings.TrimSpace(publicID)).First(&entity).Error
	})
	if err != nil {
		return nil, translateError(err)
	}
	result := toConversationRoleDomain(entity)
	roles := []domainconversation.ConversationRole{result}
	if err := r.hydrateConversationRoleDefaults(ctx, roles); err != nil {
		return nil, err
	}
	result = roles[0]
	return &result, nil
}

// DeleteConversationRoleByPublicID 删除角色(级联删除关联)。
func (r *Repo) DeleteConversationRoleByPublicID(ctx context.Context, userID uint, publicID string) error {
	return translateError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entity models.ConversationRole
		if err := tx.Where("user_id = ? AND public_id = ?", userID, strings.TrimSpace(publicID)).First(&entity).Error; err != nil {
			return err
		}
		if err := tx.Where("role_id = ?", entity.ID).Delete(&models.ConversationRoleMCPTool{}).Error; err != nil {
			return err
		}
		if err := tx.Where("role_id = ?", entity.ID).Delete(&models.ConversationRoleSkill{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.ConversationRole{}, entity.ID).Error
	}))
}

// ReorderConversationRoles 更新角色展示顺序。
func (r *Repo) ReorderConversationRoles(ctx context.Context, userID uint, publicIDs []string) error {
	if len(publicIDs) == 0 {
		return nil
	}
	return translateError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for index, publicID := range publicIDs {
			result := tx.Model(&models.ConversationRole{}).
				Where("user_id = ? AND public_id = ?", userID, strings.TrimSpace(publicID)).
				Update("sort_order", index+1)
			if result.Error != nil {
				return translateError(result.Error)
			}
			if result.RowsAffected == 0 {
				return repository.ErrNotFound
			}
		}
		return nil
	}))
}

func toConversationRoleDomain(item models.ConversationRole) domainconversation.ConversationRole {
	mcpDefaultMode := strings.TrimSpace(item.MCPDefaultMode)
	if mcpDefaultMode != domainconversation.ConversationProjectMCPDefaultModeCustom {
		mcpDefaultMode = domainconversation.ConversationProjectMCPDefaultModeInherit
	}
	return domainconversation.ConversationRole{
		ID:              item.ID,
		UserID:          item.UserID,
		PublicID:        item.PublicID,
		Name:            item.Name,
		Description:     item.Description,
		SystemPrompt:    item.SystemPrompt,
		Model:           item.Model,
		Provider:        item.Provider,
		ReasoningEffort: item.ReasoningEffort,
		MCPDefaultMode:  mcpDefaultMode,
		Color:           item.Color,
		Icon:            item.Icon,
		GroupName:       item.GroupName,
		SortOrder:       item.SortOrder,
		PinnedAt:        item.PinnedAt,
		Status:          item.Status,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
}

func toConversationRoleDomains(items []models.ConversationRole) []domainconversation.ConversationRole {
	results := make([]domainconversation.ConversationRole, 0, len(items))
	for _, item := range items {
		results = append(results, toConversationRoleDomain(item))
	}
	return results
}

func toConversationRoleModel(item *domainconversation.ConversationRole) models.ConversationRole {
	if item == nil {
		return models.ConversationRole{}
	}
	return models.ConversationRole{
		UserID:          item.UserID,
		PublicID:        item.PublicID,
		Name:            item.Name,
		Description:     item.Description,
		SystemPrompt:    item.SystemPrompt,
		Model:           item.Model,
		Provider:        item.Provider,
		ReasoningEffort: item.ReasoningEffort,
		MCPDefaultMode:  item.MCPDefaultMode,
		Color:           item.Color,
		Icon:            item.Icon,
		GroupName:       item.GroupName,
		SortOrder:       item.SortOrder,
		PinnedAt:        item.PinnedAt,
		Status:          item.Status,
	}
}

// hydrateConversationRoleDefaults 批量装载角色默认 MCP 与 Skill 关联。
func (r *Repo) hydrateConversationRoleDefaults(ctx context.Context, items []domainconversation.ConversationRole) error {
	if len(items) == 0 {
		return nil
	}
	roleIDs := make([]uint, 0, len(items))
	for _, item := range items {
		roleIDs = append(roleIDs, item.ID)
	}

	mcpRows := make([]models.ConversationRoleMCPTool, 0)
	if err := r.db.WithContext(ctx).
		Where("role_id IN ?", roleIDs).
		Order("role_id ASC, sort_order ASC, tool_id ASC").
		Find(&mcpRows).Error; err != nil {
		return translateError(err)
	}
	skillRows := make([]models.ConversationRoleSkill, 0)
	if err := r.db.WithContext(ctx).
		Where("role_id IN ?", roleIDs).
		Order("role_id ASC, sort_order ASC, skill_id ASC").
		Find(&skillRows).Error; err != nil {
		return translateError(err)
	}

	mcpIDsByRole := make(map[uint][]uint, len(items))
	for _, row := range mcpRows {
		mcpIDsByRole[row.RoleID] = append(mcpIDsByRole[row.RoleID], row.ToolID)
	}
	skillIDsByRole := make(map[uint][]uint, len(items))
	for _, row := range skillRows {
		skillIDsByRole[row.RoleID] = append(skillIDsByRole[row.RoleID], row.SkillID)
	}
	for index := range items {
		items[index].DefaultMCPToolIDs = mcpIDsByRole[items[index].ID]
		items[index].DefaultSkillIDs = skillIDsByRole[items[index].ID]
	}
	return nil
}

// replaceConversationRoleMCPTools 在事务内替换角色默认 MCP 工具关联。
func replaceConversationRoleMCPTools(tx *gorm.DB, roleID uint, toolIDs []uint) error {
	if err := tx.Where("role_id = ?", roleID).Delete(&models.ConversationRoleMCPTool{}).Error; err != nil {
		return err
	}
	rows := make([]models.ConversationRoleMCPTool, 0, len(toolIDs))
	for index, toolID := range toolIDs {
		rows = append(rows, models.ConversationRoleMCPTool{RoleID: roleID, ToolID: toolID, SortOrder: index + 1})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}

// replaceConversationRoleSkills 在事务内替换角色默认 Skill 关联。
func replaceConversationRoleSkills(tx *gorm.DB, roleID uint, skillIDs []uint) error {
	if err := tx.Where("role_id = ?", roleID).Delete(&models.ConversationRoleSkill{}).Error; err != nil {
		return err
	}
	rows := make([]models.ConversationRoleSkill, 0, len(skillIDs))
	for index, skillID := range skillIDs {
		rows = append(rows, models.ConversationRoleSkill{RoleID: roleID, SkillID: skillID, SortOrder: index + 1})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}
