package agentgroup

import (
	"context"
	"time"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/dberror"
	models "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
)

// translateError 将 gorm 底层错误统一映射为仓储语义错误。
func translateError(err error) error {
	if err == nil {
		return nil
	}
	if dberror.IsRecordNotFound(err) {
		return repository.ErrNotFound
	}
	if dberror.IsUniqueConstraint(err) {
		return repository.ErrDuplicate
	}
	return err
}

// Repo 聚合 agentgroup 域数据访问。
type Repo struct {
	db *gorm.DB
}

// NewRepo 创建仓储。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// groupRow 群组与项目摘要联查行。
type groupRow struct {
	models.AgentGroup
	ProjectPublicID string
	ProjectName     string
}

// memberRow 成员与角色摘要联查行。
type memberRow struct {
	models.AgentGroupMember
	RolePublicID string
	RoleName     string
	RoleIcon     string
	RoleColor    string
	RoleModel    string
	RoleProvider string
}

// runRow 运行与群组摘要联查行。
type runRow struct {
	models.AgentGroupRun
	GroupPublicID string
}

func toGroupModel(item *domainagentgroup.Group) models.AgentGroup {
	return models.AgentGroup{
		UserID:             item.UserID,
		PublicID:           item.PublicID,
		ProjectID:          item.ProjectID,
		Name:               item.Name,
		Description:        item.Description,
		CoordinationPrompt: item.CoordinationPrompt,
		SupervisorMemberID: item.SupervisorMemberID,
		SortOrder:          item.SortOrder,
		Status:             item.Status,
		Revision:           item.Revision,
	}
}

func toGroupDomain(entity models.AgentGroup, projectPublicID string, projectName string, members []memberRow) domainagentgroup.Group {
	group := domainagentgroup.Group{
		ID:                 entity.ID,
		UserID:             entity.UserID,
		ProjectID:          entity.ProjectID,
		ProjectPublicID:    projectPublicID,
		ProjectName:        projectName,
		PublicID:           entity.PublicID,
		Name:               entity.Name,
		Description:        entity.Description,
		CoordinationPrompt: entity.CoordinationPrompt,
		SupervisorMemberID: entity.SupervisorMemberID,
		SortOrder:          entity.SortOrder,
		Status:             entity.Status,
		Revision:           entity.Revision,
		Members:            make([]domainagentgroup.Member, 0, len(members)),
		CreatedAt:          entity.CreatedAt,
		UpdatedAt:          entity.UpdatedAt,
	}
	for i := range members {
		group.Members = append(group.Members, toMemberDomain(members[i]))
	}
	return group
}

func toMemberModel(item domainagentgroup.Member) models.AgentGroupMember {
	return models.AgentGroupMember{
		PublicID:        item.PublicID,
		GroupID:         item.GroupID,
		RoleID:          item.RoleID,
		MemberType:      item.MemberType,
		Enabled:         item.Enabled,
		ModelOverride:   item.ModelOverride,
		ReasoningEffort: item.ReasoningEffort,
		DutyInstruction: item.DutyInstruction,
		SortOrder:       item.SortOrder,
	}
}

func toMemberDomain(row memberRow) domainagentgroup.Member {
	entity := row.AgentGroupMember
	return domainagentgroup.Member{
		ID:              entity.ID,
		PublicID:        entity.PublicID,
		GroupID:         entity.GroupID,
		RoleID:          entity.RoleID,
		RolePublicID:    row.RolePublicID,
		RoleName:        row.RoleName,
		RoleIcon:        row.RoleIcon,
		RoleColor:       row.RoleColor,
		RoleModel:       row.RoleModel,
		RoleProvider:    row.RoleProvider,
		MemberType:      entity.MemberType,
		Enabled:         entity.Enabled,
		ModelOverride:   entity.ModelOverride,
		ReasoningEffort: entity.ReasoningEffort,
		DutyInstruction: entity.DutyInstruction,
		SortOrder:       entity.SortOrder,
		CreatedAt:       entity.CreatedAt,
		UpdatedAt:       entity.UpdatedAt,
	}
}

