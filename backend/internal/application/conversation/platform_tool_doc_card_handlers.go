package conversation

import (
	"context"
	"fmt"
	"strings"

	appdoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/doccard"
)

// 平台工具文档卡片域：save_doc_card / list_doc_cards / delete_doc_card。
// 卡片是关键字触发的上下文文档（lorebook 式）：用户消息命中关键字即注入；
// 写工具（save/delete）受 write_enabled 开关与批准模式管控，写入后即时失效缓存。

// platformSaveDocCard 创建或更新文档卡片（写操作，受批准模式管控）。
func (s *Service) platformSaveDocCard(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		CardID    string    `json:"card_id"`
		Category  *string   `json:"category"`
		ProjectID *uint     `json:"project_id"`
		RoleID    *uint     `json:"role_id"`
		Title     string    `json:"title"`
		Content   string    `json:"content"`
		Keywords  *[]string `json:"keywords"`
		Enabled   *bool     `json:"enabled"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	title := strings.TrimSpace(args.Title)
	if title == "" {
		return "", fmt.Errorf("title is required")
	}
	if len(title) > appdoccard.MaxTitleLen {
		return "", fmt.Errorf("title exceeds %d characters", appdoccard.MaxTitleLen)
	}
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return "", fmt.Errorf("content is required")
	}
	if len(content) > appdoccard.MaxContentLen {
		return "", fmt.Errorf("content exceeds %d characters", appdoccard.MaxContentLen)
	}
	if args.Category != nil && len(*args.Category) > 64 {
		return "", fmt.Errorf("category exceeds 64 characters")
	}
	if s.docCards == nil {
		return "", fmt.Errorf("doc card service is unavailable")
	}
	cardID := strings.TrimSpace(args.CardID)
	category := ""
	var keywords []string
	projectID := args.ProjectID
	roleID := args.RoleID
	if args.Category != nil {
		category = strings.TrimSpace(*args.Category)
	}
	if args.Keywords != nil {
		keywords = *args.Keywords
	}
	if cardID != "" && (args.Category == nil || args.Keywords == nil || projectID == nil || roleID == nil) {
		existing, err := s.docCards.GetDocCard(ctx, call.UserID, cardID)
		if err != nil {
			return "", err
		}
		if args.Category == nil {
			category = existing.Category
		}
		if args.Keywords == nil {
			keywords = existing.Keywords
		}
		if projectID == nil {
			projectID = existing.ProjectID
		}
		if roleID == nil {
			roleID = existing.RoleID
		}
	}
	projectID = normalizeDocCardBinding(projectID)
	roleID = normalizeDocCardBinding(roleID)
	item, err := s.docCards.UpsertDocCard(ctx, call.UserID, cardID, appdoccard.UpsertInput{
		Category:  category,
		ProjectID: projectID,
		RoleID:    roleID,
		Title:     title,
		Content:   content,
		Keywords:  keywords,
		Enabled:   args.Enabled,
	}, "ai")
	if err != nil {
		return "", err
	}
	s.InvalidateDocCardCache(call.UserID)
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.save_doc_card", item.CardPublicID, map[string]interface{}{
		"title":      item.Title,
		"enabled":    item.Enabled,
		"project_id": item.ProjectID,
		"role_id":    item.RoleID,
	})
	return marshalPlatformResult(map[string]interface{}{
		"card_id":    item.CardPublicID,
		"title":      item.Title,
		"category":   item.Category,
		"project_id": item.ProjectID,
		"role_id":    item.RoleID,
		"keywords":   item.Keywords,
		"enabled":    item.Enabled,
		"status":     "saved",
		"note":       "card is injected into context when the user message matches its keywords",
	})
}

// platformListDocCards 列出用户文档卡片（只读）。
func (s *Service) platformListDocCards(ctx context.Context, call platformToolCallContext) (string, error) {
	if s.docCards == nil {
		return "", fmt.Errorf("doc card service is unavailable")
	}
	cards, err := s.docCards.ListDocCards(ctx, call.UserID)
	if err != nil {
		return "", err
	}
	type cardSummary struct {
		CardID    string   `json:"card_id"`
		Category  string   `json:"category"`
		ProjectID *uint    `json:"project_id,omitempty"`
		RoleID    *uint    `json:"role_id,omitempty"`
		Title     string   `json:"title"`
		Content   string   `json:"content"`
		Keywords  []string `json:"keywords"`
		Enabled   bool     `json:"enabled"`
	}
	summaries := make([]cardSummary, 0, len(cards))
	for _, card := range cards {
		summaries = append(summaries, cardSummary{
			CardID:    card.CardPublicID,
			Category:  card.Category,
			ProjectID: card.ProjectID,
			RoleID:    card.RoleID,
			Title:     card.Title,
			Content:   card.Content,
			Keywords:  card.Keywords,
			Enabled:   card.Enabled,
		})
	}
	return marshalPlatformResult(map[string]interface{}{
		"total": len(summaries),
		"cards": summaries,
	})
}

func normalizeDocCardBinding(id *uint) *uint {
	if id == nil || *id != 0 {
		return id
	}
	return nil
}

// platformDeleteDocCard 删除文档卡片（写操作，受批准模式管控）。
func (s *Service) platformDeleteDocCard(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		CardID string `json:"card_id"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	cardID := strings.TrimSpace(args.CardID)
	if cardID == "" {
		return "", fmt.Errorf("card_id is required")
	}
	if s.docCards == nil {
		return "", fmt.Errorf("doc card service is unavailable")
	}
	if err := s.docCards.DeleteDocCard(ctx, call.UserID, cardID); err != nil {
		return "", err
	}
	s.InvalidateDocCardCache(call.UserID)
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.delete_doc_card", cardID, nil)
	return marshalPlatformResult(map[string]interface{}{
		"card_id": cardID,
		"deleted": true,
	})
}
