package agentgroup

import (
	"context"
	"time"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	models "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreateAgentGroupRun 创建运行（client_run_id 唯一，冲突返回 ErrDuplicate）。
func (r *Repo) CreateAgentGroupRun(ctx context.Context, run *domainagentgroup.Run) error {
	entity := toRunModel(run)
	if err := r.db.WithContext(ctx).Create(&entity).Error; err != nil {
		return translateError(err)
	}
	run.ID = entity.ID
	return nil
}

// CreateAgentGroupRunIfIdle 在会话无活跃运行时原子创建 pending 运行。
// PostgreSQL：事务内先取会话级 advisory xact lock，再复查活跃运行并插入，
// 保证多实例下同一会话至多创建一个活跃运行；SQLite（单实例部署）退化为
// 事务内普通检查 + 插入。返回 created=false 表示已有活跃运行。
func (r *Repo) CreateAgentGroupRunIfIdle(ctx context.Context, run *domainagentgroup.Run) (bool, error) {
	var created bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Dialector != nil && tx.Dialector.Name() == "postgres" {
			// 会话 ID 为自增主键，直接作为 advisory lock 键空间（int64 安全）。
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", agentGroupAdvisoryLockKey(run.ConversationID)).Error; err != nil {
				return translateError(err)
			}
		}
		var active int64
		if err := tx.Model(&models.AgentGroupRun{}).
			Where("conversation_id = ? AND status IN ?", run.ConversationID, []string{
				domainagentgroup.RunStatusPending,
				domainagentgroup.RunStatusRunning,
				domainagentgroup.RunStatusPausedRetryable,
				domainagentgroup.RunStatusBlocked,
			}).
			Count(&active).Error; err != nil {
			return translateError(err)
		}
		if active > 0 {
			return nil
		}
		entity := toRunModel(run)
		if err := tx.Create(&entity).Error; err != nil {
			return translateError(err)
		}
		run.ID = entity.ID
		created = true
		return nil
	})
	return created, translateError(err)
}

// agentGroupAdvisoryLockKey 把会话 ID 映射到 advisory lock 键（负数段，避开迁移锁）。
func agentGroupAdvisoryLockKey(conversationID uint) int64 {
	return -1 - int64(conversationID)
}

// BeginAgentGroupStepRetry 在单个事务内完成重试启动的三段写入：
// run CAS（paused_retryable→running + 清除 retryable_step_id）、步骤回到 running、
// 插入 Attempt N+1。任一步失败整体回滚，消除「run 已 running 但无 attempt」的
// 崩溃窗口。返回 false 表示 run 状态 CAS 冲突（并发双击重试）。
func (r *Repo) BeginAgentGroupStepRetry(
	ctx context.Context,
	runID uint,
	expectedStateVersion int,
	stepID uint,
	attempt *domainagentgroup.Attempt,
) (bool, error) {
	var ok bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		runningResult := tx.Model(&models.AgentGroupRun{}).
			Where("id = ? AND state_version = ? AND status = ?", runID, expectedStateVersion, domainagentgroup.RunStatusPausedRetryable).
			Updates(map[string]interface{}{
				"status":             domainagentgroup.RunStatusRunning,
				"retryable_step_id":  nil,
				"state_version":      gorm.Expr("state_version + 1"),
				"error_code":         "",
				"error_message":      "",
				"ended_at":           nil,
				"updated_at":         now(),
			})
		if runningResult.Error != nil {
			return translateError(runningResult.Error)
		}
		if runningResult.RowsAffected == 0 {
			return nil
		}
		stepResult := tx.Model(&models.AgentGroupStep{}).
			Where("id = ?", stepID).
			Updates(map[string]interface{}{
				"status":     domainagentgroup.StepStatusRunning,
				"updated_at": now(),
			})
		if stepResult.Error != nil {
			return translateError(stepResult.Error)
		}
		entity := toAttemptModel(attempt)
		if err := tx.Create(&entity).Error; err != nil {
			return translateError(err)
		}
		attempt.ID = entity.ID
		ok = true
		return nil
	})
	return ok, translateError(err)
}

