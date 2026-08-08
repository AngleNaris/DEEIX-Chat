package repository

import (
	"context"

	domaindynamicprompt "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/dynamicprompt"
)

// DynamicPromptRepository 定义动态提示词域依赖的持久化能力。
type DynamicPromptRepository interface {
	UpsertDynamicPrompt(ctx context.Context, item *domaindynamicprompt.DynamicPrompt) error
	DeleteDynamicPrompt(ctx context.Context, userID uint, publicID string) error
	ListDynamicPrompts(ctx context.Context, userID uint) ([]domaindynamicprompt.DynamicPrompt, error)
}