func toRunModel(item *domainagentgroup.Run) models.AgentGroupRun {
	endedAt := item.EndedAt
	if endedAt != nil {
		copied := *endedAt
		endedAt = &copied
	}
	return models.AgentGroupRun{
		PublicID:            item.PublicID,
		ClientRunID:         item.ClientRunID,
		UserID:              item.UserID,
		ConversationID:      item.ConversationID,
		GroupID:             item.GroupID,
		UserMessageID:       item.UserMessageID,
		AssistantMessageID:  item.AssistantMessageID,
		GroupRevision:       item.GroupRevision,
		ConfigSnapshotJSON:  item.ConfigSnapshotJSON,
		Status:              item.Status,
		CurrentStepID:       item.CurrentStepID,
		LastCompletedStepID: item.LastCompletedStepID,
		RetryableStepID:     item.RetryableStepID,
		StateVersion:        item.StateVersion,
		ErrorCode:           item.ErrorCode,
		ErrorMessage:        item.ErrorMessage,
		StartedAt:           item.StartedAt,
		EndedAt:             endedAt,
	}
}

func toRunDomain(entity models.AgentGroupRun, groupPublicID string) domainagentgroup.Run {
	return domainagentgroup.Run{
		ID:                  entity.ID,
		PublicID:            entity.PublicID,
		ClientRunID:         entity.ClientRunID,
		UserID:              entity.UserID,
		ConversationID:      entity.ConversationID,
		GroupID:             entity.GroupID,
		GroupPublicID:       groupPublicID,
		UserMessageID:       entity.UserMessageID,
		AssistantMessageID:  entity.AssistantMessageID,
		GroupRevision:       entity.GroupRevision,
		ConfigSnapshotJSON:  entity.ConfigSnapshotJSON,
		Status:              entity.Status,
		CurrentStepID:       entity.CurrentStepID,
		LastCompletedStepID: entity.LastCompletedStepID,
		RetryableStepID:     entity.RetryableStepID,
		StateVersion:        entity.StateVersion,
		ErrorCode:           entity.ErrorCode,
		ErrorMessage:        entity.ErrorMessage,
		StartedAt:           entity.StartedAt,
		EndedAt:             entity.EndedAt,
		CreatedAt:           entity.CreatedAt,
		UpdatedAt:           entity.UpdatedAt,
	}
}

func toStepModel(item *domainagentgroup.Step) models.AgentGroupStep {
	return models.AgentGroupStep{
		PublicID:            item.PublicID,
		GroupRunID:          item.GroupRunID,
		Sequence:            item.Sequence,
		StepType:            item.StepType,
		ActorMemberPublicID: item.ActorMemberPublicID,
		ActorNameSnapshot:   item.ActorNameSnapshot,
		ActorTypeSnapshot:   item.ActorTypeSnapshot,
		Instruction:         item.Instruction,
		Status:              item.Status,
		SuccessfulAttemptID: item.SuccessfulAttemptID,
	}
}

func toStepDomain(entity models.AgentGroupStep) domainagentgroup.Step {
	return domainagentgroup.Step{
		ID:                  entity.ID,
		PublicID:            entity.PublicID,
		GroupRunID:          entity.GroupRunID,
		Sequence:            entity.Sequence,
		StepType:            entity.StepType,
		ActorMemberPublicID: entity.ActorMemberPublicID,
		ActorNameSnapshot:   entity.ActorNameSnapshot,
		ActorTypeSnapshot:   entity.ActorTypeSnapshot,
		Instruction:         entity.Instruction,
		Status:              entity.Status,
		SuccessfulAttemptID: entity.SuccessfulAttemptID,
		CreatedAt:           entity.CreatedAt,
		UpdatedAt:           entity.UpdatedAt,
	}
}

