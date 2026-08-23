package skill

import "time"

const (
	// ScopeBuiltin 表示管理员维护的全局内置技能。
	ScopeBuiltin = "builtin"
	// ScopeUser 表示用户维护的个人自定义技能。
	ScopeUser = "user"

	// PackageTypeText 表示纯文本技能（仅 SKILL.md 内容）。
	PackageTypeText = ""
	// PackageTypePackage 表示标准 skill 包（zip 解压，SKILL.md + 引用文件）。
	PackageTypePackage = "package"

	// FileKindText 表示文本文件，内容可提供给模型。
	FileKindText = "text"
	// FileKindBinary 表示二进制文件，仅清单列出。
	FileKindBinary = "binary"
)

// PackageFile 描述技能包内的一个引用文件。
type PackageFile struct {
	Path string
	Size int64
	Kind string
}

// Skill 表示可在会话中按需加载的 SKILL.md 能力包。
type Skill struct {
	ID                    uint
	Scope                 string
	OwnerUserID           uint
	Title                 string
	Trigger               string
	Description           string
	Markdown              string
	PackageType           string
	PackageRootDir        string
	PackageStorageVersion string
	PackageFiles          []PackageFile
	Enabled               bool
	SortOrder             int
	CreatedByUserID       uint
	UpdatedByUserID       uint
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// IsPackage 返回技能是否为文件包类型。
func (s Skill) IsPackage() bool {
	return s.PackageType == PackageTypePackage
}
