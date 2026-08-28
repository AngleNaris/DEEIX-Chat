package agentgroup

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"

	appconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

// mapRunControlError 将运行控制错误映射为 (httpStatus, stableCode, publicMessage)。
// 稳定错误码全部使用小写点分隔（agent_group.*），前端据此做状态分支。
func mapRunControlError(err error) (int, string, string) {
	switch {
	case errors.Is(err, appconversation.ErrAgentGroupRunNotFound):
		return http.StatusNotFound, "agent_group.run_not_found", "agent group run not found"
	case errors.Is(err, appconversation.ErrAgentGroupRunNotRetryable):
		return http.StatusConflict, "agent_group.run_not_retryable", "agent group run is not retryable"
	case errors.Is(err, appconversation.ErrAgentGroupRunNotCancelable):
		return http.StatusConflict, "agent_group.run_not_cancelable", "agent group run is not cancelable"
	case errors.Is(err, appconversation.ErrAgentGroupRunNotAbandonable):
		return http.StatusConflict, "agent_group.run_not_abandonable", "agent group run is not abandonable"
	case errors.Is(err, appconversation.ErrAgentGroupRunStateCorrupt):
		return http.StatusInternalServerError, "agent_group.run_state_corrupt", "agent group run state is corrupt"
	case errors.Is(err, appconversation.ErrAgentGroupRetryRequestIDRequired):
		return http.StatusBadRequest, "agent_group.retry_request_id_required", "retry request id is required"
	case errors.Is(err, appconversation.ErrAgentGroupCASConflict):
		return http.StatusConflict, "agent_group.cas_conflict", "agent group run state changed concurrently"
	}
	return http.StatusInternalServerError, "agent_group.run_control_failed", "agent group run control failed"
}

// resolveRunControlError 将运行控制错误写为 JSON 错误响应。
func resolveRunControlError(c *gin.Context, err error, defaultStatus int, defaultMsg string) {
	status, code, message := mapRunControlError(err)
	if status >= http.StatusInternalServerError && !errors.Is(err, appconversation.ErrAgentGroupRunStateCorrupt) {
		status, message = defaultStatus, defaultMsg
	}
	response.ErrorWithCode(c, status, code, message)
}

// runControlStreamErrorPayload 构建 NDJSON 错误事件（稳定错误码，不暴露内部细节）。
func runControlStreamErrorPayload(err error) map[string]interface{} {
	_, code, message := mapRunControlError(err)
	return map[string]interface{}{
		"type":      "error",
		"errorCode": code,
		"message":   message,
	}
}

// RetryAgentGroupRunStep godoc
// @Summary 重试失败步骤
// @Description 从暂停的失败步骤原地重试：复用原配置快照，仅为该步骤追加一次新 Attempt，NDJSON 流式返回群组事件与正文增量
// @Tags chat
// @Accept json
// @Produce application/x-ndjson
// @Security BearerAuth
// @Param run_id path string true "运行 public_id"
// @Param step_id path string true "步骤 public_id"
// @Param body body AgentGroupStepRetryRequest true "重试参数（retryRequestID 防止双击重复）"
// @Success 200 {string} string "NDJSON stream"
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /agent-group-runs/{run_id}/steps/{step_id}/retry [post]
func (h *Handler) RetryAgentGroupRunStep(c *gin.Context) {
	userID := middleware.MustUserID(c)
	runPublicID, err := stringParam(c, "run_id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid run id")
		return
	}
	stepPublicID, err := stringParam(c, "step_id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid step id")
		return
	}
	var req AgentGroupStepRetryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	retryRequestID := strings.TrimSpace(req.RetryRequestID)
	if retryRequestID == "" {
		response.Error(c, http.StatusBadRequest, "retry request id is required")
		return
	}

	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	var clientDisconnected atomic.Bool
	flushStreamEvent := func(payload map[string]interface{}) error {
		if clientDisconnected.Load() {
			return nil
		}
		encoded, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return marshalErr
		}
		if _, writeErr := c.Writer.Write(append(encoded, '\n')); writeErr != nil {
			clientDisconnected.Store(true)
			return writeErr
		}
		c.Writer.Flush()
		return nil
	}

	input := appconversation.RetryAgentGroupRunInput{
		UserID:       userID,
		RunPublicID:  runPublicID,
		StepPublicID: stepPublicID,
		RequestID:    retryRequestID,
		Stream:       true,
		Cancelable:   true,
		OnEvent: func(eventType string, payload map[string]interface{}) error {
			payload["type"] = eventType
			_ = flushStreamEvent(payload)
			return nil
		},
	}

	result, err := h.runControl.RetryAgentGroupRunStep(c.Request.Context(), input, func(delta string) error {
		_ = flushStreamEvent(map[string]interface{}{
			"type":  "delta",
			"delta": delta,
		})
		return nil
	})
	if err != nil {
		payload := runControlStreamErrorPayload(err)
		if result != nil {
			payload["data"] = AgentGroupRunControlResult{Status: "paused_retryable"}
		}
		_ = flushStreamEvent(payload)
		return
	}
	h.recordAudit(c, "retry_agent_group_run_step", "agent_group_run", runPublicID,
		map[string]string{"step_id": stepPublicID})
	_ = flushStreamEvent(map[string]interface{}{
		"type": "completed",
		"data": AgentGroupRunControlResult{Status: "completed"},
	})
}

// CancelAgentGroupRun godoc
// @Summary 取消群组运行
// @Description 取消进行中的群组运行：中断当前 Attempt，运行回到 paused_retryable
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param run_id path string true "运行 public_id"
// @Success 200 {object} AgentGroupRunCancelResponseDoc
// @Failure 404 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /agent-group-runs/{run_id}/cancel [post]
func (h *Handler) CancelAgentGroupRun(c *gin.Context) {
	runPublicID, err := stringParam(c, "run_id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid run id")
		return
	}
	canceled, err := h.runControl.CancelAgentGroupRun(c.Request.Context(), middleware.MustUserID(c), runPublicID)
	if err != nil {
		resolveRunControlError(c, err, http.StatusInternalServerError, "cancel agent group run failed")
		return
	}
	h.recordAudit(c, "cancel_agent_group_run", "agent_group_run", runPublicID,
		map[string]bool{"canceled": canceled})
	response.Success(c, AgentGroupRunCancelResponse{Canceled: canceled})
}

// AbandonAgentGroupRun godoc
// @Summary 放弃群组运行
// @Description 放弃已暂停或被阻塞的群组运行：消息标记结局，运行不再可重试
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param run_id path string true "运行 public_id"
// @Success 200 {object} AgentGroupRunAbandonResponseDoc
// @Failure 404 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /agent-group-runs/{run_id}/abandon [post]
func (h *Handler) AbandonAgentGroupRun(c *gin.Context) {
	runPublicID, err := stringParam(c, "run_id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid run id")
		return
	}
	if _, err := h.runControl.AbandonAgentGroupRun(c.Request.Context(), middleware.MustUserID(c), runPublicID); err != nil {
		resolveRunControlError(c, err, http.StatusInternalServerError, "abandon agent group run failed")
		return
	}
	h.recordAudit(c, "abandon_agent_group_run", "agent_group_run", runPublicID, nil)
	response.Success(c, AgentGroupRunAbandonResponse{Status: "abandoned"})
}