func toAttemptModel(item *domainagentgroup.Attempt) models.AgentGroupStepAttempt {
	endedAt := item.EndedAt
	if endedAt != nil {
		copied := *endedAt
		endedAt = &copied
	}
	leaseExpiresAt := item.LeaseExpiresAt
	if leaseExpiresAt != nil {
		copied := *leaseExpiresAt
		leaseExpiresAt = &copied
	}
	return models.AgentGroupStepAttempt{
		PublicID:              item.PublicID,
		StepID:                item.StepID,
		AttemptNo:             item.AttemptNo,
		ChildRunID:            item.ChildRunID,
		RetryRequestID:        item.RetryRequestID,
		RequestedModel:        item.RequestedModel,
		ResolvedModel:         item.ResolvedModel,
		InputSnapshotJSON:     item.InputSnapshotJSON,
		ContextFingerprint:    item.ContextFingerprint,
		OutputMarkdown:        item.OutputMarkdown,
		PartialOutputMarkdown: item.PartialOutputMarkdown,
		Status:                item.Status,
		ErrorCode:             item.ErrorCode,
		ErrorMessage:          item.ErrorMessage,
		BillingRef:            item.BillingRef,
		LeaseExpiresAt:        leaseExpiresAt,
		StartedAt:             item.StartedAt,
		EndedAt:               endedAt,
	}
}

func toAttemptDomain(entity models.AgentGroupStepAttempt) domainagentgroup.Attempt {
	return domainagentgroup.Attempt{
		ID:                   entity.ID,
		PublicID:             entity.PublicID,
		StepID:               entity.StepID,
		AttemptNo:            entity.AttemptNo,
		ChildRunID:           entity.ChildRunID,
		RetryRequestID:       entity.RetryRequestID,
		RequestedModel:       entity.RequestedModel,
		ResolvedModel:        entity.ResolvedModel,
		InputSnapshotJSON:    entity.InputSnapshotJSON,
		ContextFingerprint:   entity.ContextFingerprint,
		OutputMarkdown:       entity.OutputMarkdown,
		PartialOutputMarkdown: entity.PartialOutputMarkdown,
		Status:               entity.Status,
		ErrorCode:            entity.ErrorCode,
		ErrorMessage:         entity.ErrorMessage,
		BillingRef:           entity.BillingRef,
		LeaseExpiresAt:       entity.LeaseExpiresAt,
		StartedAt:            entity.StartedAt,
		EndedAt:              entity.EndedAt,
		CreatedAt:            entity.CreatedAt,
		UpdatedAt:            entity.UpdatedAt,
	}
}

// memberQuery 构造成员与角色摘要的联查。
func memberQuery(db *gorm.DB) *gorm.DB {
	return db.Table("chat_agent_group_members AS members").
		Select("members.*, roles.public_id AS role_public_id, roles.name AS role_name, roles.icon AS role_icon, roles.color AS role_color, roles.model AS role_model, roles.provider AS role_provider").
		Joins("JOIN chat_roles AS roles ON roles.id = members.role_id")
}

// loadMembersByGroupIDs 批量加载群组成员（含角色摘要）。
func loadMembersByGroupIDs(db *gorm.DB, groupIDs []uint) (map[uint][]memberRow, error) {
	rows := make([]memberRow, 0)
	if len(groupIDs) == 0 {
		return map[uint][]memberRow{}, nil
	}
	if err := memberQuery(db).
		Where("members.group_id IN ?", groupIDs).
		Order("members.sort_order ASC, members.id ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	byGroup := make(map[uint][]memberRow, len(groupIDs))
	for _, row := range rows {
		byGroup[row.GroupID] = append(byGroup[row.GroupID], row)
	}
	return byGroup, nil
}

func now() time.Time {
	return time.Now().UTC()
}

// groupQuery 构造群组与项目摘要的联查（群组已全局化，项目可空；未绑定时项目字段为空串）。
func groupQuery(db *gorm.DB) *gorm.DB {
	return db.Table("chat_agent_groups AS groups").
		Select("groups.*, projects.public_id AS project_public_id, projects.name AS project_name").
		Joins("LEFT JOIN chat_conversation_projects AS projects ON projects.id = groups.project_id")
}

// runQuery 构造运行与群组摘要的联查。
func runQuery(db *gorm.DB) *gorm.DB {
	return db.Table("chat_agent_group_runs AS runs").
		Select("runs.*, groups.public_id AS group_public_id").
		Joins("JOIN chat_agent_groups AS groups ON groups.id = runs.group_id")
}

// ListAgentGroups 查询当前用户全部群组（含成员与角色摘要）。
// projectID 为 0 时不过滤项目（群组已全局化）；否则仅返回指定项目内群组（兼容历史调用）。
func (r *Repo) ListAgentGroups(ctx context.Context, userID uint, projectID uint) ([]domainagentgroup.Group, error) {
	var rows []groupRow
	query := groupQuery(r.db.WithContext(ctx)).
		Where("groups.user_id = ?", userID)
	if projectID != 0 {
		query = query.Where("groups.project_id = ?", projectID)
	}
	if err := query.Order("groups.sort_order ASC, groups.id ASC").
		Scan(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	if len(rows) == 0 {
		return []domainagentgroup.Group{}, nil
	}
	groupIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		groupIDs = append(groupIDs, row.ID)
	}
	members, err := loadMembersByGroupIDs(r.db, groupIDs)
	if err != nil {
		return nil, translateError(err)
	}
	groups := make([]domainagentgroup.Group, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, toGroupDomain(row.AgentGroup, row.ProjectPublicID, row.ProjectName, members[row.ID]))
	}
	return groups, nil
}

