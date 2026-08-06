package agentgroup

import (
	"net/http"
	"strings"

	appagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/agentgroup"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

// ListAgentGroups godoc
// @Summary 群组列表
// @Description 查询当前用户项目下的 Agent 群组
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param projectID query string true "项目 public_id"
// @Success 200 {object} AgentGroupListResponseDoc
// @Failure 403 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-groups [get]
func (h *Handler) ListAgentGroups(c *gin.Context) {
	userID := middleware.MustUserID(c)
	// projectID 是 query 参数（前端 listAgentGroups 以 ?projectID= 传递），
	// 不能用读取路由路径参数的 stringParam。
	projectID := strings.TrimSpace(c.Query("projectID"))
	if projectID == "" {
		response.Error(c, http.StatusBadRequest, "invalid project id")
		return
	}
	groups, err := h.service.ListAgentGroups(c.Request.Context(), userID, projectID)
	if err != nil {
		resolveError(c, err, http.StatusInternalServerError, "list agent groups failed")
		return
	}
	results := make([]AgentGroupResponse, 0, len(groups))
	for i := range groups {
		results = append(results, toAgentGroupResponse(&groups[i]))
	}
	response.Success(c, results)
}

// GetAgentGroup godoc
// @Summary 群组详情
// @Description 查询单个 Agent 群组（含成员与角色摘要）
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "群组 public_id"
// @Success 200 {object} AgentGroupResponseDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-groups/{id} [get]
func (h *Handler) GetAgentGroup(c *gin.Context) {
	userID := middleware.MustUserID(c)
	publicID, err := stringParam(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid agent group id")
		return
	}
	group, err := h.service.GetAgentGroup(c.Request.Context(), userID, publicID)
	if err != nil {
		resolveError(c, err, http.StatusInternalServerError, "get agent group failed")
		return
	}
	response.Success(c, toAgentGroupResponse(group))
}

// CreateAgentGroup godoc
// @Summary 创建群组
// @Description 创建 Agent 群组（主管 + 工作成员，成员从现有角色中选择）
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body CreateAgentGroupRequest true "群组参数"
// @Success 200 {object} AgentGroupResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 403 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-groups [post]
func (h *Handler) CreateAgentGroup(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var req CreateAgentGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	input := appagentgroup.CreateGroupInput{
		Name:               req.Name,
		Description:        req.Description,
		CoordinationPrompt: req.CoordinationPrompt,
		Supervisor: appagentgroup.MemberCreateInput{
			RolePublicID:    req.Supervisor.RolePublicID,
			ModelOverride:   req.Supervisor.ModelOverride,
			ReasoningEffort: req.Supervisor.ReasoningEffort,
			DutyInstruction: req.Supervisor.DutyInstruction,
		},
	}
	for _, worker := range req.Workers {
		input.Workers = append(input.Workers, appagentgroup.MemberCreateInput{
			RolePublicID:    worker.RolePublicID,
			ModelOverride:   worker.ModelOverride,
			ReasoningEffort: worker.ReasoningEffort,
			DutyInstruction: worker.DutyInstruction,
		})
	}
	group, err := h.service.CreateAgentGroup(c.Request.Context(), userID, req.ProjectID, input)
	if err != nil {
		resolveError(c, err, http.StatusInternalServerError, "create agent group failed")
		return
	}
	h.recordAudit(c, "create_agent_group", "agent_group", group.PublicID,
		map[string]string{"project_id": req.ProjectID})
	response.Success(c, toAgentGroupResponse(group))
}

// UpdateAgentGroup godoc
// @Summary 更新群组
// @Description 更新群组名称、描述、协调提示词与排序
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "群组 public_id"
// @Param body body UpdateAgentGroupRequest true "群组参数"
// @Success 200 {object} AgentGroupResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-groups/{id} [patch]
func (h *Handler) UpdateAgentGroup(c *gin.Context) {
	userID := middleware.MustUserID(c)
	publicID, err := stringParam(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid agent group id")
		return
	}
	var req UpdateAgentGroupRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	group, err := h.service.UpdateAgentGroup(c.Request.Context(), userID, publicID, appagentgroup.UpdateGroupInput{
		Name:               req.Name,
		Description:        req.Description,
		CoordinationPrompt: req.CoordinationPrompt,
		SortOrder:          req.SortOrder,
	})
	if err != nil {
		resolveError(c, err, http.StatusInternalServerError, "update agent group failed")
		return
	}
	h.recordAudit(c, "update_agent_group", "agent_group", publicID,
		map[string]bool{"name": req.Name != nil, "description": req.Description != nil,
			"coordination_prompt": req.CoordinationPrompt != nil, "sort_order": req.SortOrder != nil})
	response.Success(c, toAgentGroupResponse(group))
}

// DeleteAgentGroup godoc
// @Summary 删除群组
// @Description 删除没有历史记录的 Agent 群组（存在会话或运行历史时拒绝）
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "群组 public_id"
// @Success 200 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-groups/{id} [delete]
func (h *Handler) DeleteAgentGroup(c *gin.Context) {
	userID := middleware.MustUserID(c)
	publicID, err := stringParam(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid agent group id")
		return
	}
	if err = h.service.DeleteAgentGroup(c.Request.Context(), userID, publicID); err != nil {
		resolveError(c, err, http.StatusInternalServerError, "delete agent group failed")
		return
	}
	h.recordAudit(c, "delete_agent_group", "agent_group", publicID, nil)
	response.Success(c, nil)
}

// AddAgentGroupMember godoc
// @Summary 添加成员
// @Description 向群组添加工作成员（角色不可与现有成员重复）
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "群组 public_id"
// @Param body body AddAgentGroupMemberRequest true "成员参数"
// @Success 200 {object} AgentGroupResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 403 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-groups/{id}/members [post]
func (h *Handler) AddAgentGroupMember(c *gin.Context) {
	userID := middleware.MustUserID(c)
	groupPublicID, err := stringParam(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid agent group id")
		return
	}
	var req AddAgentGroupMemberRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	group, err := h.service.AddAgentGroupMember(c.Request.Context(), userID, groupPublicID, appagentgroup.MemberCreateInput{
		RolePublicID:    req.RolePublicID,
		ModelOverride:   req.ModelOverride,
		ReasoningEffort: req.ReasoningEffort,
		DutyInstruction: req.DutyInstruction,
	})
	if err != nil {
		resolveError(c, err, http.StatusInternalServerError, "add agent group member failed")
		return
	}
	h.recordAudit(c, "add_agent_group_member", "agent_group", groupPublicID,
		map[string]string{"role_public_id": req.RolePublicID})
	response.Success(c, toAgentGroupResponse(group))
}

// UpdateAgentGroupMember godoc
// @Summary 更新成员
// @Description 更新成员启用状态、模型覆盖与职责指令（主管不可禁用）
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "群组 public_id"
// @Param member_id path string true "成员 public_id"
// @Param body body UpdateAgentGroupMemberRequest true "成员参数"
// @Success 200 {object} AgentGroupResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-groups/{id}/members/{member_id} [patch]
func (h *Handler) UpdateAgentGroupMember(c *gin.Context) {
	userID := middleware.MustUserID(c)
	groupPublicID, err := stringParam(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid agent group id")
		return
	}
	memberPublicID, err := stringParam(c, "member_id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid member id")
		return
	}
	var req UpdateAgentGroupMemberRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	group, err := h.service.UpdateAgentGroupMember(c.Request.Context(), userID, groupPublicID, memberPublicID, appagentgroup.UpdateMemberInput{
		Enabled:         req.Enabled,
		ModelOverride:   req.ModelOverride,
		ReasoningEffort: req.ReasoningEffort,
		DutyInstruction: req.DutyInstruction,
		SortOrder:       req.SortOrder,
	})
	if err != nil {
		resolveError(c, err, http.StatusInternalServerError, "update agent group member failed")
		return
	}
	h.recordAudit(c, "update_agent_group_member", "agent_group", groupPublicID,
		map[string]interface{}{"member_public_id": memberPublicID, "enabled": req.Enabled,
			"model_override": req.ModelOverride, "duty_instruction_changed": req.DutyInstruction != nil})
	response.Success(c, toAgentGroupResponse(group))
}

