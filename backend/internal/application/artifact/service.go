// Package artifact 提供制品（AI 生成的 HTML/JS/CSS/文本）的保存、管理与公开分享。
package artifact

import (
	"context"
	"errors"
	"strings"

	domainartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/artifact"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/conv"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/google/uuid"
)

// 错误定义。
var (
	ErrArtifactNotFound  = errors.New("artifact not found")
	ErrShareNotFound     = errors.New("artifact share not found")
	ErrShareRevoked      = errors.New("artifact share revoked")
	ErrInvalidThumbnail  = errors.New("artifact thumbnail must be a supported raster data URL")
	ErrThumbnailTooLarge = errors.New("artifact thumbnail is too large")
)

// 制品类型枚举与长度限制。
const (
	KindHTML = "html"
	KindJS   = "js"
	KindCSS  = "css"
	KindText = "text"

	MaxTitleLen     = 255
	MaxCodeLen      = 256 * 1024
	MaxThumbnailLen = 512 * 1024
)

// ValidKind 判断制品类型是否合法。
func ValidKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindHTML, KindJS, KindCSS, KindText:
		return true
	default:
		return false
	}
}

func normalizeThumbnail(thumbnail string) (string, error) {
	thumbnail = strings.TrimSpace(thumbnail)
	if thumbnail == "" {
		return "", nil
	}
	if len(thumbnail) > MaxThumbnailLen {
		return "", ErrThumbnailTooLarge
	}
	for _, prefix := range []string{
		"data:image/webp;base64,",
		"data:image/png;base64,",
		"data:image/jpeg;base64,",
	} {
		if strings.HasPrefix(thumbnail, prefix) {
			return thumbnail, nil
		}
	}
	return "", ErrInvalidThumbnail
}

// CreateInput 创建/更新制品的输入。
type CreateInput struct {
	Title          string
	Kind           string
	Code           string
	Thumbnail      string
	ConversationID uint
	MessageID      uint
}

// ShareView 分享视图（列表/管理用）。
type ShareView struct {
	ShareID       string `json:"share_id"`
	Status        string `json:"status"`
	TitleSnapshot string `json:"title_snapshot"`
	CreatedAt     string `json:"created_at"`
}

// ArtifactView 制品视图（列表用，仅返回静态缩略图，不下发完整代码）。
type ArtifactView struct {
	ArtifactPublicID string     `json:"artifact_id"`
	Kind             string     `json:"kind"`
	Title            string     `json:"title"`
	Thumbnail        string     `json:"thumbnail,omitempty"`
	ConversationID   uint       `json:"conversation_id"`
	MessageID        uint       `json:"message_id"`
	Share            *ShareView `json:"share,omitempty"`
	CreatedAt        string     `json:"created_at"`
	UpdatedAt        string     `json:"updated_at"`
}

// ArtifactDetailView 制品详情视图（创建/读取用，含完整代码）。
type ArtifactDetailView struct {
	ArtifactPublicID string `json:"artifact_id"`
	Kind             string `json:"kind"`
	Title            string `json:"title"`
	Code             string `json:"code"`
	Thumbnail        string `json:"thumbnail,omitempty"`
	ConversationID   uint   `json:"conversation_id"`
	MessageID        uint   `json:"message_id"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// ToDetailView 把领域制品映射为详情视图。
func ToDetailView(item *domainartifact.Artifact) ArtifactDetailView {
	return ArtifactDetailView{
		ArtifactPublicID: item.ArtifactPublicID,
		Kind:             item.Kind,
		Title:            item.Title,
		Code:             item.Code,
		Thumbnail:        item.Thumbnail,
		ConversationID:   item.ConversationID,
		MessageID:        item.MessageID,
		CreatedAt:        item.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:        item.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

// PublicShareView 公开分享视图。
type PublicShareView struct {
	ShareID   string `json:"share_id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	Code      string `json:"code"`
	CreatedAt string `json:"created_at"`
}

// Service 封装制品业务能力。
type Service struct {
	repo repository.ArtifactRepository
}

// NewService 创建服务。
func NewService(repo repository.ArtifactRepository) *Service {
	return &Service{repo: repo}
}

