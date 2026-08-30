package conversation

import (
	"errors"
	"net/http"
	"strings"
	"time"

	appconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/filecontent"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

type createFileShareRequest struct {
	ExpiresInHours int `json:"expires_in_hours" binding:"omitempty,min=1,max=720"`
}

// CreateFileShare godoc
// @Summary 创建文件分享
// @Description 创建或替换当前用户指定文件的公开分享链接
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param file_id path string true "文件 ID"
// @Param body body createFileShareRequest false "分享有效期"
// @Success 200 {object} FileShareResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /files/{file_id}/share [post]
func (h *Handler) CreateFileShare(c *gin.Context) {
	userID := middleware.MustUserID(c)
	fileID := strings.TrimSpace(c.Param("file_id"))
	var req createFileShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	var ttl time.Duration
	if req.ExpiresInHours > 0 {
		ttl = time.Duration(req.ExpiresInHours) * time.Hour
	}
	result, err := h.service.CreateFileShare(c.Request.Context(), userID, fileID, ttl)
	if err != nil {
		if errors.Is(err, appconversation.ErrInvalidFileReference) {
			response.Error(c, http.StatusBadRequest, "invalid file share")
			return
		}
		if errors.Is(err, appconversation.ErrFileNotFound) {
			response.Error(c, http.StatusNotFound, "file not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, "create file share failed")
		return
	}
	h.recordAudit(c, "create_file_share", "file", fileID, map[string]interface{}{"share_id": result.ShareID, "expires_at": result.ExpiresAt})
	response.Success(c, toFileShareResult(result))
}

// GetFileShare godoc
// @Summary 查询文件分享状态
// @Tags chat
// @Produce json
// @Security BearerAuth
// @Param file_id path string true "文件 ID"
// @Success 200 {object} FileShareResponseDoc
// @Failure 400 {object} ErrorDoc
// @Router /files/{file_id}/share [get]
func (h *Handler) GetFileShare(c *gin.Context) {
	result, err := h.service.GetFileShare(c.Request.Context(), middleware.MustUserID(c), c.Param("file_id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid file share")
		return
	}
	response.Success(c, toFileShareResult(result))
}

// RevokeFileShare godoc
// @Summary 撤销文件分享
// @Tags chat
// @Produce json
// @Security BearerAuth
// @Param file_id path string true "文件 ID"
// @Success 200 {object} FileShareRevokeResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Router /files/{file_id}/share [delete]
func (h *Handler) RevokeFileShare(c *gin.Context) {
	fileID := strings.TrimSpace(c.Param("file_id"))
	err := h.service.RevokeFileShare(c.Request.Context(), middleware.MustUserID(c), fileID)
	if err != nil {
		if errors.Is(err, appconversation.ErrFileShareNotFound) {
			response.Error(c, http.StatusNotFound, "file share not found")
			return
		}
		response.Error(c, http.StatusBadRequest, "invalid file share")
		return
	}
	h.recordAudit(c, "revoke_file_share", "file", fileID, nil)
	response.Success(c, gin.H{"revoked": true})
}

// GetPublicFileShare godoc
// @Summary 查询公开文件分享
// @Tags chat
// @Produce json
// @Param share_id path string true "分享 ID"
// @Success 200 {object} PublicFileShareResponseDoc
// @Failure 404 {object} ErrorDoc
// @Router /shared-files/{share_id} [get]
func (h *Handler) GetPublicFileShare(c *gin.Context) {
	result, err := h.service.GetPublicFileShare(c.Request.Context(), c.Param("share_id"))
	if err != nil {
		response.Error(c, http.StatusNotFound, "file share not found")
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, toPublicFileShareResult(result))
}

// GetPublicFileShareContent godoc
// @Summary 读取公开文件内容
// @Tags chat
// @Produce application/octet-stream
// @Param share_id path string true "分享 ID"
// @Success 200 {file} binary
// @Failure 404 {object} ErrorDoc
// @Router /shared-files/{share_id}/content [get]
func (h *Handler) GetPublicFileShareContent(c *gin.Context) {
	result, err := h.service.OpenPublicFileShareContent(c.Request.Context(), c.Param("share_id"))
	if err != nil {
		response.Error(c, http.StatusNotFound, "file share not found")
		return
	}
	_ = filecontent.Write(c, result, true)
}