// RecoverStaleAgentGroupRuns 回收崩溃窗口遗留的僵尸运行（返回受影响运行数）：
// - stale pending：创建后从未进入 running（进程在 CAS 前崩溃）；
// - running 且没有任何 attempt（首个 attempt 创建前崩溃，租约恢复覆盖不到）。
// 两者统一转为 blocked（放弃入口可用；不转 paused_retryable 以避免
// UI 出现无可重试步骤的误导性重试入口）。有 attempt 的 running 由
// RecoverExpiredAttemptLeases 处理。
func (r *Repo) RecoverStaleAgentGroupRuns(ctx context.Context, now time.Time, cutoff time.Time) (int64, error) {
	var affected int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidates []models.AgentGroupRun
		query := tx.
			Where("status IN ? AND updated_at < ?", []string{
				domainagentgroup.RunStatusPending,
				domainagentgroup.RunStatusRunning,
			}, cutoff).
			Order("id ASC")
		if tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := query.Find(&candidates).Error; err != nil {
			return translateError(err)
		}
		if len(candidates) == 0 {
			return nil
		}

		runIDs := make([]uint, 0, len(candidates))
		for i := range candidates {
			runIDs = append(runIDs, candidates[i].ID)
		}
		var attemptCounts []struct {
			RunID uint
			Total int64
		}
		if err := tx.Model(&models.AgentGroupStepAttempt{}).
			Select("chat_agent_group_steps.group_run_id AS run_id, COUNT(*) AS total").
			Joins("JOIN chat_agent_group_steps ON chat_agent_group_steps.id = chat_agent_group_step_attempts.step_id").
			Where("chat_agent_group_steps.group_run_id IN ?", runIDs).
			Group("chat_agent_group_steps.group_run_id").
			Scan(&attemptCounts).Error; err != nil {
			return translateError(err)
		}
		attemptsByRun := make(map[uint]int64, len(attemptCounts))
		for _, item := range attemptCounts {
			attemptsByRun[item.RunID] = item.Total
		}

		for i := range candidates {
			run := candidates[i]
			switch run.Status {
			case domainagentgroup.RunStatusPending:
				// pending 一律回收。
			case domainagentgroup.RunStatusRunning:
				if attemptsByRun[run.ID] > 0 {
					continue // 有 attempt 的 running 走租约恢复路径
				}
			default:
				continue
			}
			result := tx.Model(&models.AgentGroupRun{}).
				Where("id = ? AND status = ? AND state_version = ?", run.ID, run.Status, run.StateVersion).
				Updates(map[string]interface{}{
					"status":        domainagentgroup.RunStatusBlocked,
					"state_version": gorm.Expr("state_version + 1"),
					"error_code":    domainagentgroup.ErrorCodeInterrupted,
					"error_message": "agent group run interrupted before execution started",
					"ended_at":      now,
					"updated_at":    now,
				})
			if result.Error != nil {
				return translateError(result.Error)
			}
			if result.RowsAffected > 0 {
				affected++
			}
		}
		return nil
	})
	return affected, translateError(err)
}

// GetAgentGroupRunByPublicID 查询运行。
func (r *Repo) GetAgentGroupRunByPublicID(ctx context.Context, userID uint, publicID string) (*domainagentgroup.Run, error) {
	var row runRow
	if err := runQuery(r.db.WithContext(ctx)).
		Where("runs.user_id = ? AND runs.public_id = ?", userID, publicID).
		Scan(&row).Error; err != nil {
		return nil, translateError(err)
	}
	if row.ID == 0 {
		return nil, repository.ErrNotFound
	}
	run := toRunDomain(row.AgentGroupRun, row.GroupPublicID)
	return &run, nil
}

// GetAgentGroupRunByClientRunID 按父流式运行 ID 查询。
func (r *Repo) GetAgentGroupRunByClientRunID(ctx context.Context, conversationID uint, clientRunID string) (*domainagentgroup.Run, error) {
	var row runRow
	if err := runQuery(r.db.WithContext(ctx)).
		Where("runs.conversation_id = ? AND runs.client_run_id = ?", conversationID, clientRunID).
		Scan(&row).Error; err != nil {
		return nil, translateError(err)
	}
	if row.ID == 0 {
		return nil, repository.ErrNotFound
	}
	run := toRunDomain(row.AgentGroupRun, row.GroupPublicID)
	return &run, nil
}

