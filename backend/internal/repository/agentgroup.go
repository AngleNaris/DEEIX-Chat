package repository

import (
	"context"
	"time"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
)

// AgentGroupRepository 定义 agentgroup 编排层所需的聚合仓储能力。
type AgentGroupRepository interface {
	AgentGroupQueryRepository
	AgentGroupWriteRepository
	AgentGroupRunRepository
}

// AgentGroupQueryRepository 群组与成员查询。
type AgentGroupQueryRepository interface {
	// ListAgentGroups 查询当前用户全部群组（projectID 为 0 时不过滤项目；含成员与角色摘要）。
	ListAgentGroups(ctx context.Context, userID uint, projectID uint) ([]domainagentgroup.Group, error)
	// GetAgentGroupByPublicID 查询单个群组（含成员与角色摘要）。
	GetAgentGroupByPublicID(ctx context.Context, userID uint, publicID string) (*domainagentgroup.Group, error)
	// GetAgentGroupMemberByPublicID 查询群组成员（含角色摘要）。
	GetAgentGroupMemberByPublicID(ctx context.Context, groupID uint, userID uint, publicID string) (*domainagentgroup.Member, error)
	// CountAgentGroupReferencesByRole 统计引用角色的未移除群组成员关系数量（角色删除保护）。
	CountAgentGroupReferencesByRole(ctx context.Context, roleID uint) (int64, error)
	// CountAgentGroupReferencesByProject 统计项目下群组数量（项目删除保护）。
	CountAgentGroupReferencesByProject(ctx context.Context, projectID uint) (int64, error)
	// CountAgentGroupHistory 统计群组的会话与运行历史总数（删除保护）。
	CountAgentGroupHistory(ctx context.Context, groupID uint) (int64, error)
}

// AgentGroupWriteRepository 群组与成员写入（仓储内部保证事务与不变量）。
type AgentGroupWriteRepository interface {
	// CreateAgentGroupWithSupervisor 在一个事务中创建群组、主管成员和全部工作成员。
	CreateAgentGroupWithSupervisor(ctx context.Context, group *domainagentgroup.Group, supervisor domainagentgroup.Member, workers []domainagentgroup.Member) error
	// UpdateAgentGroupByPublicID 更新群组元数据并递增配置版本。
	UpdateAgentGroupByPublicID(ctx context.Context, userID uint, publicID string, patch domainagentgroup.GroupPatch) (*domainagentgroup.Group, error)
	// DeleteAgentGroupByPublicID 硬删除没有会话/运行历史的群组。
	DeleteAgentGroupByPublicID(ctx context.Context, userID uint, publicID string) error
	// AddAgentGroupMember 添加工作成员并递增群组配置版本。
	AddAgentGroupMember(ctx context.Context, groupID uint, member domainagentgroup.Member) error
	// UpdateAgentGroupMemberByPublicID 更新成员（主管不可禁用）并递增群组配置版本。
	UpdateAgentGroupMemberByPublicID(ctx context.Context, groupID uint, userID uint, publicID string, patch domainagentgroup.MemberPatch) (*domainagentgroup.Member, error)
	// RemoveAgentGroupMemberByPublicID 移除成员关联（主管不可直接移除）并递增群组配置版本。
	RemoveAgentGroupMemberByPublicID(ctx context.Context, groupID uint, userID uint, publicID string) error
	// ReorderAgentGroupMembers 按传入顺序重排成员并递增群组配置版本。
	ReorderAgentGroupMembers(ctx context.Context, groupID uint, userID uint, orderedPublicIDs []string) error
	// ChangeAgentGroupSupervisor 在一个事务中完成主管替换并递增群组配置版本。
	ChangeAgentGroupSupervisor(ctx context.Context, groupID uint, userID uint, memberPublicID string) error
}

