package model

// DocCard 文档卡片：用户或 AI 创建的关键字触发文档（lorebook 式）。
type DocCard struct {
	BaseModel
	CardPublicID string `gorm:"size:32;not null;default:'';uniqueIndex:idx_chat_doc_cards_public_id;comment:公开卡片ID"`
	UserID       uint   `gorm:"not null;default:0;index:idx_chat_doc_cards_user_id;comment:用户ID"`
	Title        string `gorm:"size:128;not null;default:'';comment:卡片标题"`
	Content      string `gorm:"type:text;not null;default:'';comment:卡片内容"`
	KeywordsJSON string `gorm:"type:text;not null;default:'[]';comment:触发关键字JSON"`
	Enabled      bool   `gorm:"not null;default:true;index:idx_chat_doc_cards_enabled;comment:是否启用"`
	UpdatedBy    string `gorm:"size:16;not null;default:'user';comment:更新方(user/ai)"`
}

// TableName 指定表名。
func (DocCard) TableName() string {
	return "chat_doc_cards"
}
