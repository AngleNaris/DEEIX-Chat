package agentgroup

import (
	"context"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	models "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
)

// bumpGroupRevision 递增群组配置版本（版本号驱动快照冻结与运行校验）。
func bumpGroupRevision(tx *gorm.DB, groupID uint) error {
	return tx.Model(&models.AgentGroup{}).Where("id = ?", groupID).
		Updates(map[string]interface{}{
			"revision":   gorm.Expr("revision + 1"),
			"updated_at": now(),
		}).Error
}

// ensureGroupOwnershipTx 校验群组归属（事务内）。
func ensureGroupOwnershipTx(tx *gorm.DB, groupID uint, userID uint) error {
	var count int64
	if err := tx.Model(&models.AgentGroup{}).Where("id = ? AND user_id = ?", groupID, userID).Count(&count).Error; err != nil {
		return translateError(err)
	}
	if count == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// ensureGroupOwnership 校验群组归属。
func (r *Repo) ensureGroupOwnership(ctx context.Context, groupID uint, userID uint) error {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.AgentGroup{}).Where("id = ? AND user_id = ?", groupID, userID).Count(&count).Error; err != nil {
		return translateError(err)
	}
	if count == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// CreateAgentGroupWithSupervisor 在一个事务中创建群组、主管成员和全部工作成员。
func (r *Repo) CreateAgentGroupWithSupervisor(ctx context.Context, group *domainagentgroup.Group, supervisor domainagentgroup.Member, workers []domainagentgroup.Member) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		entity := toGroupModel(group)
		entity.SupervisorMemberID = 0
		if err := tx.Create(&entity).Error; err != nil {
			return translateError(err)
		}
		group.ID = entity.ID

		memberEntity := toMemberModel(supervisor)
		memberEntity.GroupID = entity.ID
		if err := tx.Create(&memberEntity).Error; err != nil {
			return translateError(err)
		}

		for _, worker := range workers {
			workerEntity := toMemberModel(worker)
			workerEntity.GroupID = entity.ID
			if err := tx.Create(&workerEntity).Error; err != nil {
				return translateError(err)
			}
		}

		if err := tx.Model(&models.AgentGroup{}).Where("id = ?", entity.ID).
			Updates(map[string]interface{}{
				"supervisor_member_id": memberEntity.ID,
				"revision":             1,
				"updated_at":           now(),
			}).Error; err != nil {
			return translateError(err)
		}

		group.SupervisorMemberID = memberEntity.ID
		group.Revision = 1
		// 调用方创建后通过 GetAgentGroupByPublicID 获取完整视图。
		return nil
	})
}

// UpdateAgentGroupByPublicID 更新群组元数据并递增配置版本。
func (r *Repo) UpdateAgentGroupByPublicID(ctx context.Context, userID uint, publicID string, patch domainagentgroup.GroupPatch) (*domainagentgroup.Group, error) {
	var existing models.AgentGroup
	if err := r.db.WithContext(ctx).Where("user_id = ? AND public_id = ?", userID, publicID).First(&existing).Error; err != nil {
		return nil, translateError(err)
	}
	fields := map[string]interface{}{
		"revision":   gorm.Expr("revision + 1"),
		"updated_at": now(),
	}
	if patch.Name != nil {
		fields["name"] = *patch.Name
	}
	if patch.Description != nil {
		fields["description"] = *patch.Description
	}
	if patch.CoordinationPrompt != nil {
		fields["coordination_prompt"] = *patch.CoordinationPrompt
	}
	if patch.SortOrder != nil {
		fields["sort_order"] = *patch.SortOrder
	}
	if patch.Status != nil {
		fields["status"] = *patch.Status
	}
	if err := r.db.WithContext(ctx).Model(&models.AgentGroup{}).Where("id = ?", existing.ID).Updates(fields).Error; err != nil {
		return nil, translateError(err)
	}
	return r.GetAgentGroupByPublicID(ctx, userID, publicID)
}

// DeleteAgentGroupByPublicID 硬删除群组及其成员（调用方须先保证无会话/运行历史）。
func (r *Repo) DeleteAgentGroupByPublicID(ctx context.Context, userID uint, publicID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var group models.AgentGroup
		if err := tx.Where("user_id = ? AND public_id = ?", userID, publicID).First(&group).Error; err != nil {
			return translateError(err)
		}
		if err := tx.Where("group_id = ?", group.ID).Delete(&models.AgentGroupMember{}).Error; err != nil {
			return translateError(err)
		}
		if err := tx.Where("id = ?", group.ID).Delete(&models.AgentGroup{}).Error; err != nil {
			return translateError(err)
		}
		return nil
	})
}

// AddAgentGroupMember 添加工作成员并递增群组配置版本。
func (r *Repo) AddAgentGroupMember(ctx context.Context, groupID uint, member domainagentgroup.Member) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		entity := toMemberModel(member)
		entity.GroupID = groupID
		if err := tx.Create(&entity).Error; err != nil {
			return translateError(err)
		}
		return bumpGroupRevision(tx, groupID)
	})
}

