package skill

import "errors"

var (
	// ErrSkillNotFound 表示技能不存在或当前用户无权访问。
	ErrSkillNotFound = errors.New("skill not found")
	// ErrInvalidSkill 表示技能参数不合法。
	ErrInvalidSkill = errors.New("invalid skill")
	// ErrSkillConflict 表示触发词在当前作用域内已存在。
	ErrSkillConflict = errors.New("skill trigger already exists")
	// ErrInvalidPackage 表示技能包不合法（zip 损坏、缺少 SKILL.md、越界等）。
	ErrInvalidPackage = errors.New("invalid skill package")
	// ErrPackageFileNotFound 表示包内文件不存在。
	ErrPackageFileNotFound = errors.New("skill package file not found")
	// ErrPackageFileUnreadable 表示包内文件为二进制，无法作为文本提供。
	ErrPackageFileUnreadable = errors.New("skill package file is not readable as text")
)
