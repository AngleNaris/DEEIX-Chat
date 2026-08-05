package agentgroup

import (
	"errors"
	"net/http"
	"strings"

	appconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/conversation"
	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

// GetAgentGroupRunDetail godoc
// @Summary 运行详情
// @Description 查询群组运行及其步骤、尝试完整视图
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param run_id path string true "运行 public_id"
// @Success 200 {object} AgentGroupRunDetailResponseDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-group-runs/{run_id} [get]
func (h *Handler) GetAgentGroupRunDetail(c *gin.Context) {
	userID := middleware.MustUserID(c)
	runPublicID, err := stringParam(c, "run_id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid run id")
		return
	}
	detail, err := h.service.GetAgentGroupRunDetail(c.Request.Context(), userID, runPublicID)
	if err != nil {
		resolveError(c, err, http.StatusInternalServerError, "get agent group run detail failed")
		return
	}
	response.Success(c, toAgentGroupRunDetailResponse(detail))
}

// LookupAgentGroupRunDetailByClientRunID godoc
// @Summary 按会话与流式运行 ID 查询运行详情
// @Description 前端消息只持有 clientRunID，刷新页面后据此恢复群组运行时间线（会话经公开 ID 解析）
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param conversationID query string true "会话公开 ID"
// @Param clientRunID query string true "父流式运行 ID"
// @Success 200 {object} AgentGroupRunDetailResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-group-runs/lookup [get]
func (h *Handler) LookupAgentGroupRunDetailByClientRunID(c *gin.Context) {
	userID := middleware.MustUserID(c)
	conversationPublicID := strings.TrimSpace(c.Query("conversationID"))
	clientRunID := strings.TrimSpace(c.Query("clientRunID"))
	if conversationPublicID == "" || clientRunID == "" {
		response.Error(c, http.StatusBadRequest, "conversationID and clientRunID are required")
		return
	}
	conversation, err := h.runControl.GetConversationByPublicID(c.Request.Context(), userID, conversationPublicID)
	if err != nil {
		if errors.Is(err, appconversation.ErrConversationNotFound) {
			response.Error(c, http.StatusNotFound, "conversation not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, "lookup agent group run detail failed")
		return
	}
	detail, err := h.service.GetAgentGroupRunDetailByClientRunID(c.Request.Context(), userID, conversation.ID, clientRunID)
	if err != nil {
		resolveError(c, err, http.StatusInternalServerError, "lookup agent group run detail failed")
		return
	}
	response.Success(c, toAgentGroupRunDetailResponse(detail))
}

// GetAgentGroupFeature godoc
// @Summary 群组功能开关
// @Description 查询 Agent 群组功能是否启用
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} AgentGroupFeatureResponseDoc
// @Failure 500 {object} ErrorDoc
// @Router /conversation-agent-groups/feature [get]
func (h *Handler) GetAgentGroupFeature(c *gin.Context) {
	enabled, err := h.service.IsEnabled(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "query agent group feature failed")
		return
	}
	response.Success(c, AgentGroupFeatureResponse{Enabled: enabled})
}

func toAgentGroupRunDetailResponse(detail *domainagentgroup.RunDetail) AgentGroupRunDetailResponse {
	result := AgentGroupRunDetailResponse{
		Run:   toAgentGroupRunResponse(&detail.Run),
		Steps: make([]AgentGroupStepResponse, 0, len(detail.Steps)),
	}
	for i := range detail.Steps {
		result.Steps = append(result.Steps, toAgentGroupStepResponse(&detail.Steps[i].Step, detail.Steps[i].Attempts))
	}
	return result
}
