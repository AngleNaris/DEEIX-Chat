package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/traceid"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RetryAgentGroupRunInput 描述一次群组运行重试请求。
// 重试不创建新的用户消息：复用暂停时持久化的用户行与配置快照，
// 仅为被暂停的步骤追加一次新 Attempt（镜像 StreamMessage 的流式契约）。
type RetryAgentGroupRunInput struct {
	UserID      uint
	RunPublicID string
	// StepPublicID 期望重试的步骤 public_id；非空时与服务端可重试步骤不一致会拒绝。
	StepPublicID string
	RequestID    string
	// OnEvent 转发群组流式事件（与首次运行同一协议）。
	OnEvent func(eventType string, payload map[string]interface{}) error
	Stream  bool
	// Cancelable 为 true 时注册取消流：客户端断开或取消时中断当前 Attempt，回到 paused_retryable。
	Cancelable bool
}

// RetryAgentGroupRunStep 重试被暂停的群组运行（A 成功、B 失败时只重试 B）：
// 校验状态 → 重建运行状态（快照/分支/摘要/用量/工具账本）→ Attempt 上限检查 →
// CAS paused_retryable → running（双击重试第二个请求在此冲突）→ 追加 Attempt N+1 →
// 按步骤类型执行 → 推进顶层 conversation_runs 行。
//
// 成功步骤不重复执行：重试只针对 RetryableStepID 指向的步骤，其余步骤保持成功不可变；
// 工具幂等账本按 BillingRef 前缀恢复，已成功的工具调用跨尝试复用，不重复计费。
func (s *Service) RetryAgentGroupRunStep(
	ctx context.Context,
	input RetryAgentGroupRunInput,
	onDelta func(string) error,
) (*SendMessageResult, error) {
	if !s.agentGroupFeatureEnabled(ctx) || s.agentGroupRunStore == nil || s.agentGroupRepo == nil {
		return nil, ErrAgentGroupFeatureDisabled
	}
	run, err := s.agentGroupRunStore.GetAgentGroupRunByPublicID(ctx, input.UserID, input.RunPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAgentGroupRunNotFound
		}
		return nil, err
	}

	// 同会话串行：与首次执行共享同一把互斥锁。
	runLock := s.agentGroupRunMutex(run.ConversationID)
	runLock.Lock()
	defer runLock.Unlock()

	// 状态守卫：仅 paused_retryable 且存在可重试步骤时可重试。
	if run.Status != domainagentgroup.RunStatusPausedRetryable || run.RetryableStepID == nil {
		return nil, ErrAgentGroupRunNotRetryable
	}

	// 定位被暂停的步骤（服务重启后从检查点恢复也走同一路径）。
	steps, err := s.agentGroupRunStore.ListStepsByRun(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	var retryStep *domainagentgroup.Step
	for i := range steps {
		if steps[i].ID == *run.RetryableStepID {
			retryStep = &steps[i]
			break
		}
	}
	if retryStep == nil {
		return nil, ErrAgentGroupRunStateCorrupt
	}
	// URL 步骤与检查点不一致（客户端指向了错误步骤）时拒绝。
	if input.StepPublicID != "" && retryStep.PublicID != input.StepPublicID {
		return nil, ErrAgentGroupRunNotRetryable
	}

	st, err := s.buildAgentGroupRunResumeState(ctx, run, input, steps)
	if err != nil {
		return nil, err
	}
	// 完成路径依赖 assistant 消息身份（completeAgentGroupRun 持久化最终答案）。
	if st.assistantMessage == nil {
		return nil, ErrAgentGroupRunStateCorrupt
	}
	member := agentGroupSnapshotMemberForStep(st.snapshot, retryStep)
	if member == nil {
		return nil, ErrAgentGroupRunStateCorrupt
	}

	// Attempt 上限：DB 统计为准；达到上限不再创建新尝试，直接 paused_retryable → blocked。
	attempts, err := s.agentGroupRunStore.CountAttemptsByStep(ctx, retryStep.ID)
	if err != nil {
		return nil, err
	}
	if attempts >= int64(st.snapshot.Limits.MaxAttemptsPerStep) {
		blockErr := st.blockPausedRetryableRun(ctx, retryStep, domainagentgroup.ErrorCodeAttemptLimitExceeded,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeAttemptLimitExceeded))
		st.updateTopLevelRun(ctx, blockErr)
		return st.failedResult(), blockErr
	}

	// Cancelable 时注册取消（与 executeAgentGroupRun 一致）。
	if input.Cancelable {
		cancelCtx, cancel := context.WithCancel(ctx)
		ctx = cancelCtx
		s.generationStreams.register(ctx, st.runID, input.UserID, cancel)
	}

	// CAS paused_retryable → running + 清除可重试标记。
	// 双击重试：第二个请求读到旧状态，CAS 冲突 → 409，绝不创建重复 Attempt。
	running := domainagentgroup.RunStatusRunning
	ok, err := s.agentGroupRunStore.CASUpdateAgentGroupRun(ctx, run.ID, st.stateVersion,
		domainagentgroup.RunStatusPausedRetryable,
		domainagentgroup.RunPatch{Status: &running, ClearRetryableStep: true})
	if err != nil {
		return st.failedResult(), err
	}
	if !ok {
		return st.failedResult(), ErrAgentGroupCASConflict
	}
	st.stateVersion++
	st.run.Status = running

	// 步骤回到 running，追加 Attempt N+1。
	if err := s.agentGroupRunStore.UpdateAgentGroupStep(ctx, retryStep.ID, map[string]interface{}{
		"status": domainagentgroup.StepStatusRunning,
	}); err != nil {
		_ = st.blockAgentGroupRun(ctx, retryStep, domainagentgroup.ErrorCodeUpstreamFatal,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeUpstreamFatal))
		return st.failedResult(), err
	}
	retryStep.Status = domainagentgroup.StepStatusRunning

	attempt, err := st.createAgentGroupStepRetryAttempt(ctx, retryStep, member, int(attempts)+1)
	if err != nil {
		_ = st.blockAgentGroupRun(ctx, retryStep, domainagentgroup.ErrorCodeUpstreamFatal,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeUpstreamFatal))
		return st.failedResult(), err
	}

	execErr := st.executeRetryableStep(ctx, retryStep, attempt, member)
	st.updateTopLevelRun(ctx, execErr)
	if execErr == nil {
		if onDelta != nil && strings.TrimSpace(st.finalAnswer) != "" {
			_ = onDelta(st.finalAnswer)
		}
		return st.completedResult(), nil
	}
	// 运行未进入终态时兜底阻塞（CAS 冲突/内部错误）。
	if st.run.Status == domainagentgroup.RunStatusRunning || st.run.Status == domainagentgroup.RunStatusPending {
		_ = st.blockAgentGroupRun(ctx, nil, domainagentgroup.ErrorCodeCASConflict,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeCASConflict))
	}
	return st.failedResult(), execErr
}

