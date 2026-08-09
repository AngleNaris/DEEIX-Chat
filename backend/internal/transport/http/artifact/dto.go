package artifact

import (
	"strings"
)

// CreateArtifactRequest 保存制品的请求体。
type CreateArtifactRequest struct {
	ArtifactID string `json:"artifactId"` // 可选，提供则更新已有制品
	Title      string `json:"title" binding:"required,max=255"`
	Kind       string `json:"kind" binding:"omitempty,oneof=html js css text"`
	Code       string `json:"code" binding:"required,max=262144"`
	Thumbnail  string `json:"thumbnail" binding:"omitempty,max=524288"`
}

// ArtifactIDParam 路径参数。
type ArtifactIDParam struct {
	ID string `uri:"id" binding:"required"`
}

// ShareIDParam 公开分享路径参数。
type ShareIDParam struct {
	ShareID string `uri:"share_id" binding:"required"`
}

// PaginationQuery 列表分页查询参数。
type PaginationQuery struct {
	Page     int `form:"page" binding:"omitempty,min=1"`
	PageSize int `form:"page_size" binding:"omitempty,min=1,max=50"`
}

func (q *PaginationQuery) page() int {
	if q.Page < 1 {
		return 1
	}
	return q.Page
}

func (q *PaginationQuery) pageSize() int {
	if q.PageSize < 1 {
		return 20
	}
	return q.PageSize
}

func sanitizeKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "html", "js", "css", "text":
		return kind
	default:
		return "text"
	}
}
