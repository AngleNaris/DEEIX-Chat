package agentgroup

import "errors"

var (
	// ErrAgentGroupNotFound 群组不存在或无权限。
	ErrAgentGroupNotFound = errors.New("agent group not found")

// ErrAgentGroupProjectNotFound 项目不存在或不属于当前用户。
ErrAgentGroupProjectNotFound = errors.New("agent group project not found")
	// ErrAgentGroupFeatureDisabled Agent 群组功能未启用。
	ErrAgentGroupFeatureDisabled = errors.New("agent group feature disabled")
	// ErrInvalidAgentGroupName 群组名称不合法。
	ErrInvalidAgentGroupName = errors.New("invalid agent group name")
	// ErrInvalidAgentGroupDescription 群组描述不合法。
	ErrInvalidAgentGroupDescription = errors.New("invalid agent group description")
	// ErrInvalidCoordinationPrompt 协调提示词不合法。
	ErrInvalidCoordinationPrompt = errors.New("invalid coordination prompt")
	// ErrInvalidDutyInstruction 职责说明不合法。
	ErrInvalidDutyInstruction = errors.New("invalid duty instruction")
	// ErrInvalidAgentGroupMemberType 成员类型不合法。
	ErrInvalidAgentGroupMemberType = errors.New("invalid agent group member type")
	// ErrAgentGroupSupervisorRequired 创建群组必须指定主管。
	ErrAgentGroupSupervisorRequired = errors.New("agent group supervisor required")
	// ErrAgentGroupSupervisorDuplicate 主管角色重复添加。
	ErrAgentGroupSupervisorDuplicate = errors.New("agent group supervisor duplicate")
	// ErrAgentGroupMemberDuplicate 成员角色重复添加。
	ErrAgentGroupMemberDuplicate = errors.New("agent group member duplicate")
	// ErrAgentGroupMemberLimitExceeded 成员数量超限。
	ErrAgentGroupMemberLimitExceeded = errors.New("agent group member limit exceeded")
	// ErrAgentGroupRoleNotFound 角色不存在或无权限。
	ErrAgentGroupRoleNotFound = errors.New("agent group role not found")
	// ErrAgentGroupSupervisorNotRemovable 主管不可直接移除（须先更换主管）。
	ErrAgentGroupSupervisorNotRemovable = errors.New("agent group supervisor not removable")
	// ErrAgentGroupSupervisorProtected 主管成员受保护（不可禁用、移除或作为工作成员重复添加）。
	ErrAgentGroupSupervisorProtected = errors.New("agent group supervisor protected")
	// ErrInvalidAgentGroupModelOverride 模型覆盖不合法。
	ErrInvalidAgentGroupModelOverride = errors.New("invalid agent group model override")
	// ErrAgentGroupInvalidMemberOrder 成员排序输入不合法。
	ErrAgentGroupInvalidMemberOrder = errors.New("invalid agent group member order")
	// ErrAgentGroupHistoryExists 群组存在会话或运行历史，不可删除。
	ErrAgentGroupHistoryExists = errors.New("agent group history exists")
	// ErrAgentGroupRunNotFound 群组运行不存在或无权限。
	ErrAgentGroupRunNotFound = errors.New("agent group run not found")
	// ErrAgentGroupDuplicateRun 同一会话重复提交同一运行（client_run_id 冲突）。
	ErrAgentGroupDuplicateRun = errors.New("duplicate agent group run")
	// ErrAgentGroupRunActive 会话存在未结束的群组运行。
	ErrAgentGroupRunActive = errors.New("agent group run active")
)
