package conversation

import (
	"context"
	"errors"
	"testing"
	"time"

	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

type fileShareRepositoryStub struct {
	repository.ConversationRepository
	share *model.FileShare
	file  *model.FileObject
}

func (s *fileShareRepositoryStub) ReplaceActiveFileShare(_ context.Context, item *model.FileShare) error {
	if s.file == nil || s.file.UserID != item.UserID || s.file.FileID != item.FileID {
		return repository.ErrNotFound
	}
	item.ID = 1
	item.CreatedAt = time.Now().UTC()
	item.UpdatedAt = item.CreatedAt
	s.share = item
	return nil
}

func (s *fileShareRepositoryStub) GetLatestFileShare(_ context.Context, userID uint, fileID string) (*model.FileShare, error) {
	if s.share == nil || s.share.UserID != userID || s.share.FileID != fileID {
		return nil, repository.ErrNotFound
	}
	return s.share, nil
}

func (s *fileShareRepositoryStub) GetFileShareByShareID(_ context.Context, shareID string) (*model.FileShare, *model.FileObject, error) {
	if s.share == nil || s.file == nil || s.share.ShareID != shareID {
		return nil, nil, repository.ErrNotFound
	}
	return s.share, s.file, nil
}

func (s *fileShareRepositoryStub) RevokeActiveFileShare(_ context.Context, userID uint, fileID string) error {
	if s.share == nil || s.share.UserID != userID || s.share.FileID != fileID || s.share.Status != "active" {
		return repository.ErrNotFound
	}
	s.share.Status = "revoked"
	return nil
}

func TestFileShareDefaultNeverExpiresAndPublicGate(t *testing.T) {
	repo := &fileShareRepositoryStub{file: &model.FileObject{FileID: "file-1", UserID: 9, FileName: "image.png", MimeType: "image/png", FileCategory: "image", SizeBytes: 42}}
	service := &Service{repo: repo}
	created, err := service.CreateFileShare(context.Background(), 9, "file-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if created.ExpiresAt != nil {
		t.Fatalf("default expiry = %v, want nil", created.ExpiresAt)
	}
	public, err := service.GetPublicFileShare(context.Background(), created.ShareID)
	if err != nil || public.FileName != "image.png" {
		t.Fatalf("public = %#v, err = %v", public, err)
	}
	expiresAt := time.Now().Add(-time.Second)
	repo.share.ExpiresAt = &expiresAt
	if _, err := service.GetPublicFileShare(context.Background(), created.ShareID); !errors.Is(err, ErrFileShareNotFound) {
		t.Fatalf("expired share error = %v", err)
	}
	owner, err := service.GetFileShare(context.Background(), 9, "file-1")
	if err != nil || owner.Status != "expired" {
		t.Fatalf("owner share = %#v, err = %v", owner, err)
	}
}