// GetActiveAgentGroupRunByConversation 查询会话当前未结束的运行。
func (r *Repo) GetActiveAgentGroupRunByConversation(ctx context.Context, conversationID uint) (*domainagentgroup.Run, error) {
	var row runRow
	err := runQuery(r.db.WithContext(ctx)).
		Where("runs.conversation_id = ? AND runs.status IN ?", conversationID, []string{
			domainagentgroup.RunStatusPending,
			domainagentgroup.RunStatusRunning,
			domainagentgroup.RunStatusPausedRetryable,
			domainagentgroup.RunStatusBlocked,
		}).
		Order("runs.id DESC").
		Scan(&row).Error
	if err != nil {
		return nil, translateError(err)
	}
	if row.ID == 0 {
		return nil, repository.ErrNotFound
	}
	run := toRunDomain(row.AgentGroupRun, row.GroupPublicID)
	return &run, nil
}

// GetAgentGroupRunDetail 查询运行及其步骤、尝试完整视图。
func (r *Repo) GetAgentGroupRunDetail(ctx context.Context, userID uint, publicID string) (*domainagentgroup.RunDetail, error) {
	run, err := r.GetAgentGroupRunByPublicID(ctx, userID, publicID)
	if err != nil {
		return nil, err
	}
	steps, err := r.ListStepsByRun(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	detail := &domainagentgroup.RunDetail{
		Run:   *run,
		Steps: make([]domainagentgroup.StepDetail, 0, len(steps)),
	}
	stepIDs := make([]uint, 0, len(steps))
	for i := range steps {
		stepIDs = append(stepIDs, steps[i].ID)
	}
	attemptsByStep, err := r.ListAttemptsBySteps(ctx, stepIDs)
	if err != nil {
		return nil, err
	}
	for i := range steps {
		detail.Steps = append(detail.Steps, domainagentgroup.StepDetail{
			Step:     steps[i],
			Attempts: attemptsByStep[steps[i].ID],
		})
	}
	return detail, nil
}

// CASUpdateAgentGroupRun 条件更新运行检查点：
// UPDATE ... SET ... WHERE id = ? AND state_version = ? AND status = ?
// 返回 false 表示 CAS 冲突（无行更新）。
func (r *Repo) CASUpdateAgentGroupRun(ctx context.Context, runID uint, expectedStateVersion int, expectedStatus string, patch domainagentgroup.RunPatch) (bool, error) {
	fields := map[string]interface{}{
		"state_version": gorm.Expr("state_version + 1"),
		"updated_at":    now(),
	}
	if patch.Status != nil {
		fields["status"] = *patch.Status
	}
	if patch.CurrentStepID != nil {
		fields["current_step_id"] = *patch.CurrentStepID
	}
	if patch.LastCompletedStepID != nil {
		fields["last_completed_step_id"] = *patch.LastCompletedStepID
	}
	if patch.RetryableStepID != nil {
		fields["retryable_step_id"] = *patch.RetryableStepID
	} else if patch.ClearRetryableStep {
		fields["retryable_step_id"] = nil
	}
	if patch.AssistantMessageID != nil {
		fields["assistant_message_id"] = *patch.AssistantMessageID
	}
	if patch.ConfigSnapshotJSON != nil {
		fields["config_snapshot_json"] = *patch.ConfigSnapshotJSON
	}
	if patch.ErrorCode != nil {
		fields["error_code"] = *patch.ErrorCode
	}
	if patch.ErrorMessage != nil {
		fields["error_message"] = *patch.ErrorMessage
	}
	if patch.EndedAt != nil {
		fields["ended_at"] = *patch.EndedAt
	}
	res := r.db.WithContext(ctx).Model(&models.AgentGroupRun{}).
		Where("id = ? AND state_version = ? AND status = ?", runID, expectedStateVersion, expectedStatus).
		Updates(fields)
	if res.Error != nil {
		return false, translateError(res.Error)
	}
	return res.RowsAffected > 0, nil
}

// CountUnfinishedStepsByRun 统计运行中未结束的逻辑步骤数量。
func (r *Repo) CountUnfinishedStepsByRun(ctx context.Context, runID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.AgentGroupStep{}).
		Where("group_run_id = ? AND status IN ?", runID, []string{
			domainagentgroup.StepStatusPending,
			domainagentgroup.StepStatusRunning,
			domainagentgroup.StepStatusInterrupted,
		}).
		Count(&count).Error
	return count, translateError(err)
}

