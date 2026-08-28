package conversation

import (
	"errors"
	"net/http"

	appconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

// ListConversationRoles godoc
// @Summary 角色列表
// @Description 查询当前用户的角色(助手)列表
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param status query string false "状态筛选: active|archived|all"
// @Success 200 {object} ConversationRoleListResponseDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-roles [get]
func (h *Handler) ListConversationRoles(c *gin.Context) {
	userID := middleware.MustUserID(c)
	items, err := h.service.ListConversationRoles(c.Request.Context(), userID, c.Query("status"))
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "list conversation roles failed")
		return
	}
	results := make([]ConversationRoleResponse, 0, len(items))
	for i := range items {
		results = append(results, toConversationRoleResponse(&items[i]))
	}
	response.Success(c, results)
}

// CreateConversationRole godoc
// @Summary 创建角色
// @Description 创建当前用户的角色(助手)
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body CreateConversationRoleRequest true "角色参数"
// @Success 200 {object} ConversationRoleResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-roles [post]
func (h *Handler) CreateConversationRole(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var req CreateConversationRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	item, err := h.service.CreateConversationRole(c.Request.Context(), userID, appconversation.ConversationRoleInput{
		Name:              req.Name,
		Description:       req.Description,
		SystemPrompt:      req.SystemPrompt,
		Model:             req.Model,
		Provider:          req.Provider,
		ReasoningEffort:   req.ReasoningEffort,
		MCPDefaultMode:    req.MCPDefaultMode,
		DefaultMCPToolIDs: req.DefaultMCPToolIDs,
		DefaultSkillIDs:   req.DefaultSkillIDs,
		Color:             req.Color,
		Icon:              req.Icon,
		GroupName:         req.GroupName,
		Pinned:            req.Pinned,
	})
	if err != nil {
		if errors.Is(err, appconversation.ErrInvalidConversationProject) {
			response.Error(c, http.StatusBadRequest, "invalid conversation role")
			return
		}
		response.Error(c, http.StatusInternalServerError, "create conversation role failed")
		return
	}
	h.recordAudit(c, "create_conversation_role", "conversation_role", item.PublicID, map[string]interface{}{
		"name":                   item.Name,
		"model":                  item.Model,
		"mcp_default_mode":       item.MCPDefaultMode,
		"default_mcp_tool_count": len(item.DefaultMCPToolIDs),
		"default_skill_count":    len(item.DefaultSkillIDs),
	})
	response.Success(c, toConversationRoleResponse(item))
}

// GetConversationRole godoc
// @Summary 角色详情
// @Description 查询当前用户单个角色
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "角色 public_id"
// @Success 200 {object} ConversationRoleResponseDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-roles/{id} [get]
func (h *Handler) GetConversationRole(c *gin.Context) {
	userID := middleware.MustUserID(c)
	publicID, err := stringParam(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid conversation role id")
		return
	}
	item, err := h.service.GetConversationRole(c.Request.Context(), userID, publicID)
	if err != nil {
		if errors.Is(err, appconversation.ErrConversationRoleNotFound) {
			response.Error(c, http.StatusNotFound, "conversation role not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, "get conversation role failed")
		return
	}
	response.Success(c, toConversationRoleResponse(item))
}

// UpdateConversationRole godoc
// @Summary 更新角色
// @Description 更新当前用户的角色
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "角色 public_id"
// @Param body body UpdateConversationRoleRequest true "角色参数"
// @Success 200 {object} ConversationRoleResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-roles/{id} [patch]
func (h *Handler) UpdateConversationRole(c *gin.Context) {
	userID := middleware.MustUserID(c)
	publicID, err := stringParam(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid conversation role id")
		return
	}
	var req UpdateConversationRoleRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	item, err := h.service.UpdateConversationRole(c.Request.Context(), userID, publicID, appconversation.ConversationRolePatchInput{
		Name:              req.Name,
		Description:       req.Description,
		SystemPrompt:      req.SystemPrompt,
		Model:             req.Model,
		Provider:          req.Provider,
		ReasoningEffort:   req.ReasoningEffort,
		MCPDefaultMode:    req.MCPDefaultMode,
		DefaultMCPToolIDs: req.DefaultMCPToolIDs,
		DefaultSkillIDs:   req.DefaultSkillIDs,
		Color:             req.Color,
		Icon:              req.Icon,
		Status:            req.Status,
		GroupName:         req.GroupName,
		Pinned:            req.Pinned,
	})
	if err != nil {
		if errors.Is(err, appconversation.ErrConversationRoleNotFound) {
			response.Error(c, http.StatusNotFound, "conversation role not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, "update conversation role failed")
		return
	}
	response.Success(c, toConversationRoleResponse(item))
}

// DeleteConversationRole godoc
// @Summary 删除角色
// @Description 删除当前用户的角色
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "角色 public_id"
// @Success 200 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-roles/{id} [delete]
func (h *Handler) DeleteConversationRole(c *gin.Context) {
	userID := middleware.MustUserID(c)
	publicID, err := stringParam(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid conversation role id")
		return
	}
	if err = h.service.DeleteConversationRole(c.Request.Context(), userID, publicID); err != nil {
		if errors.Is(err, appconversation.ErrConversationRoleNotFound) {
			response.Error(c, http.StatusNotFound, "conversation role not found")
			return
		}
		if errors.Is(err, appconversation.ErrConversationRoleInUseByAgentGroup) {
			response.ErrorWithCode(c, http.StatusConflict, "conversation.agent_group_role_in_use", "conversation role is in use by agent group member")
			return
		}
		response.Error(c, http.StatusInternalServerError, "delete conversation role failed")
		return
	}
	h.recordAudit(c, "delete_conversation_role", "conversation_role", publicID, nil)
	response.Success(c, gin.H{"deleted": true})
}

// ReorderConversationRoles godoc
// @Summary 角色排序
// @Description 更新当前用户角色展示顺序
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body ReorderConversationRolesRequest true "角色排序"
// @Success 200 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-roles/reorder [post]
func (h *Handler) ReorderConversationRoles(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var req ReorderConversationRolesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	if err := h.service.ReorderConversationRoles(c.Request.Context(), userID, req.RoleIDs); err != nil {
		if errors.Is(err, appconversation.ErrConversationRoleNotFound) {
			response.Error(c, http.StatusNotFound, "conversation role not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, "reorder conversation roles failed")
		return
	}
	response.Success(c, gin.H{"reordered": true})
}
