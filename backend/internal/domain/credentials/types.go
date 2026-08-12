// Package credentials 提供用户凭据领域模型：命名凭据（SSH 连接信息、API key 等），
// 密钥以加密形式存储，模型上下文只出现凭据描述与 {{credential: name}} 占位符，
// 执行层在工具调用发送前展开为真实值（密钥不进入会话记录）。
package credentials

import "time"

// 凭据类型。
const (
	TypeSSH     = "ssh"
	TypeAPIKey  = "api_key"
	TypeGeneric = "generic"
)

// ValidType 校验凭据类型。
func ValidType(credentialType string) bool {
	switch credentialType {
	case TypeSSH, TypeAPIKey, TypeGeneric:
		return true
	default:
		return false
	}
}

// Credential 表示一条用户凭据（chat_credentials）。
type Credential struct {
	ID          uint
	UserID      uint
	PublicID    string
	Name        string // 用户内唯一；作为 {{credential: name}} 占位符名
	Type        string
	Description string // 模型可见的描述（何时使用该凭据）
	SecretEnc   string // 加密后的密钥值（secretbox AES-256-GCM）
	MetaJSON    string // 非敏感元数据（如 SSH host/port/username），JSON 字符串
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CredentialPatch 更新凭据的局部字段（nil 表示不修改，非 nil 表示覆盖）。
type CredentialPatch struct {
	Name        *string
	Type        *string
	Description *string
	SecretEnc   *string // 非 nil 且非空才更新（值加密后由 service 填充）
	MetaJSON    *string
}
