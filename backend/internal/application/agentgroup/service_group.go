package agentgroup

import (
	"context"
	"errors"
	"strings"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

// CreateAgentGroup 创建群组（主管 + 工作成员原子落库）。
func (s *Service) CreateAgentGroup(ctx context.Context, userID uint, projectPublicID string, input CreateGroupInput) (*domainagentgroup.Group, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}
	project, err := s.projectReader.GetConversationProject(ctx, userID, projectPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAgentGroupProjectNotFound
		}
		return nil, err
	}
	normalized := CreateGroupInput{
		Name:               strings.TrimSpace(input.Name),
		Description:        strings.TrimSpace(input.Description),
		CoordinationPrompt: strings.TrimSpace(input.CoordinationPrompt),
		Supervisor:         normalizeMemberCreateInput(input.Supervisor),
	}
	for _, worker := range input.Workers {
		normalized.Workers = append(normalized.Workers, normalizeMemberCreateInput(worker))
	}
	if err := s.validateGroupFields(normalized.Name, normalized.Description, normalized.CoordinationPrompt); err != nil {
		return nil, err
	}
	if normalized.Supervisor.RolePublicID == "" {
		return nil, ErrAgentGroupSupervisorRequired
	}

	supervisorRole, err := s.resolveRole(ctx, userID, normalized.Supervisor.RolePublicID)
	if err != nil {
		return nil, err
	}
	seenRoles := map[uint]bool{supervisorRole.ID: true}
	members := make([]domainagentgroup.Member, 0, 1+len(normalized.Workers))
	members = append(members, buildMember(supervisorRole, normalized.Supervisor, 0, domainagentgroup.MemberTypeSupervisor))
	for i, workerInput := range normalized.Workers {
		// 成员类型由服务端按位置决定（工作成员），客户端契约不传 memberType。
		workerInput.MemberType = domainagentgroup.MemberTypeWorker
		if err := validateMemberInput(workerInput, i+1); err != nil {
			return nil, err
		}
		role, err := s.resolveRole(ctx, userID, workerInput.RolePublicID)
		if err != nil {
			return nil, err
		}
		if seenRoles[role.ID] {
			return nil, ErrAgentGroupMemberDuplicate
		}
		seenRoles[role.ID] = true
		members = append(members, buildMember(role, workerInput, i+1, domainagentgroup.MemberTypeWorker))
	}
	if len(members) > domainagentgroup.MaxGroupMembersPerGroup {
		return nil, ErrAgentGroupMemberLimitExceeded
	}

	group := &domainagentgroup.Group{
		UserID:             userID,
		ProjectID:          project.ID,
		PublicID:           newPublicID(),
		Name:               normalized.Name,
		Description:        normalized.Description,
		CoordinationPrompt: normalized.CoordinationPrompt,
		Status:             domainagentgroup.GroupStatusActive,
	}
	if err := s.repo.CreateAgentGroupWithSupervisor(ctx, group, members[0], members[1:]); err != nil {
		return nil, s.translateRepoError(err)
	}
	return s.repo.GetAgentGroupByPublicID(ctx, userID, group.PublicID)
}

// ListAgentGroups 查询项目内群组。
func (s *Service) ListAgentGroups(ctx context.Context, userID uint, projectPublicID string) ([]domainagentgroup.Group, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}
	project, err := s.projectReader.GetConversationProject(ctx, userID, projectPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAgentGroupProjectNotFound
		}
		return nil, err
	}
	return s.repo.ListAgentGroupsByProject(ctx, userID, project.ID)
}

// GetAgentGroup 查询单个群组。
func (s *Service) GetAgentGroup(ctx context.Context, userID uint, publicID string) (*domainagentgroup.Group, error) {
	group, err := s.repo.GetAgentGroupByPublicID(ctx, userID, publicID)
	if err != nil {
		return nil, s.translateRepoError(err)
	}
	return group, nil
}

// UpdateAgentGroup 更新群组元数据。
func (s *Service) UpdateAgentGroup(ctx context.Context, userID uint, publicID string, input UpdateGroupInput) (*domainagentgroup.Group, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}
	patch := domainagentgroup.GroupPatch{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if err := s.validateGroupFields(name, "", ""); err != nil {
			return nil, err
		}
		patch.Name = &name
	}
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		if err := s.validateGroupFields("", description, ""); err != nil {
			return nil, err
		}
		patch.Description = &description
	}
	if input.CoordinationPrompt != nil {
		prompt := strings.TrimSpace(*input.CoordinationPrompt)
		if err := s.validateGroupFields("", "", prompt); err != nil {
			return nil, err
		}
		patch.CoordinationPrompt = &prompt
	}
	patch.SortOrder = input.SortOrder
	group, err := s.repo.UpdateAgentGroupByPublicID(ctx, userID, publicID, patch)
	if err != nil {
		return nil, s.translateRepoError(err)
	}
	return group, nil
}