// unmarshalAgentGroupRunSnapshot 反序列化配置快照（重试/放弃恢复路径）。
// RunSnapshot 无 json tag，Go 默认字段名反序列化大小写不敏感，保证 round-trip。
func unmarshalAgentGroupRunSnapshot(raw string) (*domainagentgroup.RunSnapshot, error) {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return nil, ErrAgentGroupRunStateCorrupt
	}
	snapshot := &domainagentgroup.RunSnapshot{}
	if err := json.Unmarshal([]byte(raw), snapshot); err != nil {
		return nil, ErrAgentGroupRunStateCorrupt
	}
	if snapshot.Group.GroupID == 0 || len(snapshot.Members) == 0 {
		return nil, ErrAgentGroupRunStateCorrupt
	}
	return snapshot, nil
}

// rebuildAgentGroupRunBranchState 从持久化用户行直接重建分支状态，不重新运行 resolveMessageBranch
// （那会把运行自身的用户消息重新选为 parent）。判别式：
// 复用的用户行保留其原始 RunID（retry/edit 分支）；本运行创建的用户行以 ClientRunID 为 RunID。
func (s *Service) rebuildAgentGroupRunBranchState(
	ctx context.Context,
	userMessage *model.Message,
	run *domainagentgroup.Run,
) (*messageSendBranchPreparation, error) {
	if userMessage == nil {
		return nil, ErrAgentGroupRunStateCorrupt
	}
	reuseUserMessage := strings.TrimSpace(userMessage.RunID) != run.ClientRunID

	branchState := &messageBranchState{
		ParentMessageID: userMessage.ParentMessageID,
		ParentPublicID:  userMessage.ParentPublicID,
		SourceMessageID: userMessage.SourceMessageID,
		SourcePublicID:  userMessage.SourcePublicID,
	}
	if reuseUserMessage {
		reused := *userMessage
		branchState.ReuseUserMessage = &reused
	}

	// 历史锚点：与首次运行一致的祖先读取。
	// 复用分支：祖先链从被复用的用户行本身回溯（镜像 resolveMessageBranch 的
	// ListMessageAncestors(父消息.ID)——该父消息即被复用的用户行）。
	// 普通分支：从持久化的 ParentMessageID 回溯，本运行的用户消息由 buildBranchMessagePath 追加。
	var ancestors []model.Message
	ancestorLeaf := userMessage.ID
	if !reuseUserMessage && userMessage.ParentMessageID != nil {
		ancestorLeaf = *userMessage.ParentMessageID
	}
	if ancestorLeaf != 0 {
		items, ancestorErr := s.repo.ListMessageAncestors(ctx, run.ConversationID, ancestorLeaf, s.compactSvc.ResolveContextMessageLimit())
		if ancestorErr != nil {
			s.logger.Warn("rebuild_agent_group_branch_ancestors_failed",
				zap.String("trace_id", traceid.FromContext(ctx)),
				zap.Uint("conversation_id", run.ConversationID),
				zap.Uint("message_id", ancestorLeaf),
				zap.Error(ancestorErr),
			)
			ancestors = []model.Message{}
		} else {
			ancestors = items
		}
	}
	// 默认分支的上下文修剪只对非复用、default 分支应用（镜像 resolveMessageBranch）。
	// 父锚点已在 DB 行中持久化，无需重新选择，因此忽略 normalize 返回的 contextParent。
	if !reuseUserMessage && userMessage.BranchReason == "default" {
		normalized, _ := normalizeDefaultBranchContext(ancestors, nil)
		ancestors = normalized
	}
	branchState.ExistingMessages = ancestors

	return &messageSendBranchPreparation{
		branchState:            branchState,
		normalizedBranchReason: userMessage.BranchReason,
		reuseUserMessage:       reuseUserMessage,
	}, nil
}

