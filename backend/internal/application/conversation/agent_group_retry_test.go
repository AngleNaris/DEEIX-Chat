package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appcompact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/compact"
	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	persistencemodels "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	postgresagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/postgres/agentgroup"
	persistenceconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/postgres/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// agentGroupRetrySettingsFake 注入 feature flag：agentGroupFeatureEnabled 在
// agentGroupSettings 为 nil 时禁用特性，测试必须提供该假实现。
type agentGroupRetrySettingsFake struct {
	values map[string]string
}

func (f *agentGroupRetrySettingsFake) RuntimeValuesByNamespace(ctx context.Context, namespace string) (map[string]string, error) {
	return f.values, nil
}

// agentGroupRetryResolverFake 只满足非 nil 校验（重试路径不解析群组详情）。
type agentGroupRetryResolverFake struct{}

func (agentGroupRetryResolverFake) GetAgentGroupByPublicID(ctx context.Context, userID uint, publicID string) (*domainagentgroup.Group, error) {
	return nil, repository.ErrNotFound
}

func (agentGroupRetryResolverFake) CountAgentGroupReferencesByRole(ctx context.Context, roleID uint) (int64, error) {
	return 0, nil
}

func (agentGroupRetryResolverFake) CountAgentGroupReferencesByProject(ctx context.Context, projectID uint) (int64, error) {
	return 0, nil
}

// agentGroupRetryTestDBCounter 为每个测试分配独立的内存库名，
// 避免共享缓存模式下多个测试串用同一数据库。
var agentGroupRetryTestDBCounter uint64

func openAgentGroupRetryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:agent_group_retry_%d?mode=memory&cache=shared&_busy_timeout=5000",
		atomic.AddUint64(&agentGroupRetryTestDBCounter, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// 单连接：sqlite 共享缓存下并发写会抛 SQLITE_LOCKED（busy_timeout 对其无效），
	// 与仓库其余 sqlite 测试一致，把并发序列化在驱动层，锁竞争改由 CAS/状态守卫体现。
	if sqlDB, err := db.DB(); err != nil {
		t.Fatalf("sql db: %v", err)
	} else {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(
		&persistencemodels.Conversation{},
		&persistencemodels.ConversationProject{},
		&persistencemodels.ConversationShare{},
		&persistencemodels.Message{},
		&persistencemodels.Attachment{},
		&persistencemodels.FileObject{},
		&persistencemodels.ConversationRun{},
		&persistencemodels.ChatRunEvent{},
		&persistencemodels.UserSetting{},
		&persistencemodels.AgentGroup{},
		&persistencemodels.AgentGroupMember{},
		&persistencemodels.AgentGroupRun{},
		&persistencemodels.AgentGroupStep{},
		&persistencemodels.AgentGroupStepAttempt{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// newAgentGroupRetryTestService 构建测试 Service。
// routeResolver/llmClient 保持 nil：ExecuteAgentTurn 短路返回
// ErrModelRouteNotConfigured（UPSTREAM_FATAL，不可重试）——测试确定性执行缝。
func newAgentGroupRetryTestService(t *testing.T, db *gorm.DB) *Service {
	t.Helper()
	convRepo := persistenceconversation.NewRepo(db)
	cfg := config.NewRuntime(config.Config{})
	return &Service{
		cfg:                cfg,
		repo:               convRepo,
		agentGroupRunStore: postgresagentgroup.NewRepo(db),
		agentGroupRepo:     agentGroupRetryResolverFake{},
		agentGroupSettings: &agentGroupRetrySettingsFake{values: map[string]string{
			domainagentgroup.FeatureFlagKeyEnabled: "true",
		}},
		compactSvc: appcompact.NewServiceWithRuntime(cfg, convRepo, zap.NewNop()),
		logger:     zap.NewNop(),
	}
}

const (
	agentGroupRetryUserID      = 9
	agentGroupRetryClientRunID = "run-client-1"
)

// agentGroupRetrySeed 保存一次种子运行的全部持久化引用，供断言使用。
type agentGroupRetrySeed struct {
	group             *persistencemodels.AgentGroup
	conversation      *persistencemodels.Conversation
	userMessage       *persistencemodels.Message
	assistantMessage  *persistencemodels.Message
	run               *persistencemodels.AgentGroupRun
	supervisorStep    *persistencemodels.AgentGroupStep
	workerStep        *persistencemodels.AgentGroupStep
	supervisorAttempt *persistencemodels.AgentGroupStepAttempt
	workerAttempt     *persistencemodels.AgentGroupStepAttempt
	snapshot          *domainagentgroup.RunSnapshot
}

// seedAgentGroupPausedRetryableRun 构造 paused_retryable 运行检查点：
// 主管步骤与成员步骤各一次 Attempt，可重试步骤（retryable: "supervisor"/"worker"）
// 失败并作为 RetryableStepID 记录，另一步骤成功。MaxAttemptsPerStep 可配。
func seedAgentGroupPausedRetryableRun(t *testing.T, db *gorm.DB, retryable string, maxAttempts int) *agentGroupRetrySeed {
	t.Helper()
	now := time.Now()
	ended := now.Add(time.Second)
	projectID := uint(1)

	group := &persistencemodels.AgentGroup{
		UserID: agentGroupRetryUserID, PublicID: "group-agent-1", ProjectID: projectID,
		Name: "测试小组", Status: "active", Revision: 1,
	}
	if err := db.Create(group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	conversation := &persistencemodels.Conversation{
		UserID: agentGroupRetryUserID, ProjectID: &projectID, AgentGroupID: &group.ID,
		PublicID: "conv-agent-group-1", Title: "群组会话", Model: "gpt-test",
	}
	if err := db.Create(conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	userMessage := &persistencemodels.Message{
		ConversationID: conversation.ID, UserID: agentGroupRetryUserID, PublicID: "msg-user-1",
		RunID: agentGroupRetryClientRunID, Role: "user", ContentType: "text",
		Content: "请按流程完成三件事", BranchReason: "default", Status: "success",
	}
	if err := db.Create(userMessage).Error; err != nil {
		t.Fatalf("create user message: %v", err)
	}
	assistantMessage := &persistencemodels.Message{
		ConversationID: conversation.ID, UserID: agentGroupRetryUserID, PublicID: "msg-asst-1",
		RunID: agentGroupRetryClientRunID, Role: "assistant", ContentType: "text",
		Content: "", ParentMessageID: &userMessage.ID, BranchReason: "default", Status: "paused",
	}
	if err := db.Create(assistantMessage).Error; err != nil {
		t.Fatalf("create assistant message: %v", err)
	}

	snapshot := &domainagentgroup.RunSnapshot{
		Project: domainagentgroup.RunSnapshotProject{ProjectID: projectID, PublicID: "proj-1", Name: "测试项目"},
		Group:   domainagentgroup.RunSnapshotGroup{GroupID: group.ID, PublicID: group.PublicID, Name: group.Name, Revision: 1},
		Supervisor: domainagentgroup.RunSnapshotMember{
			MemberID: 11, PublicID: "member-sup", RoleID: 21, RolePublicID: "role-sup-1",
			RoleName: "主管", RoleSystemPrompt: "你是主管，负责分配任务", MemberType: domainagentgroup.MemberTypeSupervisor,
			Enabled: true, Icon: "star", Color: "#f59e0b", RoleDefaultModel: "gpt-sup", EffectiveModel: "gpt-sup", Provider: "test",
		},
		Members: []domainagentgroup.RunSnapshotMember{
			{
				MemberID: 11, PublicID: "member-sup", RoleID: 21, RolePublicID: "role-sup-1",
				RoleName: "主管", RoleSystemPrompt: "你是主管，负责分配任务", MemberType: domainagentgroup.MemberTypeSupervisor,
				Enabled: true, Icon: "star", Color: "#f59e0b", RoleDefaultModel: "gpt-sup", EffectiveModel: "gpt-sup", Provider: "test",
			},
			{
				MemberID: 12, PublicID: "member-wkr", RoleID: 22, RolePublicID: "role-wkr-1",
				RoleName: "工程师", RoleSystemPrompt: "你是工程师", MemberType: domainagentgroup.MemberTypeWorker,
				Enabled: true, Icon: "wrench", Color: "#3b82f6", RoleDefaultModel: "gpt-wkr", EffectiveModel: "gpt-wkr", Provider: "test",
			},
		},
		Limits: domainagentgroup.RunSnapshotLimits{MaxStepsPerRun: 20, MaxAttemptsPerStep: maxAttempts},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	run := &persistencemodels.AgentGroupRun{
		PublicID: "run-pub-1", ClientRunID: agentGroupRetryClientRunID, UserID: agentGroupRetryUserID,
		ConversationID: conversation.ID, GroupID: group.ID, UserMessageID: userMessage.ID,
		AssistantMessageID: &assistantMessage.ID, GroupRevision: 1, ConfigSnapshotJSON: string(raw),
		Status:    domainagentgroup.RunStatusPausedRetryable,
		ErrorCode: "UPSTREAM_RETRYABLE", ErrorMessage: "agent group step failed, retryable",
		StateVersion: 1, StartedAt: now,
	}
	if err := db.Create(run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}

	supervisorStatus, workerStatus := domainagentgroup.StepStatusSuccess, domainagentgroup.StepStatusFailed
	supervisorAttemptStatus, workerAttemptStatus := domainagentgroup.AttemptStatusSuccess, domainagentgroup.AttemptStatusError
	if retryable == "supervisor" {
		supervisorStatus, workerStatus = domainagentgroup.StepStatusFailed, domainagentgroup.StepStatusSuccess
		supervisorAttemptStatus, workerAttemptStatus = domainagentgroup.AttemptStatusError, domainagentgroup.AttemptStatusSuccess
	}
	supervisorStep := &persistencemodels.AgentGroupStep{
		PublicID: "step-sup-1", GroupRunID: run.ID, Sequence: 1,
		StepType: domainagentgroup.StepTypeSupervisorDecide, ActorMemberPublicID: "member-sup",
		ActorNameSnapshot: "主管", ActorTypeSnapshot: "supervisor", Instruction: "分配任务", Status: supervisorStatus,
	}
	workerStep := &persistencemodels.AgentGroupStep{
		PublicID: "step-wkr-1", GroupRunID: run.ID, Sequence: 2,
		StepType: domainagentgroup.StepTypeMemberExecute, ActorMemberPublicID: "member-wkr",
		ActorNameSnapshot: "工程师", ActorTypeSnapshot: "worker", Instruction: "完成任务A", Status: workerStatus,
	}
	if err := db.Create(supervisorStep).Error; err != nil {
		t.Fatalf("create supervisor step: %v", err)
	}
	if err := db.Create(workerStep).Error; err != nil {
		t.Fatalf("create worker step: %v", err)
	}
	supervisorAttempt := &persistencemodels.AgentGroupStepAttempt{
		PublicID: "att-sup-1", StepID: supervisorStep.ID, AttemptNo: 1,
		ChildRunID: agentGroupRetryClientRunID, RetryRequestID: "rr-sup-1",
		RequestedModel: "gpt-sup", ResolvedModel: "gpt-sup", InputSnapshotJSON: "{}",
		Status: supervisorAttemptStatus, BillingRef: domainagentgroup.BillingRef(run.ID, supervisorStep.ID, 1),
		StartedAt: now, EndedAt: &ended,
	}
	workerAttempt := &persistencemodels.AgentGroupStepAttempt{
		PublicID: "att-wkr-1", StepID: workerStep.ID, AttemptNo: 1,
		ChildRunID: agentGroupRetryClientRunID, RetryRequestID: "rr-wkr-1",
		RequestedModel: "gpt-wkr", ResolvedModel: "gpt-wkr", InputSnapshotJSON: "{}",
		Status: workerAttemptStatus, BillingRef: domainagentgroup.BillingRef(run.ID, workerStep.ID, 1),
		StartedAt: now, EndedAt: &ended,
	}
	if err := db.Create(supervisorAttempt).Error; err != nil {
		t.Fatalf("create supervisor attempt: %v", err)
	}
	if err := db.Create(workerAttempt).Error; err != nil {
		t.Fatalf("create worker attempt: %v", err)
	}

	retryableStepID := workerStep.ID
	successfulStepID := supervisorStep.ID
	successfulAttemptID := supervisorAttempt.ID
	if retryable == "supervisor" {
		retryableStepID = supervisorStep.ID
		successfulStepID = workerStep.ID
		successfulAttemptID = workerAttempt.ID
	}
	if err := db.Model(&persistencemodels.AgentGroupRun{}).Where("id = ?", run.ID).
		Update("retryable_step_id", retryableStepID).Error; err != nil {
		t.Fatalf("set retryable step: %v", err)
	}
	if err := db.Model(&persistencemodels.AgentGroupStep{}).Where("id = ?", successfulStepID).
		Update("successful_attempt_id", successfulAttemptID).Error; err != nil {
		t.Fatalf("set successful attempt: %v", err)
	}

	// 顶层 conversation_runs 基线行（重试路径 UPDATE，绝不新建）。
	baseline := &persistencemodels.ConversationRun{
		RunID: agentGroupRetryClientRunID, RequestID: "req-1", UserID: agentGroupRetryUserID,
		ConversationID: conversation.ID, TaskType: "chat", Status: "error",
		InputTokens: 120, OutputTokens: 60, CacheReadTokens: 30, ToolCallsCount: 1, StartedAt: now,
	}
	if err := db.Create(baseline).Error; err != nil {
		t.Fatalf("create baseline run: %v", err)
	}

	return &agentGroupRetrySeed{
		group: group, conversation: conversation, userMessage: userMessage,
		assistantMessage: assistantMessage, run: run,
		supervisorStep: supervisorStep, workerStep: workerStep,
		supervisorAttempt: supervisorAttempt, workerAttempt: workerAttempt,
		snapshot: snapshot,
	}
}

func listAgentGroupAttempts(t *testing.T, db *gorm.DB, stepID uint) []persistencemodels.AgentGroupStepAttempt {
	t.Helper()
	var rows []persistencemodels.AgentGroupStepAttempt
	if err := db.Where("step_id = ?", stepID).Order("attempt_no ASC").Find(&rows).Error; err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	return rows
}

// 验收 1：A 成功、B 失败时只重试 B——B 追加 Attempt 2（独立计费引用），
// 成功步骤不重复执行/计费，不新增步骤，顶层运行行不重复创建。
func TestAgentGroupRunRetryOnlyRetriesFailedStep(t *testing.T) {
	db := openAgentGroupRetryTestDB(t)
	seed := seedAgentGroupPausedRetryableRun(t, db, "worker", 3)
	service := newAgentGroupRetryTestService(t, db)

	_, err := service.RetryAgentGroupRunStep(context.Background(), RetryAgentGroupRunInput{
		UserID: agentGroupRetryUserID, RunPublicID: seed.run.PublicID, RequestID: "retry-req-1",
	}, nil)
	if !errors.Is(err, ErrAgentGroupRunBlocked) {
		t.Fatalf("retry error = %v, want ErrAgentGroupRunBlocked", err)
	}

	workerAttempts := listAgentGroupAttempts(t, db, seed.workerStep.ID)
	if len(workerAttempts) != 2 {
		t.Fatalf("worker attempts = %d, want 2 (original + retry)", len(workerAttempts))
	}
	retryAttempt := workerAttempts[1]
	if retryAttempt.AttemptNo != 2 || retryAttempt.Status != domainagentgroup.AttemptStatusError {
		t.Fatalf("retry attempt = no %d status %q, want no 2 / error", retryAttempt.AttemptNo, retryAttempt.Status)
	}
	if want := domainagentgroup.BillingRef(seed.run.ID, seed.workerStep.ID, 2); retryAttempt.BillingRef != want {
		t.Fatalf("retry attempt billing ref = %q, want %q", retryAttempt.BillingRef, want)
	}

	// 成功步骤不重复执行、不重复计费。
	supervisorAttempts := listAgentGroupAttempts(t, db, seed.supervisorStep.ID)
	if len(supervisorAttempts) != 1 {
		t.Fatalf("supervisor attempts = %d, want 1 (successful step not re-executed)", len(supervisorAttempts))
	}
	// 不新增步骤；步骤本身不可变。
	var stepCount int64
	if err := db.Model(&persistencemodels.AgentGroupStep{}).Where("group_run_id = ?", seed.run.ID).Count(&stepCount).Error; err != nil {
		t.Fatalf("count steps: %v", err)
	}
	if stepCount != 2 {
		t.Fatalf("steps = %d, want 2", stepCount)
	}
	var supRow persistencemodels.AgentGroupStep
	if err := db.First(&supRow, seed.supervisorStep.ID).Error; err != nil {
		t.Fatalf("load supervisor step: %v", err)
	}
	if supRow.Status != domainagentgroup.StepStatusSuccess {
		t.Fatalf("supervisor step status = %q, want success", supRow.Status)
	}
	var wkrRow persistencemodels.AgentGroupStep
	if err := db.First(&wkrRow, seed.workerStep.ID).Error; err != nil {
		t.Fatalf("load worker step: %v", err)
	}
	if wkrRow.Status != domainagentgroup.StepStatusFailed {
		t.Fatalf("worker step status = %q, want failed", wkrRow.Status)
	}

	// 运行 → blocked（UPSTREAM_FATAL 不可重试）；顶层运行行不新建、推进为 error。
	var runRow persistencemodels.AgentGroupRun
	if err := db.First(&runRow, seed.run.ID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if runRow.Status != domainagentgroup.RunStatusBlocked {
		t.Fatalf("run status = %q, want blocked", runRow.Status)
	}
	var runCount int64
	if err := db.Model(&persistencemodels.ConversationRun{}).
		Where("user_id = ? AND conversation_id = ?", agentGroupRetryUserID, seed.conversation.ID).Count(&runCount).Error; err != nil {
		t.Fatalf("count conversation runs: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("conversation runs = %d, want 1 (no duplicate)", runCount)
	}
	var topRun persistencemodels.ConversationRun
	if err := db.Where("run_id = ?", seed.run.ClientRunID).First(&topRun).Error; err != nil {
		t.Fatalf("load top run: %v", err)
	}
	if topRun.Status != "error" {
		t.Fatalf("top run status = %q, want error", topRun.Status)
	}
	// assistant 消息标记 blocked 结局（不暴露上游诊断细节）。
	var asstMsg persistencemodels.Message
	if err := db.First(&asstMsg, seed.assistantMessage.ID).Error; err != nil {
		t.Fatalf("load assistant message: %v", err)
	}
	if asstMsg.Status != "error" || asstMsg.ErrorCode != "agent_group_blocked" {
		t.Fatalf("assistant message = status %q code %q, want error / agent_group_blocked", asstMsg.Status, asstMsg.ErrorCode)
	}
}

// 验收 2：B 成功、主管失败时只重试主管。
func TestAgentGroupRunRetryTargetsSupervisorWhenMemberSucceeded(t *testing.T) {
	db := openAgentGroupRetryTestDB(t)
	seed := seedAgentGroupPausedRetryableRun(t, db, "supervisor", 3)
	service := newAgentGroupRetryTestService(t, db)

	_, err := service.RetryAgentGroupRunStep(context.Background(), RetryAgentGroupRunInput{
		UserID: agentGroupRetryUserID, RunPublicID: seed.run.PublicID, RequestID: "retry-req-sup",
	}, nil)
	if !errors.Is(err, ErrAgentGroupRunBlocked) {
		t.Fatalf("retry error = %v, want ErrAgentGroupRunBlocked", err)
	}

	supervisorAttempts := listAgentGroupAttempts(t, db, seed.supervisorStep.ID)
	if len(supervisorAttempts) != 2 {
		t.Fatalf("supervisor attempts = %d, want 2", len(supervisorAttempts))
	}
	retryAttempt := supervisorAttempts[1]
	if retryAttempt.AttemptNo != 2 || retryAttempt.Status != domainagentgroup.AttemptStatusError {
		t.Fatalf("retry attempt = no %d status %q, want no 2 / error", retryAttempt.AttemptNo, retryAttempt.Status)
	}
	if want := domainagentgroup.BillingRef(seed.run.ID, seed.supervisorStep.ID, 2); retryAttempt.BillingRef != want {
		t.Fatalf("retry attempt billing ref = %q, want %q", retryAttempt.BillingRef, want)
	}

	// 成功成员步骤不重试、不新增 Attempt。
	workerAttempts := listAgentGroupAttempts(t, db, seed.workerStep.ID)
	if len(workerAttempts) != 1 {
		t.Fatalf("worker attempts = %d, want 1", len(workerAttempts))
	}
	var wkrRow persistencemodels.AgentGroupStep
	if err := db.First(&wkrRow, seed.workerStep.ID).Error; err != nil {
		t.Fatalf("load worker step: %v", err)
	}
	if wkrRow.Status != domainagentgroup.StepStatusSuccess {
		t.Fatalf("worker step status = %q, want success", wkrRow.Status)
	}
}

// 验收 3：服务重启后可以从中断步骤恢复——全新 Service 实例从 DB 检查点重建，
// 且只追加一次 Attempt；再次调用被状态守卫拒绝，绝不重复执行。
func TestAgentGroupRunRetryResumesFromPersistedCheckpointAfterRestart(t *testing.T) {
	db := openAgentGroupRetryTestDB(t)
	seed := seedAgentGroupPausedRetryableRun(t, db, "worker", 3)

	// 模拟新进程：全新 Service + 全新 repo 实例，仅共享数据库。
	restarted := newAgentGroupRetryTestService(t, db)
	_, err := restarted.RetryAgentGroupRunStep(context.Background(), RetryAgentGroupRunInput{
		UserID: agentGroupRetryUserID, RunPublicID: seed.run.PublicID, RequestID: "retry-req-restart",
	}, nil)
	if !errors.Is(err, ErrAgentGroupRunBlocked) {
		t.Fatalf("restart retry error = %v, want ErrAgentGroupRunBlocked", err)
	}
	attempts := listAgentGroupAttempts(t, db, seed.workerStep.ID)
	if len(attempts) != 2 {
		t.Fatalf("restart resume attempts = %d, want 2 (checkpoint honored)", len(attempts))
	}

	// 重启后再次重试（如 UI 抖动重复点击）→ 状态守卫拒绝，不产生 Attempt 3。
	again := newAgentGroupRetryTestService(t, db)
	_, err = again.RetryAgentGroupRunStep(context.Background(), RetryAgentGroupRunInput{
		UserID: agentGroupRetryUserID, RunPublicID: seed.run.PublicID, RequestID: "retry-req-restart-again",
	}, nil)
	if !errors.Is(err, ErrAgentGroupRunNotRetryable) {
		t.Fatalf("second retry error = %v, want ErrAgentGroupRunNotRetryable", err)
	}
	attempts = listAgentGroupAttempts(t, db, seed.workerStep.ID)
	if len(attempts) != 2 {
		t.Fatalf("attempts after duplicate restart retry = %d, want 2", len(attempts))
	}
}

// 验收 4：双击重试不会创建重复调用——两个独立 Service 实例（各自持锁）
// 并发重试同一运行，恰一个成功推进，另一个 CAS 冲突/状态守卫拒绝。
func TestAgentGroupRunRetryDoubleClickCreatesSingleAttempt(t *testing.T) {
	db := openAgentGroupRetryTestDB(t)
	seed := seedAgentGroupPausedRetryableRun(t, db, "worker", 3)
	svcA := newAgentGroupRetryTestService(t, db)
	svcB := newAgentGroupRetryTestService(t, db)

	ctxA := context.Background()
	ctxB := context.Background()
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = svcA.RetryAgentGroupRunStep(ctxA, RetryAgentGroupRunInput{
			UserID: agentGroupRetryUserID, RunPublicID: seed.run.PublicID, RequestID: "retry-req-double-a",
		}, nil)
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = svcB.RetryAgentGroupRunStep(ctxB, RetryAgentGroupRunInput{
			UserID: agentGroupRetryUserID, RunPublicID: seed.run.PublicID, RequestID: "retry-req-double-b",
		}, nil)
	}()
	wg.Wait()

	blocked, guarded := 0, 0
	for _, err := range errs {
		switch {
		case errors.Is(err, ErrAgentGroupRunBlocked):
			blocked++
		case errors.Is(err, ErrAgentGroupCASConflict) || errors.Is(err, ErrAgentGroupRunNotRetryable):
			guarded++
		default:
			t.Fatalf("unexpected double-click retry error: %v", err)
		}
	}
	if blocked != 1 || guarded != 1 {
		t.Fatalf("double-click outcome = blocked %d guarded %d, want 1/1 (errs: %v)", blocked, guarded, errs)
	}
	attempts := listAgentGroupAttempts(t, db, seed.workerStep.ID)
	if len(attempts) != 2 {
		t.Fatalf("double-click attempts = %d, want 2 (single retry)", len(attempts))
	}
}

// 验收 5：Attempt 上限——达到 MaxAttemptsPerStep 时运行 blocked（ATTEMPT_LIMIT_EXCEEDED），
// 不创建新 Attempt。
func TestAgentGroupRunRetryAttemptLimitBlocksWithoutNewAttempt(t *testing.T) {
	db := openAgentGroupRetryTestDB(t)
	seed := seedAgentGroupPausedRetryableRun(t, db, "worker", 1)
	service := newAgentGroupRetryTestService(t, db)

	_, err := service.RetryAgentGroupRunStep(context.Background(), RetryAgentGroupRunInput{
		UserID: agentGroupRetryUserID, RunPublicID: seed.run.PublicID, RequestID: "retry-req-limit",
	}, nil)
	if !errors.Is(err, ErrAgentGroupRunBlocked) {
		t.Fatalf("limit retry error = %v, want ErrAgentGroupRunBlocked", err)
	}
	var runRow persistencemodels.AgentGroupRun
	if err := db.First(&runRow, seed.run.ID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if runRow.Status != domainagentgroup.RunStatusBlocked {
		t.Fatalf("run status = %q, want blocked", runRow.Status)
	}
	if runRow.ErrorCode != domainagentgroup.ErrorCodeAttemptLimitExceeded {
		t.Fatalf("run error code = %q, want %q", runRow.ErrorCode, domainagentgroup.ErrorCodeAttemptLimitExceeded)
	}
	attempts := listAgentGroupAttempts(t, db, seed.workerStep.ID)
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1 (limit must not create attempt)", len(attempts))
	}
}

// 验收 6：守卫——StepPublicID 与可重试步骤不一致、或运行不在 paused_retryable 时拒绝，
// 且不产生任何新 Attempt。
func TestAgentGroupRunRetryRejectsWrongStepAndNonPausedRun(t *testing.T) {
	db := openAgentGroupRetryTestDB(t)
	seed := seedAgentGroupPausedRetryableRun(t, db, "worker", 3)
	service := newAgentGroupRetryTestService(t, db)

	_, err := service.RetryAgentGroupRunStep(context.Background(), RetryAgentGroupRunInput{
		UserID: agentGroupRetryUserID, RunPublicID: seed.run.PublicID,
		StepPublicID: seed.supervisorStep.PublicID, RequestID: "retry-req-wrong-step",
	}, nil)
	if !errors.Is(err, ErrAgentGroupRunNotRetryable) {
		t.Fatalf("wrong step error = %v, want ErrAgentGroupRunNotRetryable", err)
	}

	if err := db.Model(&persistencemodels.AgentGroupRun{}).Where("id = ?", seed.run.ID).
		Update("status", domainagentgroup.RunStatusCompleted).Error; err != nil {
		t.Fatalf("mark run completed: %v", err)
	}
	_, err = service.RetryAgentGroupRunStep(context.Background(), RetryAgentGroupRunInput{
		UserID: agentGroupRetryUserID, RunPublicID: seed.run.PublicID, RequestID: "retry-req-completed",
	}, nil)
	if !errors.Is(err, ErrAgentGroupRunNotRetryable) {
		t.Fatalf("completed run error = %v, want ErrAgentGroupRunNotRetryable", err)
	}

	for _, stepID := range []uint{seed.workerStep.ID, seed.supervisorStep.ID} {
		if attempts := listAgentGroupAttempts(t, db, stepID); len(attempts) != 1 {
			t.Fatalf("guarded retry created attempt for step %d: %d", stepID, len(attempts))
		}
	}
}

// 验收 7：工具幂等账本——重试恢复时只种子化可重试步骤的成功/复用工具行；
// 失败行与成功步骤的工具行不进入账本。
func TestAgentGroupRunResumeSeedsToolLedgerFromRetryableStepOnly(t *testing.T) {
	db := openAgentGroupRetryTestDB(t)
	seed := seedAgentGroupPausedRetryableRun(t, db, "worker", 3)
	now := time.Now()
	toolRows := []persistencemodels.ChatRunEvent{
		{
			// 可重试步骤（S2）此前的成功调用 → 必须进入账本（跨 Attempt 去重）。
			MessageID: seed.assistantMessage.ID, ConversationID: seed.conversation.ID, UserID: agentGroupRetryUserID,
			RunID: domainagentgroup.BillingRef(seed.run.ID, seed.workerStep.ID, 1), EventScope: "tool_call",
			EventID: "tc-wkr-ok", EventType: "web_search", ToolName: "web_search", Status: "success",
			InputJSON: `{"query":"天气"}`, OutputJSON: `{"result":"晴"}`, StartedAt: now,
		},
		{
			// 可重试步骤失败的调用 → 不得进入账本。
			MessageID: seed.assistantMessage.ID, ConversationID: seed.conversation.ID, UserID: agentGroupRetryUserID,
			RunID: domainagentgroup.BillingRef(seed.run.ID, seed.workerStep.ID, 1), EventScope: "tool_call",
			EventID: "tc-wkr-err", EventType: "web_search", ToolName: "web_search", Status: "error",
			InputJSON: `{"query":"错误"}`, StartedAt: now,
		},
		{
			// 成功步骤（S1）的调用 → 不得进入账本（成功步骤不重试）。
			MessageID: seed.assistantMessage.ID, ConversationID: seed.conversation.ID, UserID: agentGroupRetryUserID,
			RunID: domainagentgroup.BillingRef(seed.run.ID, seed.supervisorStep.ID, 1), EventScope: "tool_call",
			EventID: "tc-sup-ok", EventType: "code_review", ToolName: "code_review", Status: "success",
			InputJSON: `{"file":"a.go"}`, OutputJSON: `{"ok":true}`, StartedAt: now,
		},
	}
	for i := range toolRows {
		if err := db.Create(&toolRows[i]).Error; err != nil {
			t.Fatalf("create tool row: %v", err)
		}
	}

	service := newAgentGroupRetryTestService(t, db)
	run, err := service.agentGroupRunStore.GetAgentGroupRunByPublicID(context.Background(), agentGroupRetryUserID, seed.run.PublicID)
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	steps, err := service.agentGroupRunStore.ListStepsByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("load steps: %v", err)
	}
	st, err := service.buildAgentGroupRunResumeState(context.Background(), run,
		RetryAgentGroupRunInput{UserID: agentGroupRetryUserID}, steps)
	if err != nil {
		t.Fatalf("build resume state: %v", err)
	}

	if _, ok := st.ledger.lookup("web_search", `{"query":"天气"}`); !ok {
		t.Fatalf("successful tool call of retryable step must be in ledger")
	}
	if _, ok := st.ledger.lookup("web_search", `{"query":"错误"}`); ok {
		t.Fatalf("failed tool call of retryable step must not be in ledger")
	}
	if _, ok := st.ledger.lookup("code_review", `{"file":"a.go"}`); ok {
		t.Fatalf("tool call of succeeded step must not be in ledger")
	}
}

// 验收 8：租约恢复——崩溃/重启后，租约过期的 running Attempt 转为 interrupted，
// 对应运行回到 paused_retryable 并指向被中断步骤；未过期的 Attempt 不受影响。
func TestAgentGroupLeaseRecoveryPausesRunAtInterruptedStep(t *testing.T) {
	db := openAgentGroupRetryTestDB(t)
	now := time.Now()

	group := &persistencemodels.AgentGroup{
		UserID: agentGroupRetryUserID, PublicID: "group-lease-1", ProjectID: 1, Name: "g", Status: "active",
	}
	if err := db.Create(group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	type leaseRun struct {
		runID, stepID, attemptID uint
	}
	createRunningRun := func(publicID string, attemptLease time.Time) leaseRun {
		run := &persistencemodels.AgentGroupRun{
			PublicID: publicID, ClientRunID: publicID, UserID: agentGroupRetryUserID, ConversationID: 1,
			GroupID: group.ID, ConfigSnapshotJSON: "{}", Status: domainagentgroup.RunStatusRunning,
			StateVersion: 1, StartedAt: now.Add(-2 * time.Hour),
		}
		if err := db.Create(run).Error; err != nil {
			t.Fatalf("create run: %v", err)
		}
		step := &persistencemodels.AgentGroupStep{
			PublicID: "step-" + publicID, GroupRunID: run.ID, Sequence: 1,
			StepType: domainagentgroup.StepTypeMemberExecute, ActorMemberPublicID: "m-" + publicID,
			Status: domainagentgroup.StepStatusRunning,
		}
		if err := db.Create(step).Error; err != nil {
			t.Fatalf("create step: %v", err)
		}
		attempt := &persistencemodels.AgentGroupStepAttempt{
			PublicID: "att-" + publicID, StepID: step.ID, AttemptNo: 1, ChildRunID: publicID,
			RetryRequestID: "rr-" + publicID, BillingRef: "lease:1:1", Status: domainagentgroup.AttemptStatusRunning,
			LeaseExpiresAt: &attemptLease, StartedAt: now.Add(-2 * time.Hour),
		}
		if err := db.Create(attempt).Error; err != nil {
			t.Fatalf("create attempt: %v", err)
		}
		return leaseRun{runID: run.ID, stepID: step.ID, attemptID: attempt.ID}
	}

	expired := now.Add(-time.Hour)
	expiredRun := createRunningRun("run-lease-expired", expired)
	liveRun := createRunningRun("run-lease-live", now.Add(time.Hour))

	store := postgresagentgroup.NewRepo(db)
	recovered, err := store.RecoverExpiredAttemptLeases(context.Background(), now)
	if err != nil {
		t.Fatalf("recover leases: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered runs = %d, want 1", recovered)
	}

	var expiredAttempt persistencemodels.AgentGroupStepAttempt
	if err := db.First(&expiredAttempt, expiredRun.attemptID).Error; err != nil {
		t.Fatalf("load expired attempt: %v", err)
	}
	if expiredAttempt.Status != domainagentgroup.AttemptStatusInterrupted {
		t.Fatalf("expired attempt status = %q, want interrupted", expiredAttempt.Status)
	}
	var expiredRunRow persistencemodels.AgentGroupRun
	if err := db.First(&expiredRunRow, expiredRun.runID).Error; err != nil {
		t.Fatalf("load expired run: %v", err)
	}
	if expiredRunRow.Status != domainagentgroup.RunStatusPausedRetryable {
		t.Fatalf("expired run status = %q, want paused_retryable", expiredRunRow.Status)
	}
	if expiredRunRow.RetryableStepID == nil || *expiredRunRow.RetryableStepID != expiredRun.stepID {
		t.Fatalf("expired run retryable step = %v, want %d", expiredRunRow.RetryableStepID, expiredRun.stepID)
	}

	var liveAttempt persistencemodels.AgentGroupStepAttempt
	if err := db.First(&liveAttempt, liveRun.attemptID).Error; err != nil {
		t.Fatalf("load live attempt: %v", err)
	}
	if liveAttempt.Status != domainagentgroup.AttemptStatusRunning {
		t.Fatalf("live attempt status = %q, want running", liveAttempt.Status)
	}
	var liveRunRow persistencemodels.AgentGroupRun
	if err := db.First(&liveRunRow, liveRun.runID).Error; err != nil {
		t.Fatalf("load live run: %v", err)
	}
	if liveRunRow.Status != domainagentgroup.RunStatusRunning {
		t.Fatalf("live run status = %q, want running", liveRunRow.Status)
	}
}
