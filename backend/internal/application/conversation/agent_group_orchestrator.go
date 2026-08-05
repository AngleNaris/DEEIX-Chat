package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/traceid"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// agentGroupRunState 承载一次 Agent 群组运行的串行编排状态。
// 运行期间只在内存跟踪 stateVersion 与 stepSequence（新建运行从 1 开始，
// T7 恢复路径改为从 DB 加载检查点）。
type agentGroupRunState struct {
	service               *Service
	input                 SendMessageInput
	preferStream          bool
	conversation          *model.Conversation
	snapshot              *domainagentgroup.RunSnapshot
	run                   *domainagentgroup.Run
	branchPreparation     *messageSendBranchPreparation
	contextMessages       []model.Message
	runID                 string
	startedAt             time.Time
	userMessage           *model.Message
	assistantMessage      *model.Message
	stateVersion          int
	stepSequence          int
	completedStepCount    int
	finalAnswer           string
	summaries             []agentGroupContextSummary
	totalInputTokens      int64
	totalCacheReadTokens  int64
	totalCacheWriteTokens int64
	totalOutputTokens     int64
	totalReasoningTokens  int64
	totalToolCalls        int
	attemptLease          time.Duration
	// ledger 是跨 Attempt 的工具执行幂等账本；persistToolCalls 为 true 时
	// 工具行以 BillingRef 为 RunID 落盘（重试时按前缀恢复种子）。
	ledger           *toolExecutionLedger
	persistToolCalls bool
}

// executeAgentGroupRun 执行一次 Agent 群组串行运行（主管决策 → 成员执行 → 循环）。
// 与普通消息路径共享分支准备、消息对创建、上下文组装与顶层 run 日志，
// 但模型执行全部经内部 Actor 回合（ExecuteAgentTurn）逐 Attempt 独立计费，
// 因此顶层 SendMessageResult.Billable 恒为 false（调用方释放用量授权即可）。
func (s *Service) executeAgentGroupRun(
	ctx context.Context,
	input SendMessageInput,
	onDelta func(string) error,
	preferStream bool,
	conversation *model.Conversation,
	runID string,
	startedAt time.Time,
) (*SendMessageResult, error) {
	if !s.agentGroupFeatureEnabled(ctx) || s.agentGroupRunStore == nil || s.agentGroupRepo == nil {
		return nil, ErrAgentGroupFeatureDisabled
	}

	// 同会话串行：同一会话内严格排队，不同会话可并发执行。
	runLock := s.agentGroupRunMutex(input.ConversationID)
	runLock.Lock()
	defer runLock.Unlock()

	// 活跃运行守卫（含 blocked：阻塞会话直到 T7 放弃接口）。
	active, err := s.agentGroupRunStore.GetActiveAgentGroupRunByConversation(ctx, input.ConversationID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if active != nil {
		return nil, ErrAgentGroupRunInProgress
	}

	// 同一 ClientRunID 重复提交守卫（客户端重试同一流式 run）。
	existing, err := s.agentGroupRunStore.GetAgentGroupRunByClientRunID(ctx, input.ConversationID, runID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, ErrDuplicateMessageGenerationRun
	}

	// 冻结配置快照（成员覆盖 > 角色默认 > 平台默认；不保存凭证）。
	snapshot, err := s.buildAgentGroupRunSnapshot(ctx, input, conversation)
	if err != nil {
		return nil, err
	}

	// 分支准备 + 消息对创建（与普通路径共享）。
	branchPreparation, err := s.prepareMessageSendBranch(ctx, &input)
	if err != nil {
		return nil, err
	}
	resolvedAttachments, err := s.resolveAttachments(ctx, input.UserID, input.FileIDs)
	if err != nil {
		return nil, err
	}
	pair, err := s.createMessagePair(ctx, input, runID, branchPreparation, resolvedAttachments, nil)
	if err != nil {
		return nil, err
	}
	s.persistInitialConversationFallbackTitle(ctx, *conversation, *pair.user)

	// 上下文消息一次组装，整个运行复用（token 预算由执行器内部应用）。
	contextMessages := buildBranchMessagePath(branchPreparation.branchState, pair.user)
	cfg := s.cfg.Snapshot()
	compactPolicy := s.resolveContextCompactionPolicy(ctx, cfg, input.UserID)
	if compactPolicy.EffectiveEnabled() {
		if snapshotContext, err := s.getCachedSnapshot(ctx, input.ConversationID); err == nil && snapshotContext != nil {
			contextMessages = s.expandContextMessagesToSnapshotBoundary(
				ctx, input.ConversationID, pair.user.ID, contextMessages, snapshotContext, compactPolicy)
		}
	}
	contextMessages = recoverAssistantRetryUserStates(contextMessages)

	// 创建运行（pending，StateVersion=1）。
	run := &domainagentgroup.Run{
		PublicID:           normalizePublicID(uuid.NewString()),
		ClientRunID:        runID,
		UserID:             input.UserID,
		ConversationID:     input.ConversationID,
		GroupID:            snapshot.Group.GroupID,
		GroupPublicID:      snapshot.Group.PublicID,
		UserMessageID:      pair.user.ID,
		GroupRevision:      snapshot.Group.Revision,
		ConfigSnapshotJSON: marshalAgentGroupRunSnapshot(snapshot),
		Status:             domainagentgroup.RunStatusPending,
		StateVersion:       1,
		StartedAt:          startedAt,
	}
	if err := s.agentGroupRunStore.CreateAgentGroupRun(ctx, run); err != nil {
		return nil, err
	}
	s.RecordAudit(ctx, AuditInput{
		UserID:     input.UserID,
		RequestID:  input.RequestID,
		Action:     "create_agent_group_run",
		Resource:   "agent_group_run",
		ResourceID: run.PublicID,
		Detail: map[string]interface{}{
			"conversation_id":  input.ConversationID,
			"group_public_id":  run.GroupPublicID,
			"group_revision":   run.GroupRevision,
			"client_run_id":    runID,
			"status":           run.Status,
		},
	})

	st := &agentGroupRunState{
		service:           s,
		input:             input,
		preferStream:      preferStream,
		conversation:      conversation,
		snapshot:          snapshot,
		run:               run,
		branchPreparation: branchPreparation,
		contextMessages:   contextMessages,
		runID:             runID,
		startedAt:         startedAt,
		userMessage:       pair.user,
		assistantMessage:  pair.assistant,
		stateVersion:      1,
		attemptLease:      s.agentGroupAttemptLease(ctx),
		ledger:            newToolExecutionLedger(),
		persistToolCalls:  true,
	}

	// Cancelable 时注册取消。
	if input.Cancelable {
		cancelCtx, cancel := context.WithCancel(ctx)
		ctx = cancelCtx
		s.generationStreams.register(ctx, runID, input.UserID, cancel)
	}

	// CAS pending → running。
	running := domainagentgroup.RunStatusRunning
	ok, err := s.agentGroupRunStore.CASUpdateAgentGroupRun(ctx, run.ID, 1, domainagentgroup.RunStatusPending, domainagentgroup.RunPatch{Status: &running})
	if err != nil {
		return st.failedResult(), err
	}
	if !ok {
		_ = st.blockAgentGroupRun(ctx, nil, domainagentgroup.ErrorCodeCASConflict, genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeCASConflict))
		return st.failedResult(), ErrAgentGroupRunBlocked
	}
	st.stateVersion = 2

	err = st.runSerial(ctx)
	st.persistTopLevelRun(ctx, err)
	if err == nil {
		if onDelta != nil && strings.TrimSpace(st.finalAnswer) != "" {
			_ = onDelta(st.finalAnswer)
		}
		return st.completedResult(), nil
	}
	// 运行未进入终态时兜底阻塞（CAS 冲突/内部错误）。
	if st.run.Status == domainagentgroup.RunStatusRunning || st.run.Status == domainagentgroup.RunStatusPending {
		_ = st.blockAgentGroupRun(ctx, nil, domainagentgroup.ErrorCodeCASConflict, genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeCASConflict))
	}
	return st.failedResult(), err
}