// loadAgentGroupAssistantMessage 定位运行的 assistant 消息。
// 优先 run.AssistantMessageID（T7 起所有暂停路径都会持久化）；
// 旧数据兜底：扫描最近消息按 RunID 匹配。找不到返回 nil（由调用方决定是否可继续）。
func (s *Service) loadAgentGroupAssistantMessage(
	ctx context.Context,
	run *domainagentgroup.Run,
	conversation *model.Conversation,
) (*model.Message, error) {
	if run.AssistantMessageID != nil {
		message, err := s.repo.GetMessageByID(ctx, run.ConversationID, *run.AssistantMessageID)
		if err == nil && message != nil {
			return message, nil
		}
		if err != nil {
			s.logger.Warn("load_agent_group_assistant_message_by_id_failed",
				zap.String("trace_id", traceid.FromContext(ctx)),
				zap.Uint("conversation_id", run.ConversationID),
				zap.Uint("message_id", *run.AssistantMessageID),
				zap.Error(err),
			)
		}
	}
	recent, _, err := s.repo.ListRecentMessages(ctx, run.ConversationID, 64)
	if err != nil {
		return nil, err
	}
	for i := len(recent) - 1; i >= 0; i-- {
		if recent[i].Role == "assistant" && recent[i].RunID == run.ClientRunID {
			return &recent[i], nil
		}
	}
	return nil, nil
}

