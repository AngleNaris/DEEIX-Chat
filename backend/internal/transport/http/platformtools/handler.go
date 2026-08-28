package platformtools

import (
	"errors"
	"net/http"

	appconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

// Handler 封装平台工具（platform tools）HTTP 处理：ask 模式下写操作的批准/拒绝。
type Handler struct {
	service *appconversation.Service
}

// NewHandler 创建处理器。
func NewHandler(service *appconversation.Service) *Handler {
	return &Handler{service: service}
}

// GetApproval godoc
// @Summary 查询平台工具写操作批准状态
// @Description 查询当前单实例进程内、属于当前用户的批准记录；服务重启或记录过期后返回 404
// @Tags platform-tools
// @Produce json
// @Security BearerAuth
// @Param approval_id path string true "待批准记录 ID"
// @Success 200 {object} ApprovalResponse
// @Failure 404 {object} response.Envelope
// @Router /platform-tools/approvals/{approval_id} [get]
func (h *Handler) GetApproval(c *gin.Context) {
	approvalID := c.Param("approval_id")
	if approvalID == "" {
		response.Error(c, http.StatusBadRequest, "approval_id is required")
		return
	}
	summary, err := h.service.GetPlatformWriteApproval(approvalID, middleware.MustUserID(c))
	if err != nil {
		response.ErrorFrom(c, http.StatusNotFound, err)
		return
	}
	response.Success(c, ApprovalResponse{Approval: summary})
}

// Approve godoc
// @Summary 批准平台工具写操作
// @Description 批准一条待确认的平台工具写操作（ask 批准模式），批准后异步执行
// @Tags platform-tools
// @Produce json
// @Security BearerAuth
// @Param approval_id path string true "待批准记录 ID"
// @Success 200 {object} ApprovalResponse
// @Failure 403 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Failure 500 {object} response.Envelope
// @Router /platform-tools/approvals/{approval_id}/approve [post]
func (h *Handler) Approve(c *gin.Context) {
	h.act(c, true)
}

// Reject godoc
// @Summary 拒绝平台工具写操作
// @Description 拒绝一条待确认的平台工具写操作（ask 批准模式），不执行
// @Tags platform-tools
// @Produce json
// @Security BearerAuth
// @Param approval_id path string true "待批准记录 ID"
// @Success 200 {object} ApprovalResponse
// @Failure 404 {object} response.Envelope
// @Failure 500 {object} response.Envelope
// @Router /platform-tools/approvals/{approval_id}/reject [post]
func (h *Handler) Reject(c *gin.Context) {
	h.act(c, false)
}

func (h *Handler) act(c *gin.Context, approve bool) {
	approvalID := c.Param("approval_id")
	if approvalID == "" {
		response.Error(c, http.StatusBadRequest, "approval_id is required")
		return
	}
	userID := middleware.MustUserID(c)
	summary, err := h.service.ApprovePlatformWrite(c.Request.Context(), approvalID, userID, approve)
	if err != nil {
		switch {
		case errors.Is(err, appconversation.ErrPlatformWriteDisabled):
			response.ErrorFrom(c, http.StatusForbidden, err)
		case errors.Is(err, appconversation.ErrPlatformApprovalNotFound):
			response.ErrorFrom(c, http.StatusNotFound, err)
		default:
			response.ErrorFrom(c, http.StatusInternalServerError, err)
		}
		return
	}
	response.Success(c, ApprovalResponse{Approval: summary})
}
