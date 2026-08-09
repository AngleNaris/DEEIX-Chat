package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domainartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/artifact"
)

type artifactRepositoryStub struct {
	items []domainartifact.Artifact
}

func (r *artifactRepositoryStub) CreateArtifact(context.Context, *domainartifact.Artifact) error {
	return nil
}

func (r *artifactRepositoryStub) UpdateArtifact(context.Context, *domainartifact.Artifact) error {
	return nil
}

func (r *artifactRepositoryStub) GetArtifactByPublicID(context.Context, uint, string) (*domainartifact.Artifact, error) {
	return nil, errors.New("not implemented")
}

func (r *artifactRepositoryStub) GetArtifactByID(context.Context, uint) (*domainartifact.Artifact, error) {
	return nil, errors.New("not implemented")
}

func (r *artifactRepositoryStub) ListArtifacts(context.Context, uint, int, int) ([]domainartifact.Artifact, int64, error) {
	return r.items, int64(len(r.items)), nil
}

func (r *artifactRepositoryStub) DeleteArtifact(context.Context, uint, string) error {
	return nil
}

func (r *artifactRepositoryStub) ReplaceActiveArtifactShare(
	context.Context,
	uint,
	uint,
	*domainartifact.ArtifactShare,
) error {
	return nil
}

func (r *artifactRepositoryStub) GetActiveArtifactShare(
	context.Context,
	uint,
	uint,
) (*domainartifact.ArtifactShare, error) {
	return nil, errors.New("no active share")
}

func (r *artifactRepositoryStub) GetArtifactShareByShareID(
	context.Context,
	string,
) (*domainartifact.ArtifactShare, error) {
	return nil, errors.New("not implemented")
}

func (r *artifactRepositoryStub) RevokeArtifactShare(context.Context, uint, string) error {
	return nil
}

func TestListArtifactsReturnsThumbnailWithoutCode(t *testing.T) {
	repo := &artifactRepositoryStub{
		items: []domainartifact.Artifact{
			{
				ID:               1,
				ArtifactPublicID: "artifact-1",
				Kind:             KindHTML,
				Title:            "Preview",
				Code:             "<html><body>full source</body></html>",
				Thumbnail:        "data:image/webp;base64,dGVzdA==",
				CreatedAt:        time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC),
				UpdatedAt:        time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC),
			},
		},
	}

	views, total, err := NewService(repo).ListArtifacts(context.Background(), 1, 1, 20)
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if total != 1 || len(views) != 1 {
		t.Fatalf("ListArtifacts() total = %d, len = %d", total, len(views))
	}
	if views[0].Thumbnail != repo.items[0].Thumbnail {
		t.Fatalf("thumbnail = %q, want %q", views[0].Thumbnail, repo.items[0].Thumbnail)
	}

	payload, err := json.Marshal(views[0])
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(payload), `"code"`) || strings.Contains(string(payload), "full source") {
		t.Fatalf("list payload leaked artifact code: %s", payload)
	}
}

func TestNormalizeThumbnail(t *testing.T) {
	t.Run("accepts supported raster data URL", func(t *testing.T) {
		got, err := normalizeThumbnail(" data:image/webp;base64,dGVzdA== ")
		if err != nil {
			t.Fatalf("normalizeThumbnail() error = %v", err)
		}
		if got != "data:image/webp;base64,dGVzdA==" {
			t.Fatalf("normalizeThumbnail() = %q", got)
		}
	})

	t.Run("rejects unsupported media type", func(t *testing.T) {
		if _, err := normalizeThumbnail("data:image/svg+xml;base64,PHN2Zz4="); !errors.Is(err, ErrInvalidThumbnail) {
			t.Fatalf("normalizeThumbnail() error = %v, want %v", err, ErrInvalidThumbnail)
		}
	})

	t.Run("rejects oversized payload", func(t *testing.T) {
		value := "data:image/webp;base64," + strings.Repeat("a", MaxThumbnailLen)
		if _, err := normalizeThumbnail(value); !errors.Is(err, ErrThumbnailTooLarge) {
			t.Fatalf("normalizeThumbnail() error = %v, want %v", err, ErrThumbnailTooLarge)
		}
	})
}
