package agentgroup

import (
	"errors"
	"net/http"

	appagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/agentgroup"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

// Handler 群组 HTTP 处理器。
type Handler struct {
	service    *appagentgroup.Service
	runControl runControlService
}

// NewHandler 创建群组 HTTP 处理器。
func NewHandler(service *appagentgroup.Service, runControl runControlService) *Handler {
	return &Handler{service: service, runControl: runControl}
}

// recordAudit 记录群组域审计事件（§18：所有变更记录操作者与资源 ID）。
func (h *Handler) recordAudit(c *gin.Context, action string, resource string, resourceID string, detail interface{}) {
	h.service.RecordAudit(c.Request.Context(), appagentgroup.AuditInput{
		UserID:     middleware.MustUserID(c),
		RequestID:  middleware.MustRequestID(c),
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		ClientIP:   c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
		Detail:     detail,
	})
}

// resolveError 将应用层错误映射为 HTTP 状态与消息。
func resolveError(c *gin.Context, err error, defaultStatus int, defaultMsg string) {
	switch {
	case errors.Is(err, appagentgroup.ErrAgentGroupFeatureDisabled):
		response.ErrorWithCode(c, http.StatusForbidden, "FEATURE_DISABLED", "agent group feature disabled")
	case errors.Is(err, appagentgroup.ErrAgentGroupNotFound),
		errors.Is(err, appagentgroup.ErrAgentGroupRunNotFound),
		errors.Is(err, appagentgroup.ErrAgentGroupRoleNotFound):
		response.Error(c, http.StatusNotFound, "agent group not found")
	case errors.Is(err, appagentgroup.ErrAgentGroupHistoryExists),
		errors.Is(err, appagentgroup.ErrAgentGroupRunActive),
		errors.Is(err, appagentgroup.ErrAgentGroupDuplicateRun):
		response.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, appagentgroup.ErrInvalidAgentGroupName),
		errors.Is(err, appagentgroup.ErrInvalidAgentGroupDescription),
		errors.Is(err, appagentgroup.ErrInvalidCoordinationPrompt),
		errors.Is(err, appagentgroup.ErrInvalidDutyInstruction),
		errors.Is(err, appagentgroup.ErrInvalidReasoningEffort),
		errors.Is(err, appagentgroup.ErrInvalidAgentGroupMemberType),
		errors.Is(err, appagentgroup.ErrInvalidAgentGroupModelOverride),
		errors.Is(err, appagentgroup.ErrAgentGroupSupervisorRequired),
		errors.Is(err, appagentgroup.ErrAgentGroupSupervisorDuplicate),
		errors.Is(err, appagentgroup.ErrAgentGroupMemberDuplicate),
		errors.Is(err, appagentgroup.ErrAgentGroupMemberLimitExceeded),
		errors.Is(err, appagentgroup.ErrAgentGroupSupervisorNotRemovable),
		errors.Is(err, appagentgroup.ErrAgentGroupSupervisorProtected),
		errors.Is(err, appagentgroup.ErrAgentGroupInvalidMemberOrder):
		response.Error(c, http.StatusBadRequest, err.Error())
	default:
		response.Error(c, defaultStatus, defaultMsg)
	}
}
