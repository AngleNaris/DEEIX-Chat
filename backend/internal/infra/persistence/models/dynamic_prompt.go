package model

// DynamicPrompt 动态提示词：用户创建的命名脚本/文本片段，提示词中
// 以 {{script: name}} 引用，发送时展开（js 沙箱执行 / text 直接插入）。
type DynamicPrompt struct {
	BaseModel
	PublicID  string `gorm:"size:32;not null;default:'';uniqueIndex:idx_chat_dynamic_prompts_public_id;comment:公开ID"`
	UserID    uint   `gorm:"not null;default:0;index:idx_chat_dynamic_prompts_user_id;comment:用户ID"`
	Name      string `gorm:"size:64;not null;default:'';uniqueIndex:idx_chat_dynamic_prompts_user_name,priority:2;comment:名称(用户内唯一)"`
	Kind      string `gorm:"size:8;not null;default:'js';comment:类型(js/text)"`
	Content   string `gorm:"type:text;not null;default:'';comment:内容(js 源码或文本)"`
	Enabled   bool   `gorm:"not null;default:true;index:idx_chat_dynamic_prompts_enabled;comment:是否启用"`
	UpdatedBy string `gorm:"size:16;not null;default:'user';comment:更新方(user/ai)"`
}

// TableName 指定表名。
func (DynamicPrompt) TableName() string {
	return "chat_dynamic_prompts"
}
