package repository

import (
	"context"

	domaincredentials "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/credentials"
)

// CredentialRepository 用户凭据数据访问（全部按 user_id 作用域）。
type CredentialRepository interface {
	// CreateCredential 创建凭据（(user_id, name) 唯一）。
	CreateCredential(ctx context.Context, item *domaincredentials.Credential) error
	// UpdateCredential 更新凭据的局部字段（patch 中 nil 字段不修改；不存在返回 ErrNotFound）。
	UpdateCredential(ctx context.Context, userID uint, publicID string, patch *domaincredentials.CredentialPatch) error
	// DeleteCredential 删除凭据（软删除）。
	DeleteCredential(ctx context.Context, userID uint, publicID string) error
	// ListCredentials 列出用户全部凭据（按创建时间升序）。
	ListCredentials(ctx context.Context, userID uint) ([]domaincredentials.Credential, error)
	// GetCredentialByPublicID 查询单个凭据。
	GetCredentialByPublicID(ctx context.Context, userID uint, publicID string) (*domaincredentials.Credential, error)
	// GetCredentialByName 按名称查询凭据（{{credential: name}} 展开用）。
	GetCredentialByName(ctx context.Context, userID uint, name string) (*domaincredentials.Credential, error)
}