// runSerial 是严格的串行状态机主循环：主管决策 → 成员执行 → 循环，直到 finish。
// 每一步完成（含失败）后才创建下一步；成员步骤成功后才回到主管（自动继续）。
func (st *agentGroupRunState) runSerial(ctx context.Context) error {
	s := st.service
	for {
		// 步骤上限。
		if st.stepSequence >= st.snapshot.Limits.MaxStepsPerRun {
			return st.blockAgentGroupRun(ctx, nil, domainagentgroup.ErrorCodeStepLimitExceeded,
				genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeStepLimitExceeded))
		}

		// 主管决策步骤。
		supervisorStep, supervisorAttempt, err := st.createAgentGroupStepAndAttempt(
			ctx, &st.snapshot.Supervisor, domainagentgroup.StepTypeSupervisorDecide, "")
		if err != nil {
			return err
		}
		st.emitAgentGroupStepStarted(ctx, supervisorStep, supervisorAttempt, &st.snapshot.Supervisor)
		decision, supervisorOutput, err := st.runSupervisorDecision(ctx, supervisorStep, supervisorAttempt, &st.snapshot.Supervisor)
		if err != nil {
			return st.failAgentGroupStepAndPause(ctx, supervisorStep, supervisorAttempt, &st.snapshot.Supervisor, err)
		}

		if decision.Action == agentGroupSupervisorActionFinish {
			st.finalAnswer = decision.Answer
			if strings.TrimSpace(st.finalAnswer) == "" {
				st.finalAnswer = strings.TrimSpace(supervisorOutput.Text)
			}
			if err := st.finishAgentGroupStepSuccess(ctx, supervisorStep, supervisorAttempt, &st.snapshot.Supervisor, supervisorOutput); err != nil {
				return err
			}
			return st.completeAgentGroupRun(ctx)
		}

		// delegate：runSupervisorDecision 已校验成员（10.2），这里仅兜底解析。
		member := agentGroupSnapshotMemberByID(st.snapshot, decision.MemberID)
		if member == nil {
			return st.failAgentGroupStepAndPause(ctx, supervisorStep, supervisorAttempt, &st.snapshot.Supervisor, ErrAgentGroupInvalidMember)
		}
		if err := st.finishAgentGroupStepSuccess(ctx, supervisorStep, supervisorAttempt, &st.snapshot.Supervisor, supervisorOutput); err != nil {
			return err
		}

		// 成员执行步骤。
		memberStep, memberAttempt, err := st.createAgentGroupStepAndAttempt(
			ctx, member, domainagentgroup.StepTypeMemberExecute, decision.Instruction)
		if err != nil {
			return err
		}
		st.emitAgentGroupStepStarted(ctx, memberStep, memberAttempt, member)
		memberOutput, err := s.ExecuteAgentTurn(ctx, st.agentTurnInput(
			memberStep, memberAttempt, member,
			agentGroupMemberSystemPrompt(st.snapshot, member),
			agentGroupMemberUserContent(st.input.Content, decision, agentGroupContextBrief(st.summaries)),
			nil,
		))
		if err != nil {
			return st.failAgentGroupStepAndPause(ctx, memberStep, memberAttempt, member, err)
		}
		if err := st.finishAgentGroupStepSuccess(ctx, memberStep, memberAttempt, member, memberOutput); err != nil {
			return err
		}
		st.recordStepSummary(memberStep, member, decision.Instruction, memberOutput)
	}
}

