package conversation

import (
	"context"
	"fmt"
	"strings"

	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
)

// 角色 / 项目 / Agent 群组的平台工具实现（全部限定用户本人数据）。

// platformListRoles 列出用户角色。
func (s *Service) platformListRoles(ctx context.Context, call platformToolCallContext) (string, error) {
	roles, err := s.ListConversationRoles(ctx, call.UserID, "")
	if err != nil {
		return "", err
	}
	summary := make([]map[string]interface{}, 0, len(roles))
	for _, role := range roles {
		summary = append(summary, platformRoleSummary(role))
	}
	return marshalPlatformResult(map[string]interface{}{"roles": summary})
}

func platformRoleSummary(role model.ConversationRole) map[string]interface{} {
	return map[string]interface{}{
		"role_id":       role.PublicID,
		"name":          role.Name,
		"description":   role.Description,
		"model":         role.Model,
		"group_name":    role.GroupName,
		"color":         role.Color,
		"icon":          role.Icon,
		"system_prompt": role.SystemPrompt,
	}
}

// platformCreateRole 创建用户角色（写操作，受批准模式管控）。
func (s *Service) platformCreateRole(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		SystemPrompt string `json:"system_prompt"`
		Model        string `json:"model"`
		GroupName    string `json:"group_name"`
		Color        string `json:"color"`
		Icon         string `json:"icon"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Name) == "" {
		return "", fmt.Errorf("name is required")
	}
	role, err := s.CreateConversationRole(ctx, call.UserID, ConversationRoleInput{
		Name:         args.Name,
		Description:  args.Description,
		SystemPrompt: args.SystemPrompt,
		Model:        args.Model,
		Color:        args.Color,
		Icon:         args.Icon,
		GroupName:    args.GroupName,
	})
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.create_role", role.PublicID, map[string]interface{}{
		"name": role.Name,
	})
	return marshalPlatformResult(map[string]interface{}{
		"role_id": role.PublicID,
		"name":    role.Name,
		"created": true,
	})
}

// platformListProjects 列出用户项目。
func (s *Service) platformListProjects(ctx context.Context, call platformToolCallContext) (string, error) {
	projects, err := s.ListConversationProjects(ctx, call.UserID, "")
	if err != nil {
		return "", err
	}
	summary := make([]map[string]interface{}, 0, len(projects))
	for _, project := range projects {
		summary = append(summary, map[string]interface{}{
			"project_id":    project.PublicID,
			"name":          project.Name,
			"description":   project.Description,
			"system_prompt": project.SystemPrompt,
			"color":         project.Color,
			"icon":          project.Icon,
		})
	}
	return marshalPlatformResult(map[string]interface{}{"projects": summary})
}

// platformCreateProject 创建用户项目（写操作，受批准模式管控）。
func (s *Service) platformCreateProject(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		SystemPrompt string `json:"system_prompt"`
		Color        string `json:"color"`
		Icon         string `json:"icon"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Name) == "" {
		return "", fmt.Errorf("name is required")
	}
	project, err := s.CreateConversationProject(ctx, call.UserID, ConversationProjectInput{
		Name:         args.Name,
		Description:  args.Description,
		SystemPrompt: args.SystemPrompt,
		Color:        args.Color,
		Icon:         args.Icon,
	})
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.create_project", project.PublicID, map[string]interface{}{
		"name": project.Name,
	})
	return marshalPlatformResult(map[string]interface{}{
		"project_id": project.PublicID,
		"name":       project.Name,
		"created":    true,
	})
}

// platformListAgentGroups 列出用户 Agent 群组（群组开关关闭时返回错误）。
func (s *Service) platformListAgentGroups(ctx context.Context, call platformToolCallContext) (string, error) {
	if s.agentGroupWriter == nil {
		return "", fmt.Errorf("agent group service is unavailable")
	}
	groups, err := s.agentGroupWriter.ListAgentGroups(ctx, call.UserID)
	if err != nil {
		return "", err
	}
	summary := make([]map[string]interface{}, 0, len(groups))
	for _, group := range groups {
		summary = append(summary, platformAgentGroupSummary(group))
	}
	return marshalPlatformResult(map[string]interface{}{"agent_groups": summary})
}

func platformAgentGroupSummary(group domainagentgroup.Group) map[string]interface{} {
	members := make([]map[string]interface{}, 0, len(group.Members))
	for _, member := range group.Members {
		members = append(members, map[string]interface{}{
			"member_type":   member.MemberType,
			"role_public_id": member.RolePublicID,
			"role_name":     member.RoleName,
			"duty_instruction": member.DutyInstruction,
		})
	}
	return map[string]interface{}{
		"group_id":        group.PublicID,
		"name":            group.Name,
		"description":     group.Description,
		"supervisor_id":   group.SupervisorMemberID,
		"members":         members,
	}
}

