package memory

import (
	"strings"
	"time"
)

const (
	CategoryIdentity   = "identity"
	CategoryActivity   = "activity"
	CategoryContext    = "context"
	CategoryPreference = "preference"
	CategoryCapability = "capability"
	CategoryExperience = "experience"
)

// CanonicalCategory accepts current categories and legacy client values.
func CanonicalCategory(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CategoryIdentity, "profile":
		return CategoryIdentity, true
	case CategoryActivity:
		return CategoryActivity, true
	case CategoryContext, "custom", "global":
		return CategoryContext, true
	case CategoryPreference:
		return CategoryPreference, true
	case CategoryCapability:
		return CategoryCapability, true
	case CategoryExperience:
		return CategoryExperience, true
	default:
		return "", false
	}
}

// NormalizeCategory keeps legacy database rows readable without a migration.
func NormalizeCategory(value string) string {
	if category, ok := CanonicalCategory(value); ok {
		return category
	}
	return CategoryContext
}

// UserMemory 表示用户长期记忆。
type UserMemory struct {
	ID        uint
	UserID    uint
	MemoryKey string
	Value     string
	Scope     string
	UpdatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}
