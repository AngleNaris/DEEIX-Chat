package skill

import (
	"bytes"
	"context"
	"errors"
	"io"
	pathpkg "path"
	"strconv"
	"strings"

	appstorage "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/objectstorage"
	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

const (
	maxSkillTitleLength       = 64
	maxSkillTriggerLength     = 64
	maxSkillDescriptionLength = 256
	maxSkillMarkdownLength    = 10000
)

// Service 封装技能业务逻辑。
type Service struct {
	repo          repository.SkillRepository
	auditWriter   auditWriter
	storeProvider appstorage.Provider
}

type auditWriter interface {
	Write(ctx context.Context, requestID string, actorUserID uint, action string, resource string, resourceID string, ip string, userAgent string, detail interface{})
}

// NewService 创建技能服务。
func NewService(repo repository.SkillRepository) *Service {
	return &Service{repo: repo}
}

// SetAuditWriter 注入审计写入器。
func (s *Service) SetAuditWriter(writer auditWriter) {
	s.auditWriter = writer
}

// SetObjectStoreProvider 注入对象存储，用于技能包文件读写。
func (s *Service) SetObjectStoreProvider(provider appstorage.Provider) {
	s.storeProvider = provider
}

// AuditInput 描述技能审计写入。
type AuditInput struct {
	UserID     uint
	RequestID  string
	Action     string
	ResourceID string
	ClientIP   string
	UserAgent  string
	Detail     interface{}
}

// RecordAudit 记录技能审计日志。
func (s *Service) RecordAudit(ctx context.Context, input AuditInput) {
	if s.auditWriter == nil {
		return
	}
	s.auditWriter.Write(
		ctx,
		strings.TrimSpace(input.RequestID),
		input.UserID,
		strings.TrimSpace(input.Action),
		"skills",
		strings.TrimSpace(input.ResourceID),
		strings.TrimSpace(input.ClientIP),
		strings.TrimSpace(input.UserAgent),
		input.Detail,
	)
}

// ListVisible 查询当前用户可使用的技能。
func (s *Service) ListVisible(ctx context.Context, userID uint, input ListInput) ([]domainskill.Skill, int64, error) {
	if userID == 0 {
		return nil, 0, repository.ErrInvalidInput
	}
	page, pageSize := normalizePage(input.Page, input.PageSize)
	return s.repo.ListSkills(ctx, repository.SkillListFilter{
		IDs:           input.IDs,
		Query:         strings.TrimSpace(input.Query),
		VisibleUserID: &userID,
	}, (page-1)*pageSize, pageSize)
}

// ListMine 查询当前用户自定义技能。
func (s *Service) ListMine(ctx context.Context, userID uint, input ListInput) ([]domainskill.Skill, int64, error) {
	if userID == 0 {
		return nil, 0, repository.ErrInvalidInput
	}
	page, pageSize := normalizePage(input.Page, input.PageSize)
	return s.repo.ListSkills(ctx, repository.SkillListFilter{
		Query:          strings.TrimSpace(input.Query),
		SearchMarkdown: true,
		Scope:          domainskill.ScopeUser,
		OwnerUserID:    &userID,
		Enabled:        input.Enabled,
	}, (page-1)*pageSize, pageSize)
}

// ListAdminBuiltin 查询管理员内置技能列表。
func (s *Service) ListAdminBuiltin(ctx context.Context, input ListInput) ([]domainskill.Skill, int64, error) {
	page, pageSize := normalizePage(input.Page, input.PageSize)
	return s.repo.ListSkills(ctx, repository.SkillListFilter{
		Query:          strings.TrimSpace(input.Query),
		SearchMarkdown: true,
		Scope:          domainskill.ScopeBuiltin,
		Enabled:        input.Enabled,
	}, (page-1)*pageSize, pageSize)
}

// ResolveAvailable 查询当前用户可使用的技能。
func (s *Service) ResolveAvailable(ctx context.Context, userID uint, id uint) (*domainskill.Skill, error) {
	if userID == 0 || id == 0 {
		return nil, repository.ErrInvalidInput
	}
	item, err := s.repo.GetSkill(ctx, id)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if !item.Enabled {
		return nil, ErrSkillNotFound
	}
	if item.Scope == domainskill.ScopeBuiltin {
		return item, nil
	}
	if item.Scope == domainskill.ScopeUser && item.OwnerUserID == userID {
		return item, nil
	}
	return nil, ErrSkillNotFound
}

// CreateUser 创建用户自定义技能。
func (s *Service) CreateUser(ctx context.Context, userID uint, input WriteInput) (*domainskill.Skill, error) {
	if userID == 0 {
		return nil, repository.ErrInvalidInput
	}
	item, err := normalizeWriteInput(input, domainskill.ScopeUser, userID, userID)
	if err != nil {
		return nil, err
	}
	return s.create(ctx, item)
}

// CreateBuiltin 创建管理员内置技能。
func (s *Service) CreateBuiltin(ctx context.Context, actorUserID uint, input WriteInput) (*domainskill.Skill, error) {
	if actorUserID == 0 {
		return nil, repository.ErrInvalidInput
	}
	item, err := normalizeWriteInput(input, domainskill.ScopeBuiltin, 0, actorUserID)
	if err != nil {
		return nil, err
	}
	return s.create(ctx, item)
}

// UpdateUser 更新当前用户自定义技能。
func (s *Service) UpdateUser(ctx context.Context, userID uint, id uint, input PatchInput) (*domainskill.Skill, error) {
	if userID == 0 || id == 0 {
		return nil, repository.ErrInvalidInput
	}
	item, err := s.repo.GetSkill(ctx, id)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if item.Scope != domainskill.ScopeUser || item.OwnerUserID != userID {
		return nil, ErrSkillNotFound
	}
	return s.update(ctx, id, userID, input)
}

// UpdateBuiltin 更新管理员内置技能。
func (s *Service) UpdateBuiltin(ctx context.Context, actorUserID uint, id uint, input PatchInput) (*domainskill.Skill, error) {
	if actorUserID == 0 || id == 0 {
		return nil, repository.ErrInvalidInput
	}
	item, err := s.repo.GetSkill(ctx, id)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if item.Scope != domainskill.ScopeBuiltin {
		return nil, ErrSkillNotFound
	}
	return s.update(ctx, id, actorUserID, input)
}

// DeleteUser 删除当前用户自定义技能。
func (s *Service) DeleteUser(ctx context.Context, userID uint, id uint) error {
	if userID == 0 || id == 0 {
		return repository.ErrInvalidInput
	}
	item, err := s.repo.GetSkill(ctx, id)
	if err != nil {
		return mapRepositoryError(err)
	}
	if item.Scope != domainskill.ScopeUser || item.OwnerUserID != userID {
		return ErrSkillNotFound
	}
	if err := s.deletePackageFiles(ctx, *item); err != nil {
		return err
	}
	return mapRepositoryError(s.repo.DeleteSkill(ctx, id))
}

// DeleteBuiltin 删除管理员内置技能。
func (s *Service) DeleteBuiltin(ctx context.Context, actorUserID uint, id uint) error {
	if actorUserID == 0 || id == 0 {
		return repository.ErrInvalidInput
	}
	item, err := s.repo.GetSkill(ctx, id)
	if err != nil {
		return mapRepositoryError(err)
	}
	if item.Scope != domainskill.ScopeBuiltin {
		return ErrSkillNotFound
	}
	if err := s.deletePackageFiles(ctx, *item); err != nil {
		return err
	}
	return mapRepositoryError(s.repo.DeleteSkill(ctx, id))
}

// PreviewPackage 解析上传的技能包并返回元数据预览，不落库。
func (s *Service) PreviewPackage(ctx context.Context, zipData []byte) (*PackagePreview, error) {
	if ctx == nil {
		return nil, ErrInvalidPackage
	}
	preview, _, err := ParsePackage(zipData)
	if err != nil {
		return nil, err
	}
	return preview, nil
}

// ImportUserPackage 创建用户包技能：解析 zip、落库元数据并存储包文件。
func (s *Service) ImportUserPackage(ctx context.Context, userID uint, zipData []byte) (*domainskill.Skill, error) {
	if userID == 0 {
		return nil, repository.ErrInvalidInput
	}
	return s.importPackageData(ctx, zipData, domainskill.ScopeUser, userID, userID)
}

// ImportBuiltinPackage 创建管理员内置包技能。
func (s *Service) ImportBuiltinPackage(ctx context.Context, actorUserID uint, zipData []byte) (*domainskill.Skill, error) {
	if actorUserID == 0 {
		return nil, repository.ErrInvalidInput
	}
	return s.importPackageData(ctx, zipData, domainskill.ScopeBuiltin, 0, actorUserID)
}

// ReplaceUserPackage 重新上传覆盖当前用户的包技能。
func (s *Service) ReplaceUserPackage(ctx context.Context, userID uint, id uint, zipData []byte) (*domainskill.Skill, error) {
	if userID == 0 || id == 0 {
		return nil, repository.ErrInvalidInput
	}
	item, err := s.repo.GetSkill(ctx, id)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if item.Scope != domainskill.ScopeUser || item.OwnerUserID != userID {
		return nil, ErrSkillNotFound
	}
	return s.replacePackage(ctx, *item, zipData)
}

// ReplaceBuiltinPackage 重新上传覆盖内置包技能。
func (s *Service) ReplaceBuiltinPackage(ctx context.Context, actorUserID uint, id uint, zipData []byte) (*domainskill.Skill, error) {
	if actorUserID == 0 || id == 0 {
		return nil, repository.ErrInvalidInput
	}
	item, err := s.repo.GetSkill(ctx, id)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if item.Scope != domainskill.ScopeBuiltin {
		return nil, ErrSkillNotFound
	}
	return s.replacePackage(ctx, *item, zipData)
}

// GetPackageFile 读取包内文本文件内容，仅允许命中当前技能清单的文本文件。
func (s *Service) GetPackageFile(ctx context.Context, userID uint, skillID uint, filePath string) ([]byte, error) {
	if userID == 0 || skillID == 0 {
		return nil, repository.ErrInvalidInput
	}
	item, err := s.ResolveAvailable(ctx, userID, skillID)
	if err != nil {
		return nil, err
	}
	if !item.IsPackage() {
		return nil, ErrPackageFileNotFound
	}
	rel := normalizePackagePath(filePath)
	if rel == "" {
		return nil, ErrPackageFileNotFound
	}
	var target *domainskill.PackageFile
	for index := range item.PackageFiles {
		if item.PackageFiles[index].Path == rel {
			target = &item.PackageFiles[index]
			break
		}
	}
	if target == nil || target.Kind != domainskill.FileKindText {
		return nil, ErrPackageFileNotFound
	}
	if s.storeProvider == nil {
		return nil, ErrInvalidSkill
	}
	store, err := s.storeProvider.Open(ctx)
	if err != nil {
		return nil, err
	}
	reader, _, err := store.Open(ctx, packageObjectKey(item.Scope, item.ID, rel))
	if err != nil {
		if errors.Is(err, objectstore.ErrNotFound) {
			return nil, ErrPackageFileNotFound
		}
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, maxPackageFileReadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxPackageFileReadBytes {
		return data[:maxPackageFileReadBytes], nil
	}
	return data, nil
}

// importPackageData 解析 zip 并创建包技能。
func (s *Service) importPackageData(ctx context.Context, zipData []byte, scope string, ownerUserID uint, actorUserID uint) (*domainskill.Skill, error) {
	if scope != domainskill.ScopeBuiltin && scope != domainskill.ScopeUser {
		return nil, ErrInvalidSkill
	}
	preview, contents, err := ParsePackage(zipData)
	if err != nil {
		return nil, err
	}
	item := &domainskill.Skill{
		Scope:           scope,
		OwnerUserID:     ownerUserID,
		Title:           preview.Title,
		Trigger:         preview.Trigger,
		Description:     preview.Description,
		Markdown:        preview.Markdown,
		PackageType:     domainskill.PackageTypePackage,
		PackageRootDir:  preview.RootDir,
		PackageFiles:    preview.Files,
		CreatedByUserID: actorUserID,
		UpdatedByUserID: actorUserID,
	}
	return s.importPackage(ctx, item, contents)
}

// importPackage 创建技能行并写入包文件；任一步失败时回滚已写入的文件。
func (s *Service) importPackage(ctx context.Context, item *domainskill.Skill, contents map[string][]byte) (*domainskill.Skill, error) {
	result, err := s.repo.CreateSkill(ctx, item)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if err := s.writePackageFiles(ctx, result, contents); err != nil {
		_ = s.repo.DeleteSkill(ctx, result.ID)
		return nil, err
	}
	filesJSON := encodePackageFilesJSON(item.PackageFiles)
	patched, err := s.repo.PatchSkill(ctx, result.ID, repository.SkillPatch{
		PackageFilesJSON:   &filesJSON,
		UpdatedByUserID:    item.UpdatedByUserID,
		UpdatedByUserIDSet: true,
	})
	if err != nil {
		_ = s.deletePackageFiles(ctx, *result)
		_ = s.repo.DeleteSkill(ctx, result.ID)
		return nil, mapRepositoryError(err)
	}
	return patched, nil
}

// replacePackage 用新 zip 覆盖已有包技能。
func (s *Service) replacePackage(ctx context.Context, item domainskill.Skill, zipData []byte) (*domainskill.Skill, error) {
	preview, contents, err := ParsePackage(zipData)
	if err != nil {
		return nil, err
	}
	patch := repository.SkillPatch{
		Title:              &preview.Title,
		Trigger:            &preview.Trigger,
		Description:        &preview.Description,
		Markdown:           &preview.Markdown,
		PackageRootDir:     &preview.RootDir,
		PackageFilesJSON:   ptrString(encodePackageFilesJSON(preview.Files)),
		UpdatedByUserID:    item.UpdatedByUserID,
		UpdatedByUserIDSet: true,
	}
	// 先写新文件再更新元数据；失败时清理新文件并保留旧状态。
	if err := s.writePackageFiles(ctx, &item, contents); err != nil {
		return nil, err
	}
	patched, err := s.repo.PatchSkill(ctx, item.ID, patch)
	if err != nil {
		_ = s.deletePackageFiles(ctx, item)
		return nil, mapRepositoryError(err)
	}
	// 删除旧清单中不再存在的文件（同名文件已被新内容覆盖）。
	if err := s.deleteStalePackageFiles(ctx, item, contents); err != nil {
		return nil, err
	}
	return patched, nil
}

// writePackageFiles 将包文件写入对象存储，key = skills/{scope}/{skillID}/{path}。
func (s *Service) writePackageFiles(ctx context.Context, item *domainskill.Skill, contents map[string][]byte) error {
	if len(contents) == 0 {
		return nil
	}
	if s.storeProvider == nil {
		return ErrInvalidSkill
	}
	store, err := s.storeProvider.Open(ctx)
	if err != nil {
		return err
	}
	for rel, data := range contents {
		key := packageObjectKey(item.Scope, item.ID, rel)
		if _, err := store.Put(ctx, key, bytes.NewReader(data), objectstore.PutOptions{
			SizeBytes:   int64(len(data)),
			ContentType: "application/octet-stream",
		}); err != nil {
			return err
		}
	}
	return nil
}

// deletePackageFiles 删除技能包在对象存储中的全部文件。
func (s *Service) deletePackageFiles(ctx context.Context, item domainskill.Skill) error {
	if !item.IsPackage() || len(item.PackageFiles) == 0 {
		return nil
	}
	return s.deleteStalePackageFiles(ctx, item, nil)
}

// deleteStalePackageFiles 删除对象存储中旧清单的文件；keep 非空时跳过仍存在的路径。
func (s *Service) deleteStalePackageFiles(ctx context.Context, item domainskill.Skill, keep map[string][]byte) error {
	if !item.IsPackage() || len(item.PackageFiles) == 0 {
		return nil
	}
	if s.storeProvider == nil {
		return ErrInvalidSkill
	}
	store, err := s.storeProvider.Open(ctx)
	if err != nil {
		return err
	}
	for _, file := range item.PackageFiles {
		if _, exists := keep[file.Path]; exists {
			continue
		}
		if err := store.Delete(ctx, packageObjectKey(item.Scope, item.ID, file.Path)); err != nil {
			return err
		}
	}
	return nil
}

// packageObjectKey 构造包文件的对象存储 key。
func packageObjectKey(scope string, skillID uint, relPath string) string {
	return "skills/" + strings.TrimSpace(scope) + "/" + strconv.FormatUint(uint64(skillID), 10) + "/" + normalizePackagePath(relPath)
}

// normalizePackagePath 规范化包内相对路径，拒绝越界路径。
func normalizePackagePath(value string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	cleanName := pathpkg.Clean(normalized)
	if cleanName == "." || cleanName == "" || strings.HasPrefix(cleanName, "../") || strings.HasPrefix(cleanName, "/") {
		return ""
	}
	return cleanName
}

func ptrString(value string) *string {
	return &value
}

// ListInput 定义技能列表入参。
type ListInput struct {
	IDs      []uint
	Query    string
	Enabled  *bool
	Page     int
	PageSize int
}

// WriteInput 定义技能创建入参。
type WriteInput struct {
	Title       string
	Trigger     string
	Description string
	Markdown    string
	Enabled     bool
	SortOrder   int
}

// PatchInput 定义技能更新入参。
type PatchInput struct {
	Title       *string
	Trigger     *string
	Description *string
	Markdown    *string
	Enabled     *bool
	SortOrder   *int
}

func (s *Service) create(ctx context.Context, item *domainskill.Skill) (*domainskill.Skill, error) {
	result, err := s.repo.CreateSkill(ctx, item)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	return result, nil
}

func (s *Service) update(ctx context.Context, id uint, actorUserID uint, input PatchInput) (*domainskill.Skill, error) {
	patch, err := normalizePatchInput(input, actorUserID)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.PatchSkill(ctx, id, patch)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	return item, nil
}

func normalizeWriteInput(input WriteInput, scope string, ownerUserID uint, actorUserID uint) (*domainskill.Skill, error) {
	title, trigger, description, markdown, err := normalizeFields(input)
	if err != nil {
		return nil, err
	}
	if scope != domainskill.ScopeBuiltin && scope != domainskill.ScopeUser {
		return nil, ErrInvalidSkill
	}
	return &domainskill.Skill{
		Scope:           scope,
		OwnerUserID:     ownerUserID,
		Title:           title,
		Trigger:         trigger,
		Description:     description,
		Markdown:        markdown,
		Enabled:         input.Enabled,
		SortOrder:       input.SortOrder,
		CreatedByUserID: actorUserID,
		UpdatedByUserID: actorUserID,
	}, nil
}

func normalizePatchInput(input PatchInput, actorUserID uint) (repository.SkillPatch, error) {
	patch := repository.SkillPatch{
		UpdatedByUserIDSet: true,
		UpdatedByUserID:    actorUserID,
	}
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" || runeCount(title) > maxSkillTitleLength {
			return repository.SkillPatch{}, ErrInvalidSkill
		}
		patch.Title = &title
	}
	if input.Trigger != nil {
		trigger := normalizeTrigger(*input.Trigger)
		if trigger == "" || runeCount(trigger) > maxSkillTriggerLength {
			return repository.SkillPatch{}, ErrInvalidSkill
		}
		patch.Trigger = &trigger
	}
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		if runeCount(description) > maxSkillDescriptionLength {
			return repository.SkillPatch{}, ErrInvalidSkill
		}
		patch.Description = &description
	}
	if input.Markdown != nil {
		markdown := strings.TrimSpace(*input.Markdown)
		if markdown == "" || runeCount(markdown) > maxSkillMarkdownLength {
			return repository.SkillPatch{}, ErrInvalidSkill
		}
		patch.Markdown = &markdown
	}
	if input.Enabled != nil {
		patch.Enabled = input.Enabled
	}
	if input.SortOrder != nil {
		patch.SortOrder = input.SortOrder
	}
	return patch, nil
}