// DeleteAgentGroup 删除没有历史记录的群组。
func (s *Service) DeleteAgentGroup(ctx context.Context, userID uint, publicID string) error {
	if err := s.requireEnabled(ctx); err != nil {
		return err
	}
	group, err := s.repo.GetAgentGroupByPublicID(ctx, userID, publicID)
	if err != nil {
		return s.translateRepoError(err)
	}
	history, err := s.repo.CountAgentGroupHistory(ctx, group.ID)
	if err != nil {
		return err
	}
	if history > 0 {
		return ErrAgentGroupHistoryExists
	}
	return s.translateRepoError(s.repo.DeleteAgentGroupByPublicID(ctx, userID, publicID))
}

// AddAgentGroupMember 添加工作成员。
func (s *Service) AddAgentGroupMember(ctx context.Context, userID uint, groupPublicID string, input MemberCreateInput) (*domainagentgroup.Group, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}
	normalized := normalizeMemberCreateInput(input)
	// 客户端契约不传 memberType；显式请求以主管身份加入时拒绝（§17 角色分工不变量）。
	if normalized.MemberType == domainagentgroup.MemberTypeSupervisor {
		return nil, ErrAgentGroupSupervisorDuplicate
	}
	// 添加成员始终是工作成员，类型由服务端强制。
	normalized.MemberType = domainagentgroup.MemberTypeWorker
	if err := validateMemberInput(normalized, 0); err != nil {
		return nil, err
	}
	group, err := s.repo.GetAgentGroupByPublicID(ctx, userID, groupPublicID)
	if err != nil {
		return nil, s.translateRepoError(err)
	}
	role, err := s.resolveRole(ctx, userID, normalized.RolePublicID)
	if err != nil {
		return nil, err
	}
	for _, existing := range group.Members {
		if existing.RoleID == role.ID {
			return nil, ErrAgentGroupMemberDuplicate
		}
	}
	if len(group.Members) >= domainagentgroup.MaxGroupMembersPerGroup {
		return nil, ErrAgentGroupMemberLimitExceeded
	}
	member := buildMember(role, normalized, len(group.Members), domainagentgroup.MemberTypeWorker)
	if err := s.repo.AddAgentGroupMember(ctx, group.ID, member); err != nil {
		return nil, s.translateRepoError(err)
	}
	return s.repo.GetAgentGroupByPublicID(ctx, userID, groupPublicID)
}

// UpdateAgentGroupMember 更新成员（主管不可禁用）。
func (s *Service) UpdateAgentGroupMember(ctx context.Context, userID uint, groupPublicID string, memberPublicID string, input UpdateMemberInput) (*domainagentgroup.Group, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}
	group, err := s.repo.GetAgentGroupByPublicID(ctx, userID, groupPublicID)
	if err != nil {
		return nil, s.translateRepoError(err)
	}
	var target *domainagentgroup.Member
	for i := range group.Members {
		if group.Members[i].PublicID == memberPublicID {
			target = &group.Members[i]
			break
		}
	}
	if target == nil {
		return nil, ErrAgentGroupNotFound
	}
	if input.Enabled != nil && !*input.Enabled && target.MemberType == domainagentgroup.MemberTypeSupervisor {
		return nil, ErrAgentGroupSupervisorProtected
	}
	patch := domainagentgroup.MemberPatch{
		Enabled:         input.Enabled,
		SortOrder:       input.SortOrder,
	}
	if input.ModelOverride != nil {
		value := strings.TrimSpace(*input.ModelOverride)
		if exceedsRuneLimit(value, 128) {
			return nil, ErrInvalidAgentGroupModelOverride
		}
		patch.ModelOverride = &value
	}
	if input.DutyInstruction != nil {
		value := strings.TrimSpace(*input.DutyInstruction)
		if exceedsRuneLimit(value, domainagentgroup.MaxDutyInstructionRunes) {
			return nil, ErrInvalidDutyInstruction
		}
		patch.DutyInstruction = &value
	}
	if _, err := s.repo.UpdateAgentGroupMemberByPublicID(ctx, group.ID, userID, memberPublicID, patch); err != nil {
		return nil, s.translateRepoError(err)
	}
	return s.repo.GetAgentGroupByPublicID(ctx, userID, groupPublicID)
}

// RemoveAgentGroupMember 移除工作成员（主管须先更换）。
func (s *Service) RemoveAgentGroupMember(ctx context.Context, userID uint, groupPublicID string, memberPublicID string) (*domainagentgroup.Group, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}
	group, err := s.repo.GetAgentGroupByPublicID(ctx, userID, groupPublicID)
	if err != nil {
		return nil, s.translateRepoError(err)
	}
	var target *domainagentgroup.Member
	for i := range group.Members {
		if group.Members[i].PublicID == memberPublicID {
			target = &group.Members[i]
			break
		}
	}
	if target == nil {
		return nil, ErrAgentGroupNotFound
	}
	if target.MemberType == domainagentgroup.MemberTypeSupervisor {
		return nil, ErrAgentGroupSupervisorProtected
	}
	if err := s.repo.RemoveAgentGroupMemberByPublicID(ctx, group.ID, userID, memberPublicID); err != nil {
		return nil, s.translateRepoError(err)
	}
	return s.repo.GetAgentGroupByPublicID(ctx, userID, groupPublicID)
}