// CountStepsByRun 统计运行累计步骤数量。
func (r *Repo) CountStepsByRun(ctx context.Context, runID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.AgentGroupStep{}).Where("group_run_id = ?", runID).Count(&count).Error
	return count, translateError(err)
}

// CreateAgentGroupStep 创建逻辑步骤（(group_run_id, sequence) 唯一）。
func (r *Repo) CreateAgentGroupStep(ctx context.Context, step *domainagentgroup.Step) error {
	entity := toStepModel(step)
	if err := r.db.WithContext(ctx).Create(&entity).Error; err != nil {
		return translateError(err)
	}
	step.ID = entity.ID
	return nil
}

// UpdateAgentGroupStep 更新步骤检查点（合并 updated_at）。
func (r *Repo) UpdateAgentGroupStep(ctx context.Context, stepID uint, fields map[string]interface{}) error {
	merged := make(map[string]interface{}, len(fields)+1)
	for k, v := range fields {
		merged[k] = v
	}
	merged["updated_at"] = now()
	if err := r.db.WithContext(ctx).Model(&models.AgentGroupStep{}).Where("id = ?", stepID).Updates(merged).Error; err != nil {
		return translateError(err)
	}
	return nil
}

// ListStepsByRun 查询运行的全部步骤（按 sequence 升序）。
func (r *Repo) ListStepsByRun(ctx context.Context, runID uint) ([]domainagentgroup.Step, error) {
	var entities []models.AgentGroupStep
	if err := r.db.WithContext(ctx).Where("group_run_id = ?", runID).Order("sequence ASC").Find(&entities).Error; err != nil {
		return nil, translateError(err)
	}
	steps := make([]domainagentgroup.Step, 0, len(entities))
	for i := range entities {
		steps = append(steps, toStepDomain(entities[i]))
	}
	return steps, nil
}

// GetAgentGroupStepByPublicID 查询步骤（校验运行归属）。
func (r *Repo) GetAgentGroupStepByPublicID(ctx context.Context, userID uint, runPublicID string, stepPublicID string) (*domainagentgroup.Step, error) {
	var entity models.AgentGroupStep
	err := r.db.WithContext(ctx).
		Joins("JOIN chat_agent_group_runs ON chat_agent_group_runs.id = chat_agent_group_steps.group_run_id").
		Where("chat_agent_group_runs.public_id = ? AND chat_agent_group_runs.user_id = ? AND chat_agent_group_steps.public_id = ?",
			runPublicID, userID, stepPublicID).
		First(&entity).Error
	if err != nil {
		return nil, translateError(err)
	}
	step := toStepDomain(entity)
	return &step, nil
}

// CreateAgentGroupStepAttempt 创建尝试（(step_id, attempt_no) 与 retry_request_id 唯一）。
func (r *Repo) CreateAgentGroupStepAttempt(ctx context.Context, attempt *domainagentgroup.Attempt) error {
	entity := toAttemptModel(attempt)
	if err := r.db.WithContext(ctx).Create(&entity).Error; err != nil {
		return translateError(err)
	}
	attempt.ID = entity.ID
	return nil
}

