// Package artifact 定义用户保存的制品（AI 生成的 HTML/JS/CSS/文本）及其公开分享。
package artifact

import "time"

// Artifact 用户保存的制品。
type Artifact struct {
	ID               uint
	ArtifactPublicID string
	UserID           uint
	ConversationID   uint
	MessageID        uint
	Kind             string
	Title            string
	Code             string
	Thumbnail        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ArtifactShare 制品公开分享（分享行内保存标题快照；代码内容实时引用制品）。
type ArtifactShare struct {
	ID            uint
	ShareID       string
	ArtifactID    uint
	UserID        uint
	TitleSnapshot string
	Status        string
	RevokedAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
