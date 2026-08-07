package skill

import (
	"time"

	appskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/skill"
	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
)

// PackageFileResponse 表示技能包内文件清单项。
type PackageFileResponse struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Kind string `json:"kind"`
}

// SkillResponse 表示技能响应。
type SkillResponse struct {
	ID              uint                  `json:"id"`
	Scope           string                `json:"scope"`
	Title           string                `json:"title"`
	Trigger         string                `json:"trigger"`
	Description     string                `json:"description"`
	Markdown        string                `json:"markdown"`
	PackageType     string                `json:"packageType"`
	PackageRootDir  string                `json:"packageRootDir,omitempty"`
	Files           []PackageFileResponse `json:"files,omitempty"`
	Enabled         bool                  `json:"enabled"`
	SortOrder       int                   `json:"sortOrder"`
	CreatedByUserID uint                  `json:"createdByUserID"`
	UpdatedByUserID uint                  `json:"updatedByUserID"`
	CreatedAt       time.Time             `json:"createdAt"`
	UpdatedAt       time.Time             `json:"updatedAt"`
}

// SkillSummaryResponse 表示技能发现列表响应，不包含 SKILL.md 内容。
type SkillSummaryResponse struct {
	ID          uint      `json:"id"`
	Scope       string    `json:"scope"`
	Title       string    `json:"title"`
	Trigger     string    `json:"trigger"`
	Description string    `json:"description"`
	PackageType string    `json:"packageType"`
	Enabled     bool      `json:"enabled"`
	SortOrder   int       `json:"sortOrder"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// SkillDataResponse 包裹单条技能响应。
type SkillDataResponse struct {
	Skill SkillResponse `json:"skill"`
}

// SkillDeleteDataResponse 表示删除响应。
type SkillDeleteDataResponse struct {
	Deleted bool `json:"deleted"`
}

// PackagePreviewResponse 表示技能包解析预览。
type PackagePreviewResponse struct {
	Title       string                `json:"title"`
	Trigger     string                `json:"trigger"`
	Description string                `json:"description"`
	Markdown    string                `json:"markdown"`
	RootDir     string                `json:"rootDir"`
	Files       []PackageFileResponse `json:"files"`
}

// PackagePreviewDataResponse 包裹技能包预览响应。
type PackagePreviewDataResponse struct {
	Preview PackagePreviewResponse `json:"preview"`
}

// SkillPackageFileResponse 表示技能包内文件内容响应。
type SkillPackageFileResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// SkillPackageFileDataResponse 包裹技能包文件内容响应。
type SkillPackageFileDataResponse struct {
	File SkillPackageFileResponse `json:"file"`
}

// WriteSkillRequest 表示创建技能请求。
type WriteSkillRequest struct {
	Title       string `json:"title" binding:"required,max=64"`
	Trigger     string `json:"trigger" binding:"required,max=64"`
	Description string `json:"description,omitempty" binding:"max=256"`
	Markdown    string `json:"markdown" binding:"required,max=10000"`
	Enabled     bool   `json:"enabled,omitempty"`
	SortOrder   int    `json:"sortOrder,omitempty"`
}

// PatchSkillRequest 表示更新技能请求。
type PatchSkillRequest struct {
	Title       *string `json:"title,omitempty" binding:"omitempty,max=64"`
	Trigger     *string `json:"trigger,omitempty" binding:"omitempty,max=64"`
	Description *string `json:"description,omitempty" binding:"omitempty,max=256"`
	Markdown    *string `json:"markdown,omitempty" binding:"omitempty,max=10000"`
	Enabled     *bool   `json:"enabled,omitempty"`
	SortOrder   *int    `json:"sortOrder,omitempty"`
}

// SkillSummaryPageResponseDoc 用于 Swagger 展示技能发现分页响应。
type SkillSummaryPageResponseDoc struct {
	ErrorMsg string `json:"errorMsg"`
	Data     struct {
		Total   int64                  `json:"total"`
		Results []SkillSummaryResponse `json:"results"`
	} `json:"data"`
}

// SkillPageResponseDoc 用于 Swagger 展示完整分页响应。
type SkillPageResponseDoc struct {
	ErrorMsg string `json:"errorMsg"`
	Data     struct {
		Total   int64           `json:"total"`
		Results []SkillResponse `json:"results"`
	} `json:"data"`
}

// SkillResponseDoc 用于 Swagger 展示单条响应。
type SkillResponseDoc struct {
	ErrorMsg string            `json:"errorMsg"`
	Data     SkillDataResponse `json:"data"`
}

// SkillDeleteResponseDoc 用于 Swagger 展示删除响应。
type SkillDeleteResponseDoc struct {
	ErrorMsg string                  `json:"errorMsg"`
	Data     SkillDeleteDataResponse `json:"data"`
}

// SkillPackagePreviewDoc 用于 Swagger 展示技能包预览响应。
type SkillPackagePreviewDoc struct {
	ErrorMsg string                     `json:"errorMsg"`
	Data     PackagePreviewDataResponse `json:"data"`
}

// SkillPackageFileResponseDoc 用于 Swagger 展示技能包文件内容响应。
type SkillPackageFileResponseDoc struct {
	ErrorMsg string                       `json:"errorMsg"`
	Data     SkillPackageFileDataResponse `json:"data"`
}

// ErrorDoc 表示错误响应。
type ErrorDoc struct {
	ErrorMsg string `json:"errorMsg"`
}

func toSkillSummaryResponses(items []domainskill.Skill) []SkillSummaryResponse {
	results := make([]SkillSummaryResponse, 0, len(items))
	for _, item := range items {
		results = append(results, toSkillSummaryResponse(item))
	}
	return results
}

func toSkillSummaryResponse(item domainskill.Skill) SkillSummaryResponse {
	return SkillSummaryResponse{
		ID:          item.ID,
		Scope:       item.Scope,
		Title:       item.Title,
		Trigger:     item.Trigger,
		Description: item.Description,
		PackageType: item.PackageType,
		Enabled:     item.Enabled,
		SortOrder:   item.SortOrder,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

func toSkillResponses(items []domainskill.Skill) []SkillResponse {
	results := make([]SkillResponse, 0, len(items))
	for _, item := range items {
		results = append(results, toSkillResponse(item))
	}
	return results
}

func toSkillResponse(item domainskill.Skill) SkillResponse {
	return SkillResponse{
		ID:              item.ID,
		Scope:           item.Scope,
		Title:           item.Title,
		Trigger:         item.Trigger,
		Description:     item.Description,
		Markdown:        item.Markdown,
		PackageType:     item.PackageType,
		PackageRootDir:  item.PackageRootDir,
		Files:           toPackageFileResponses(item.PackageFiles),
		Enabled:         item.Enabled,
		SortOrder:       item.SortOrder,
		CreatedByUserID: item.CreatedByUserID,
		UpdatedByUserID: item.UpdatedByUserID,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
}

func toPackageFileResponses(files []domainskill.PackageFile) []PackageFileResponse {
	if len(files) == 0 {
		return nil
	}
	results := make([]PackageFileResponse, 0, len(files))
	for _, file := range files {
		results = append(results, PackageFileResponse{
			Path: file.Path,
			Size: file.Size,
			Kind: file.Kind,
		})
	}
	return results
}

func toPackagePreviewResponse(preview *appskill.PackagePreview) PackagePreviewResponse {
	files := make([]PackageFileResponse, 0, len(preview.Files))
	for _, file := range preview.Files {
		files = append(files, PackageFileResponse{
			Path: file.Path,
			Size: file.Size,
			Kind: file.Kind,
		})
	}
	return PackagePreviewResponse{
		Title:       preview.Title,
		Trigger:     preview.Trigger,
		Description: preview.Description,
		Markdown:    preview.Markdown,
		RootDir:     preview.RootDir,
		Files:       files,
	}
}
