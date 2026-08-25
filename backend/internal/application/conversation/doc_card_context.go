package conversation

import (
	"context"
	"strings"
	"time"

	appdoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/doccard"
	"go.uber.org/zap"
)

// 文档卡片（lorebook 式）触发注入：
// 发送消息时对最新用户消息做关键字子串匹配（大小写不敏感），命中的启用卡片
// 以 <cards> 段注入用户上下文（与记忆 <mems> 同通道，位于其后）。
// 与记忆的语义召回不同，卡片是用户可精确控制的关键字触发，不依赖向量服务。

const (
	// docCardCacheTTL 卡片列表缓存时长（写入后即时失效）。
	docCardCacheTTL = 3 * time.Minute
	// docCardMaxMatched 单轮注入的卡片数量上限。
	docCardMaxMatched = 5
	// docCardContentLimit 单张卡片注入的内容上限（按 rune）。
	docCardContentLimit = 2000
	// docCardContextMaxTokens 限制命中卡片绕过主历史预算后的聚合占用。
	docCardContextMaxTokens int64 = 1600
)

type cachedDocCards struct {
	cards     []appdoccard.CardView
	expiresAt time.Time
}

// getCachedDocCards 读取用户文档卡片（带缓存回填）。
func (s *Service) getCachedDocCards(ctx context.Context, userID uint) []appdoccard.CardView {
	if userID == 0 || s.docCards == nil {
		return nil
	}
	if cached, ok := s.docCardCache.Load(userID); ok {
		entry, ok2 := cached.(*cachedDocCards)
		if ok2 && time.Now().Before(entry.expiresAt) {
			return entry.cards
		}
	}
	cards, err := s.docCards.ListDocCards(ctx, userID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("doc_cards_list_failed", zap.Error(err))
		}
		return nil
	}
	s.docCardCache.Store(userID, &cachedDocCards{
		cards:     cards,
		expiresAt: time.Now().Add(docCardCacheTTL),
	})
	return cards
}

// matchDocCards 关键字子串匹配（大小写不敏感），返回命中的启用卡片。
// projectID/roleID 为会话的项目/角色绑定：卡片绑定维度与会话取交集（见 docCardScopeMatches）。
// 命中顺序按卡片列表顺序（ListDocCards 已按 updated_at DESC）。
func matchDocCards(query string, cards []appdoccard.CardView, projectID uint, roleID uint, maxCards int) []appdoccard.CardView {
	if strings.TrimSpace(query) == "" || len(cards) == 0 {
		return nil
	}
	if maxCards <= 0 {
		maxCards = docCardMaxMatched
	}
	lower := strings.ToLower(query)
	matched := make([]appdoccard.CardView, 0, maxCards)
	for _, card := range cards {
		if !card.Enabled || len(matched) >= maxCards {
			continue
		}
		if !docCardScopeMatches(card, projectID, roleID) {
			continue
		}
		for _, keyword := range card.Keywords {
			kw := strings.ToLower(strings.TrimSpace(keyword))
			if kw != "" && strings.Contains(lower, kw) {
				matched = append(matched, card)
				break
			}
		}
	}
	return matched
}

// docCardScopeMatches 卡片作用域匹配：所有绑定维度取交集（AND）。
// 未绑定的维度不参与限制——只绑项目时任意角色会话命中，只绑角色时任意项目会话命中；
// 两个维度都绑定时要求会话的项目与角色同时命中；均未绑定 = 全局卡片始终命中。
func docCardScopeMatches(card appdoccard.CardView, projectID uint, roleID uint) bool {
	if card.ProjectID == nil && card.RoleID == nil {
		return true
	}
	if card.ProjectID != nil && (projectID == 0 || *card.ProjectID != projectID) {
		return false
	}
	if card.RoleID != nil && (roleID == 0 || *card.RoleID != roleID) {
		return false
	}
	return true
}

// formatDocCardsContext 生成 <cards> 注入片段（每张内容截断）。
func formatDocCardsContext(cards []appdoccard.CardView, contentLimit int) string {
	if len(cards) == 0 {
		return ""
	}
	if contentLimit <= 0 {
		contentLimit = docCardContentLimit
	}
	const wrapperPrefix = "\n<cards>\n"
	const wrapperSuffix = "\n</cards>"
	items := make([]string, 0, len(cards))
	usedTokens := estimateTokens(wrapperPrefix + wrapperSuffix)
	for _, card := range cards {
		title := strings.TrimSpace(card.Title)
		content := strings.TrimSpace(card.Content)
		if title == "" || content == "" {
			continue
		}
		if runes := []rune(content); len(runes) > contentLimit {
			content = string(runes[:contentLimit]) + "…"
		}
		remainingTokens := docCardContextMaxTokens - usedTokens
		if remainingTokens <= 0 {
			break
		}
		prefix := `<card k="` + xmlEscapeAttr(title) + `">`
		suffix := `</card>`
		overheadTokens := estimateTokens(prefix + suffix)
		if overheadTokens >= remainingTokens {
			break
		}
		escapedContent := fitXMLTextToTokenBudget(content, remainingTokens-overheadTokens)
		if escapedContent == "" {
			break
		}
		item := prefix + escapedContent + suffix
		itemTokens := estimateTokens(item)
		if itemTokens > remainingTokens {
			break
		}
		items = append(items, item)
		usedTokens += itemTokens
	}
	if len(items) == 0 {
		return ""
	}
	return wrapperPrefix + strings.Join(items, "\n") + wrapperSuffix
}