// platformCreateAgentGroup 创建用户 Agent 群组（写操作，受批准模式管控；群组开关关闭时返回错误）。
func (s *Service) platformCreateAgentGroup(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Name              string `json:"name"`
		Description       string `json:"description"`
		CoordinationPrompt string `json:"coordination_prompt"`
		SupervisorRoleID  string `json:"supervisor_role_id"`
		SupervisorDuty    string `json:"supervisor_duty"`
		Workers           []struct {
			RoleID          string `json:"role_id"`
			DutyInstruction string `json:"duty_instruction"`
		} `json:"workers"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Name) == "" || strings.TrimSpace(args.SupervisorRoleID) == "" {
		return "", fmt.Errorf("name and supervisor_role_id are required")
	}
	if s.agentGroupWriter == nil {
		return "", fmt.Errorf("agent group service is unavailable")
	}
	input := AgentGroupCreateInput{
		Name:               args.Name,
		Description:        args.Description,
		CoordinationPrompt: args.CoordinationPrompt,
		Supervisor: AgentGroupMemberCreateInput{
			RolePublicID:    strings.TrimSpace(args.SupervisorRoleID),
			MemberType:      domainagentgroup.MemberTypeSupervisor,
			DutyInstruction: args.SupervisorDuty,
		},
	}
	for _, worker := range args.Workers {
		if strings.TrimSpace(worker.RoleID) == "" {
			continue
		}
		input.Workers = append(input.Workers, AgentGroupMemberCreateInput{
			RolePublicID:    strings.TrimSpace(worker.RoleID),
			MemberType:      domainagentgroup.MemberTypeWorker,
			DutyInstruction: worker.DutyInstruction,
		})
	}
	group, err := s.agentGroupWriter.CreateAgentGroup(ctx, call.UserID, input)
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.create_agent_group", group.PublicID, map[string]interface{}{
		"name": group.Name,
	})
	return marshalPlatformResult(map[string]interface{}{
		"group_id": group.PublicID,
		"name":     group.Name,
		"created":  true,
	})
}

// 删除类平台工具（与创建/更新工具对称，全部写操作受批准模式管控）。

// platformDeleteSkill 删除用户自己的技能。
func (s *Service) platformDeleteSkill(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		SkillID uint `json:"skill_id"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.SkillID == 0 {
		return "", fmt.Errorf("skill_id is required")
	}
	if s.skillResolver == nil {
		return "", fmt.Errorf("skill service is unavailable")
	}
	if err := s.skillResolver.DeleteUser(ctx, call.UserID, args.SkillID); err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.delete_skill", fmt.Sprintf("%d", args.SkillID), nil)
	return marshalPlatformResult(map[string]interface{}{
		"skill_id": args.SkillID,
		"deleted":  true,
	})
}

// platformUpdateRole 更新用户角色（写操作，受批准模式管控）。
// 所有字段可选，仅更新显式提供的字段；group_name 传空字符串表示移出分组。
func (s *Service) platformUpdateRole(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		RoleID         string  `json:"role_id"`
		Name           *string `json:"name"`
		Description    *string `json:"description"`
		SystemPrompt   *string `json:"system_prompt"`
		Model          *string `json:"model"`
		GroupName      *string `json:"group_name"`
		Color          *string `json:"color"`
		Icon           *string `json:"icon"`
		Pinned         *bool   `json:"pinned"`
		ReasoningEffort *string `json:"reasoning_effort"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	roleID := strings.TrimSpace(args.RoleID)
	if roleID == "" {
		return "", fmt.Errorf("role_id is required")
	}
	role, err := s.UpdateConversationRole(ctx, call.UserID, roleID, ConversationRolePatchInput{
		Name:            args.Name,
		Description:     args.Description,
		SystemPrompt:    args.SystemPrompt,
		Model:           args.Model,
		GroupName:       args.GroupName,
		Color:           args.Color,
		Icon:            args.Icon,
		Pinned:          args.Pinned,
		ReasoningEffort: args.ReasoningEffort,
	})
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.update_role", role.PublicID, map[string]interface{}{
		"name":       role.Name,
		"group_name": role.GroupName,
		"pinned":     role.PinnedAt != nil,
	})
	return marshalPlatformResult(map[string]interface{}{
		"role_id":    role.PublicID,
		"name":       role.Name,
		"group_name": role.GroupName,
		"pinned":     role.PinnedAt != nil,
		"updated":    true,
	})
}

// platformUpdateProject 更新用户项目（写操作，受批准模式管控）。
func (s *Service) platformUpdateProject(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		ProjectID    string  `json:"project_id"`
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		SystemPrompt *string `json:"system_prompt"`
		Color        *string `json:"color"`
		Icon         *string `json:"icon"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	projectID := strings.TrimSpace(args.ProjectID)
	if projectID == "" {
		return "", fmt.Errorf("project_id is required")
	}
	project, err := s.UpdateConversationProject(ctx, call.UserID, projectID, ConversationProjectPatchInput{
		Name:         args.Name,
		Description:  args.Description,
		SystemPrompt: args.SystemPrompt,
		Color:        args.Color,
		Icon:         args.Icon,
	})
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.update_project", project.PublicID, map[string]interface{}{
		"name": project.Name,
	})
	return marshalPlatformResult(map[string]interface{}{
		"project_id": project.PublicID,
		"name":       project.Name,
		"updated":    true,
	})
}

