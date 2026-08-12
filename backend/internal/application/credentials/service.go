// Package credentials 提供用户凭据管理：命名凭据（SSH/API key 等）加密存储，
// 模型上下文只见描述与 {{credential: name}} 占位符；执行层经 ResolveValue 解密展开，
// 密钥不进入会话记录、展示与分享快照。
package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	domaincredentials "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/credentials"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/conv"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/secretbox"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/google/uuid"
)

// 错误定义。
var (
	ErrCredentialNotFound = errors.New("credential not found")
	ErrNameConflict       = errors.New("credential name already exists")
)

// 长度限制。
const (
	MaxNameLen        = 64
	MaxDescriptionLen = 255
	MaxMetaKeys       = 16
)

// UpsertInput 创建/更新凭据的输入。
type UpsertInput struct {
	Name        string
	Type        string
	Description string
	Value       string            // 创建必填；更新时为空表示不修改密钥
	Meta        map[string]string // 非敏感元数据（SSH host/port/username 等）
}

// View 凭据对外视图（永不含密钥）。
type View struct {
	PublicID    string            `json:"public_id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Meta        map[string]string `json:"meta,omitempty"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

// Service 封装凭据业务能力。
type Service struct {
	repo              repository.CredentialRepository
	dataEncryptionKey string
}

// NewService 创建服务。
func NewService(repo repository.CredentialRepository, dataEncryptionKey string) *Service {
	return &Service{repo: repo, dataEncryptionKey: dataEncryptionKey}
}

func (s *Service) encryptValue(value string) (string, error) {
	return secretbox.EncryptString(s.dataEncryptionKey, value)
}

func (s *Service) decryptValue(secretEnc string) (string, error) {
	return secretbox.DecryptString(s.dataEncryptionKey, secretEnc)
}

func marshalMeta(meta map[string]string) string {
	if len(meta) == 0 {
		return ""
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return ""
	}
	return string(data)
}

func unmarshalMeta(metaJSON string) map[string]string {
	metaJSON = strings.TrimSpace(metaJSON)
	if metaJSON == "" {
		return nil
	}
	var meta map[string]string
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		return nil
	}
	return meta
}

func toView(item *domaincredentials.Credential) View {
	return View{
		PublicID:    item.PublicID,
		Name:        item.Name,
		Type:        item.Type,
		Description: item.Description,
		Meta:        unmarshalMeta(item.MetaJSON),
		CreatedAt:   item.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:   item.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

// CreateCredential 创建凭据。
func (s *Service) CreateCredential(ctx context.Context, userID uint, input UpsertInput) (*View, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, errors.New("credential name is required")
	}
	if len([]rune(name)) > MaxNameLen {
		return nil, errors.New("credential name too long")
	}
	credentialType := strings.TrimSpace(input.Type)
	if credentialType == "" || !domaincredentials.ValidType(credentialType) {
		return nil, errors.New("credential type must be one of: ssh, api_key, generic")
	}
	value := input.Value
	if strings.TrimSpace(value) == "" {
		return nil, errors.New("credential value is required")
	}
	if len(value) > 64*1024 {
		return nil, errors.New("credential value too large")
	}
	description := strings.TrimSpace(input.Description)
	if len([]rune(description)) > MaxDescriptionLen {
		return nil, errors.New("credential description too long")
	}
	if len(input.Meta) > MaxMetaKeys {
		return nil, errors.New("credential meta has too many keys")
	}
	secretEnc, err := s.encryptValue(value)
	if err != nil {
		return nil, err
	}
	item := &domaincredentials.Credential{
		UserID:      userID,
		PublicID:    conv.NormalizePublicID(uuid.NewString()),
		Name:        name,
		Type:        credentialType,
		Description: description,
		SecretEnc:   secretEnc,
		MetaJSON:    marshalMeta(input.Meta),
	}
	if err := s.repo.CreateCredential(ctx, item); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, ErrNameConflict
		}
		return nil, err
	}
	view := toView(item)
	return &view, nil
}