// runSupervisorDecision 执行一次主管决策回合并解析结构化输出，带有限自动纠错：
// 决策无法解析或 delegate 校验失败时，把具体错误与有效成员清单回喂主管重新决策，
// 至多 agentGroupSupervisorMaxCorrections 轮；纠错耗尽仍无效才返回错误
// （解析失败 → ErrAgentGroupInvalidDecision；校验失败 → ErrAgentGroupInvalidMember）。
// runSerial 与 executeRetryableStep（重试路径）共用，保证重试同样受益于自动纠错。
// 中间纠错回合的用量一并累计，保证计费准确；最终输出由 finishAgentGroupStepSuccess 累计。
func (st *agentGroupRunState) runSupervisorDecision(
	ctx context.Context,
	step *domainagentgroup.Step,
	attempt *domainagentgroup.Attempt,
	member *domainagentgroup.RunSnapshotMember,
) (*agentGroupSupervisorDecision, *AgentTurnOutput, error) {
	s := st.service
	baseUser := agentGroupSupervisorUserContent(st.input.Content, st.snapshot.Members, agentGroupContextBrief(st.summaries))
	output, err := s.ExecuteAgentTurn(ctx, st.agentTurnInput(
		step, attempt, member,
		agentGroupSupervisorSystemPrompt(st.snapshot),
		baseUser,
		agentGroupSupervisorOptions(nil),
	))
	if err != nil {
		return nil, nil, err
	}

	for round := 0; round <= agentGroupSupervisorMaxCorrections; round++ {
		decision, decisionErr := resolveAgentGroupSupervisorDecision(output.Text)
		if decisionErr == nil && decision.Action == agentGroupSupervisorActionDelegate {
			decisionErr = validateAgentGroupDelegation(st.snapshot, decision)
		}
		if decisionErr == nil {
			return decision, output, nil
		}
		if round >= agentGroupSupervisorMaxCorrections {
			// 纠错耗尽：归一化最终错误（解析失败 → InvalidDecision；校验失败 → InvalidMember）。
			if errors.Is(decisionErr, ErrAgentGroupInvalidDecision) {
				return nil, output, ErrAgentGroupInvalidDecision
			}
			return nil, output, ErrAgentGroupInvalidMember
		}
		// 回喂纠错提示重新决策；中间回合用量累计进总账。
		st.accumulateUsage(output)
		output, err = s.ExecuteAgentTurn(ctx, st.agentTurnInput(
			step, attempt, member,
			agentGroupSupervisorSystemPrompt(st.snapshot),
			baseUser+"\n\n"+agentGroupSupervisorCorrectionHint(decisionErr, st.snapshot.Members),
			agentGroupSupervisorOptions(nil),
		))
		if err != nil {
			return nil, nil, err
		}
	}
	// 不可达（循环内已返回）。
	return nil, output, ErrAgentGroupInvalidDecision
}

// agentTurnInput 组装一次内部 Actor 回合输入。
// ClientRunID 采用 groupRunID:stepID:attemptNo，保证计费幂等与审计可追溯。
func (st *agentGroupRunState) agentTurnInput(
	step *domainagentgroup.Step,
	attempt *domainagentgroup.Attempt,
	member *domainagentgroup.RunSnapshotMember,
	systemPrompt string,
	userContent string,
	options map[string]interface{},
) AgentTurnInput {
	return AgentTurnInput{
		UserID:            st.input.UserID,
		ConversationID:    st.input.ConversationID,
		RequestID:         strings.TrimSpace(st.input.RequestID),
		ClientRunID:       domainagentgroup.BillingRef(st.run.ID, step.ID, attempt.AttemptNo),
		ActorID:           member.PublicID,
		ActorName:         member.RoleName,
		ActorType:         member.MemberType,
		ActorIcon:         member.Icon,
		ActorColor:        member.Color,
		PlatformModelName: member.EffectiveModel,
		SystemPrompt:      systemPrompt,
		UserContent:       userContent,
		DomainMessages:    st.contextMessages,
		FileIDs:           st.input.FileIDs,
		SkillIDs:          st.input.SkillIDs,
		SelectedToolIDs:   st.input.SelectedToolIDs,
		Options:           options,
		Stream:            st.preferStream,
		OnEvent:           st.forwardAgentGroupTurnEvent(step, attempt, member),
		Ledger:            st.ledger,
		PersistToolCalls:  st.persistToolCalls,
	}
}