// GetAgentGroupByPublicID 查询单个群组（含成员与角色摘要）。
func (r *Repo) GetAgentGroupByPublicID(ctx context.Context, userID uint, publicID string) (*domainagentgroup.Group, error) {
	var row groupRow
	if err := groupQuery(r.db.WithContext(ctx)).
		Where("groups.user_id = ? AND groups.public_id = ?", userID, publicID).
		Scan(&row).Error; err != nil {
		return nil, translateError(err)
	}
	if row.ID == 0 {
		return nil, repository.ErrNotFound
	}
	members, err := loadMembersByGroupIDs(r.db, []uint{row.ID})
	if err != nil {
		return nil, translateError(err)
	}
	group := toGroupDomain(row.AgentGroup, row.ProjectPublicID, row.ProjectName, members[row.ID])
	return &group, nil
}

// GetAgentGroupMemberByPublicID 查询群组成员（含角色摘要）。
func (r *Repo) GetAgentGroupMemberByPublicID(ctx context.Context, groupID uint, userID uint, publicID string) (*domainagentgroup.Member, error) {
	if err := r.ensureGroupOwnership(ctx, groupID, userID); err != nil {
		return nil, err
	}
	var row memberRow
	if err := memberQuery(r.db.WithContext(ctx)).
		Where("members.group_id = ? AND members.public_id = ?", groupID, publicID).
		Scan(&row).Error; err != nil {
		return nil, translateError(err)
	}
	if row.ID == 0 {
		return nil, repository.ErrNotFound
	}
	member := toMemberDomain(row)
	return &member, nil
}

// CountAgentGroupReferencesByRole 统计引用角色的群组成员关系数量（角色删除保护）。
func (r *Repo) CountAgentGroupReferencesByRole(ctx context.Context, roleID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.AgentGroupMember{}).Where("role_id = ?", roleID).Count(&count).Error
	return count, translateError(err)
}

// CountAgentGroupReferencesByProject 统计项目下群组数量（项目删除保护）。
func (r *Repo) CountAgentGroupReferencesByProject(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.AgentGroup{}).Where("project_id = ?", projectID).Count(&count).Error
	return count, translateError(err)
}

// CountAgentGroupHistory 统计群组的会话与运行历史总数（删除保护）。
func (r *Repo) CountAgentGroupHistory(ctx context.Context, groupID uint) (int64, error) {
	var convCount int64
	if err := r.db.WithContext(ctx).Model(&models.Conversation{}).Where("agent_group_id = ?", groupID).Count(&convCount).Error; err != nil {
		return 0, translateError(err)
	}
	var runCount int64
	if err := r.db.WithContext(ctx).Model(&models.AgentGroupRun{}).Where("group_id = ?", groupID).Count(&runCount).Error; err != nil {
		return 0, translateError(err)
	}
	return convCount + runCount, nil
}