// buildAgentGroupRunResumeState 从持久化检查点重建运行状态（服务重启/重试恢复）。
// 重建内容：配置快照、会话、用户/assistant 消息、分支上下文、步骤序列、
// 成功步骤摘要、顶层用量基线，以及按被暂停步骤 BillingRef 前缀恢复的工具幂等账本。
func (s *Service) buildAgentGroupRunResumeState(
	ctx context.Context,
	run *domainagentgroup.Run,
	retryInput RetryAgentGroupRunInput,
	steps []domainagentgroup.Step,
) (*agentGroupRunState, error) {
	snapshot, err := unmarshalAgentGroupRunSnapshot(run.ConfigSnapshotJSON)
	if err != nil {
		return nil, err
	}
	if snapshot.CredentialWriteAttempted && !snapshot.CredentialWriteResumeSafe {
		return nil, ErrAgentGroupRunStateCorrupt
	}
	conversation, err := s.repo.GetConversationByUser(ctx, run.ConversationID, run.UserID)
	if err != nil {
		return nil, err
	}
	if conversation == nil {
		return nil, ErrConversationNotFound
	}
	userMessage, err := s.repo.GetMessageByID(ctx, run.ConversationID, run.UserMessageID)
	if err != nil {
		return nil, err
	}
	if userMessage == nil {
		return nil, ErrAgentGroupRunStateCorrupt
	}
	assistantMessage, err := s.loadAgentGroupAssistantMessage(ctx, run, conversation)
	if err != nil {
		return nil, err
	}

	// 分支状态 + 上下文消息（与首次执行相同的组装顺序）。
	branchPreparation, err := s.rebuildAgentGroupRunBranchState(ctx, userMessage, run)
	if err != nil {
		return nil, err
	}
	contextMessages := buildBranchMessagePath(branchPreparation.branchState, userMessage)
	cfg := s.cfg.Snapshot()
	compactPolicy := s.resolveContextCompactionPolicy(ctx, cfg, run.UserID)
	if compactPolicy.EffectiveEnabled() {
		if snapshotContext, cacheErr := s.getCachedSnapshot(ctx, run.ConversationID); cacheErr == nil && snapshotContext != nil {
			contextMessages = s.expandContextMessagesToSnapshotBoundary(
				ctx, run.ConversationID, userMessage.ID, contextMessages, snapshotContext, compactPolicy)
		}
	}
	contextMessages = recoverAssistantRetryUserStates(contextMessages)
	// 与首次运行保持一致：只保留历史用户消息并剔除本次 GroupRun 用户行，
	// 让重试回合的专用 UserContent（含 <completed_steps>）成为最新 user 消息。
	contextMessages = agentGroupHistoricalUserContext(contextMessages, userMessage.ID)

	// 步骤序列与成功摘要：Sequence 从现有步骤总数继续；
	// 摘要重建全部成功成员结果与主管委派记录。
	summaries, completedStepCount, err := s.rebuildAgentGroupRunSummaries(ctx, run.ID, steps)
	if err != nil {
		return nil, err
	}

	// 顶层用量基线：首次运行的 conversation_runs 行（聚合全部 Attempt）。
	var totalInput, totalCacheRead, totalCacheWrite, totalOutput, totalReasoning int64
	var totalToolCalls int
	if baselineRuns, runErr := s.repo.ListConversationRunsByRunIDs(ctx, run.UserID, run.ConversationID, []string{run.ClientRunID}); runErr == nil && len(baselineRuns) > 0 {
		totalInput = baselineRuns[0].InputTokens
		totalCacheRead = baselineRuns[0].CacheReadTokens
		totalCacheWrite = baselineRuns[0].CacheWriteTokens
		totalOutput = baselineRuns[0].OutputTokens
		totalReasoning = baselineRuns[0].ReasoningTokens
		totalToolCalls = baselineRuns[0].ToolCallsCount
	}

	// 工具幂等账本：恢复本步骤此前成功/复用的工具行（跨尝试去重，避免重复副作用与重复计费）。
	// 前缀取 "groupRunID:stepID:"，覆盖该步骤全部 Attempt（AttemptNo 从 1 开始）。
	ledger := newToolExecutionLedger()
	if run.RetryableStepID != nil {
		if toolRows, listErr := s.repo.ListConversationToolCallsByRunIDPrefix(
			ctx, run.UserID, run.ConversationID, domainagentgroup.BillingRefStepPrefix(run.ID, *run.RetryableStepID)); listErr == nil {
			for _, row := range toolRows {
				if !strings.EqualFold(row.Status, "success") && !strings.EqualFold(row.Status, "reused") {
					continue
				}
				if modelToolName := strings.TrimSpace(row.ToolName); modelToolName != "" && !isCredentialPlatformTool(modelToolName) {
					ledger.store(row.ToolName, row.InputJSON, toolExecutionRecord{

						row:    row,
						result: buildToolResultForModel(row, modelToolName),
					})
				}
			}
		}
	}

	return &agentGroupRunState{
		service: s,
		input: SendMessageInput{
			UserID:                run.UserID,
			ConversationID:        run.ConversationID,
			RequestID:             strings.TrimSpace(retryInput.RequestID),
			ContentType:           userMessage.ContentType,
			Content:               userMessage.Content,
			PlatformModelName:     conversation.Model,
			ClientRunID:           run.ClientRunID,
			FileIDs:               parseAttachmentSnapshotFileIDs(userMessage.Attachments),
			SelectedToolIDs:       append([]uint(nil), snapshot.RequestedToolIDs...),
			SkillIDs:              append([]uint(nil), snapshot.RequestedSkillIDs...),
			ParentMessagePublicID: userMessage.ParentPublicID,
			SourceMessagePublicID: userMessage.SourcePublicID,
			BranchReason:          userMessage.BranchReason,
			Cancelable:            retryInput.Cancelable,
			OnEvent:               retryInput.OnEvent,
		},
		preferStream:          retryInput.Stream,
		conversation:          conversation,
		snapshot:              snapshot,
		run:                   run,
		branchPreparation:     branchPreparation,
		contextMessages:       contextMessages,
		runID:                 run.ClientRunID,
		startedAt:             run.StartedAt,
		userMessage:           userMessage,
		assistantMessage:      assistantMessage,
		stateVersion:          run.StateVersion,
		stepSequence:          len(steps),
		completedStepCount:    completedStepCount,
		summaries:             summaries,
		totalInputTokens:      totalInput,
		totalCacheReadTokens:  totalCacheRead,
		totalCacheWriteTokens: totalCacheWrite,
		totalOutputTokens:     totalOutput,
		totalReasoningTokens:  totalReasoning,
		totalToolCalls:        totalToolCalls,
		attemptLease:          s.agentGroupAttemptLease(ctx),
		ledger:                ledger,
		persistToolCalls:      true,
		mcpActivation:         newMCPActivationState(snapshot.ActivatedMCPServerIDs),
		credentialAttempted:   snapshot.CredentialWriteAttempted,
	}, nil
}

