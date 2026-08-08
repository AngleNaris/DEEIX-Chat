// Package dynamicprompt 定义动态提示词：用户创建的命名脚本/文本片段，
// 在提示词中以 {{script: name}} 引用，发送时按用户时区等上下文展开。
package dynamicprompt

import "time"

// Kind 动态提示词类型。
const (
	KindJS   = "js"   // JS 脚本：沙箱执行后插入结果
	KindText = "text" // 纯文本：直接插入内容
)

// ValidKind 判断类型是否合法。
func ValidKind(kind string) bool {
	return kind == KindJS || kind == KindText
}

// DynamicPrompt 动态提示词。
type DynamicPrompt struct {
	ID            uint
	PublicID      string
	UserID        uint
	Name          string
	Kind          string
	Content       string
	Enabled       bool
	UpdatedBy     string // user/ai
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
