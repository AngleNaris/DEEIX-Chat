package skill

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	appskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/skill"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

// Handler 封装技能 HTTP 处理。
type Handler struct {
	service *appskill.Service
}

// NewHandler 创建技能处理器。
func NewHandler(service *appskill.Service) *Handler {
	return &Handler{service: service}
}

// maxPackageZipUploadBytes 技能包 zip 上传大小上限（应用层同值）。
const maxPackageZipUploadBytes = 10 << 20

// ListVisibleSkills godoc
// @Summary 查询当前用户可用技能
// @Description 返回管理员内置和当前用户自定义的已启用技能摘要，用于会话按需选择 Skill 上下文
// @Tags skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param q query string false "搜索关键词"
// @Param id query []int false "按技能 ID 筛选，可重复传递" collectionFormat(multi)
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} SkillSummaryPageResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /skills [get]
func (h *Handler) ListVisibleSkills(c *gin.Context) {
	page, pageSize := pageParams(c)
	rawIDs := c.QueryArray("id")
	if len(rawIDs) > config.MaxMCPSelectedToolsPerMessage {
		response.ErrorWithCode(c, http.StatusBadRequest, "skill.too_many_ids", "too many skill ids")
		return
	}
	ids := make([]uint, 0, len(rawIDs))
	seenIDs := make(map[uint]struct{}, len(rawIDs))
	for _, rawID := range rawIDs {
		parsed, err := strconv.ParseUint(rawID, 10, strconv.IntSize)
		if err != nil || parsed == 0 {
			response.Error(c, http.StatusBadRequest, "invalid skill id")
			return
		}
		id := uint(parsed)
		if _, exists := seenIDs[id]; exists {
			continue
		}
		seenIDs[id] = struct{}{}
		ids = append(ids, id)
	}
	items, total, err := h.service.ListVisible(c.Request.Context(), middleware.MustUserID(c), appskill.ListInput{
		IDs:      ids,
		Query:    c.Query("q"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		writeSkillError(c, err)
		return
	}
	response.SuccessPage(c, total, toSkillSummaryResponses(items))
}

// GetVisibleSkill godoc
// @Summary 查询当前用户可用技能详情
// @Description 按需返回单个可用 Skill 的完整 SKILL.md 内容，用于用户查看详情
// @Tags skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "技能ID"
// @Success 200 {object} SkillResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /skills/{id} [get]
func (h *Handler) GetVisibleSkill(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	item, err := h.service.ResolveAvailable(c.Request.Context(), middleware.MustUserID(c), id)
	if err != nil {
		writeSkillError(c, err)
		return
	}
	response.Success(c, SkillDataResponse{Skill: toSkillResponse(*item)})
}

// ListMySkills godoc
// @Summary 查询我的自定义技能
// @Tags skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param q query string false "搜索关键词"
// @Param enabled query bool false "是否启用"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} SkillPageResponseDoc
// @Failure 500 {object} ErrorDoc
// @Router /skills/mine [get]
func (h *Handler) ListMySkills(c *gin.Context) {
	page, pageSize := pageParams(c)
	items, total, err := h.service.ListMine(c.Request.Context(), middleware.MustUserID(c), appskill.ListInput{
		Query:    c.Query("q"),
		Enabled:  boolQuery(c, "enabled"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		writeSkillError(c, err)
		return
	}
	response.SuccessPage(c, total, toSkillResponses(items))
}

// CreateMySkill godoc
// @Summary 创建我的自定义技能
// @Tags skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body WriteSkillRequest true "技能配置"
// @Success 200 {object} SkillResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /skills/mine [post]
func (h *Handler) CreateMySkill(c *gin.Context) {
	var req WriteSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	item, err := h.service.CreateUser(c.Request.Context(), middleware.MustUserID(c), writeInputFromRequest(req))
	if err != nil {
		writeSkillError(c, err)
		return
	}
	response.Success(c, SkillDataResponse{Skill: toSkillResponse(*item)})
}

// PatchMySkill godoc
// @Summary 更新我的自定义技能
// @Tags skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "技能ID"
// @Param body body PatchSkillRequest true "更新字段"
// @Success 200 {object} SkillResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /skills/mine/{id} [patch]
func (h *Handler) PatchMySkill(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req PatchSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	item, err := h.service.UpdateUser(c.Request.Context(), middleware.MustUserID(c), id, patchInputFromRequest(req))
	if err != nil {
		writeSkillError(c, err)
		return
	}
	response.Success(c, SkillDataResponse{Skill: toSkillResponse(*item)})
}

// DeleteMySkill godoc
// @Summary 删除我的自定义技能
// @Tags skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "技能ID"
// @Success 200 {object} SkillDeleteResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /skills/mine/{id} [delete]
func (h *Handler) DeleteMySkill(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := h.service.DeleteUser(c.Request.Context(), middleware.MustUserID(c), id); err != nil {
		writeSkillError(c, err)
		return
	}
	response.Success(c, SkillDeleteDataResponse{Deleted: true})
}

// ListAdminSkills godoc
// @Summary 管理员查询内置技能
// @Tags admin-skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param q query string false "搜索关键词"
// @Param enabled query bool false "是否启用"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} SkillPageResponseDoc
// @Failure 500 {object} ErrorDoc
// @Router /admin/skills [get]
func (h *Handler) ListAdminSkills(c *gin.Context) {
	page, pageSize := pageParams(c)
	items, total, err := h.service.ListAdminBuiltin(c.Request.Context(), appskill.ListInput{
		Query:    c.Query("q"),
		Enabled:  boolQuery(c, "enabled"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		writeSkillError(c, err)
		return
	}
	response.SuccessPage(c, total, toSkillResponses(items))
}

// CreateAdminSkill godoc
// @Summary 管理员创建内置技能
// @Tags admin-skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body WriteSkillRequest true "技能配置"
// @Success 200 {object} SkillResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /admin/skills [post]
func (h *Handler) CreateAdminSkill(c *gin.Context) {
	var req WriteSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	userID := middleware.MustUserID(c)
	item, err := h.service.CreateBuiltin(c.Request.Context(), userID, writeInputFromRequest(req))
	if err != nil {
		writeSkillError(c, err)
		return
	}
	h.service.RecordAudit(c.Request.Context(), auditInput(c, "skill.create_builtin", item.ID, map[string]interface{}{"trigger": item.Trigger}))
	response.Success(c, SkillDataResponse{Skill: toSkillResponse(*item)})
}

// PatchAdminSkill godoc
// @Summary 管理员更新内置技能
// @Tags admin-skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "技能ID"
// @Param body body PatchSkillRequest true "更新字段"
// @Success 200 {object} SkillResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /admin/skills/{id} [patch]
func (h *Handler) PatchAdminSkill(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req PatchSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	item, err := h.service.UpdateBuiltin(c.Request.Context(), middleware.MustUserID(c), id, patchInputFromRequest(req))
	if err != nil {
		writeSkillError(c, err)
		return
	}
	h.service.RecordAudit(c.Request.Context(), auditInput(c, "skill.update_builtin", item.ID, map[string]interface{}{"trigger": item.Trigger}))
	response.Success(c, SkillDataResponse{Skill: toSkillResponse(*item)})
}

// DeleteAdminSkill godoc
// @Summary 管理员删除内置技能
// @Tags admin-skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "技能ID"
// @Success 200 {object} SkillDeleteResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /admin/skills/{id} [delete]
func (h *Handler) DeleteAdminSkill(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := h.service.DeleteBuiltin(c.Request.Context(), middleware.MustUserID(c), id); err != nil {
		writeSkillError(c, err)
		return
	}
	h.service.RecordAudit(c.Request.Context(), auditInput(c, "skill.delete_builtin", id, nil))
	response.Success(c, SkillDeleteDataResponse{Deleted: true})
}

// PreviewMySkillPackage godoc
// @Summary 解析我的技能包（zip 预览）
// @Description 上传 zip 技能包，解析 SKILL.md 与文件清单并返回预览，不落库
// @Tags skills
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "技能包 zip 文件"
// @Success 200 {object} SkillPackagePreviewDoc
// @Failure 400 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /skills/mine/import/preview [post]
func (h *Handler) PreviewMySkillPackage(c *gin.Context) {
	zipData, ok := readPackageZip(c)
	if !ok {
		return
	}
	preview, err := h.service.PreviewPackage(c.Request.Context(), zipData)
	if err != nil {
		writeSkillError(c, err)
		return
	}
	response.Success(c, PackagePreviewDataResponse{Preview: toPackagePreviewResponse(preview)})
}

// ImportMySkillPackage godoc
// @Summary 导入我的技能包（zip）
// @Description 上传 zip 技能包并创建为用户自定义包技能
// @Tags skills
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "技能包 zip 文件"
// @Success 200 {object} SkillResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /skills/mine/import [post]
func (h *Handler) ImportMySkillPackage(c *gin.Context) {
	zipData, ok := readPackageZip(c)
	if !ok {
		return
	}
	item, err := h.service.ImportUserPackage(c.Request.Context(), middleware.MustUserID(c), zipData)
	if err != nil {
		writeSkillError(c, err)
		return
	}
	response.Success(c, SkillDataResponse{Skill: toSkillResponse(*item)})
}

// ReplaceMySkillPackage godoc
// @Summary 重新上传我的技能包（zip）
// @Description 用新的 zip 覆盖当前用户包技能的文件与元数据
// @Tags skills
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param id path int true "技能ID"
// @Param file formData file true "技能包 zip 文件"
// @Success 200 {object} SkillResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /skills/mine/{id}/package [post]
func (h *Handler) ReplaceMySkillPackage(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	zipData, ok := readPackageZip(c)
	if !ok {
		return
	}
	item, err := h.service.ReplaceUserPackage(c.Request.Context(), middleware.MustUserID(c), id, zipData)
	if err != nil {
		writeSkillError(c, err)
		return
	}
	response.Success(c, SkillDataResponse{Skill: toSkillResponse(*item)})
}

// GetSkillPackageFile godoc
// @Summary 读取技能包内文件内容
// @Description 读取当前用户可用的包技能内文本文件内容（仅文本文件）
// @Tags skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "技能ID"
// @Param filepath path string true "包内相对路径，如 scripts/roll.py"
// @Success 200 {object} SkillPackageFileResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 415 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /skills/{id}/files/{filepath} [get]
func (h *Handler) GetSkillPackageFile(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	filePath := strings.TrimPrefix(c.Param("filepath"), "/")
	if filePath == "" {
		response.Error(c, http.StatusBadRequest, "invalid file path")
		return
	}
	content, err := h.service.GetPackageFile(c.Request.Context(), middleware.MustUserID(c), id, filePath)
	if err != nil {
		writeSkillError(c, err)
		return
	}
	response.Success(c, SkillPackageFileDataResponse{
		File: SkillPackageFileResponse{
			Path:    filePath,
			Content: string(content),
		},
	})
}

// PreviewAdminSkillPackage godoc
// @Summary 管理员解析技能包（zip 预览）
// @Tags admin-skills
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "技能包 zip 文件"
// @Success 200 {object} SkillPackagePreviewDoc
// @Failure 400 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /admin/skills/import/preview [post]
func (h *Handler) PreviewAdminSkillPackage(c *gin.Context) {
	zipData, ok := readPackageZip(c)
	if !ok {
		return
	}
	preview, err := h.service.PreviewPackage(c.Request.Context(), zipData)
	if err != nil {
		writeSkillError(c, err)
		return
	}
	response.Success(c, PackagePreviewDataResponse{Preview: toPackagePreviewResponse(preview)})
}

// ImportAdminSkillPackage godoc
// @Summary 管理员导入内置技能包（zip）
// @Tags admin-skills
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "技能包 zip 文件"
// @Success 200 {object} SkillResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /admin/skills/import [post]
func (h *Handler) ImportAdminSkillPackage(c *gin.Context) {
	zipData, ok := readPackageZip(c)
	if !ok {
		return
	}
	userID := middleware.MustUserID(c)
	item, err := h.service.ImportBuiltinPackage(c.Request.Context(), userID, zipData)
	if err != nil {
		writeSkillError(c, err)
		return
	}
	h.service.RecordAudit(c.Request.Context(), auditInput(c, "skill.import_builtin_package", item.ID, map[string]interface{}{"trigger": item.Trigger, "files": len(item.PackageFiles)}))
	response.Success(c, SkillDataResponse{Skill: toSkillResponse(*item)})
}

// ReplaceAdminSkillPackage godoc
// @Summary 管理员重新上传内置技能包（zip）
// @Tags admin-skills
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param id path int true "技能ID"
// @Param file formData file true "技能包 zip 文件"
// @Success 200 {object} SkillResponseDoc
// @Failure 400 {object} ErrorDoc
// @Failure 404 {object} ErrorDoc
// @Failure 409 {object} ErrorDoc
// @Failure 500 {object} ErrorDoc
// @Router /admin/skills/{id}/package [post]
func (h *Handler) ReplaceAdminSkillPackage(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	zipData, ok := readPackageZip(c)
	if !ok {
		return
	}
	userID := middleware.MustUserID(c)
	item, err := h.service.ReplaceBuiltinPackage(c.Request.Context(), userID, id, zipData)
	if err != nil {
		writeSkillError(c, err)
		return
	}
	h.service.RecordAudit(c.Request.Context(), auditInput(c, "skill.replace_builtin_package", item.ID, map[string]interface{}{"trigger": item.Trigger, "files": len(item.PackageFiles)}))
	response.Success(c, SkillDataResponse{Skill: toSkillResponse(*item)})
}

// readPackageZip 读取 multipart 上传的 zip 文件内容。
func readPackageZip(c *gin.Context) ([]byte, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPackageZipUploadBytes)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "skill package file is required")
		return nil, false
	}
	if fileHeader.Size > maxPackageZipUploadBytes {
		response.Error(c, http.StatusBadRequest, "skill package file is too large")
		return nil, false
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, "skill package file is unreadable")
		return nil, false
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxPackageZipUploadBytes+1))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "skill package file is unreadable")
		return nil, false
	}
	return data, true
}