// createAgentGroupStepAndAttempt 按 11.2 持久化顺序创建步骤与首次尝试：
// 未结束步骤守卫 → 步骤上限 → 创建 Step → 创建 Attempt(running) → CAS 更新运行检查点。
func (st *agentGroupRunState) createAgentGroupStepAndAttempt(
	ctx context.Context,
	member *domainagentgroup.RunSnapshotMember,
	stepType string,
	instruction string,
) (*domainagentgroup.Step, *domainagentgroup.Attempt, error) {
	store := st.service.agentGroupRunStore

	// 未结束步骤守卫（同会话串行下通常为 0；防止恢复路径重复创建）。
	unfinished, err := store.CountUnfinishedStepsByRun(ctx, st.run.ID)
	if err != nil {
		return nil, nil, err
	}
	if unfinished > 0 {
		return nil, nil, st.blockAgentGroupRun(ctx, nil, domainagentgroup.ErrorCodeCASConflict,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeCASConflict))
	}

	// 步骤上限（DB 侧统计为准，与内存计数双保险）。
	stepCount, err := store.CountStepsByRun(ctx, st.run.ID)
	if err != nil {
		return nil, nil, err
	}
	if stepCount >= int64(st.snapshot.Limits.MaxStepsPerRun) {
		return nil, nil, st.blockAgentGroupRun(ctx, nil, domainagentgroup.ErrorCodeStepLimitExceeded,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeStepLimitExceeded))
	}

	// 创建逻辑步骤（running：立即进入执行）。
	st.stepSequence++
	step := &domainagentgroup.Step{
		PublicID:            normalizePublicID(uuid.NewString()),
		GroupRunID:          st.run.ID,
		Sequence:            st.stepSequence,
		StepType:            stepType,
		ActorMemberPublicID: member.PublicID,
		ActorNameSnapshot:   member.RoleName,
		ActorTypeSnapshot:   member.MemberType,
		Instruction:         instruction,
		Status:              domainagentgroup.StepStatusRunning,
	}
	if err := store.CreateAgentGroupStep(ctx, step); err != nil {
		return nil, nil, err
	}

	// 创建首次尝试（running）。
	now := time.Now()
	leaseExpiresAt := now.Add(st.attemptLease)
	attempt := &domainagentgroup.Attempt{
		PublicID:          normalizePublicID(uuid.NewString()),
		StepID:            step.ID,
		AttemptNo:         1,
		RetryRequestID:    normalizePublicID(uuid.NewString()),
		RequestedModel:    member.EffectiveModel,
		ResolvedModel:     member.EffectiveModel,
		InputSnapshotJSON: marshalAgentGroupAttemptInput(st.snapshot, stepType, instruction),
		Status:            domainagentgroup.AttemptStatusRunning,
		BillingRef:        domainagentgroup.BillingRef(st.run.ID, step.ID, 1),
		LeaseExpiresAt:    &leaseExpiresAt,
		StartedAt:         now,
	}
	if err := store.CreateAgentGroupStepAttempt(ctx, attempt); err != nil {
		return nil, nil, err
	}

	// CAS 更新运行检查点（CurrentStepID + StateVersion）。
	ok, err := store.CASUpdateAgentGroupRun(ctx, st.run.ID, st.stateVersion, domainagentgroup.RunStatusRunning,
		domainagentgroup.RunPatch{CurrentStepID: &step.ID})
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, st.blockAgentGroupRun(ctx, step, domainagentgroup.ErrorCodeCASConflict,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeCASConflict))
	}
	st.stateVersion++
	return step, attempt, nil
}

// finishAgentGroupStepSuccess 最终化一步成功（成功步骤不可变、不重复计费）：
// CAS attempt success → step success + successful_attempt_id → CAS 运行检查点 → 事件。
func (st *agentGroupRunState) finishAgentGroupStepSuccess(
	ctx context.Context,
	step *domainagentgroup.Step,
	attempt *domainagentgroup.Attempt,
	member *domainagentgroup.RunSnapshotMember,
	output *AgentTurnOutput,
) error {
	store := st.service.agentGroupRunStore
	now := time.Now()

	// 1. CAS attempt → success。
	success := domainagentgroup.AttemptStatusSuccess
	patch := domainagentgroup.AttemptPatch{Status: &success, OutputMarkdown: &output.Text, EndedAt: &now}
	if output != nil && strings.TrimSpace(output.PlatformModelName) != "" {
		resolvedModel := output.PlatformModelName
		patch.ResolvedModel = &resolvedModel
	}
	ok, err := store.CASUpdateAgentGroupStepAttempt(ctx, attempt.ID, domainagentgroup.AttemptStatusRunning, patch)
	if err != nil {
		return err
	}
	if !ok {
		return st.blockAgentGroupRun(ctx, step, domainagentgroup.ErrorCodeCASConflict,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeCASConflict))
	}

	// 2. 步骤 success + successful_attempt_id。
	stepSuccess := domainagentgroup.StepStatusSuccess
	if err := store.UpdateAgentGroupStep(ctx, step.ID, map[string]interface{}{
		"status": stepSuccess, "successful_attempt_id": attempt.ID,
	}); err != nil {
		return err
	}
	step.Status = stepSuccess
	step.SuccessfulAttemptID = &attempt.ID

	// 3. CAS 运行检查点（LastCompletedStepID + StateVersion）。
	ok, err = store.CASUpdateAgentGroupRun(ctx, st.run.ID, st.stateVersion, domainagentgroup.RunStatusRunning,
		domainagentgroup.RunPatch{LastCompletedStepID: &step.ID})
	if err != nil {
		return err
	}
	if !ok {
		return st.blockAgentGroupRun(ctx, step, domainagentgroup.ErrorCodeCASConflict,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeCASConflict))
	}
	st.stateVersion++
	st.completedStepCount++
	st.accumulateUsage(output)

	// 4. 完成事件（携带完整输出）。
	st.emitAgentGroupStepCompleted(ctx, step, attempt, member, output)
	return nil
}