// CreateArtifact 保存制品（public id 由调用方生成，便于幂等更新）。
func (s *Service) CreateArtifact(ctx context.Context, userID uint, publicID string, input CreateInput) (*domainartifact.Artifact, error) {
	if !ValidKind(input.Kind) {
		input.Kind = KindText
	}
	thumbnail, err := normalizeThumbnail(input.Thumbnail)
	if err != nil {
		return nil, err
	}
	item := &domainartifact.Artifact{
		ArtifactPublicID: strings.TrimSpace(publicID),
		UserID:           userID,
		ConversationID:   input.ConversationID,
		MessageID:        input.MessageID,
		Kind:             strings.ToLower(strings.TrimSpace(input.Kind)),
		Title:            strings.TrimSpace(input.Title),
		Code:             input.Code,
		Thumbnail:        thumbnail,
	}
	if item.ArtifactPublicID == "" {
		item.ArtifactPublicID = conv.NormalizePublicID(uuid.NewString())
	}
	if err := s.repo.CreateArtifact(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

// UpdateArtifact 更新制品标题/类型/代码（保持 public id）。
func (s *Service) UpdateArtifact(ctx context.Context, userID uint, publicID string, input CreateInput) (*domainartifact.Artifact, error) {
	existing, err := s.repo.GetArtifactByPublicID(ctx, userID, publicID)
	if err != nil {
		return nil, err
	}
	if !ValidKind(input.Kind) {
		input.Kind = KindText
	}
	thumbnail, err := normalizeThumbnail(input.Thumbnail)
	if err != nil {
		return nil, err
	}
	existing.Title = strings.TrimSpace(input.Title)
	existing.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	existing.Code = input.Code
	existing.Thumbnail = thumbnail
	if err := s.repo.UpdateArtifact(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// GetArtifact 返回制品详情（含完整代码）。
func (s *Service) GetArtifact(ctx context.Context, userID uint, publicID string) (*domainartifact.Artifact, error) {
	return s.repo.GetArtifactByPublicID(ctx, userID, publicID)
}

// ListArtifacts 分页列出制品（含分享状态）。
func (s *Service) ListArtifacts(ctx context.Context, userID uint, page int, pageSize int) ([]ArtifactView, int64, error) {
	items, total, err := s.repo.ListArtifacts(ctx, userID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	views := make([]ArtifactView, 0, len(items))
	for _, item := range items {
		view := ArtifactView{
			ArtifactPublicID: item.ArtifactPublicID,
			Kind:             item.Kind,
			Title:            item.Title,
			Thumbnail:        item.Thumbnail,
			ConversationID:   item.ConversationID,
			MessageID:        item.MessageID,
			CreatedAt:        item.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:        item.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
		if share, err := s.repo.GetActiveArtifactShare(ctx, userID, item.ID); err == nil && share != nil {
			view.Share = &ShareView{
				ShareID:       share.ShareID,
				Status:        share.Status,
				TitleSnapshot: share.TitleSnapshot,
				CreatedAt:     share.CreatedAt.Format("2006-01-02 15:04:05"),
			}
		}
		views = append(views, view)
	}
	return views, total, nil
}

// DeleteArtifact 删除制品及其分享。
func (s *Service) DeleteArtifact(ctx context.Context, userID uint, publicID string) error {
	return s.repo.DeleteArtifact(ctx, userID, publicID)
}

// CreateShare 创建/重新生成制品分享（旧 active 分享自动撤销）。
func (s *Service) CreateShare(ctx context.Context, userID uint, artifactPublicID string) (*ShareView, error) {
	item, err := s.repo.GetArtifactByPublicID(ctx, userID, artifactPublicID)
	if err != nil {
		return nil, err
	}
	share := &domainartifact.ArtifactShare{
		ShareID:       conv.NormalizePublicID(uuid.NewString()),
		TitleSnapshot: item.Title,
	}
	if err := s.repo.ReplaceActiveArtifactShare(ctx, userID, item.ID, share); err != nil {
		return nil, err
	}
	return &ShareView{
		ShareID:       share.ShareID,
		Status:        "active",
		TitleSnapshot: share.TitleSnapshot,
		CreatedAt:     share.CreatedAt.Format("2006-01-02 15:04:05"),
	}, nil
}

// GetShare 返回制品的当前分享。
func (s *Service) GetShare(ctx context.Context, userID uint, artifactPublicID string) (*ShareView, error) {
	item, err := s.repo.GetArtifactByPublicID(ctx, userID, artifactPublicID)
	if err != nil {
		return nil, err
	}
	share, err := s.repo.GetActiveArtifactShare(ctx, userID, item.ID)
	if err != nil {
		return nil, err
	}
	return &ShareView{
		ShareID:       share.ShareID,
		Status:        share.Status,
		TitleSnapshot: share.TitleSnapshot,
		CreatedAt:     share.CreatedAt.Format("2006-01-02 15:04:05"),
	}, nil
}

// RevokeShare 撤销制品分享。
func (s *Service) RevokeShare(ctx context.Context, userID uint, artifactPublicID string) error {
	item, err := s.repo.GetArtifactByPublicID(ctx, userID, artifactPublicID)
	if err != nil {
		return err
	}
	share, err := s.repo.GetActiveArtifactShare(ctx, userID, item.ID)
	if err != nil {
		return err
	}
	return s.repo.RevokeArtifactShare(ctx, userID, share.ShareID)
}

// GetPublicShare 公开读取分享（token 即密钥；分享已撤销返回 ErrShareNotFound）。
func (s *Service) GetPublicShare(ctx context.Context, shareID string) (*PublicShareView, error) {
	share, err := s.repo.GetArtifactShareByShareID(ctx, shareID)
	if err != nil {
		return nil, err
	}
	if share.Status != "active" {
		return nil, ErrShareNotFound
	}
	item, err := s.repo.GetArtifactByID(ctx, share.ArtifactID)
	if err != nil {
		return nil, err
	}
	return &PublicShareView{
		ShareID:   share.ShareID,
		Title:     share.TitleSnapshot,
		Kind:      item.Kind,
		Code:      item.Code,
		CreatedAt: share.CreatedAt.Format("2006-01-02 15:04:05"),
	}, nil
}