// platformUpdateAgentGroup 更新用户 Agent 群组元数据（写操作，受批准模式管控；群组开关关闭时报错）。
func (s *Service) platformUpdateAgentGroup(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		GroupID             string  `json:"group_id"`
		Name                *string `json:"name"`
		Description         *string `json:"description"`
		CoordinationPrompt  *string `json:"coordination_prompt"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	groupID := strings.TrimSpace(args.GroupID)
	if groupID == "" {
		return "", fmt.Errorf("group_id is required")
	}
	if s.agentGroupWriter == nil {
		return "", fmt.Errorf("agent group service is unavailable")
	}
	group, err := s.agentGroupWriter.UpdateAgentGroup(ctx, call.UserID, groupID, AgentGroupUpdateInput{
		Name:               args.Name,
		Description:        args.Description,
		CoordinationPrompt: args.CoordinationPrompt,
	})
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.update_agent_group", group.PublicID, map[string]interface{}{
		"name": group.Name,
	})
	return marshalPlatformResult(map[string]interface{}{
		"group_id": group.PublicID,
		"name":     group.Name,
		"updated":  true,
	})
}

// platformDeleteRole 删除用户角色（被群组引用时服务拒绝）。
func (s *Service) platformDeleteRole(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		RoleID string `json:"role_id"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	roleID := strings.TrimSpace(args.RoleID)
	if roleID == "" {
		return "", fmt.Errorf("role_id is required")
	}
	if err := s.DeleteConversationRole(ctx, call.UserID, roleID); err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.delete_role", roleID, nil)
	return marshalPlatformResult(map[string]interface{}{
		"role_id": roleID,
		"deleted": true,
	})
}

// platformDeleteProject 删除用户项目（保留项目下的会话）。
func (s *Service) platformDeleteProject(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		ProjectID string `json:"project_id"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	projectID := strings.TrimSpace(args.ProjectID)
	if projectID == "" {
		return "", fmt.Errorf("project_id is required")
	}
	_, err := s.DeleteConversationProject(ctx, call.UserID, projectID, false, DeleteConversationOptions{})
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.delete_project", projectID, nil)
	return marshalPlatformResult(map[string]interface{}{
		"project_id": projectID,
		"deleted":    true,
	})
}

// platformDeleteAgentGroup 删除用户 Agent 群组（有运行历史时服务拒绝；群组开关关闭时报错）。
func (s *Service) platformDeleteAgentGroup(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		GroupID string `json:"group_id"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	groupID := strings.TrimSpace(args.GroupID)
	if groupID == "" {
		return "", fmt.Errorf("group_id is required")
	}
	if s.agentGroupWriter == nil {
		return "", fmt.Errorf("agent group service is unavailable")
	}
	if err := s.agentGroupWriter.DeleteAgentGroup(ctx, call.UserID, groupID); err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.delete_agent_group", groupID, nil)
	return marshalPlatformResult(map[string]interface{}{
		"group_id": groupID,
		"deleted":  true,
	})
}

// platformDeleteConversation 删除用户会话（可选连带删除会话文件）。
func (s *Service) platformDeleteConversation(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		ConversationID uint `json:"conversation_id"`
		DeleteFiles    bool `json:"delete_files"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.ConversationID == 0 {
		return "", fmt.Errorf("conversation_id is required")
	}
	conversation, err := s.GetConversation(ctx, call.UserID, args.ConversationID)
	if err != nil {
		return "", err
	}
	result, err := s.DeleteConversation(ctx, call.UserID, conversation.PublicID, DeleteConversationOptions{DeleteFiles: args.DeleteFiles})
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.delete_conversation", conversation.PublicID, map[string]interface{}{
		"delete_files": args.DeleteFiles,
	})
	return marshalPlatformResult(map[string]interface{}{
		"conversation_id": args.ConversationID,
		"deleted":         result.Deleted,
	})
}
