package repository

import (
	"context"

	domainartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/artifact"
)

// ArtifactRepository 定义制品域依赖的持久化能力。
type ArtifactRepository interface {
	CreateArtifact(ctx context.Context, item *domainartifact.Artifact) error
	UpdateArtifact(ctx context.Context, item *domainartifact.Artifact) error
	GetArtifactByPublicID(ctx context.Context, userID uint, publicID string) (*domainartifact.Artifact, error)
	GetArtifactByID(ctx context.Context, artifactID uint) (*domainartifact.Artifact, error)
	ListArtifacts(ctx context.Context, userID uint, page int, pageSize int) ([]domainartifact.Artifact, int64, error)
	DeleteArtifact(ctx context.Context, userID uint, publicID string) error
	// ReplaceActiveArtifactShare 在同一事务内把该制品旧 active 分享置 revoked 并插入新分享。
	ReplaceActiveArtifactShare(ctx context.Context, userID uint, artifactID uint, share *domainartifact.ArtifactShare) error
	GetActiveArtifactShare(ctx context.Context, userID uint, artifactID uint) (*domainartifact.ArtifactShare, error)
	GetArtifactShareByShareID(ctx context.Context, shareID string) (*domainartifact.ArtifactShare, error)
	RevokeArtifactShare(ctx context.Context, userID uint, shareID string) error
}
