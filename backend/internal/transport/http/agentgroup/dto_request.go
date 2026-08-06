package agentgroup

// CreateAgentGroupRequest 创建群组请求。
type CreateAgentGroupRequest struct {
	ProjectID          string                    `json:"projectID" binding:"required,max=32"`
	Name               string                    `json:"name" binding:"required,max=80"`
	Description        string                    `json:"description,omitempty" binding:"max=255"`
	CoordinationPrompt string                    `json:"coordinationPrompt,omitempty" binding:"max=12000"`
	Supervisor         AgentGroupMemberRequest   `json:"supervisor" binding:"required"`
	Workers            []AgentGroupMemberRequest `json:"workers,omitempty" binding:"max=31,dive"`
}

// AgentGroupMemberRequest 成员（主管/工作成员）参数。
type AgentGroupMemberRequest struct {
	RolePublicID    string `json:"rolePublicID" binding:"required,max=32"`
	ModelOverride   string `json:"modelOverride,omitempty" binding:"max=128"`
	ReasoningEffort string `json:"reasoningEffort,omitempty" binding:"omitempty,oneof= low medium high xhigh"`
	DutyInstruction string `json:"dutyInstruction,omitempty" binding:"max=4000"`
}

// UpdateAgentGroupRequest 更新群组请求。
type UpdateAgentGroupRequest struct {
	Name               *string `json:"name,omitempty" binding:"omitempty,max=80"`
	Description        *string `json:"description,omitempty" binding:"omitempty,max=255"`
	CoordinationPrompt *string `json:"coordinationPrompt,omitempty" binding:"omitempty,max=12000"`
	SortOrder          *int    `json:"sortOrder,omitempty" binding:"omitempty,min=0"`
}

// AddAgentGroupMemberRequest 添加工作成员请求。
type AddAgentGroupMemberRequest struct {
	RolePublicID    string `json:"rolePublicID" binding:"required,max=32"`
	ModelOverride   string `json:"modelOverride,omitempty" binding:"max=128"`
	ReasoningEffort string `json:"reasoningEffort,omitempty" binding:"omitempty,oneof= low medium high xhigh"`
	DutyInstruction string `json:"dutyInstruction,omitempty" binding:"max=4000"`
}

// UpdateAgentGroupMemberRequest 更新成员请求。
type UpdateAgentGroupMemberRequest struct {
	Enabled         *bool   `json:"enabled,omitempty"`
	ModelOverride   *string `json:"modelOverride,omitempty" binding:"omitempty,max=128"`
	ReasoningEffort *string `json:"reasoningEffort,omitempty" binding:"omitempty,oneof= low medium high xhigh"`
	DutyInstruction *string `json:"dutyInstruction,omitempty" binding:"omitempty,max=4000"`
	SortOrder       *int    `json:"sortOrder,omitempty" binding:"omitempty,min=0"`
}

// ReorderAgentGroupMembersRequest 重排成员请求。
type ReorderAgentGroupMembersRequest struct {
	OrderedPublicIDs []string `json:"orderedPublicIDs" binding:"required,max=32"`
}

// ChangeAgentGroupSupervisorRequest 更换主管请求。
type ChangeAgentGroupSupervisorRequest struct {
	MemberPublicID string `json:"memberPublicID" binding:"required,max=32"`
}

// AgentGroupStepRetryRequest 重试失败步骤请求。
// retryRequestID 为客户端幂等键：相同键不会创建重复 Attempt。
type AgentGroupStepRetryRequest struct {
	RetryRequestID string `json:"retryRequestID" binding:"omitempty,max=64"`
}
