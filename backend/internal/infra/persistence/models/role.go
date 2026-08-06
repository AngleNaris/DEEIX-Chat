package model

// ConversationRole 存储用户角色(助手)配置。
// 角色 = 项目的全部能力 + 默认模型 + 图标。
type ConversationRole struct {
	BaseModel
	UserID         uint   `gorm:"not null;index:idx_chat_roles_user_id;comment:用户ID"`
	PublicID       string `gorm:"size:32;not null;default:'';uniqueIndex:idx_chat_roles_public_id;comment:公开角色ID"`
	Name           string `gorm:"size:80;not null;default:'';comment:角色名称"`
	Description    string `gorm:"size:255;not null;default:'';comment:角色描述"`
	SystemPrompt   string `gorm:"type:text;not null;default:'';comment:角色系统提示词"`
	Model          string `gorm:"size:128;not null;default:'';comment:默认模型"`
	Provider       string `gorm:"size:32;not null;default:'';comment:默认模型供应商"`
	ReasoningEffort string `gorm:"size:16;not null;default:'';comment:默认思考强度档位(low/medium/high/xhigh，空=继承用户全局默认)"`
	MCPDefaultMode string `gorm:"size:16;not null;default:'inherit';comment:MCP默认模式(inherit/custom)"`
	Color          string `gorm:"size:32;not null;default:'';comment:角色颜色"`
	Icon           string `gorm:"size:32;not null;default:'';comment:角色图标"`
	GroupName      string `gorm:"size:80;not null;default:'';comment:分组名称"`
	SortOrder      int    `gorm:"not null;default:0;index:idx_chat_roles_sort_order;comment:展示顺序"`
	Status         string `gorm:"size:32;not null;default:'active';index:idx_chat_roles_status;comment:角色状态(active/archived)"`
}

// TableName 指定表名。
func (ConversationRole) TableName() string {
	return "chat_roles"
}

// ConversationRoleMCPTool 记录角色默认启用的 MCP 工具。
type ConversationRoleMCPTool struct {
	RoleID     uint `gorm:"primaryKey;comment:角色ID"`
	ToolID     uint `gorm:"primaryKey;index:idx_role_mcp_tools_tool;comment:MCP工具ID"`
	SortOrder  int  `gorm:"not null;default:0;comment:角色内排序"`
}

// TableName 指定角色 MCP 工具关联表名。
func (ConversationRoleMCPTool) TableName() string {
	return "chat_role_mcp_tools"
}

// ConversationRoleSkill 记录角色默认启用的技能。
type ConversationRoleSkill struct {
	RoleID    uint `gorm:"primaryKey;comment:角色ID"`
	SkillID   uint `gorm:"primaryKey;index:idx_role_skills_skill;comment:技能ID"`
	SortOrder int  `gorm:"not null;default:0;comment:角色内排序"`
}

// TableName 指定角色技能关联表名。
func (ConversationRoleSkill) TableName() string {
	return "chat_role_skills"
}
