package agentgroup

import (
	"context"
	"time"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	models "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
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
	for i := range steps {
		attempts, err := r.ListAttemptsByStep(ctx, steps[i].ID)
		if err != nil {
			return nil, err
		}
		detail.Steps = append(detail.Steps, domainagentgroup.StepDetail{
			Step:     steps[i],
			Attempts: attempts,
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
	var entities []models.AgentGroupStepAttempt
	if err := r.db.WithContext(ctx).Where("step_id = ?", stepID).Order("attempt_no ASC").Find(&entities).Error; err != nil {
		return nil, translateError(err)
	}
	attempts := make([]domainagentgroup.Attempt, 0, len(entities))
	for i := range entities {
		attempts = append(attempts, toAttemptDomain(entities[i]))
	}
	return attempts, nil
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

// RecoverExpiredAttemptLeases 将租约过期的 running 尝试转为 interrupted，
// 并把对应运行转为 paused_retryable（返回受影响运行数）。
// 事务内：先标记尝试，再逐个推进运行检查点（CAS 语义，冲突跳过）。
func (r *Repo) RecoverExpiredAttemptLeases(ctx context.Context, now time.Time) (int64, error) {
	var affected int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var expired []models.AgentGroupStepAttempt
		if err := tx.Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ?",
			domainagentgroup.AttemptStatusRunning, now).Find(&expired).Error; err != nil {
			return translateError(err)
		}
		if len(expired) == 0 {
			return nil
		}
		attemptIDs := make([]uint, 0, len(expired))
		stepIDs := make([]uint, 0, len(expired))
		for _, a := range expired {
			attemptIDs = append(attemptIDs, a.ID)
			stepIDs = append(stepIDs, a.StepID)
		}
		if err := tx.Model(&models.AgentGroupStepAttempt{}).
			Where("id IN ? AND status = ?", attemptIDs, domainagentgroup.AttemptStatusRunning).
			Updates(map[string]interface{}{
				"status":     domainagentgroup.AttemptStatusInterrupted,
				"updated_at": now,
			}).Error; err != nil {
			return translateError(err)
		}

		var steps []models.AgentGroupStep
		if err := tx.Where("id IN ?", stepIDs).Find(&steps).Error; err != nil {
			return translateError(err)
		}
		// 每个运行取最先出现的步骤作为可重试步骤。
		stepByRun := make(map[uint]uint, len(steps))
		for _, s := range steps {
			if _, ok := stepByRun[s.GroupRunID]; !ok {
				stepByRun[s.GroupRunID] = s.ID
			}
		}
		for runID, stepID := range stepByRun {
			var runEntity models.AgentGroupRun
			if err := tx.Where("id = ?", runID).First(&runEntity).Error; err != nil {
				return translateError(err)
			}
			if runEntity.Status != domainagentgroup.RunStatusRunning || runEntity.RetryableStepID != nil {
				continue
			}
			if err := tx.Model(&models.AgentGroupRun{}).
				Where("id = ? AND state_version = ? AND status = ?", runID, runEntity.StateVersion, domainagentgroup.RunStatusRunning).
				Updates(map[string]interface{}{
					"status":            domainagentgroup.RunStatusPausedRetryable,
					"retryable_step_id": stepID,
					"state_version":     gorm.Expr("state_version + 1"),
					"updated_at":        now,
				}).Error; err != nil {
				return translateError(err)
			}
			affected++
		}
		return nil
	})
	return affected, translateError(err)
}
