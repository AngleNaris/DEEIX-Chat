package conversation

import (
	"context"
	"errors"
	"strings"
	"time"

	appupload "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/upload"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/google/uuid"
)

const (
	maxFileShareTTL = 30 * 24 * time.Hour
)

// FileShareResult is returned to the owner when managing a file share.
type FileShareResult struct {
	ShareID   string
	FileID    string
	Status    string
	ExpiresAt *time.Time
	CreatedAt time.Time
}

// PublicFileShareResult contains only metadata safe for the public share page.
type PublicFileShareResult struct {
	ShareID   string
	FileID    string
	FileName  string
	MimeType  string
	Category  string
	SizeBytes int64
	CreatedAt time.Time
	ExpiresAt *time.Time
}

func (s *Service) CreateFileShare(ctx context.Context, userID uint, fileID string, ttl time.Duration) (*FileShareResult, error) {
	fileID = strings.TrimSpace(fileID)
	if userID == 0 || fileID == "" {
		return nil, ErrInvalidFileReference
	}
	if ttl < 0 || ttl > maxFileShareTTL {
		return nil, ErrInvalidFileReference
	}
	var expiresAt *time.Time
	if ttl > 0 {
		value := time.Now().UTC().Add(ttl)
		expiresAt = &value
	}
	item := &model.FileShare{
		ShareID:   normalizePublicID(uuid.NewString()),
		FileID:    fileID,
		UserID:    userID,
		Status:    "active",
		ExpiresAt: expiresAt,
	}
	if err := s.repo.ReplaceActiveFileShare(ctx, item); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	return toFileShareResult(item), nil
}

func (s *Service) GetFileShare(ctx context.Context, userID uint, fileID string) (*FileShareResult, error) {
	fileID = strings.TrimSpace(fileID)
	if userID == 0 || fileID == "" {
		return nil, ErrInvalidFileReference
	}
	item, err := s.repo.GetLatestFileShare(ctx, userID, fileID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return &FileShareResult{Status: "none"}, nil
		}
		return nil, err
	}
	return toFileShareResult(item), nil
}

func (s *Service) RevokeFileShare(ctx context.Context, userID uint, fileID string) error {
	fileID = strings.TrimSpace(fileID)
	if userID == 0 || fileID == "" {
		return ErrInvalidFileReference
	}
	if err := s.repo.RevokeActiveFileShare(ctx, userID, fileID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrFileShareNotFound
		}
		return err
	}
	return nil
}

func (s *Service) resolvePublicFileShare(ctx context.Context, shareID string) (*model.FileShare, *model.FileObject, error) {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return nil, nil, ErrFileShareNotFound
	}
	share, file, err := s.repo.GetFileShareByShareID(ctx, shareID)
	if err != nil || share.Status != "active" || (share.ExpiresAt != nil && !share.ExpiresAt.After(time.Now())) {
		return nil, nil, ErrFileShareNotFound
	}
	return share, file, nil
}

func (s *Service) GetPublicFileShare(ctx context.Context, shareID string) (*PublicFileShareResult, error) {
	share, file, err := s.resolvePublicFileShare(ctx, shareID)
	if err != nil {
		return nil, err
	}
	return &PublicFileShareResult{
		ShareID:   share.ShareID,
		FileID:    file.FileID,
		FileName:  file.FileName,
		MimeType:  file.MimeType,
		Category:  file.FileCategory,
		SizeBytes: file.SizeBytes,
		CreatedAt: share.CreatedAt,
		ExpiresAt: share.ExpiresAt,
	}, nil
}

func (s *Service) OpenPublicFileShareContent(ctx context.Context, shareID string) (*appupload.FileContentResult, error) {
	share, file, err := s.resolvePublicFileShare(ctx, shareID)
	if err != nil {
		return nil, err
	}
	result, err := s.uploadSvc.OpenFileContent(ctx, share.UserID, file.FileID)
	if err != nil {
		return nil, ErrFileShareNotFound
	}
	return result, nil
}

func toFileShareResult(item *model.FileShare) *FileShareResult {
	status := item.Status
	if status == "active" && item.ExpiresAt != nil && !item.ExpiresAt.After(time.Now()) {
		status = "expired"
	}
	return &FileShareResult{
		ShareID:   item.ShareID,
		FileID:    item.FileID,
		Status:    status,
		ExpiresAt: item.ExpiresAt,
		CreatedAt: item.CreatedAt,
	}
}
