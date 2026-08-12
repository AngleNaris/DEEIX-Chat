package model

// Credential 存储用户凭据（chat_credentials）。
// 密钥值只以加密形式落库（SecretEnc，secretbox AES-256-GCM），
// 读取/列表/分享路径一律不返回明文。
type Credential struct {
	BaseModel
	UserID      uint   `gorm:"not null;index:idx_chat_credentials_user_id;comment:用户ID"`
	PublicID    string `gorm:"size:32;not null;default:'';uniqueIndex:idx_chat_credentials_public_id;comment:公开凭据ID"`
	Name        string `gorm:"size:64;not null;default:'';uniqueIndex:idx_chat_credentials_user_name;comment:凭据名称(用户内唯一)"`
	Type        string `gorm:"size:16;not null;default:'generic';comment:类型(ssh/api_key/generic)"`
	Description string `gorm:"size:255;not null;default:'';comment:描述(模型可见)"`
	SecretEnc   string `gorm:"type:text;not null;default:'';comment:加密后的密钥值"`
	MetaJSON    string `gorm:"type:text;not null;default:'';comment:非敏感元数据JSON"`
}

// TableName 指定表名。
func (Credential) TableName() string {
	return "chat_credentials"
}