func normalizeFields(input WriteInput) (string, string, string, string, error) {
	title := strings.TrimSpace(input.Title)
	trigger := normalizeTrigger(input.Trigger)
	description := strings.TrimSpace(input.Description)
	markdown := strings.TrimSpace(input.Markdown)
	if title == "" || runeCount(title) > maxSkillTitleLength {
		return "", "", "", "", ErrInvalidSkill
	}
	if trigger == "" || runeCount(trigger) > maxSkillTriggerLength {
		return "", "", "", "", ErrInvalidSkill
	}
	if runeCount(description) > maxSkillDescriptionLength {
		return "", "", "", "", ErrInvalidSkill
	}
	if markdown == "" || runeCount(markdown) > maxSkillMarkdownLength {
		return "", "", "", "", ErrInvalidSkill
	}
	return title, trigger, description, markdown, nil
}

func normalizeTrigger(value string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(value), "/"))
}

func runeCount(value string) int {
	return len([]rune(value))
}

func normalizePage(page int, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	const maxPageSize = 100
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func mapRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, repository.ErrNotFound) {
		return ErrSkillNotFound
	}
	if errors.Is(err, repository.ErrDuplicate) {
		return ErrSkillConflict
	}
	if errors.Is(err, repository.ErrInvalidInput) {
		return ErrInvalidSkill
	}
	return err
}