// failAgentGroupStepAndPause 最终化一步失败并暂停/阻塞运行：
// CAS attempt 终态 → step 终态 → CAS run paused_retryable/blocked → 事件 → 消息状态。
func (st *agentGroupRunState) failAgentGroupStepAndPause(
	ctx context.Context,
	step *domainagentgroup.Step,
	attempt *domainagentgroup.Attempt,
	member *domainagentgroup.RunSnapshotMember,
	execErr error,
) error {
	store := st.service.agentGroupRunStore
	code, retryable := classifyAgentGroupError(execErr)
	message := genericAgentGroupErrorMessage(code)
	now := time.Now()
	// 用户停止/断连时 ctx 已被取消：终态 CAS 必须落到独立上下文，否则运行卡在 running。
	persistCtx, cancelPersist := finalizePersistContext(ctx)
	defer cancelPersist()

	// 1. CAS attempt → 终态。
	attemptStatus := domainagentgroup.AttemptStatusError
	switch code {
	case domainagentgroup.ErrorCodeCanceled:
		attemptStatus = domainagentgroup.AttemptStatusCanceled
	case domainagentgroup.ErrorCodeInterrupted:
		attemptStatus = domainagentgroup.AttemptStatusInterrupted
	}
	ok, err := store.CASUpdateAgentGroupStepAttempt(persistCtx, attempt.ID, domainagentgroup.AttemptStatusRunning,
		domainagentgroup.AttemptPatch{Status: &attemptStatus, ErrorCode: &code, ErrorMessage: &message, EndedAt: &now})
	if err != nil {
		return err
	}
	if !ok {
		return st.blockAgentGroupRun(persistCtx, step, domainagentgroup.ErrorCodeCASConflict,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeCASConflict))
	}

	// 2. 步骤终态。
	stepStatus := domainagentgroup.StepStatusFailed
	switch code {
	case domainagentgroup.ErrorCodeCanceled:
		stepStatus = domainagentgroup.StepStatusCanceled
	case domainagentgroup.ErrorCodeInterrupted:
		stepStatus = domainagentgroup.StepStatusInterrupted
	}
	if err := store.UpdateAgentGroupStep(persistCtx, step.ID, map[string]interface{}{"status": stepStatus}); err != nil {
		return err
	}
	step.Status = stepStatus

	// 3. CAS 运行 → paused_retryable / blocked（可重试步骤原地重试，成功步骤不可变）。
	runStatus := domainagentgroup.RunStatusBlocked
	if retryable {
		runStatus = domainagentgroup.RunStatusPausedRetryable
	}
	runPatch := domainagentgroup.RunPatch{
		Status: &runStatus, ErrorCode: &code, ErrorMessage: &message, EndedAt: &now,
	}
	if retryable {
		runPatch.RetryableStepID = &step.ID
	}
	// 持久化 assistant 消息身份，供重试/放弃时恢复顶层消息状态。
	if st.assistantMessage != nil {
		runPatch.AssistantMessageID = &st.assistantMessage.ID
	}
	ok, err = store.CASUpdateAgentGroupRun(persistCtx, st.run.ID, st.stateVersion, domainagentgroup.RunStatusRunning, runPatch)
	if err != nil {
		return err
	}
	if !ok {
		return st.blockAgentGroupRun(persistCtx, step, domainagentgroup.ErrorCodeCASConflict,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeCASConflict))
	}
	st.stateVersion++
	st.run.Status = runStatus
	st.run.ErrorCode = code
	st.run.ErrorMessage = message

	// 4. 事件（只携带通用错误码，不暴露上游诊断）。
	st.emitAgentGroupStepFailed(ctx, step, attempt, member, code, message)
	st.emitAgentGroupRunPaused(ctx, step, runStatus, code)

	// 5. 消息状态。
	st.markGroupAssistantMessageState(ctx, code)

	// 6. 返回语义错误。
	switch {
	case code == domainagentgroup.ErrorCodeCanceled:
		return ErrMessageGenerationCanceled
	case retryable:
		return ErrAgentGroupRunPaused
	default:
		return ErrAgentGroupRunBlocked
	}
}

