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
// projectID/roleID 为会话的项目/角色绑定：卡片绑定任一命中即触发，无绑定=全局。
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

// docCardScopeMatches 卡片作用域匹配：无绑定（全局）始终命中；
// 绑定项目/角色时，会话对应维度命中即触发。
func docCardScopeMatches(card appdoccard.CardView, projectID uint, roleID uint) bool {
	if card.ProjectID == nil && card.RoleID == nil {
		return true
	}
	if card.ProjectID != nil && projectID != 0 && *card.ProjectID == projectID {
		return true
	}
	if card.RoleID != nil && roleID != 0 && *card.RoleID == roleID {
		return true
	}
	return false
}

// formatDocCardsContext 生成 <cards> 注入片段（每张内容截断）。
func formatDocCardsContext(cards []appdoccard.CardView, contentLimit int) string {
	if len(cards) == 0 {
		return ""
	}
	if contentLimit <= 0 {
		contentLimit = docCardContentLimit
	}
	items := make([]string, 0, len(cards))
	for _, card := range cards {
		title := strings.TrimSpace(card.Title)
		content := strings.TrimSpace(card.Content)
		if title == "" || content == "" {
			continue
		}
		if runes := []rune(content); len(runes) > contentLimit {
			content = string(runes[:contentLimit]) + "…"
		}
		items = append(items, `<card k="`+xmlEscapeAttr(title)+`">`+xmlEscapeText(content)+`</card>`)
	}
	if len(items) == 0 {
		return ""
	}
	return "\n<cards>\n" + strings.Join(items, "\n") + "\n</cards>"
}