// CASUpdateAgentGroupStepAttempt 条件更新尝试：
// UPDATE ... SET ... WHERE id = ? AND status = ?
// 返回 false 表示 CAS 冲突（无行更新）。
func (r *Repo) CASUpdateAgentGroupStepAttempt(ctx context.Context, attemptID uint, expectedStatus string, patch domainagentgroup.AttemptPatch) (bool, error) {
	fields := map[string]interface{}{"updated_at": now()}
	if patch.Status != nil {
		fields["status"] = *patch.Status
	}
	if patch.ChildRunID != nil {
		fields["child_run_id"] = *patch.ChildRunID
	}
	if patch.ResolvedModel != nil {
		fields["resolved_model"] = *patch.ResolvedModel
	}
	if patch.OutputMarkdown != nil {
		fields["output_markdown"] = *patch.OutputMarkdown
	}
	if patch.PartialOutputMarkdown != nil {
		fields["partial_output_markdown"] = *patch.PartialOutputMarkdown
	}
	if patch.ThinkMarkdown != nil {
		fields["think_markdown"] = *patch.ThinkMarkdown
	}
	if patch.ToolCallsJSON != nil {
		fields["tool_calls_json"] = *patch.ToolCallsJSON
	}
	if patch.InputSnapshotJSON != nil {
		fields["input_snapshot_json"] = *patch.InputSnapshotJSON
	}
	if patch.ErrorCode != nil {
		fields["error_code"] = *patch.ErrorCode
	}
	if patch.ErrorMessage != nil {
		fields["error_message"] = *patch.ErrorMessage
	}
	if patch.ContextFingerprint != nil {
		fields["context_fingerprint"] = *patch.ContextFingerprint
	}
	if patch.BillingRef != nil {
		fields["billing_ref"] = *patch.BillingRef
	}
	if patch.LeaseExpiresAt != nil {
		fields["lease_expires_at"] = *patch.LeaseExpiresAt
	}
	if patch.EndedAt != nil {
		fields["ended_at"] = *patch.EndedAt
	}
	res := r.db.WithContext(ctx).Model(&models.AgentGroupStepAttempt{}).
		Where("id = ? AND status = ?", attemptID, expectedStatus).
		Updates(fields)
	if res.Error != nil {
		return false, translateError(res.Error)
	}
	return res.RowsAffected > 0, nil
}

// ListAttemptsByStep 查询步骤的全部尝试（按 attempt_no 升序）。
func (r *Repo) ListAttemptsByStep(ctx context.Context, stepID uint) ([]domainagentgroup.Attempt, error) {
	grouped, err := r.ListAttemptsBySteps(ctx, []uint{stepID})
	if err != nil {
		return nil, err
	}
	return grouped[stepID], nil
}

// ListAttemptsBySteps 批量查询多个步骤的尝试。
func (r *Repo) ListAttemptsBySteps(ctx context.Context, stepIDs []uint) (map[uint][]domainagentgroup.Attempt, error) {
	grouped := make(map[uint][]domainagentgroup.Attempt, len(stepIDs))
	if len(stepIDs) == 0 {
		return grouped, nil
	}
	var entities []models.AgentGroupStepAttempt
	if err := r.db.WithContext(ctx).
		Where("step_id IN ?", stepIDs).
		Order("step_id ASC, attempt_no ASC").
		Find(&entities).Error; err != nil {
		return nil, translateError(err)
	}
	for i := range entities {
		attempt := toAttemptDomain(entities[i])
		grouped[attempt.StepID] = append(grouped[attempt.StepID], attempt)
	}
	return grouped, nil
}

// GetAgentGroupStepAttemptByPublicID 查询尝试（校验运行归属）。
func (r *Repo) GetAgentGroupStepAttemptByPublicID(ctx context.Context, userID uint, runPublicID string, attemptPublicID string) (*domainagentgroup.Attempt, error) {
	var entity models.AgentGroupStepAttempt
	err := r.db.WithContext(ctx).
		Joins("JOIN chat_agent_group_steps ON chat_agent_group_steps.id = chat_agent_group_step_attempts.step_id").
		Joins("JOIN chat_agent_group_runs ON chat_agent_group_runs.id = chat_agent_group_steps.group_run_id").
		Where("chat_agent_group_runs.public_id = ? AND chat_agent_group_runs.user_id = ? AND chat_agent_group_step_attempts.public_id = ?",
			runPublicID, userID, attemptPublicID).
		First(&entity).Error
	if err != nil {
		return nil, translateError(err)
	}
	attempt := toAttemptDomain(entity)
	return &attempt, nil
}

