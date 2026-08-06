package agentgroup

// CreateGroupInput 创建群组请求。
type CreateGroupInput struct {
	Name               string
	Description        string
	CoordinationPrompt string
	Supervisor         MemberCreateInput
	Workers            []MemberCreateInput
}

// MemberCreateInput 添加成员请求。
type MemberCreateInput struct {
	RolePublicID    string
	MemberType      string
	ModelOverride   string
	ReasoningEffort string
	DutyInstruction string
}

// UpdateGroupInput 更新群组请求。
type UpdateGroupInput struct {
	Name               *string
	Description        *string
	CoordinationPrompt *string
	SortOrder          *int
}

// UpdateMemberInput 更新成员请求。
type UpdateMemberInput struct {
	Enabled         *bool
	ModelOverride   *string
	ReasoningEffort *string
	DutyInstruction *string
	SortOrder       *int
}