// rebuildAgentGroupRunSummaries 重建成功步骤的上下文摘要与已完成步骤数。
// 覆盖成员执行步骤（完整成功结果）与主管决策步骤（委派/完成记录）：
// 主管决策历史是跨轮次连续记忆的核心，缺失会导致主管重复委派同一成员；
// 成员结果统一在 brief 渲染时按总预算裁剪，避免恢复路径提前丢失具体内容。
// 不暴露工具参数/失败诊断。
func (s *Service) rebuildAgentGroupRunSummaries(
	ctx context.Context,
	runID uint,
	steps []domainagentgroup.Step,
) ([]agentGroupContextSummary, int, error) {
	summaries := make([]agentGroupContextSummary, 0)
	completed := 0
	for _, step := range steps {
		if step.Status == domainagentgroup.StepStatusSuccess {
			completed++
		}
		if step.SuccessfulAttemptID == nil {
			continue
		}
		attempts, err := s.agentGroupRunStore.ListAttemptsByStep(ctx, step.ID)
		if err != nil {
			return nil, 0, err
		}
		output := ""
		for _, attempt := range attempts {
			if attempt.ID == *step.SuccessfulAttemptID {
				output = attempt.OutputMarkdown
				break
			}
		}
		switch step.StepType {
		case domainagentgroup.StepTypeMemberExecute:
			summaries = append(summaries, agentGroupContextSummary{
				sequence:      step.Sequence,
				stepType:      step.StepType,
				actorMemberID: step.ActorMemberPublicID,
				actorName:     step.ActorNameSnapshot,
				instruction:   step.Instruction,
				outputSummary: output,
			})
		case domainagentgroup.StepTypeSupervisorDecide:
			// 决策步骤：解析成功则按委派/完成记录渲染；老数据无法解析时退化为原文片段。
			if decision, parseErr := resolveAgentGroupSupervisorDecision(output); parseErr == nil {
				summaries = append(summaries, agentGroupContextSummary{
					sequence:      step.Sequence,
					stepType:      step.StepType,
					actorMemberID: step.ActorMemberPublicID,
					actorName:     step.ActorNameSnapshot,
					instruction:   agentGroupDecisionHeading(decision),
					outputSummary: agentGroupDecisionBriefText(decision),
				})
			} else {
				summaries = append(summaries, agentGroupContextSummary{
					sequence:      step.Sequence,
					stepType:      step.StepType,
					actorMemberID: step.ActorMemberPublicID,
					actorName:     step.ActorNameSnapshot,
					instruction:   "委派决策",
					outputSummary: compactSnippet(output, 240),
				})
			}
		}
	}
	return summaries, completed, nil
}

// agentGroupSnapshotMemberForStep 按步骤的 Actor 引用解析快照成员（主管优先匹配）。
func agentGroupSnapshotMemberForStep(snapshot *domainagentgroup.RunSnapshot, step *domainagentgroup.Step) *domainagentgroup.RunSnapshotMember {
	if snapshot == nil || step == nil || strings.TrimSpace(step.ActorMemberPublicID) == "" {
		return nil
	}
	if snapshot.Supervisor.PublicID == step.ActorMemberPublicID {
		return &snapshot.Supervisor
	}
	return agentGroupSnapshotMemberByID(snapshot, step.ActorMemberPublicID)
}

// createAgentGroupStepRetryAttempt 为被暂停步骤追加一次新尝试（Attempt N+1）。
// 与 createAgentGroupStepAndAttempt 的差异：不新建 Step，也不 CAS 更新 CurrentStepID
// （重试步骤仍是最新检查点），仅在 DB 中追加一次 Attempt 并发布重试事件。
func (st *agentGroupRunState) createAgentGroupStepRetryAttempt(
	ctx context.Context,
	step *domainagentgroup.Step,
	member *domainagentgroup.RunSnapshotMember,
	attemptNo int,
) (*domainagentgroup.Attempt, error) {
	now := time.Now()
	leaseExpiresAt := now.Add(st.attemptLease)
	attempt := &domainagentgroup.Attempt{
		PublicID:          normalizePublicID(uuid.NewString()),
		StepID:            step.ID,
		AttemptNo:         attemptNo,
		RetryRequestID:    normalizePublicID(uuid.NewString()),
		RequestedModel:    member.EffectiveModel,
		ResolvedModel:     member.EffectiveModel,
		InputSnapshotJSON: marshalAgentGroupAttemptInput(st.snapshot, step.StepType, step.Instruction),
		Status:            domainagentgroup.AttemptStatusRunning,
		BillingRef:        domainagentgroup.BillingRef(st.run.ID, step.ID, attemptNo),
		LeaseExpiresAt:    &leaseExpiresAt,
		StartedAt:         now,
	}
	if err := st.service.agentGroupRunStore.CreateAgentGroupStepAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	st.emitAgentGroupStepRetryStarted(ctx, step, attempt, member)
	return attempt, nil
}

