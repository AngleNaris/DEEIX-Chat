package doccard

import (
	"net/http"

	"github.com/gin-gonic/gin"

	appdoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/doccard"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
)

// UpsertDocCardRequest 创建/更新卡片的请求体。
type UpsertDocCardRequest struct {
	Title    string   `json:"title" binding:"required,max=128"`
	Content  string   `json:"content" binding:"required,max=20000"`
	Keywords []string `json:"keywords" binding:"max=20"`
	Enabled  *bool    `json:"enabled"`
}

// CardIDParam 路径参数。
type CardIDParam struct {
	ID string `uri:"id" binding:"required"`
}

// Handler 文档卡片域 HTTP 处理器。
type Handler struct {
	svc *appdoccard.Service
}

// NewHandler 创建处理器。
func NewHandler(svc *appdoccard.Service) *Handler {
	return &Handler{svc: svc}
}

// CreateDocCard 创建卡片。
func (h *Handler) CreateDocCard(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var req UpsertDocCardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.svc.UpsertDocCard(c, userID, "", appdoccard.UpsertInput{
		Title:    req.Title,
		Content:  req.Content,
		Keywords: req.Keywords,
		Enabled:  req.Enabled,
	}, "user")
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, item)
}

// UpdateDocCard 更新卡片。
func (h *Handler) UpdateDocCard(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var param CardIDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	var req UpsertDocCardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.svc.UpsertDocCard(c, userID, param.ID, appdoccard.UpsertInput{
		Title:    req.Title,
		Content:  req.Content,
		Keywords: req.Keywords,
		Enabled:  req.Enabled,
	}, "user")
	if err != nil {
		response.Error(c, http.StatusNotFound, "doc card not found")
		return
	}
	response.Success(c, item)
}

// ListDocCards 列出全部卡片。
func (h *Handler) ListDocCards(c *gin.Context) {
	userID := middleware.MustUserID(c)
	items, err := h.svc.ListDocCards(c, userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, items)
}

// DeleteDocCard 删除卡片。
func (h *Handler) DeleteDocCard(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var param CardIDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.DeleteDocCard(c, userID, param.ID); err != nil {
		response.Error(c, http.StatusNotFound, "doc card not found")
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