// AgentGroupRunRepository 运行、步骤与尝试的持久化（CAS 乐观锁并发控制）。
type AgentGroupRunRepository interface {
	// CreateAgentGroupRun 创建运行（client_run_id 唯一）。
	CreateAgentGroupRun(ctx context.Context, run *domainagentgroup.Run) error
	// CreateAgentGroupRunIfIdle 在会话无活跃运行时原子创建 pending 运行；
	// created=false 表示该会话已存在活跃运行（多实例 admission 守卫）。
	CreateAgentGroupRunIfIdle(ctx context.Context, run *domainagentgroup.Run) (bool, error)
	// GetAgentGroupRunByPublicID 查询运行。
	GetAgentGroupRunByPublicID(ctx context.Context, userID uint, publicID string) (*domainagentgroup.Run, error)
	// GetAgentGroupRunByClientRunID 按父流式运行 ID 查询。
	GetAgentGroupRunByClientRunID(ctx context.Context, conversationID uint, clientRunID string) (*domainagentgroup.Run, error)
	// GetActiveAgentGroupRunByConversation 查询会话当前未结束的运行。
	GetActiveAgentGroupRunByConversation(ctx context.Context, conversationID uint) (*domainagentgroup.Run, error)
	// GetAgentGroupRunDetail 查询运行及其步骤、尝试完整视图。
	GetAgentGroupRunDetail(ctx context.Context, userID uint, publicID string) (*domainagentgroup.RunDetail, error)
	// CASUpdateAgentGroupRun 条件更新运行检查点：
	// UPDATE ... SET ... WHERE id = ? AND state_version = ? AND status = ?
	// 返回 false 表示 CAS 冲突（无行更新）。
	CASUpdateAgentGroupRun(ctx context.Context, runID uint, expectedStateVersion int, expectedStatus string, patch domainagentgroup.RunPatch) (bool, error)
	// CountUnfinishedStepsByRun 统计运行中未结束的逻辑步骤数量。
	CountUnfinishedStepsByRun(ctx context.Context, runID uint) (int64, error)
	// CountStepsByRun 统计运行累计步骤数量。
	CountStepsByRun(ctx context.Context, runID uint) (int64, error)
	// CreateAgentGroupStep 创建逻辑步骤（(group_run_id, sequence) 唯一）。
	CreateAgentGroupStep(ctx context.Context, step *domainagentgroup.Step) error
	// UpdateAgentGroupStep 更新步骤检查点。
	UpdateAgentGroupStep(ctx context.Context, stepID uint, fields map[string]interface{}) error
	// ListStepsByRun 查询运行的全部步骤（按 sequence 升序）。
	ListStepsByRun(ctx context.Context, runID uint) ([]domainagentgroup.Step, error)
	// GetAgentGroupStepByPublicID 查询步骤。
	GetAgentGroupStepByPublicID(ctx context.Context, userID uint, runPublicID string, stepPublicID string) (*domainagentgroup.Step, error)
	// CreateAgentGroupStepAttempt 创建尝试（(step_id, attempt_no) 与 retry_request_id 唯一）。
	CreateAgentGroupStepAttempt(ctx context.Context, attempt *domainagentgroup.Attempt) error
	// CASUpdateAgentGroupStepAttempt 条件更新尝试：
	// UPDATE ... SET ... WHERE id = ? AND status = ?
	// 返回 false 表示 CAS 冲突（无行更新）。
	CASUpdateAgentGroupStepAttempt(ctx context.Context, attemptID uint, expectedStatus string, patch domainagentgroup.AttemptPatch) (bool, error)
	// ListAttemptsByStep 查询步骤的全部尝试（按 attempt_no 升序）。
	ListAttemptsByStep(ctx context.Context, stepID uint) ([]domainagentgroup.Attempt, error)
	// ListAttemptsBySteps 批量查询多个步骤的尝试，结果按 step_id 分组且组内按 attempt_no 升序。
	ListAttemptsBySteps(ctx context.Context, stepIDs []uint) (map[uint][]domainagentgroup.Attempt, error)
	// GetAgentGroupStepAttemptByPublicID 查询尝试。
	GetAgentGroupStepAttemptByPublicID(ctx context.Context, userID uint, runPublicID string, attemptPublicID string) (*domainagentgroup.Attempt, error)
	// CountAttemptsByStep 统计步骤累计尝试数量。
	CountAttemptsByStep(ctx context.Context, stepID uint) (int64, error)
	// BeginAgentGroupStepRetry 在单事务内完成重试启动：run CAS（paused_retryable→running
	// + 清除可重试标记）、步骤回 running、插入 Attempt N+1；false 表示 run 状态 CAS 冲突。
	BeginAgentGroupStepRetry(ctx context.Context, runID uint, expectedStateVersion int, stepID uint, attempt *domainagentgroup.Attempt) (bool, error)
	// RenewAgentGroupStepAttemptLease 仅为仍在 running 的尝试续租。
	RenewAgentGroupStepAttemptLease(ctx context.Context, attemptID uint, leaseExpiresAt time.Time) (bool, error)
	// RecoverExpiredAttemptLeases 将租约过期的 running 尝试转为 interrupted，
	// 并把对应运行转为 paused_retryable（返回受影响运行数）。
	RecoverExpiredAttemptLeases(ctx context.Context, now time.Time) (int64, error)
	// RecoverStaleAgentGroupRuns 回收崩溃窗口遗留的僵尸运行：stale pending 与
	// 无 attempt 的 stale running 统一转 blocked（返回受影响运行数）。
	RecoverStaleAgentGroupRuns(ctx context.Context, now time.Time, cutoff time.Time) (int64, error)
}