// executeRetryableStep 执行被暂停步骤的新尝试（attempt N+1）。
// 主管步骤重试后按决策续跑：delegate → 成员步骤 → runSerial；finish → 完成运行。
// 成员步骤重试成功后回到 runSerial 继续后续步骤（后续步骤按检查点推进，不重复执行）。
func (st *agentGroupRunState) executeRetryableStep(
	ctx context.Context,
	step *domainagentgroup.Step,
	attempt *domainagentgroup.Attempt,
	member *domainagentgroup.RunSnapshotMember,
) error {
	s := st.service
	switch step.StepType {
	case domainagentgroup.StepTypeSupervisorDecide:
		// 主管步骤重试走与首次执行相同的决策路径（含自动纠错），保证重试不重复失败。
		decision, output, err := st.runSupervisorDecision(ctx, step, attempt, member)
		if err != nil {
			return st.failAgentGroupStepAndPause(ctx, step, attempt, member, output, err)
		}
		if decision.Action == agentGroupSupervisorActionFinish {
			st.finalAnswer = decision.Answer
			if strings.TrimSpace(st.finalAnswer) == "" {
				st.finalAnswer = strings.TrimSpace(output.Text)
			}
			if err := st.finishAgentGroupStepSuccess(ctx, step, attempt, member, output); err != nil {
				return err
			}
			return st.completeAgentGroupRun(ctx)
		}
		target := agentGroupSnapshotMemberByID(st.snapshot, decision.MemberID)
		if target == nil {
			return st.failAgentGroupStepAndPause(ctx, step, attempt, member, output, ErrAgentGroupInvalidMember)
		}
		if err := st.finishAgentGroupStepSuccess(ctx, step, attempt, member, output); err != nil {
			return err
		}
		st.recordSupervisorDecisionSummary(step, decision)
		return st.executeMemberStepAfterSupervisor(ctx, target, *decision)

	case domainagentgroup.StepTypeMemberExecute:
		// 成员步骤重试：从步骤指令重建最小决策（不重跑主管，主管步骤已成功）。
		decision := agentGroupSupervisorDecision{
			Action:      agentGroupSupervisorActionDelegate,
			MemberID:    step.ActorMemberPublicID,
			Instruction: step.Instruction,
		}
		output, err := s.ExecuteAgentTurn(ctx, st.agentTurnInput(
			step, attempt, member,
			agentGroupMemberSystemPrompt(st.snapshot, member),
			agentGroupMemberUserContent(st.input.Content, &decision, agentGroupContextBrief(st.summaries)),
			nil,
		))
		if err != nil {
			return st.failAgentGroupStepAndPause(ctx, step, attempt, member, output, err)
		}
		if err := st.finishAgentGroupStepSuccess(ctx, step, attempt, member, output); err != nil {
			return err
		}
		st.recordStepSummary(step, member, step.Instruction, output)
		return st.runSerial(ctx)

	default:
		return st.blockAgentGroupRun(ctx, step, domainagentgroup.ErrorCodeUpstreamFatal,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeUpstreamFatal))
	}
}

// executeMemberStepAfterSupervisor 主管重试 delegate 后的成员执行段
// （与 runSerial 的成员段完全等价：新步骤 + 新尝试 + 执行 + 摘要 + 回到主管）。
func (st *agentGroupRunState) executeMemberStepAfterSupervisor(
	ctx context.Context,
	member *domainagentgroup.RunSnapshotMember,
	decision agentGroupSupervisorDecision,
) error {
	s := st.service
	memberStep, memberAttempt, err := st.createAgentGroupStepAndAttempt(
		ctx, member, domainagentgroup.StepTypeMemberExecute, decision.Instruction)
	if err != nil {
		return err
	}
	st.emitAgentGroupStepStarted(ctx, memberStep, memberAttempt, member)
	memberOutput, err := s.ExecuteAgentTurn(ctx, st.agentTurnInput(
		memberStep, memberAttempt, member,
		agentGroupMemberSystemPrompt(st.snapshot, member),
		agentGroupMemberUserContent(st.input.Content, &decision, agentGroupContextBrief(st.summaries)),
		nil,
	))
	if err != nil {
		return st.failAgentGroupStepAndPause(ctx, memberStep, memberAttempt, member, memberOutput, err)
	}
	if err := st.finishAgentGroupStepSuccess(ctx, memberStep, memberAttempt, member, memberOutput); err != nil {
		return err
	}
	st.recordStepSummary(memberStep, member, decision.Instruction, memberOutput)
	return st.runSerial(ctx)
}

// blockPausedRetryableRun 从 paused_retryable 无条件阻塞运行
// （Attempt 上限场景；blockAgentGroupRun 仅支持 running 状态的 CAS）。
func (st *agentGroupRunState) blockPausedRetryableRun(ctx context.Context, step *domainagentgroup.Step, code string, message string) error {
	store := st.service.agentGroupRunStore
	now := time.Now()
	// 与 failAgentGroupStepAndPause 一致：断连场景用独立上下文保证终态落库。
	persistCtx, cancelPersist := finalizePersistContext(ctx)
	defer cancelPersist()
	blocked := domainagentgroup.RunStatusBlocked
	runPatch := domainagentgroup.RunPatch{Status: &blocked, ErrorCode: &code, ErrorMessage: &message, EndedAt: &now}
	if st.assistantMessage != nil {
		runPatch.AssistantMessageID = &st.assistantMessage.ID
	}
	ok, err := store.CASUpdateAgentGroupRun(persistCtx, st.run.ID, st.stateVersion, domainagentgroup.RunStatusPausedRetryable, runPatch)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAgentGroupCASConflict
	}
	st.stateVersion++
	st.run.Status = blocked
	st.run.ErrorCode = code
	st.run.ErrorMessage = message
	st.emitAgentGroupRunPaused(ctx, step, blocked, code)
	st.markGroupAssistantMessageState(ctx, code)
	return ErrAgentGroupRunBlocked
}

