package repository

import (
	"context"

	domaindoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/doccard"
)

// DocCardRepository 定义文档卡片域依赖的持久化能力。
type DocCardRepository interface {
	UpsertDocCard(ctx context.Context, item *domaindoccard.DocCard) error
	DeleteDocCard(ctx context.Context, userID uint, publicID string) error
	ListDocCards(ctx context.Context, userID uint) ([]domaindoccard.DocCard, error)
	GetDocCardByPublicID(ctx context.Context, userID uint, publicID string) (*domaindoccard.DocCard, error)
}