// CountAttemptsByStep 统计步骤累计尝试数量。
func (r *Repo) CountAttemptsByStep(ctx context.Context, stepID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.AgentGroupStepAttempt{}).Where("step_id = ?", stepID).Count(&count).Error
	return count, translateError(err)
}

// RenewAgentGroupStepAttemptLease 仅为仍在 running 的尝试续租。
func (r *Repo) RenewAgentGroupStepAttemptLease(ctx context.Context, attemptID uint, leaseExpiresAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&models.AgentGroupStepAttempt{}).
		Where("id = ? AND status = ?", attemptID, domainagentgroup.AttemptStatusRunning).
		Updates(map[string]interface{}{
			"lease_expires_at": leaseExpiresAt,
			"updated_at":       now(),
		})
	if result.Error != nil {
		return false, translateError(result.Error)
	}
	return result.RowsAffected > 0, nil
}

// RecoverExpiredAttemptLeases 将租约过期的 running 尝试转为 interrupted，
// 并把对应运行转为 paused_retryable（返回受影响运行数）。
func (r *Repo) RecoverExpiredAttemptLeases(ctx context.Context, now time.Time) (int64, error) {
	var affected int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var expired []models.AgentGroupStepAttempt
		query := tx.Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ?",
			domainagentgroup.AttemptStatusRunning, now).
			Order("step_id ASC, id ASC")
		if tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := query.Find(&expired).Error; err != nil {
			return translateError(err)
		}
		for i := range expired {
			attempt := expired[i]
			var step models.AgentGroupStep
			if err := tx.Where("id = ?", attempt.StepID).First(&step).Error; err != nil {
				return translateError(err)
			}
			var run models.AgentGroupRun
			runQuery := tx.Where("id = ?", step.GroupRunID)
			if tx.Dialector.Name() != "sqlite" {
				runQuery = runQuery.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			if err := runQuery.First(&run).Error; err != nil {
				return translateError(err)
			}
			if run.Status != domainagentgroup.RunStatusRunning || run.RetryableStepID != nil || step.Status != domainagentgroup.StepStatusRunning {
				continue
			}

			attemptResult := tx.Model(&models.AgentGroupStepAttempt{}).
				Where("id = ? AND status = ? AND lease_expires_at < ?", attempt.ID, domainagentgroup.AttemptStatusRunning, now).
				Updates(map[string]interface{}{
					"status":           domainagentgroup.AttemptStatusInterrupted,
					"error_code":       domainagentgroup.ErrorCodeInterrupted,
					"error_message":    "agent group attempt lease expired",
					"lease_expires_at": nil,
					"ended_at":         now,
					"updated_at":       now,
				})
			if attemptResult.Error != nil {
				return translateError(attemptResult.Error)
			}
			if attemptResult.RowsAffected == 0 {
				continue
			}
			stepResult := tx.Model(&models.AgentGroupStep{}).
				Where("id = ? AND status = ?", step.ID, domainagentgroup.StepStatusRunning).
				Updates(map[string]interface{}{
					"status":     domainagentgroup.StepStatusInterrupted,
					"updated_at": now,
				})
			if stepResult.Error != nil {
				return translateError(stepResult.Error)
			}
			if stepResult.RowsAffected == 0 {
				return repository.ErrConflict
			}
			runResult := tx.Model(&models.AgentGroupRun{}).
				Where("id = ? AND state_version = ? AND status = ? AND retryable_step_id IS NULL", run.ID, run.StateVersion, domainagentgroup.RunStatusRunning).
				Updates(map[string]interface{}{
					"status":            domainagentgroup.RunStatusPausedRetryable,
					"retryable_step_id": step.ID,
					"state_version":     gorm.Expr("state_version + 1"),
					"error_code":        domainagentgroup.ErrorCodeInterrupted,
					"error_message":     "agent group attempt lease expired",
					"ended_at":          now,
					"updated_at":        now,
				})
			if runResult.Error != nil {
				return translateError(runResult.Error)
			}
			if runResult.RowsAffected == 0 {
				return repository.ErrConflict
			}
			affected++
		}
		return nil
	})
	return affected, translateError(err)
}