// UpdateAgentGroupMemberByPublicID 更新成员并递增群组配置版本。
func (r *Repo) UpdateAgentGroupMemberByPublicID(ctx context.Context, groupID uint, userID uint, publicID string, patch domainagentgroup.MemberPatch) (*domainagentgroup.Member, error) {
	if err := r.ensureGroupOwnership(ctx, groupID, userID); err != nil {
		return nil, err
	}
	var existing models.AgentGroupMember
	if err := r.db.WithContext(ctx).Where("group_id = ? AND public_id = ?", groupID, publicID).First(&existing).Error; err != nil {
		return nil, translateError(err)
	}
	fields := map[string]interface{}{"updated_at": now()}
	if patch.Enabled != nil {
		fields["enabled"] = *patch.Enabled
	}
	if patch.ModelOverride != nil {
		fields["model_override"] = *patch.ModelOverride
	}
	if patch.DutyInstruction != nil {
		fields["duty_instruction"] = *patch.DutyInstruction
	}
	if patch.SortOrder != nil {
		fields["sort_order"] = *patch.SortOrder
	}
	if err := r.db.WithContext(ctx).Model(&models.AgentGroupMember{}).Where("id = ?", existing.ID).Updates(fields).Error; err != nil {
		return nil, translateError(err)
	}
	if err := bumpGroupRevision(r.db, groupID); err != nil {
		return nil, err
	}
	var row memberRow
	if err := memberQuery(r.db).Where("members.group_id = ? AND members.public_id = ?", groupID, publicID).Scan(&row).Error; err != nil {
		return nil, translateError(err)
	}
	member := toMemberDomain(row)
	return &member, nil
}

// RemoveAgentGroupMemberByPublicID 移除工作成员并递增群组配置版本（主管不可直接移除）。
func (r *Repo) RemoveAgentGroupMemberByPublicID(ctx context.Context, groupID uint, userID uint, publicID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureGroupOwnershipTx(tx, groupID, userID); err != nil {
			return err
		}
		var member models.AgentGroupMember
		if err := tx.Where("group_id = ? AND public_id = ?", groupID, publicID).First(&member).Error; err != nil {
			return translateError(err)
		}
		if member.MemberType == domainagentgroup.MemberTypeSupervisor {
			return repository.ErrConflict
		}
		if err := tx.Where("id = ?", member.ID).Delete(&models.AgentGroupMember{}).Error; err != nil {
			return translateError(err)
		}
		return bumpGroupRevision(tx, groupID)
	})
}

// ReorderAgentGroupMembers 按传入顺序重排成员并递增群组配置版本。
func (r *Repo) ReorderAgentGroupMembers(ctx context.Context, groupID uint, userID uint, orderedPublicIDs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureGroupOwnershipTx(tx, groupID, userID); err != nil {
			return err
		}
		var existing []models.AgentGroupMember
		if err := tx.Where("group_id = ?", groupID).Find(&existing).Error; err != nil {
			return translateError(err)
		}
		if len(existing) != len(orderedPublicIDs) {
			return repository.ErrInvalidInput
		}
		byPublicID := make(map[string]uint, len(existing))
		for _, m := range existing {
			byPublicID[m.PublicID] = m.ID
		}
		seen := make(map[string]bool, len(orderedPublicIDs))
		for i, publicID := range orderedPublicIDs {
			id, ok := byPublicID[publicID]
			if !ok || seen[publicID] {
				return repository.ErrInvalidInput
			}
			seen[publicID] = true
			if err := tx.Model(&models.AgentGroupMember{}).Where("id = ?", id).
				Updates(map[string]interface{}{
					"sort_order": i,
					"updated_at": now(),
				}).Error; err != nil {
				return translateError(err)
			}
		}
		return bumpGroupRevision(tx, groupID)
	})
}

// ChangeAgentGroupSupervisor 在一个事务中完成主管替换并递增群组配置版本。
func (r *Repo) ChangeAgentGroupSupervisor(ctx context.Context, groupID uint, userID uint, memberPublicID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureGroupOwnershipTx(tx, groupID, userID); err != nil {
			return err
		}
		var target models.AgentGroupMember
		if err := tx.Where("group_id = ? AND public_id = ?", groupID, memberPublicID).First(&target).Error; err != nil {
			return translateError(err)
		}
		if target.MemberType == domainagentgroup.MemberTypeSupervisor {
			return nil
		}
		var group models.AgentGroup
		if err := tx.Where("id = ?", groupID).First(&group).Error; err != nil {
			return translateError(err)
		}
		if group.SupervisorMemberID != 0 {
			if err := tx.Model(&models.AgentGroupMember{}).Where("id = ?", group.SupervisorMemberID).
				Updates(map[string]interface{}{
					"member_type": domainagentgroup.MemberTypeWorker,
					"updated_at":  now(),
				}).Error; err != nil {
				return translateError(err)
			}
		}
		if err := tx.Model(&models.AgentGroupMember{}).Where("id = ?", target.ID).
			Updates(map[string]interface{}{
				"member_type": domainagentgroup.MemberTypeSupervisor,
				"updated_at":  now(),
			}).Error; err != nil {
			return translateError(err)
		}
		if err := tx.Model(&models.AgentGroup{}).Where("id = ?", groupID).
			Updates(map[string]interface{}{
				"supervisor_member_id": target.ID,
				"revision":             gorm.Expr("revision + 1"),
				"updated_at":           now(),
			}).Error; err != nil {
			return translateError(err)
		}
		return nil
	})
}