// ReorderAgentGroupMembers 按传入顺序重排成员。
func (s *Service) ReorderAgentGroupMembers(ctx context.Context, userID uint, groupPublicID string, orderedPublicIDs []string) (*domainagentgroup.Group, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}
	group, err := s.repo.GetAgentGroupByPublicID(ctx, userID, groupPublicID)
	if err != nil {
		return nil, s.translateRepoError(err)
	}
	if len(orderedPublicIDs) != len(group.Members) {
		return nil, ErrAgentGroupInvalidMemberOrder
	}
	if err := s.repo.ReorderAgentGroupMembers(ctx, group.ID, userID, orderedPublicIDs); err != nil {
		return nil, s.translateRepoError(err)
	}
	return s.repo.GetAgentGroupByPublicID(ctx, userID, groupPublicID)
}

// ChangeAgentGroupSupervisor 更换主管（原主管自动转为工作成员）。
func (s *Service) ChangeAgentGroupSupervisor(ctx context.Context, userID uint, groupPublicID string, memberPublicID string) (*domainagentgroup.Group, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}
	group, err := s.repo.GetAgentGroupByPublicID(ctx, userID, groupPublicID)
	if err != nil {
		return nil, s.translateRepoError(err)
	}
	found := false
	for i := range group.Members {
		if group.Members[i].PublicID == memberPublicID {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrAgentGroupNotFound
	}
	if err := s.repo.ChangeAgentGroupSupervisor(ctx, group.ID, userID, memberPublicID); err != nil {
		return nil, s.translateRepoError(err)
	}
	return s.repo.GetAgentGroupByPublicID(ctx, userID, groupPublicID)
}

// resolveRole 查询角色并校验存在性。
func (s *Service) resolveRole(ctx context.Context, userID uint, publicID string) (*domainconversation.ConversationRole, error) {
	role, err := s.roleReader.GetConversationRole(ctx, userID, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAgentGroupRoleNotFound
		}
		return nil, err
	}
	return role, nil
}

// validateGroupFields 校验群组字段长度。
func (s *Service) validateGroupFields(name string, description string, coordinationPrompt string) error {
	if exceedsRuneLimit(name, domainagentgroup.MaxGroupNameRunes) {
		return ErrInvalidAgentGroupName
	}
	if exceedsRuneLimit(description, domainagentgroup.MaxGroupDescriptionRunes) {
		return ErrInvalidAgentGroupDescription
	}
	if exceedsRuneLimit(coordinationPrompt, domainagentgroup.MaxCoordinationPromptRunes) {
		return ErrInvalidCoordinationPrompt
	}
	return nil
}

// validateMemberInput 校验成员输入。
func validateMemberInput(input MemberCreateInput, index int) error {
	if input.RolePublicID == "" {
		return ErrAgentGroupRoleNotFound
	}
	// 成员类型由服务端按位置决定（主管/工作成员），不要求客户端提供。
	if exceedsRuneLimit(input.ModelOverride, 128) {
		return ErrInvalidAgentGroupModelOverride
	}
	if exceedsRuneLimit(input.DutyInstruction, domainagentgroup.MaxDutyInstructionRunes) {
		return ErrInvalidDutyInstruction
	}
	_ = index
	return nil
}

// normalizeMemberCreateInput 规范化成员输入。
func normalizeMemberCreateInput(input MemberCreateInput) MemberCreateInput {
	return MemberCreateInput{
		RolePublicID:    strings.TrimSpace(input.RolePublicID),
		MemberType:      strings.TrimSpace(input.MemberType),
		ModelOverride:   strings.TrimSpace(input.ModelOverride),
		DutyInstruction: strings.TrimSpace(input.DutyInstruction),
	}
}

// buildMember 由角色与输入组装成员域对象。
func buildMember(role *domainconversation.ConversationRole, input MemberCreateInput, sortOrder int, memberType string) domainagentgroup.Member {
	return domainagentgroup.Member{
		PublicID:        newPublicID(),
		RoleID:          role.ID,
		RolePublicID:    role.PublicID,
		RoleName:        role.Name,
		RoleIcon:        role.Icon,
		RoleColor:       role.Color,
		RoleModel:       role.Model,
		RoleProvider:    role.Provider,
		MemberType:      memberType,
		Enabled:         true,
		ModelOverride:   input.ModelOverride,
		DutyInstruction: input.DutyInstruction,
		SortOrder:       sortOrder,
	}
}

// translateRepoError 将仓储错误映射为应用层哨兵错误。
func (s *Service) translateRepoError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, repository.ErrNotFound) {
		return ErrAgentGroupNotFound
	}
	if errors.Is(err, repository.ErrDuplicate) {
		return ErrAgentGroupMemberDuplicate
	}
	if errors.Is(err, repository.ErrConflict) {
		return ErrAgentGroupSupervisorProtected
	}
	return err
}