func writeInputFromRequest(req WriteSkillRequest) appskill.WriteInput {
	return appskill.WriteInput{
		Title:       req.Title,
		Trigger:     req.Trigger,
		Description: req.Description,
		Markdown:    req.Markdown,
		Enabled:     req.Enabled,
		SortOrder:   req.SortOrder,
	}
}

func patchInputFromRequest(req PatchSkillRequest) appskill.PatchInput {
	return appskill.PatchInput{
		Title:       req.Title,
		Trigger:     req.Trigger,
		Description: req.Description,
		Markdown:    req.Markdown,
		Enabled:     req.Enabled,
		SortOrder:   req.SortOrder,
	}
}

func idParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, strconv.IntSize)
	if err != nil || id == 0 {
		response.Error(c, http.StatusBadRequest, "invalid skill id")
		return 0, false
	}
	return uint(id), true
}

func boolQuery(c *gin.Context, key string) *bool {
	raw := c.Query(key)
	if raw == "" {
		return nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return nil
	}
	return &parsed
}

func pageParams(c *gin.Context) (int, int) {
	page := 1
	pageSize := 20
	const maxPageSize = 100
	if raw := c.Query("page"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if raw := c.Query("page_size"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func auditInput(c *gin.Context, action string, resourceID uint, detail interface{}) appskill.AuditInput {
	return appskill.AuditInput{
		UserID:     middleware.MustUserID(c),
		RequestID:  middleware.MustRequestID(c),
		Action:     action,
		ResourceID: strconv.FormatUint(uint64(resourceID), 10),
		ClientIP:   c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
		Detail:     detail,
	}
}

func writeSkillError(c *gin.Context, err error) {
	if errors.Is(err, appskill.ErrSkillNotFound) {
		response.Error(c, http.StatusNotFound, "skill not found")
		return
	}
	if errors.Is(err, appskill.ErrSkillConflict) {
		response.Error(c, http.StatusConflict, "skill trigger already exists")
		return
	}
	if errors.Is(err, appskill.ErrInvalidSkill) {
		response.ErrorFrom(c, http.StatusBadRequest, err)
		return
	}
	if errors.Is(err, appskill.ErrInvalidPackage) {
		response.ErrorFrom(c, http.StatusBadRequest, err)
		return
	}
	if errors.Is(err, appskill.ErrPackageFileNotFound) {
		response.ErrorFrom(c, http.StatusNotFound, err)
		return
	}
	if errors.Is(err, appskill.ErrPackageFileUnreadable) {
		response.ErrorFrom(c, http.StatusUnsupportedMediaType, err)
		return
	}
	response.Error(c, http.StatusInternalServerError, "skill operation failed")
}
