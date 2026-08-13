package agentgroup

import (
	"strconv"
	"strings"
	"time"
)

// 群组状态。
const (
	GroupStatusActive   = "active"
	GroupStatusArchived = "archived"
)

// 成员类型。
const (
	MemberTypeSupervisor = "supervisor"
	MemberTypeWorker     = "worker"
)

// 群组运行状态（chat_agent_group_runs.status）。
const (
	RunStatusPending         = "pending"
	RunStatusRunning         = "running"
	RunStatusPausedRetryable = "paused_retryable"
	RunStatusBlocked         = "blocked"
	RunStatusCompleted       = "completed"
	RunStatusAbandoned       = "abandoned"
)

// 逻辑步骤类型（chat_agent_group_steps.step_type）。
const (
	StepTypeSupervisorDecide = "supervisor_decide"
	StepTypeMemberExecute    = "member_execute"
)

// 逻辑步骤状态（chat_agent_group_steps.status）。
const (
	StepStatusPending     = "pending"
	StepStatusRunning     = "running"
	StepStatusInterrupted = "interrupted"
	StepStatusSuccess     = "success"
	StepStatusFailed      = "failed"
	StepStatusCanceled    = "canceled"
)

// Attempt 状态（chat_agent_group_step_attempts.status）。
const (
	AttemptStatusPending     = "pending"
	AttemptStatusRunning     = "running"
	AttemptStatusSuccess     = "success"
	AttemptStatusError       = "error"
	AttemptStatusInterrupted = "interrupted"
	AttemptStatusCanceled    = "canceled"
)

// 工具执行语义（MCP 与内置工具共用）。
const (
	ToolSemanticsReadOnly      = "read_only"
	ToolSemanticsIdempotent    = "idempotent"
	ToolSemanticsSideEffecting = "side_effecting"
	ToolSemanticsUnknown       = "unknown"
)

// 运行限制配置项命名空间与键（通过运行时系统设置动态覆盖）。
const (
	FeatureFlagNamespace  = "agent_group"
	FeatureFlagKeyEnabled = "enabled"

	SettingKeyMaxStepsPerRun      = "max_steps_per_run"
	SettingKeyMaxAttemptsPerStep  = "max_attempts_per_step"
	SettingKeyAttemptLeaseSeconds = "attempt_lease_seconds"
)

// 输入长度限制（按 rune 计，与角色/项目字段口径一致）。
const (
	MaxGroupNameRunes          = 80
	MaxGroupDescriptionRunes   = 255
	MaxCoordinationPromptRunes = 12000
	MaxDutyInstructionRunes    = 4000
	MaxGroupMembersPerGroup    = 32
)

// 运行与重试限制（可在运行时通过系统设置覆盖）。
const (
	DefaultMaxStepsPerRun     = 64
	DefaultMaxAttemptsPerStep = 8
	DefaultAttemptLease       = 10 * time.Minute
)

// 整体错误码（chat_agent_group_runs.error_code / Attempt.error_code）。
const (
	ErrorCodeFeatureDisabled       = "FEATURE_DISABLED"
	ErrorCodeStepLimitExceeded     = "STEP_LIMIT_EXCEEDED"
	ErrorCodeAttemptLimitExceeded  = "ATTEMPT_LIMIT_EXCEEDED"
	ErrorCodeInvalidMemberSchedule = "INVALID_MEMBER_SCHEDULE"
	ErrorCodeToolSideEffectUnknown = "TOOL_SIDE_EFFECT_UNKNOWN"
	ErrorCodeUpstreamRetryable     = "UPSTREAM_RETRYABLE"
	ErrorCodeUpstreamFatal         = "UPSTREAM_FATAL"
	ErrorCodeInterrupted           = "INTERRUPTED"
	ErrorCodeCanceled              = "CANCELED"
	ErrorCodeCASConflict           = "CAS_CONFLICT"
)

