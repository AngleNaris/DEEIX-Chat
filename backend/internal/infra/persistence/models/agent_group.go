package model

import "time"

// AgentGroup 存储 Agent 群组配置（chat_agent_groups）。
// 群组已从项目绑定中拆除（§C1），ProjectID 仅保留历史字段，0=未绑定。
type AgentGroup struct {
	BaseModel
	UserID             uint   `gorm:"not null;index:idx_chat_agent_groups_user_id;comment:用户ID"`
	PublicID           string `gorm:"size:32;not null;default:'';uniqueIndex:idx_chat_agent_groups_public_id;comment:公开群组ID"`
	ProjectID          uint   `gorm:"index:idx_chat_agent_groups_project_id;comment:所属项目ID(0=未绑定)"`
	Name               string `gorm:"size:80;not null;default:'';comment:群组名称"`
	Description        string `gorm:"size:255;not null;default:'';comment:群组描述"`
	CoordinationPrompt string `gorm:"type:text;not null;default:'';comment:仅提供给主管的群组协调提示词"`
	SupervisorMemberID uint   `gorm:"not null;default:0;comment:当前主管成员ID"`
	SortOrder          int    `gorm:"not null;default:0;index:idx_chat_agent_groups_sort_order;comment:项目内排序"`
	Status             string `gorm:"size:32;not null;default:'active';index:idx_chat_agent_groups_status;comment:群组状态(active/archived)"`
	Revision           int    `gorm:"not null;default:0;comment:配置版本"`
}

// TableName 指定表名。
func (AgentGroup) TableName() string {
	return "chat_agent_groups"
}

// AgentGroupMember 存储群组成员关联（chat_agent_group_members）。
// 成员移除只删除关联，不删除角色；历史运行依赖运行快照。
type AgentGroupMember struct {
	BaseModel
	PublicID        string `gorm:"size:32;not null;default:'';uniqueIndex:idx_chat_agent_group_members_public_id;comment:公开成员ID"`
	GroupID         uint   `gorm:"not null;index:idx_chat_agent_group_members_group_id;uniqueIndex:idx_chat_agent_group_members_group_role;comment:群组ID"`
	RoleID          uint   `gorm:"not null;uniqueIndex:idx_chat_agent_group_members_group_role;comment:角色ID"`
	MemberType      string `gorm:"size:16;not null;default:'worker';comment:成员类型(supervisor/worker)"`
	Enabled         bool   `gorm:"not null;default:true;comment:是否允许主管调度"`
	ModelOverride   string `gorm:"size:128;not null;default:'';comment:当前群组内的模型覆盖"`
	ReasoningEffort string `gorm:"size:16;not null;default:'';comment:思考强度(low/medium/high/xhigh/max，空=继承用户全局默认)"`
	DutyInstruction string `gorm:"type:text;not null;default:'';comment:当前群组内的职责说明"`
	SortOrder       int    `gorm:"not null;default:0;comment:成员排序"`
}

// TableName 指定表名。
func (AgentGroupMember) TableName() string {
	return "chat_agent_group_members"
}

// AgentGroupRun 存储一条用户消息触发的完整群组执行（chat_agent_group_runs）。
type AgentGroupRun struct {
	BaseModel
	PublicID            string     `gorm:"size:32;not null;default:'';uniqueIndex:idx_chat_agent_group_runs_public_id;comment:公开运行ID"`
	ClientRunID         string     `gorm:"size:128;not null;default:'';uniqueIndex:idx_chat_agent_group_runs_client_run_id;comment:父流式运行ID"`
	UserID              uint       `gorm:"not null;index:idx_chat_agent_group_runs_user_id;comment:用户ID"`
	ConversationID      uint       `gorm:"not null;index:idx_chat_agent_group_runs_conversation_id;comment:会话ID"`
	GroupID             uint       `gorm:"not null;index:idx_chat_agent_group_runs_group_id;comment:群组ID"`
	UserMessageID       uint       `gorm:"not null;comment:用户消息ID"`
	AssistantMessageID  *uint      `gorm:"comment:完成后生成的最终主管消息ID"`
	GroupRevision       int        `gorm:"not null;default:0;comment:群组配置版本"`
	ConfigSnapshotJSON  string     `gorm:"type:text;not null;default:'';comment:项目、群组、成员和模型快照JSON"`
	Status              string     `gorm:"size:32;not null;default:'pending';index:idx_chat_agent_group_runs_status;comment:运行状态"`
	CurrentStepID       *uint      `gorm:"comment:当前步骤ID"`
	LastCompletedStepID *uint      `gorm:"comment:最后成功步骤ID"`
	RetryableStepID     *uint      `gorm:"comment:当前可重试步骤ID"`
	StateVersion        int        `gorm:"not null;default:0;comment:CAS版本"`
	ErrorCode           string     `gorm:"size:64;not null;default:'';comment:整体错误码"`
	ErrorMessage        string     `gorm:"type:text;not null;default:'';comment:整体错误信息"`
	StartedAt           time.Time  `gorm:"not null;comment:开始时间"`
	EndedAt             *time.Time `gorm:"comment:结束时间"`
}