// updateTopLevelRun 推进顶层 conversation_runs 行（重试/放弃路径）。
// 首次运行已用 CreateConversationRun 写入该行，这里按 RunID UPDATE（绝不重复创建）。
// 状态映射与 persistTopLevelRun 一致：nil → success；canceled → canceled；interrupted → interrupted；其余 → error。
func (st *agentGroupRunState) updateTopLevelRun(ctx context.Context, retErr error) {
	endedAt := time.Now()
	status := "success"
	errorCode := ""
	errorMessage := ""
	switch {
	case retErr == nil:
		// success：清除首次运行写入的暂停错误信息。
	case errors.Is(retErr, ErrMessageGenerationCanceled):
		status = "canceled"
		errorCode = classifyRunErrorCode(retErr)
		errorMessage = truncateError(retErr.Error(), 255)
	case st.assistantMessage != nil && st.assistantMessage.Status == "interrupted":
		status = "interrupted"
		errorCode = classifyRunErrorCode(retErr)
		errorMessage = truncateError(retErr.Error(), 255)
	default:
		status = "error"
		errorCode = classifyRunErrorCode(retErr)
		errorMessage = truncateError(retErr.Error(), 255)
	}
	patch := repository.ConversationRunPatch{
		Status:           &status,
		ErrorCode:        &errorCode,
		ErrorMessage:     &errorMessage,
		EndedAt:          &endedAt,
		InputTokens:      &st.totalInputTokens,
		OutputTokens:     &st.totalOutputTokens,
		CacheReadTokens:  &st.totalCacheReadTokens,
		CacheWriteTokens: &st.totalCacheWriteTokens,
		ReasoningTokens:  &st.totalReasoningTokens,
		ToolCallsCount:   &st.totalToolCalls,
	}
	// 断连场景 ctx 已取消：审计行用独立上下文落库（失败仅记日志，不影响运行终态）。
	persistCtx, cancelPersist := finalizePersistContext(ctx)
	defer cancelPersist()
	if _, err := st.service.repo.UpdateConversationRun(persistCtx, st.input.UserID, st.input.ConversationID, st.runID, patch); err != nil {
		st.service.logger.Error("update_conversation_run_failed",
			zap.String("trace_id", traceid.FromContext(ctx)),
			zap.String("run_id", st.runID),
			zap.Error(err),
		)
	}
}

// CancelAgentGroupRun 取消运行中的群组运行（仅 running 可取消）。
// 取消经由 generation stream 传播：编排器 ctx 被取消 → 当前 Attempt 中断 → 运行回到 paused_retryable。
func (s *Service) CancelAgentGroupRun(ctx context.Context, userID uint, runPublicID string) (bool, error) {
	if s.agentGroupRunStore == nil {
		return false, ErrAgentGroupFeatureDisabled
	}
	run, err := s.agentGroupRunStore.GetAgentGroupRunByPublicID(ctx, userID, runPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, ErrAgentGroupRunNotFound
		}
		return false, err
	}
	if run.Status != domainagentgroup.RunStatusRunning {
		return false, ErrAgentGroupRunNotCancelable
	}
	return s.CancelMessageGeneration(ctx, userID, run.ClientRunID), nil
}