// Group 表示项目内 Agent 群组。
type Group struct {
	ID                 uint
	UserID             uint
	ProjectID          uint
	ProjectPublicID    string
	ProjectName        string
	PublicID           string
	Name               string
	Description        string
	CoordinationPrompt string
	SupervisorMemberID uint
	SortOrder          int
	Status             string
	Revision           int
	Members            []Member
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// GroupPatch 表示群组的局部更新。
type GroupPatch struct {
	Name               *string
	Description        *string
	CoordinationPrompt *string
	SortOrder          *int
	Status             *string
}

// Member 表示群组成员（角色与群组的关联）。
type Member struct {
	ID            uint
	PublicID      string
	GroupID       uint
	RoleID        uint
	RolePublicID  string
	RoleName      string
	RoleIcon      string
	RoleColor     string
	RoleModel     string
	RoleProvider  string
	MemberType    string
	Enabled       bool
	ModelOverride string
	// ReasoningEffort 是思考强度语义档位（""/low/medium/high/xhigh/max），空串=继承用户全局默认。
	ReasoningEffort string
	DutyInstruction string
	SortOrder       int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// MemberCreate 表示添加成员请求。
type MemberCreate struct {
	RolePublicID    string
	MemberType      string
	ModelOverride   string
	ReasoningEffort string
	DutyInstruction string
}

// MemberPatch 表示成员的局部更新。
type MemberPatch struct {
	Enabled         *bool
	ModelOverride   *string
	ReasoningEffort *string
	DutyInstruction *string
	SortOrder       *int
}

// SupervisorChange 表示更换主管请求。
type SupervisorChange struct {
	MemberPublicID string
}

// Run 表示一条用户消息触发的完整群组执行（chat_agent_group_runs）。
type Run struct {
	ID                  uint
	PublicID            string
	ClientRunID         string
	UserID              uint
	ConversationID      uint
	GroupID             uint
	GroupPublicID       string
	UserMessageID       uint
	AssistantMessageID  *uint
	GroupRevision       int
	ConfigSnapshotJSON  string
	Status              string
	CurrentStepID       *uint
	LastCompletedStepID *uint
	RetryableStepID     *uint
	StateVersion        int
	ErrorCode           string
	ErrorMessage        string
	StartedAt           time.Time
	EndedAt             *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Step 表示一个不可变的逻辑步骤（chat_agent_group_steps）。
type Step struct {
	ID                  uint
	PublicID            string
	GroupRunID          uint
	Sequence            int
	StepType            string
	ActorMemberPublicID string
	ActorNameSnapshot   string
	ActorTypeSnapshot   string
	Instruction         string
	Status              string
	SuccessfulAttemptID *uint
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Attempt 表示一次实际模型执行（chat_agent_group_step_attempts）。
type Attempt struct {
	ID                    uint
	PublicID              string
	StepID                uint
	AttemptNo             int
	ChildRunID            string
	RetryRequestID        string
	RequestedModel        string
	ResolvedModel         string
	InputSnapshotJSON     string
	ContextFingerprint    string
	OutputMarkdown        string
	PartialOutputMarkdown string
	Status                string
	ErrorCode             string
	ErrorMessage          string
	BillingRef            string
	LeaseExpiresAt        *time.Time
	StartedAt             time.Time
	EndedAt               *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// RunDetail 运行及其步骤、尝试的完整视图。
type RunDetail struct {
	Run   Run
	Steps []StepDetail
}

// StepDetail 步骤及其尝试历史。
type StepDetail struct {
	Step     Step
	Attempts []Attempt
}

// RunPatch 表示运行检查点的局部更新（配合 CAS 乐观锁使用）。
// 指针字段仅在非 nil 时写入；StateVersion 由服务端递增后传入。
type RunPatch struct {
	Status              *string
	CurrentStepID       *uint
	LastCompletedStepID *uint
	RetryableStepID     *uint
	ClearRetryableStep  bool
	AssistantMessageID  *uint
	ConfigSnapshotJSON  *string
	ErrorCode           *string
	ErrorMessage        *string
	EndedAt             *time.Time
	StateVersion        *int
}

// AttemptPatch 表示 Attempt 检查点的局部更新（配合 CAS 乐观锁使用）。
type AttemptPatch struct {
	Status                *string
	ChildRunID            *string
	ResolvedModel         *string
	OutputMarkdown        *string
	PartialOutputMarkdown *string
	InputSnapshotJSON     *string
	ErrorCode             *string
	ErrorMessage          *string
	ContextFingerprint    *string
	BillingRef            *string
	LeaseExpiresAt        *time.Time
	EndedAt               *time.Time
}

// ResolveEffectiveModel 返回成员的最终有效模型。
// 优先级：群组成员 model_override > 角色默认模型 > 平台默认模型。
func ResolveEffectiveModel(roleDefaultModel, modelOverride, platformDefaultModel string) string {
	if model := strings.TrimSpace(modelOverride); model != "" {
		return model
	}
	if model := strings.TrimSpace(roleDefaultModel); model != "" {
		return model
	}
	return strings.TrimSpace(platformDefaultModel)
}

// IsValidMemberType 校验成员类型枚举。
func IsValidMemberType(value string) bool {
	return value == MemberTypeSupervisor || value == MemberTypeWorker
}

// IsValidRunStatus 校验运行状态枚举。
func IsValidRunStatus(value string) bool {
	switch value {
	case RunStatusPending, RunStatusRunning, RunStatusPausedRetryable,
		RunStatusBlocked, RunStatusCompleted, RunStatusAbandoned:
		return true
	}
	return false
}

// IsValidAttemptStatus 校验 Attempt 状态枚举。
func IsValidAttemptStatus(value string) bool {
	switch value {
	case AttemptStatusPending, AttemptStatusRunning, AttemptStatusSuccess,
		AttemptStatusError, AttemptStatusInterrupted, AttemptStatusCanceled:
		return true
	}
	return false
}

// BillingRefStepPrefix 返回逻辑步骤全部 Attempt 的计费引用前缀：groupRunID:stepID:。
// 用于按前缀恢复同一步骤全部尝试（AttemptNo 从 1 开始）的持久化工具行。
func BillingRefStepPrefix(groupRunID, stepID uint) string {
	return strconv.FormatUint(uint64(groupRunID), 10) + ":" +
		strconv.FormatUint(uint64(stepID), 10) + ":"
}

// BillingRef 返回 Attempt 的计费幂等引用：groupRunID:stepID:attemptNo。
func BillingRef(groupRunID, stepID uint, attemptNo int) string {
	return BillingRefStepPrefix(groupRunID, stepID) + strconv.Itoa(attemptNo)
}
