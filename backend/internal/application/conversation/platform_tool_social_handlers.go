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
