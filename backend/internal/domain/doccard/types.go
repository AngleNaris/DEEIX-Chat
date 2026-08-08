// Package doccard 定义文档卡片（lorebook 式关键字触发文档）。
package doccard

import "time"

// DocCard 文档卡片：用户或 AI 创建的文档片段，当用户消息命中关键字时
// 注入到模型上下文（与记忆的语义召回不同，卡片是精确关键字触发）。
type DocCard struct {
	ID           uint
	CardPublicID string
	UserID       uint
	Category     string
	ProjectID    *uint // 可选绑定项目（空=全局）
	RoleID       *uint // 可选绑定角色（空=全局）
	Title        string
	Content      string
	Keywords     []string
	Enabled      bool
	UpdatedBy    string // user/ai
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
