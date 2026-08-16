package credentials

import (
	"errors"
	"net/http"
	"strings"

	appcredentials "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/credentials"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

// Handler 封装用户凭据 HTTP 处理。
type Handler struct {
	service *appcredentials.Service
}

// NewHandler 创建处理器。
func NewHandler(service *appcredentials.Service) *Handler {
	return &Handler{service: service}
}

// CreateCredentialRequest 创建凭据请求。
type CreateCredentialRequest struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Value       string            `json:"value"`
	Meta        map[string]string `json:"meta"`
}

// UpdateCredentialRequest 更新凭据请求（value 为空表示不修改密钥）。
type UpdateCredentialRequest struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Value       string            `json:"value"`
	Meta        map[string]string `json:"meta"`
}

// CredentialListResponse 凭据列表响应。
type CredentialListResponse struct {
	Results []CredentialResponseItem `json:"results"`
}

// CredentialResponseItem 凭据传输响应 DTO（不包含密钥值）。
type CredentialResponseItem struct {
	PublicID    string            `json:"public_id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Meta        map[string]string `json:"meta,omitempty"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

// CredentialResponse 单个凭据响应。
type CredentialResponse struct {
	Credential CredentialResponseItem `json:"credential"`
}

func toCredentialResponseItem(view appcredentials.View) CredentialResponseItem {
	return CredentialResponseItem{
		PublicID: view.PublicID, Name: view.Name, Type: view.Type, Description: view.Description,
		Meta: view.Meta, CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
	}
}

func toCredentialResponseItems(views []appcredentials.View) []CredentialResponseItem {
	items := make([]CredentialResponseItem, 0, len(views))
	for _, view := range views {
		items = append(items, toCredentialResponseItem(view))
	}
	return items
}

// CredentialListResponseDoc 包裹凭据列表响应。
type CredentialListResponseDoc struct {
	ErrorMsg string                 `json:"errorMsg"`
	Data     CredentialListResponse `json:"data"`
}

// CredentialResponseDoc 包裹单个凭据响应。
type CredentialResponseDoc struct {
	ErrorMsg string             `json:"errorMsg"`
	Data     CredentialResponse `json:"data"`
}

// ErrorDoc 凭据接口错误响应。
type ErrorDoc struct {
	ErrorMsg string `json:"errorMsg"`
}

// ListCredentials godoc
// @Summary 凭据列表
// @Description 查询当前用户保存的凭据（不包含密钥值）
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} CredentialListResponseDoc
// @Failure 500 {object} ErrorDoc
// @Router /credentials [get]
func (h *Handler) ListCredentials(c *gin.Context) {
	userID := middleware.MustUserID(c)
	views, err := h.service.ListCredentials(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "list credentials failed")
		return
	}
	response.Success(c, CredentialListResponse{Results: toCredentialResponseItems(views)})
}

// CreateCredential godoc
// @Summary 创建凭据
// @Description 保存一条命名凭据（SSH/API key 等），密钥加密存储且永不回显
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body CreateCredentialRequest true "凭据信息"
// @Success 200 {object} CredentialResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /credentials [post]
func (h *Handler) CreateCredential(c *gin.Context) {
	userID := middleware.MustUserID(c)
	var req CreateCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	view, err := h.service.CreateCredential(c.Request.Context(), userID, appcredentials.UpsertInput{
		Name:        req.Name,
		Type:        req.Type,
		Description: req.Description,
		Value:       req.Value,
		Meta:        req.Meta,
	})
	if err != nil {
		switch {
		case errors.Is(err, appcredentials.ErrNameConflict):
			response.Error(c, http.StatusConflict, "credential name already exists")
		default:
			response.Error(c, http.StatusBadRequest, err.Error())
		}
		return
	}
	response.Success(c, CredentialResponse{Credential: toCredentialResponseItem(*view)})
}

// UpdateCredential godoc
// @Summary 更新凭据
// @Description 更新凭据名称/类型/描述/元数据；value 为空表示不修改密钥
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "凭据公开 ID"
// @Param body body UpdateCredentialRequest true "更新项"
// @Success 200 {object} CredentialResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /credentials/{id} [put]
func (h *Handler) UpdateCredential(c *gin.Context) {
	userID := middleware.MustUserID(c)
	publicID := strings.TrimSpace(c.Param("id"))
	var req UpdateCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	view, err := h.service.UpdateCredential(c.Request.Context(), userID, publicID, appcredentials.UpsertInput{
		Name:        req.Name,
		Type:        req.Type,
		Description: req.Description,
		Value:       req.Value,
		Meta:        req.Meta,
	})
	if err != nil {
		switch {
		case errors.Is(err, appcredentials.ErrCredentialNotFound):
			response.Error(c, http.StatusNotFound, "credential not found")
		case errors.Is(err, appcredentials.ErrNameConflict):
			response.Error(c, http.StatusConflict, "credential name already exists")
		default:
			response.Error(c, http.StatusBadRequest, err.Error())
		}
		return
	}
	response.Success(c, CredentialResponse{Credential: toCredentialResponseItem(*view)})
}

// DeleteCredential godoc
// @Summary 删除凭据
// @Description 删除一条凭据（软删除）
// @Tags chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "凭据公开 ID"
// @Success 200 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /credentials/{id} [delete]
func (h *Handler) DeleteCredential(c *gin.Context) {
	userID := middleware.MustUserID(c)
	publicID := strings.TrimSpace(c.Param("id"))
	if err := h.service.DeleteCredential(c.Request.Context(), userID, publicID); err != nil {
		if errors.Is(err, appcredentials.ErrCredentialNotFound) {
			response.Error(c, http.StatusNotFound, "credential not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, "delete credential failed")
		return
	}
	response.Success(c, map[string]bool{"deleted": true})
}
