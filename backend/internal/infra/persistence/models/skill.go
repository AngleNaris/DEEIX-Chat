package model

// Skill 记录平台内置和用户自定义的 SKILL.md 能力包。
type Skill struct {
	ControlPlaneModel
	Scope                 string `gorm:"size:32;not null;default:'user';index:idx_skills_scope;uniqueIndex:idx_skills_scope_owner_trigger;comment:作用域(builtin/user)"`
	OwnerUserID           uint   `gorm:"not null;default:0;index:idx_skills_owner;uniqueIndex:idx_skills_scope_owner_trigger;comment:所属用户ID，内置技能为0"`
	Title                 string `gorm:"size:64;not null;default:'';comment:技能标题"`
	Trigger               string `gorm:"size:64;not null;default:'';index:idx_skills_trigger;uniqueIndex:idx_skills_scope_owner_trigger;comment:slash触发词，不含斜杠"`
	Description           string `gorm:"size:256;not null;default:'';comment:技能说明"`
	Markdown              string `gorm:"type:text;not null;default:'';comment:SKILL.md内容"`
	PackageType           string `gorm:"size:16;not null;default:'';comment:技能包类型(空=纯文本/package=文件包)"`
	PackageRootDir        string `gorm:"size:255;not null;default:'';comment:zip顶层目录名，用于路径映射"`
	PackageStorageVersion string `gorm:"size:64;not null;default:'';comment:包文件对象存储代次"`
	PackageFilesJSON      string `gorm:"type:text;not null;default:'';comment:包内文件清单JSON"`
	Enabled               bool   `gorm:"not null;default:true;index:idx_skills_enabled;comment:是否启用"`
	SortOrder             int    `gorm:"not null;default:0;index:idx_skills_sort_order;comment:排序值"`
	CreatedByUserID       uint   `gorm:"not null;default:0;index:idx_skills_created_by;comment:创建人ID"`
	UpdatedByUserID       uint   `gorm:"not null;default:0;index:idx_skills_updated_by;comment:最后更新人ID"`
}

// TableName 指定表名。
func (Skill) TableName() string {
	return "skills"
}