// TableName 指定表名。
func (AgentGroupRun) TableName() string {
	return "chat_agent_group_runs"
}

// AgentGroupStep 存储一个不可变的逻辑步骤（chat_agent_group_steps）。
type AgentGroupStep struct {
	BaseModel
	PublicID            string `gorm:"size:32;not null;default:'';uniqueIndex:idx_chat_agent_group_steps_public_id;comment:公开步骤ID"`
	GroupRunID          uint   `gorm:"not null;uniqueIndex:idx_chat_agent_group_steps_run_sequence;index:idx_chat_agent_group_steps_group_run_id;comment:所属运行ID"`
	Sequence            int    `gorm:"not null;default:0;uniqueIndex:idx_chat_agent_group_steps_run_sequence;comment:串行步骤编号"`
	StepType            string `gorm:"size:32;not null;default:'';comment:步骤类型(supervisor_decide/member_execute)"`
	ActorMemberPublicID string `gorm:"size:32;not null;default:'';comment:Actor成员快照ID"`
	ActorNameSnapshot   string `gorm:"size:80;not null;default:'';comment:Actor名称快照"`
	ActorTypeSnapshot   string `gorm:"size:16;not null;default:'';comment:主管或成员"`
	Instruction         string `gorm:"type:text;not null;default:'';comment:主管指令或当前步骤任务"`
	Status              string `gorm:"size:32;not null;default:'pending';comment:逻辑步骤状态"`
	SuccessfulAttemptID *uint  `gorm:"comment:成功Attempt ID"`
}

// TableName 指定表名。
func (AgentGroupStep) TableName() string {
	return "chat_agent_group_steps"
}

// AgentGroupStepAttempt 存储一次实际模型执行（chat_agent_group_step_attempts）。
// 重试增加新 Attempt，不覆盖旧 Attempt。
type AgentGroupStepAttempt struct {
	BaseModel
	PublicID              string     `gorm:"size:32;not null;default:'';uniqueIndex:idx_chat_agent_group_attempts_public_id;comment:公开Attempt ID"`
	StepID                uint       `gorm:"not null;uniqueIndex:idx_chat_agent_group_attempts_step_attempt_no;index:idx_chat_agent_group_attempts_step_id;comment:所属逻辑步骤ID"`
	AttemptNo             int        `gorm:"not null;default:0;uniqueIndex:idx_chat_agent_group_attempts_step_attempt_no;comment:尝试序号"`
	ChildRunID            string     `gorm:"size:128;not null;default:'';index:idx_chat_agent_group_attempts_child_run_id;comment:对应 ConversationRun.RunID"`
	RetryRequestID        string     `gorm:"size:128;not null;default:'';index:idx_chat_agent_group_attempts_retry_request_id_v2;comment:重试幂等键"`
	RequestedModel        string     `gorm:"size:128;not null;default:'';comment:请求模型快照"`
	ResolvedModel         string     `gorm:"size:128;not null;default:'';comment:实际模型快照"`
	InputSnapshotJSON     string     `gorm:"type:text;not null;default:'';comment:当前步骤输入"`
	ContextFingerprint    string     `gorm:"size:64;not null;default:'';comment:上下文指纹"`
	OutputMarkdown        string     `gorm:"type:text;not null;default:'';comment:成员或主管输出"`
	PartialOutputMarkdown string     `gorm:"type:text;not null;default:'';comment:中断前的部分输出"`
	ThinkMarkdown         string     `gorm:"type:text;not null;default:'';comment:思维过程Markdown"`
	ToolCallsJSON         string     `gorm:"type:text;not null;default:'';comment:工具调用JSON快照"`
	Status                string     `gorm:"size:32;not null;default:'pending';index:idx_chat_agent_group_attempts_status;comment:Attempt状态"`
	ErrorCode             string     `gorm:"size:64;not null;default:'';comment:错误码"`
	ErrorMessage          string     `gorm:"type:text;not null;default:'';comment:错误信息"`
	BillingRef            string     `gorm:"size:128;not null;default:'';comment:Attempt计费幂等引用"`
	LeaseExpiresAt        *time.Time `gorm:"index:idx_chat_agent_group_attempts_lease;comment:运行租约"`
	StartedAt             time.Time  `gorm:"not null;comment:开始时间"`
	EndedAt               *time.Time `gorm:"comment:结束时间"`
}

// TableName 指定表名。
func (AgentGroupStepAttempt) TableName() string {
	return "chat_agent_group_step_attempts"
}
