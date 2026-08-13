package agentgroup

// RunSnapshot 表示 GroupRun 创建时冻结的配置快照（config_snapshot_json）。
// 快照只保存恢复执行所需的稳定标识、公开配置和不可变参数，
// 不保存 API Key、OAuth Token、Cookie 或其他凭证。
// 领域层不携带 JSON 契约（分层约束），序列化由 application 层完成，
// Go 默认字段名与大小写不敏感反序列化保证持久化往返一致。
type RunSnapshot struct {
	Project    RunSnapshotProject
	Group      RunSnapshotGroup
	Supervisor RunSnapshotMember
	Members    []RunSnapshotMember
	Limits     RunSnapshotLimits
	// RequestedToolIDs / RequestedSkillIDs 是请求级工具与技能选择（重试恢复时还原执行能力）。
	RequestedToolIDs      []uint
	RequestedSkillIDs     []uint
	ActivatedMCPServerIDs []uint
}

// RunSnapshotProject 项目快照。
type RunSnapshotProject struct {
	ProjectID      uint
	PublicID       string
	Name           string
	SystemPrompt   string
	MCPDefaultMode string
	MCPToolIDs     []uint
	SkillIDs       []uint
}

// RunSnapshotGroup 群组快照。
type RunSnapshotGroup struct {
	GroupID            uint
	PublicID           string
	Name               string
	Revision           int
	CoordinationPrompt string
}

// RunSnapshotMember 成员快照（主管与成员共用）。
type RunSnapshotMember struct {
	MemberID         uint
	PublicID         string
	RoleID           uint
	RolePublicID     string
	RoleName         string
	RoleSystemPrompt string
	MemberType       string
	Enabled          bool
	Icon             string
	Color            string
	DutyInstruction  string
	// ReasoningEffort 是成员配置的思考强度档位（""/low/medium/high/xhigh/max），空串=继承用户全局默认。
	ReasoningEffort  string
	RoleDefaultModel string
	ModelOverride    string
	EffectiveModel   string
	Provider         string
}

// RunSnapshotLimits 运行限制快照。
type RunSnapshotLimits struct {
	MaxStepsPerRun     int
	MaxAttemptsPerStep int
}