// AbandonAgentGroupRun 放弃暂停/阻塞的群组运行（仅 paused_retryable / blocked 可放弃）。
// 放弃是不可逆操作：CAS → abandoned + EndedAt，assistant 消息标记 canceled(agent_group_abandoned)，
// 顶层运行行推进为 canceled，发布 group_run_abandoned 事件。
func (s *Service) AbandonAgentGroupRun(ctx context.Context, userID uint, runPublicID string) (*SendMessageResult, error) {
	if s.agentGroupRunStore == nil || s.agentGroupRepo == nil {
		return nil, ErrAgentGroupFeatureDisabled
	}
	run, err := s.agentGroupRunStore.GetAgentGroupRunByPublicID(ctx, userID, runPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAgentGroupRunNotFound
		}
		return nil, err
	}
	runLock := s.agentGroupRunMutex(run.ConversationID)
	runLock.Lock()
	defer runLock.Unlock()

	if run.Status != domainagentgroup.RunStatusPausedRetryable && run.Status != domainagentgroup.RunStatusBlocked {
		return nil, ErrAgentGroupRunNotAbandonable
	}

	// 放弃不需要完整状态重建，仅需消息身份用于标记与事件转发。
	st := &agentGroupRunState{
		service:   s,
		input:     SendMessageInput{UserID: run.UserID, ConversationID: run.ConversationID},
		run:       run,
		runID:     run.ClientRunID,
		startedAt: run.StartedAt,
	}
	if conversation, convErr := s.repo.GetConversationByUser(ctx, run.ConversationID, run.UserID); convErr == nil && conversation != nil {
		if userMessage, msgErr := s.repo.GetMessageByID(ctx, run.ConversationID, run.UserMessageID); msgErr == nil && userMessage != nil {
			st.userMessage = userMessage
		}
		if assistantMessage, msgErr := s.loadAgentGroupAssistantMessage(ctx, run, conversation); msgErr == nil {
			st.assistantMessage = assistantMessage
		}
	}

	now := time.Now()
	abandoned := domainagentgroup.RunStatusAbandoned
	code := domainagentgroup.ErrorCodeCanceled
	message := "agent group run abandoned"
	ok, err := s.agentGroupRunStore.CASUpdateAgentGroupRun(ctx, run.ID, run.StateVersion, run.Status,
		domainagentgroup.RunPatch{Status: &abandoned, ErrorCode: &code, ErrorMessage: &message, EndedAt: &now})
	if err != nil {
		return st.failedResult(), err
	}
	if !ok {
		return st.failedResult(), ErrAgentGroupCASConflict
	}
	st.stateVersion = run.StateVersion + 1
	st.run.Status = abandoned
	st.run.ErrorCode = code
	st.run.ErrorMessage = message

	st.markAgentGroupAbandonedMessages(ctx)
	st.emitAgentGroupRunAbandoned(ctx)
	st.updateTopLevelRun(ctx, ErrMessageGenerationCanceled)
	return st.failedResult(), nil
}

// markAgentGroupAbandonedMessages 标记放弃结局下的消息状态：
// assistant canceled(agent_group_abandoned)；用户消息 success（已接受，结局由 run 持久化持有）。
func (st *agentGroupRunState) markAgentGroupAbandonedMessages(ctx context.Context) {
	persistCtx := ctx
	var cancel context.CancelFunc
	if persistCtx == nil || persistCtx.Err() != nil {
		persistCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}
	if st.assistantMessage != nil {
		message := "agent group run abandoned"
		if err := st.service.repo.UpdateMessageState(persistCtx, st.assistantMessage.ID, "canceled", "agent_group_abandoned", message); err != nil {
			st.service.logger.Error("update_agent_group_abandoned_message_state_failed",
				zap.String("trace_id", traceid.FromContext(ctx)),
				zap.Uint("message_id", st.assistantMessage.ID),
				zap.Error(err),
			)
		}
		st.assistantMessage.Status = "canceled"
		st.assistantMessage.ErrorCode = "agent_group_abandoned"
		st.assistantMessage.ErrorMessage = message
	}
	if st.userMessage != nil && st.userMessage.Status != "success" {
		if err := st.service.repo.UpdateMessageState(persistCtx, st.userMessage.ID, "success", "", ""); err != nil {
			st.service.logger.Error("update_agent_group_abandoned_user_message_state_failed",
				zap.String("trace_id", traceid.FromContext(ctx)),
				zap.Uint("message_id", st.userMessage.ID),
				zap.Error(err),
			)
		}
		st.userMessage.Status = "success"
	}
}

// agentGroupLeaseRecoveryInterval 是 Agent 群组 Attempt 租约恢复扫描周期。
// 服务重启（或崩溃）后，running 状态的 Attempt 在租约过期时被回收为
// interrupted，对应运行回到 paused_retryable，可从被中断的步骤继续。
const agentGroupLeaseRecoveryInterval = time.Minute

// startAgentGroupLeaseRecoveryWorker 启动租约恢复后台 worker。
// 仅做 DB 回收，不标记任何消息状态（消息标记由暂停/重试路径负责）。
func (s *Service) startAgentGroupLeaseRecoveryWorker(ctx context.Context) {
	if s == nil || s.agentGroupRunStore == nil {
		return
	}
	go func() {
		s.recoverExpiredAgentGroupLeases(ctx)
		ticker := time.NewTicker(agentGroupLeaseRecoveryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.recoverExpiredAgentGroupLeases(ctx)
			}
		}
	}()
}

// recoverExpiredAgentGroupLeases 将租约过期的 running Attempt 转为 interrupted，
// 并把对应运行转为 paused_retryable（返回受影响运行数）。
func (s *Service) recoverExpiredAgentGroupLeases(ctx context.Context) {
	recoverCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	recovered, err := s.agentGroupRunStore.RecoverExpiredAttemptLeases(recoverCtx, time.Now())
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("agent_group_lease_recovery_failed",
				zap.String("trace_id", traceid.FromContext(ctx)),
				zap.Error(err),
			)
		}
		return
	}
	if recovered > 0 && s.logger != nil {
		s.logger.Info("agent_group_lease_recovery_recovered",
			zap.String("trace_id", traceid.FromContext(ctx)),
			zap.Int64("runs_recovered", recovered),
		)
	}
}