// UpdateCredential 更新凭据；value 为空表示不修改密钥。
func (s *Service) UpdateCredential(ctx context.Context, userID uint, publicID string, input UpsertInput) (*View, error) {
	patch := &domaincredentials.CredentialPatch{}
	if name := strings.TrimSpace(input.Name); name != "" {
		if len([]rune(name)) > MaxNameLen {
			return nil, errors.New("credential name too long")
		}
		patch.Name = &name
	}
	if credentialType := strings.TrimSpace(input.Type); credentialType != "" {
		if !domaincredentials.ValidType(credentialType) {
			return nil, errors.New("credential type must be one of: ssh, api_key, generic")
		}
		patch.Type = &credentialType
	}
	description := strings.TrimSpace(input.Description)
	patch.Description = &description
	if len([]rune(description)) > MaxDescriptionLen {
		return nil, errors.New("credential description too long")
	}
	if len(input.Meta) > MaxMetaKeys {
		return nil, errors.New("credential meta has too many keys")
	}
	if value := input.Value; strings.TrimSpace(value) != "" {
		if len(value) > 64*1024 {
			return nil, errors.New("credential value too large")
		}
		secretEnc, err := s.encryptValue(value)
		if err != nil {
			return nil, err
		}
		patch.SecretEnc = &secretEnc
	}
	if input.Meta != nil {
		metaJSON := marshalMeta(input.Meta)
		patch.MetaJSON = &metaJSON
	}
	if err := s.repo.UpdateCredential(ctx, userID, publicID, patch); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, ErrNameConflict
		}
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}
	item, err := s.repo.GetCredentialByPublicID(ctx, userID, publicID)
	if err != nil {
		return nil, ErrCredentialNotFound
	}
	view := toView(item)
	return &view, nil
}

// DeleteCredential 删除凭据。
func (s *Service) DeleteCredential(ctx context.Context, userID uint, publicID string) error {
	if err := s.repo.DeleteCredential(ctx, userID, publicID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrCredentialNotFound
		}
		return err
	}
	return nil
}

// UpdateCredentialByName 按名称更新凭据（平台工具按 name 操作，模型只认识 name）。
func (s *Service) UpdateCredentialByName(ctx context.Context, userID uint, name string, input UpsertInput) (*View, error) {
	item, err := s.repo.GetCredentialByName(ctx, userID, strings.TrimSpace(name))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}
	return s.UpdateCredential(ctx, userID, item.PublicID, input)
}

// DeleteCredentialByName 按名称删除凭据（平台工具按 name 操作）。
func (s *Service) DeleteCredentialByName(ctx context.Context, userID uint, name string) error {
	item, err := s.repo.GetCredentialByName(ctx, userID, strings.TrimSpace(name))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrCredentialNotFound
		}
		return err
	}
	return s.DeleteCredential(ctx, userID, item.PublicID)
}

// ListCredentials 列出用户全部凭据（视图，无密钥）。
func (s *Service) ListCredentials(ctx context.Context, userID uint) ([]View, error) {
	items, err := s.repo.ListCredentials(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]View, 0, len(items))
	for i := range items {
		views = append(views, toView(&items[i]))
	}
	return views, nil
}

// GetCredentialByPublicID 查询单个凭据（视图，无密钥）。
func (s *Service) GetCredentialByPublicID(ctx context.Context, userID uint, publicID string) (*View, error) {
	item, err := s.repo.GetCredentialByPublicID(ctx, userID, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}
	view := toView(item)
	return &view, nil
}

// ResolveValue 按名称解密凭据值（{{credential: name}} 执行层展开用）。
// 未找到返回空串与 nil 错误（占位符保留原样，不阻塞执行）。
func (s *Service) ResolveValue(ctx context.Context, userID uint, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	item, err := s.repo.GetCredentialByName(ctx, userID, name)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	value, err := s.decryptValue(item.SecretEnc)
	if err != nil {
		return "", err
	}
	return value, nil
}

// ListNames 列出用户全部凭据名（占位符展开预检/提示用）。
func (s *Service) ListNames(ctx context.Context, userID uint) ([]string, error) {
	items, err := s.repo.ListCredentials(ctx, userID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names, nil
}
