package dynamicprompt

import (
	"net/http"

	"github.com/gin-gonic/gin"

	appdynamicprompt "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/dynamicprompt"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
)

// UpsertPromptRequest 创建/更新动态提示词的请求体。
type UpsertPromptRequest struct {
	Name    string `json:"name" binding:"required,max=64"`
	Kind    string `json:"kind" binding:"omitempty,oneof=js text"`
	Content string `json:"content" binding:"required,max=20000"`
	Enabled *bool  `json:"enabled"`
}

// PromptIDParam 路径参数。
type PromptIDParam struct {
	ID string `uri:"id" binding:"required"`
}

// Handler 动态提示词域 HTTP 处理器。
type Handler struct {
	svc *appdynamicprompt.Service
}

// NewHandler 创建处理器。
func NewHandler(svc *appdynamicprompt.Service) *Handler {
	return &Handler{svc: svc}
}

// CreatePrompt 创建动态提示词。
func (h *Handler) CreatePrompt(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var req UpsertPromptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.svc.UpsertDynamicPrompt(c, userID, "", appdynamicprompt.UpsertInput{
		Name:    req.Name,
		Kind:    req.Kind,
		Content: req.Content,
		Enabled: req.Enabled,
	}, "user")
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, item)
}

// UpdatePrompt 更新动态提示词。
func (h *Handler) UpdatePrompt(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var param PromptIDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	var req UpsertPromptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.svc.UpsertDynamicPrompt(c, userID, param.ID, appdynamicprompt.UpsertInput{
		Name:    req.Name,
		Kind:    req.Kind,
		Content: req.Content,
		Enabled: req.Enabled,
	}, "user")
	if err != nil {
		response.Error(c, http.StatusNotFound, "dynamic prompt not found")
		return
	}
	response.Success(c, item)
}

// ListPrompts 列出全部动态提示词。
func (h *Handler) ListPrompts(c *gin.Context) {
	userID := middleware.MustUserID(c)
	items, err := h.svc.ListDynamicPrompts(c, userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, items)
}

// DeletePrompt 删除动态提示词。
func (h *Handler) DeletePrompt(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var param PromptIDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.DeleteDynamicPrompt(c, userID, param.ID); err != nil {
		response.Error(c, http.StatusNotFound, "dynamic prompt not found")
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