// blockAgentGroupRun 无条件将运行置为 blocked（CAS 冲突/步骤上限/致命错误）。
func (st *agentGroupRunState) blockAgentGroupRun(ctx context.Context, step *domainagentgroup.Step, code string, message string) error {
	store := st.service.agentGroupRunStore
	now := time.Now()
	// 与 failAgentGroupStepAndPause 一致：断连场景用独立上下文保证终态落库。
	persistCtx, cancelPersist := finalizePersistContext(ctx)
	defer cancelPersist()
	blocked := domainagentgroup.RunStatusBlocked
	runPatch := domainagentgroup.RunPatch{Status: &blocked, ErrorCode: &code, ErrorMessage: &message, EndedAt: &now}
	// 持久化 assistant 消息身份，供放弃时恢复顶层消息状态。
	if st.assistantMessage != nil {
		runPatch.AssistantMessageID = &st.assistantMessage.ID
	}
	ok, err := store.CASUpdateAgentGroupRun(persistCtx, st.run.ID, st.stateVersion, domainagentgroup.RunStatusRunning, runPatch)
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

// completeAgentGroupRun 完成运行：顶层消息持久化（聚合用量）→ 用户消息 success →
// CAS run completed + assistant_message_id → 完成事件。
func (st *agentGroupRunState) completeAgentGroupRun(ctx context.Context) error {
	s := st.service
	latency := time.Since(st.startedAt).Milliseconds()
	if latency < 0 {
		latency = 0
	}

	// 1. 顶层消息持久化（聚合全部 Attempt 用量；不设置用户消息状态，由本函数标记）。
	if err := s.persistSuccessfulMessageGeneration(ctx, persistMessageGenerationInput{
		SendInput:        st.input,
		Conversation:     st.conversation,
		UserMessage:      st.userMessage,
		AssistantMessage: st.assistantMessage,
		AssistantText:    st.finalAnswer,
		InputTokens:      st.totalInputTokens,
		CacheReadTokens:  st.totalCacheReadTokens,
		CacheWriteTokens: st.totalCacheWriteTokens,
		OutputTokens:     st.totalOutputTokens,
		ReasoningTokens:  st.totalReasoningTokens,
		AssistantLatency: latency,
		ReuseUserMessage: st.branchPreparation.reuseUserMessage,
	}); err != nil {
		return err
	}

	// 2. 用户消息 success（群组路径由 run 持久化持有结局）。
	if !st.branchPreparation.reuseUserMessage && st.userMessage.Status != "success" {
		if err := s.repo.UpdateMessageState(ctx, st.userMessage.ID, "success", "", ""); err != nil {
			s.logger.Error("update_user_message_state_failed",
				zap.String("trace_id", traceid.FromContext(ctx)),
				zap.Uint("message_id", st.userMessage.ID),
				zap.Error(err),
			)
		}
		st.userMessage.Status = "success"
	}

	// 3. CAS 运行 → completed + assistant_message_id。
	completed := domainagentgroup.RunStatusCompleted
	endedAt := time.Now()
	ok, err := s.agentGroupRunStore.CASUpdateAgentGroupRun(ctx, st.run.ID, st.stateVersion, domainagentgroup.RunStatusRunning,
		domainagentgroup.RunPatch{Status: &completed, AssistantMessageID: &st.assistantMessage.ID, EndedAt: &endedAt})
	if err != nil {
		return err
	}
	if !ok {
		return st.blockAgentGroupRun(ctx, nil, domainagentgroup.ErrorCodeCASConflict,
			genericAgentGroupErrorMessage(domainagentgroup.ErrorCodeCASConflict))
	}
	st.stateVersion++
	st.run.Status = completed

	// 4. 完成事件。
	st.emitAgentGroupRunCompleted(ctx)
	s.RecordAudit(ctx, AuditInput{
		UserID:     st.input.UserID,
		RequestID:  st.input.RequestID,
		Action:     "complete_agent_group_run",
		Resource:   "agent_group_run",
		ResourceID: st.run.PublicID,
		Detail: map[string]interface{}{
			"conversation_id":   st.input.ConversationID,
			"group_public_id":   st.run.GroupPublicID,
			"latency_ms":        latency,
			"assistant_message": st.assistantMessage.PublicID,
		},
	})
	return nil
}

// markGroupAssistantMessageState 标记群组结局下的消息状态：
// 用户消息一律 success（已接受，由 run 持久化持有），assistant 消息按结局标记。
func (st *agentGroupRunState) markGroupAssistantMessageState(ctx context.Context, code string) {
	if st.assistantMessage == nil {
		return
	}
	messageStatus := "error"
	messageErrorCode := "agent_group_paused"
	switch code {
	case domainagentgroup.ErrorCodeCanceled:
		messageStatus = "canceled"
		messageErrorCode = "generation_canceled"
	case domainagentgroup.ErrorCodeInterrupted:
		messageStatus = "interrupted"
		messageErrorCode = "agent_group_paused"
	case domainagentgroup.ErrorCodeStepLimitExceeded, domainagentgroup.ErrorCodeAttemptLimitExceeded,
		domainagentgroup.ErrorCodeCASConflict, domainagentgroup.ErrorCodeUpstreamFatal:
		messageErrorCode = "agent_group_blocked"
	}
	message := genericAgentGroupErrorMessage(code)

	persistCtx, cancel := finalizePersistContext(ctx)
	defer cancel()
	if err := st.service.repo.UpdateMessageState(persistCtx, st.assistantMessage.ID, messageStatus, messageErrorCode, message); err != nil {
		st.service.logger.Error("update_assistant_message_state_failed",
			zap.String("trace_id", traceid.FromContext(ctx)),
			zap.Uint("message_id", st.assistantMessage.ID),
			zap.Error(err),
		)
	}
	st.assistantMessage.Status = messageStatus
	st.assistantMessage.ErrorCode = messageErrorCode
	st.assistantMessage.ErrorMessage = message

	if st.userMessage != nil && st.userMessage.Status != "success" {
		if err := st.service.repo.UpdateMessageState(persistCtx, st.userMessage.ID, "success", "", ""); err != nil {
			st.service.logger.Error("update_user_message_state_failed",
				zap.String("trace_id", traceid.FromContext(ctx)),
				zap.Uint("message_id", st.userMessage.ID),
				zap.Error(err),
			)
		}
		st.userMessage.Status = "success"
	}
}

// recordStepSummary 记录已完成成员步骤摘要（供后续主管/成员上下文复用）。
func (st *agentGroupRunState) recordStepSummary(
	step *domainagentgroup.Step,
	member *domainagentgroup.RunSnapshotMember,
	instruction string,
	output *AgentTurnOutput,
) {
	summary := ""
	if output != nil {
		summary = compactSnippet(output.Text, 512)
	}
	st.summaries = append(st.summaries, agentGroupContextSummary{
		sequence:      step.Sequence,
		stepType:      step.StepType,
		actorName:     member.RoleName,
		instruction:   instruction,
		outputSummary: summary,
	})
}

// accumulateUsage 聚合一次成功 Attempt 的用量。
func (st *agentGroupRunState) accumulateUsage(output *AgentTurnOutput) {
	if output == nil {
		return
	}
	if usage := output.Usage; usage != nil {
		st.totalInputTokens += usage.InputTokens
		st.totalCacheReadTokens += usage.CacheReadTokens
		st.totalCacheWriteTokens += usage.CacheWriteTokens
		st.totalOutputTokens += usage.OutputTokens
		st.totalReasoningTokens += usage.ReasoningTokens
	}
	st.totalToolCalls += len(output.ToolCallRows)
}

// persistTopLevelRun 写入顶层 conversation_runs 日志行（聚合全部 Attempt 用量）。
func (st *agentGroupRunState) persistTopLevelRun(ctx context.Context, retErr error) {
	endedAt := time.Now()
	run := &model.Run{
		RunID:              st.runID,
		RequestID:          strings.TrimSpace(st.input.RequestID),
		UserID:             st.input.UserID,
		ConversationID:     st.input.ConversationID,
		TaskType:           "agent_group",
		Endpoint:           llm.EndpointResponses,
		Provider:           st.conversation.Provider,
		ProviderProtocol:   "responses",
		RequestedModelName: st.conversation.Model,
		PlatformModelName:  st.conversation.Model,
		InputTokens:        st.totalInputTokens,
		OutputTokens:       st.totalOutputTokens,
		CacheReadTokens:    st.totalCacheReadTokens,
		CacheWriteTokens:   st.totalCacheWriteTokens,
		ReasoningTokens:    st.totalReasoningTokens,
		ToolCallsCount:     st.totalToolCalls,
		TotalLatencyMS:     time.Since(st.startedAt).Milliseconds(),
		StartedAt:          st.startedAt,
		EndedAt:            &endedAt,
	}
	switch {
	case retErr == nil:
		run.Status = "success"
	case errors.Is(retErr, ErrMessageGenerationCanceled):
		run.Status = "canceled"
		run.ErrorCode = classifyRunErrorCode(retErr)
		run.ErrorMessage = truncateError(retErr.Error(), 255)
	case st.assistantMessage != nil && st.assistantMessage.Status == "interrupted":
		run.Status = "interrupted"
		run.ErrorCode = classifyRunErrorCode(retErr)
		run.ErrorMessage = truncateError(retErr.Error(), 255)
	default:
		run.Status = "error"
		run.ErrorCode = classifyRunErrorCode(retErr)
		run.ErrorMessage = truncateError(retErr.Error(), 255)
	}
	if run.TotalLatencyMS < 0 {
		run.TotalLatencyMS = 0
	}
	// 断连场景 ctx 已取消：审计行用独立上下文落库（失败仅记日志，不影响运行终态）。
	persistCtx, cancelPersist := finalizePersistContext(ctx)
	defer cancelPersist()
	if err := st.service.repo.CreateConversationRun(persistCtx, run); err != nil {
		st.service.logger.Error("create_conversation_run_failed",
			zap.String("trace_id", traceid.FromContext(ctx)),
			zap.String("run_id", run.RunID),
			zap.Error(err),
		)
	}
}

// completedResult 返回群组运行成功结果（Billable=false：计费发生在各 Attempt）。
func (st *agentGroupRunState) completedResult() *SendMessageResult {
	latency := time.Since(st.startedAt).Milliseconds()
	if latency < 0 {
		latency = 0
	}
	return &SendMessageResult{
		UserMessage:      *st.userMessage,
		AssistantMessage: *st.assistantMessage,
		Billable:         false,
		LatencyMS:        latency,
		StartedAt:        st.startedAt,
	}
}

// failedResult 返回群组运行失败结果（消息状态已在最终化时标记）。
func (st *agentGroupRunState) failedResult() *SendMessageResult {
	latency := time.Since(st.startedAt).Milliseconds()
	if latency < 0 {
		latency = 0
	}
	result := &SendMessageResult{
		Billable:  false,
		LatencyMS: latency,
		StartedAt: st.startedAt,
	}
	if st.userMessage != nil {
		result.UserMessage = *st.userMessage
	}
	if st.assistantMessage != nil {
		result.AssistantMessage = *st.assistantMessage
	}
	return result
}

// agentGroupRunMutex 返回会话级串行锁（不同会话可并发）。
func (s *Service) agentGroupRunMutex(conversationID uint) *sync.Mutex {
	key := conversationID
	if value, ok := s.agentGroupRunLocks.Load(key); ok {
		return value.(*sync.Mutex)
	}
	value, _ := s.agentGroupRunLocks.LoadOrStore(key, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// buildAgentGroupRunSnapshot 冻结运行配置快照：
// 成员覆盖 > 角色默认 > 平台默认；跳过禁用成员；主管缺失视为群组无效。
// 快照不保存任何凭证；请求级工具与技能选择一并冻结，供重试恢复执行能力。
func (s *Service) buildAgentGroupRunSnapshot(ctx context.Context, input SendMessageInput, conversation *model.Conversation) (*domainagentgroup.RunSnapshot, error) {
	if conversation.AgentGroupPublicID == "" {
		return nil, ErrConversationAgentGroupNotFound
	}
	group, err := s.agentGroupRepo.GetAgentGroupByPublicID(ctx, input.UserID, conversation.AgentGroupPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrConversationAgentGroupNotFound
		}
		return nil, err
	}

	project, err := s.GetConversationProject(ctx, input.UserID, conversation.ProjectPublicID)
	if err != nil {
		return nil, err
	}

	snapshot := &domainagentgroup.RunSnapshot{
		Project: domainagentgroup.RunSnapshotProject{
			ProjectID:      project.ID,
			PublicID:       project.PublicID,
			Name:           project.Name,
			SystemPrompt:   project.SystemPrompt,
			MCPDefaultMode: project.MCPDefaultMode,
			MCPToolIDs:     append([]uint(nil), project.DefaultMCPToolIDs...),
			SkillIDs:       append([]uint(nil), project.DefaultSkillIDs...),
		},
		Group: domainagentgroup.RunSnapshotGroup{
			GroupID:            group.ID,
			PublicID:           group.PublicID,
			Name:               group.Name,
			Revision:           group.Revision,
			CoordinationPrompt: group.CoordinationPrompt,
		},
		Limits:            s.agentGroupRunLimits(ctx),
		RequestedToolIDs:  append([]uint(nil), input.SelectedToolIDs...),
		RequestedSkillIDs: append([]uint(nil), input.SkillIDs...),
	}

	for i := range group.Members {
		member := &group.Members[i]
		if !member.Enabled {
			continue
		}
		role, err := s.GetConversationRole(ctx, input.UserID, member.RolePublicID)
		if err != nil {
			return nil, err
		}
		snapshotMember := domainagentgroup.RunSnapshotMember{
			MemberID:         member.ID,
			PublicID:         member.PublicID,
			RoleID:           role.ID,
			RolePublicID:     role.PublicID,
			RoleName:         role.Name,
			RoleSystemPrompt: role.SystemPrompt,
			MemberType:       member.MemberType,
			Enabled:          member.Enabled,
			Icon:             role.Icon,
			Color:            role.Color,
			DutyInstruction:  member.DutyInstruction,
			RoleDefaultModel: role.Model,
			ModelOverride:    member.ModelOverride,
			EffectiveModel:   domainagentgroup.ResolveEffectiveModel(role.Model, member.ModelOverride, conversation.Model),
			Provider:         role.Provider,
		}
		if member.MemberType == domainagentgroup.MemberTypeSupervisor {
			snapshot.Supervisor = snapshotMember
		} else {
			snapshot.Members = append(snapshot.Members, snapshotMember)
		}
	}
	if snapshot.Supervisor.MemberID == 0 {
		return nil, ErrConversationAgentGroupNotFound
	}
	return snapshot, nil
}

// agentGroupFeatureEnabled 读取 agent_group.enabled（默认关闭）。
func (s *Service) agentGroupFeatureEnabled(ctx context.Context) bool {
	if s.agentGroupSettings == nil {
		return false
	}
	values, err := s.agentGroupSettings.RuntimeValuesByNamespace(ctx, domainagentgroup.FeatureFlagNamespace)
	if err != nil {
		s.logger.Warn("agent_group_settings_read_failed",
			zap.String("trace_id", traceid.FromContext(ctx)),
			zap.Error(err),
		)
		return false
	}
	return strings.TrimSpace(values[domainagentgroup.FeatureFlagKeyEnabled]) == "true"
}

// agentGroupRunLimits 读取运行限制（settings 命名空间 agent_group，缺省回落默认值）。
func (s *Service) agentGroupRunLimits(ctx context.Context) domainagentgroup.RunSnapshotLimits {
	limits := domainagentgroup.RunSnapshotLimits{
		MaxStepsPerRun:     domainagentgroup.DefaultMaxStepsPerRun,
		MaxAttemptsPerStep: domainagentgroup.DefaultMaxAttemptsPerStep,
	}
	if s.agentGroupSettings == nil {
		return limits
	}
	values, err := s.agentGroupSettings.RuntimeValuesByNamespace(ctx, domainagentgroup.FeatureFlagNamespace)
	if err != nil {
		s.logger.Warn("agent_group_settings_read_failed",
			zap.String("trace_id", traceid.FromContext(ctx)),
			zap.Error(err),
		)
		return limits
	}
	if value, ok := parseAgentGroupPositiveInt(values[domainagentgroup.SettingKeyMaxStepsPerRun]); ok {
		limits.MaxStepsPerRun = value
	}
	if value, ok := parseAgentGroupPositiveInt(values[domainagentgroup.SettingKeyMaxAttemptsPerStep]); ok {
		limits.MaxAttemptsPerStep = value
	}
	return limits
}

// agentGroupAttemptLease 读取 Attempt 租约时长。
func (s *Service) agentGroupAttemptLease(ctx context.Context) time.Duration {
	lease := domainagentgroup.DefaultAttemptLease
	if s.agentGroupSettings == nil {
		return lease
	}
	values, err := s.agentGroupSettings.RuntimeValuesByNamespace(ctx, domainagentgroup.FeatureFlagNamespace)
	if err != nil {
		return lease
	}
	if raw := strings.TrimSpace(values[domainagentgroup.SettingKeyAttemptLeaseSeconds]); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			lease = time.Duration(seconds) * time.Second
		}
	}
	return lease
}