// RemoveAgentGroupMember godoc
// @Summary 移除成员
// @Description 移除工作成员（主管须先更换）
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "群组 public_id"
// @Param member_id path string true "成员 public_id"
// @Success 200 {object} AgentGroupResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-groups/{id}/members/{member_id} [delete]
func (h *Handler) RemoveAgentGroupMember(c *gin.Context) {
	userID := middleware.MustUserID(c)
	groupPublicID, err := stringParam(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid agent group id")
		return
	}
	memberPublicID, err := stringParam(c, "member_id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid member id")
		return
	}
	group, err := h.service.RemoveAgentGroupMember(c.Request.Context(), userID, groupPublicID, memberPublicID)
	if err != nil {
		resolveError(c, err, http.StatusInternalServerError, "remove agent group member failed")
		return
	}
	h.recordAudit(c, "remove_agent_group_member", "agent_group", groupPublicID,
		map[string]string{"member_public_id": memberPublicID})
	response.Success(c, toAgentGroupResponse(group))
}

// ReorderAgentGroupMembers godoc
// @Summary 重排成员
// @Description 按传入顺序重排群组成员
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "群组 public_id"
// @Param body body ReorderAgentGroupMembersRequest true "成员顺序"
// @Success 200 {object} AgentGroupResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-groups/{id}/members/reorder [post]
func (h *Handler) ReorderAgentGroupMembers(c *gin.Context) {
	userID := middleware.MustUserID(c)
	groupPublicID, err := stringParam(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid agent group id")
		return
	}
	var req ReorderAgentGroupMembersRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	group, err := h.service.ReorderAgentGroupMembers(c.Request.Context(), userID, groupPublicID, req.OrderedPublicIDs)
	if err != nil {
		resolveError(c, err, http.StatusInternalServerError, "reorder agent group members failed")
		return
	}
	h.recordAudit(c, "reorder_agent_group_members", "agent_group", groupPublicID,
		map[string]int{"count": len(req.OrderedPublicIDs)})
	response.Success(c, toAgentGroupResponse(group))
}

// ChangeAgentGroupSupervisor godoc
// @Summary 更换主管
// @Description 更换群组主管（原主管自动转为工作成员）
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "群组 public_id"
// @Param body body ChangeAgentGroupSupervisorRequest true "新主管成员"
// @Success 200 {object} AgentGroupResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-groups/{id}/supervisor [post]
func (h *Handler) ChangeAgentGroupSupervisor(c *gin.Context) {
	userID := middleware.MustUserID(c)
	groupPublicID, err := stringParam(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid agent group id")
		return
	}
	var req ChangeAgentGroupSupervisorRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	group, err := h.service.ChangeAgentGroupSupervisor(c.Request.Context(), userID, groupPublicID, req.MemberPublicID)
	if err != nil {
		resolveError(c, err, http.StatusInternalServerError, "change agent group supervisor failed")
		return
	}
	h.recordAudit(c, "change_agent_group_supervisor", "agent_group", groupPublicID,
		map[string]string{"new_supervisor_member_public_id": req.MemberPublicID})
	response.Success(c, toAgentGroupResponse(group))
}
