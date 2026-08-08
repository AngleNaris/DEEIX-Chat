package artifact

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	appartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/artifact"
	domainartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/artifact"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
)

// Handler 制品域 HTTP 处理器。
type Handler struct {
	svc *appartifact.Service
}

// NewHandler 创建处理器。
func NewHandler(svc *appartifact.Service) *Handler {
	return &Handler{svc: svc}
}

// CreateArtifact 保存或更新制品。
func (h *Handler) CreateArtifact(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var req CreateArtifactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	input := appartifact.CreateInput{
		Title: req.Title,
		Kind:  sanitizeKind(req.Kind),
		Code:  req.Code,
	}
	var item interface{}
	var err error
	if strings.TrimSpace(req.ArtifactID) != "" {
		item, err = h.svc.UpdateArtifact(c, userID, req.ArtifactID, input)
	} else {
		item, err = h.svc.CreateArtifact(c, userID, "", input)
	}
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, appartifact.ToDetailView(item.(*domainartifact.Artifact)))
}

// ListArtifacts 分页列出制品。
func (h *Handler) ListArtifacts(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var query PaginationQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	items, total, err := h.svc.ListArtifacts(c, userID, query.page(), query.pageSize())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{"total": total, "page": query.page(), "items": items})
}

// GetArtifact 返回制品详情。
func (h *Handler) GetArtifact(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var param ArtifactIDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.svc.GetArtifact(c, userID, param.ID)
	if err != nil {
		response.Error(c, http.StatusNotFound, "artifact not found")
		return
	}
	response.Success(c, appartifact.ToDetailView(item))
}

// DeleteArtifact 删除制品。
func (h *Handler) DeleteArtifact(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var param ArtifactIDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.DeleteArtifact(c, userID, param.ID); err != nil {
		response.Error(c, http.StatusNotFound, "artifact not found")
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// CreateShare 创建/重新生成分享。
func (h *Handler) CreateShare(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var param ArtifactIDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	share, err := h.svc.CreateShare(c, userID, param.ID)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, share)
}

// GetShare 返回当前分享状态。
func (h *Handler) GetShare(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var param ArtifactIDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	share, err := h.svc.GetShare(c, userID, param.ID)
	if err != nil {
		response.Error(c, http.StatusNotFound, "no active share")
		return
	}
	response.Success(c, share)
}

// RevokeShare 撤销分享。
func (h *Handler) RevokeShare(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var param ArtifactIDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.RevokeShare(c, userID, param.ID); err != nil {
		response.Error(c, http.StatusNotFound, "no active share")
		return
	}
	response.Success(c, gin.H{"revoked": true})
}

// GetPublicShare 公开读取分享（免认证）。
func (h *Handler) GetPublicShare(c *gin.Context) {
	var param ShareIDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	view, err := h.svc.GetPublicShare(c, param.ShareID)
	if err != nil {
		response.Error(c, http.StatusNotFound, "share not found")
		return
	}
	c.Header("Cache-Control", "public, max-age=60")
	response.Success(c, view)
}
