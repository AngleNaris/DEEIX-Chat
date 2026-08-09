package model

import "time"

// Artifact 用户保存的制品（AI 生成的 HTML/JS/CSS/文本代码），可公开分享。
type Artifact struct {
	BaseModel
	ArtifactPublicID string `gorm:"size:32;not null;default:'';uniqueIndex:idx_chat_artifacts_public_id;comment:公开制品ID"`
	UserID           uint   `gorm:"not null;default:0;index:idx_chat_artifacts_user_id;comment:用户ID"`
	ConversationID   uint   `gorm:"not null;default:0;index:idx_chat_artifacts_conversation_id;comment:来源会话ID"`
	MessageID        uint   `gorm:"not null;default:0;index:idx_chat_artifacts_message_id;comment:来源消息ID"`
	Kind             string `gorm:"size:16;not null;default:'text';index:idx_chat_artifacts_kind;comment:制品类型(html/js/css/text)"`
	Title            string `gorm:"size:255;not null;default:'';comment:制品标题"`
	Code             string `gorm:"type:text;not null;default:'';comment:制品代码"`
	Thumbnail        string `gorm:"type:text;not null;default:'';comment:静态缩略图(data URL)"`
}

// TableName 指定表名。
func (Artifact) TableName() string {
	return "chat_artifacts"
}

// ArtifactShare 制品公开分享记录（每制品一 active，分享时保存标题快照）。
type ArtifactShare struct {
	BaseModel
	ShareID       string     `gorm:"size:32;not null;default:'';uniqueIndex:idx_chat_artifact_shares_share_id;comment:公开分享ID"`
	ArtifactID    uint       `gorm:"not null;default:0;index:idx_chat_artifact_shares_artifact_id;comment:制品ID"`
	UserID        uint       `gorm:"not null;default:0;index:idx_chat_artifact_shares_user_id;comment:制品所有者ID"`
	TitleSnapshot string     `gorm:"size:255;not null;default:'';comment:分享时标题快照"`
	Status        string     `gorm:"size:32;not null;default:'active';index:idx_chat_artifact_shares_status;comment:分享状态(active/revoked)"`
	RevokedAt     *time.Time `gorm:"index:idx_chat_artifact_shares_revoked_at;comment:撤销时间"`
}

// TableName 指定表名。
func (ArtifactShare) TableName() string {
	return "chat_artifact_shares"
}