func parseAgentGroupPositiveInt(raw string) (int, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
}

// classifyAgentGroupError 将执行错误映射为群组错误码与可重试性。
func classifyAgentGroupError(err error) (code string, retryable bool) {
	switch {
	case errors.Is(err, ErrMessageGenerationCanceled):
		return domainagentgroup.ErrorCodeCanceled, true
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return domainagentgroup.ErrorCodeInterrupted, true
	case errors.Is(err, ErrAgentGroupInvalidMember), errors.Is(err, ErrAgentGroupInvalidDecision):
		return domainagentgroup.ErrorCodeInvalidMemberSchedule, true
	case errors.Is(err, ErrUpstreamRequestFailed), errors.Is(err, ErrUpstreamEmptyResponse),
		errors.Is(err, ErrToolRunFinalAnswerMissing):
		return domainagentgroup.ErrorCodeUpstreamRetryable, true
	default:
		return domainagentgroup.ErrorCodeUpstreamFatal, false
	}
}

// genericAgentGroupErrorMessage 返回面向用户的通用错误说明（不暴露上游细节/诊断）。
func genericAgentGroupErrorMessage(code string) string {
	switch code {
	case domainagentgroup.ErrorCodeCanceled:
		return "agent group run canceled"
	case domainagentgroup.ErrorCodeInterrupted:
		return "agent group run interrupted"
	case domainagentgroup.ErrorCodeInvalidMemberSchedule:
		return "supervisor made an invalid delegation"
	case domainagentgroup.ErrorCodeUpstreamRetryable:
		return "agent group step failed, retryable"
	case domainagentgroup.ErrorCodeStepLimitExceeded:
		return "agent group step limit exceeded"
	case domainagentgroup.ErrorCodeAttemptLimitExceeded:
		return "agent group attempt limit exceeded"
	case domainagentgroup.ErrorCodeCASConflict:
		return "agent group run state conflict"
	default:
		return "agent group step failed"
	}
}

// finalizePersistContext 返回最终化持久化使用的上下文：
// 请求 ctx 已取消（用户停止/客户端断连）时切换为独立的 5 秒后台上下文，
// 保证 paused_retryable/blocked 等终态一定能写入 —— 否则运行会卡在 running，
// 重试 API 永远返回 NotRetryable，生成中断后无法重试。
func finalizePersistContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx != nil && ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// marshalAgentGroupRunSnapshot 序列化配置快照（失败回落空对象，不允许影响运行）。
func marshalAgentGroupRunSnapshot(snapshot *domainagentgroup.RunSnapshot) string {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// marshalAgentGroupAttemptInput 序列化步骤输入快照（仅供重试恢复与审计）。
func marshalAgentGroupAttemptInput(snapshot *domainagentgroup.RunSnapshot, stepType string, instruction string) string {
	payload := map[string]interface{}{
		"stepType":    stepType,
		"instruction": instruction,
		"group":       snapshot.Group.PublicID,
		"revision":    snapshot.Group.Revision,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
