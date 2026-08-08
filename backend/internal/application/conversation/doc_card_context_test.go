package conversation

import (
	"strings"
	"testing"

	appdoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/doccard"
)

func docCard(title string, enabled bool, keywords ...string) appdoccard.CardView {
	return appdoccard.CardView{
		CardPublicID: title,
		Title:        title,
		Content:      "content of " + title,
		Keywords:     keywords,
		Enabled:      enabled,
	}
}

func TestMatchDocCards(t *testing.T) {
	cards := []appdoccard.CardView{
		docCard("world", true, "魔法森林", "精灵"),
		docCard("char", true, "Alice"),
		docCard("off", false, "禁用词"),
		docCard("none", true, "never-match"),
	}
	// 命中启用卡片（大小写不敏感，多关键字任一命中）。
	matched := matchDocCards("我在魔法森林遇到Alice", cards, 0, 0, 0)
	if len(matched) != 2 {
		t.Fatalf("expected 2 matched cards, got %d", len(matched))
	}
	if matched[0].CardPublicID != "world" || matched[1].CardPublicID != "char" {
		t.Fatalf("unexpected order: %+v", matched)
	}
	// 禁用卡片不参与匹配。
	matched = matchDocCards("禁用词", cards, 0, 0, 0)
	if len(matched) != 0 {
		t.Fatalf("disabled card must not match, got %+v", matched)
	}
	// 上限。
	matched = matchDocCards("魔法森林 Alice", cards, 0, 0, 1)
	if len(matched) != 1 {
		t.Fatalf("expected cap 1, got %d", len(matched))
	}
	// 空查询/空列表。
	if matchDocCards("", cards, 0, 0, 0) != nil || matchDocCards("x", nil, 0, 0, 0) != nil {
		t.Fatalf("empty inputs must return nil")
	}
}

func TestMatchDocCardsScopeBinding(t *testing.T) {
	projectA := uint(11)
	projectB := uint(22)
	roleA := uint(33)
	boundCards := []appdoccard.CardView{
		{CardPublicID: "g", Title: "global", Content: "g", Keywords: []string{"k"}, Enabled: true},
		{CardPublicID: "p", Title: "proj", Content: "p", Keywords: []string{"k"}, Enabled: true, ProjectID: &projectA},
		{CardPublicID: "r", Title: "role", Content: "r", Keywords: []string{"k"}, Enabled: true, RoleID: &roleA},
		{CardPublicID: "pr", Title: "both", Content: "b", Keywords: []string{"k"}, Enabled: true, ProjectID: &projectB, RoleID: &roleA},
	}
	ids := func(cards []appdoccard.CardView) []string {
		result := make([]string, 0, len(cards))
		for _, card := range cards {
			result = append(result, card.CardPublicID)
		}
		return result
	}
	// 会话在项目 A、无角色：命中 global + project A；role 与 both 不命中。
	got := ids(matchDocCards("k", boundCards, projectA, 0, 0))
	if len(got) != 2 || got[0] != "g" || got[1] != "p" {
		t.Fatalf("project scope mismatch: %+v", got)
	}
	// 会话在项目 B、角色 A：命中 global + role A + both（任一维度命中）。
	got = ids(matchDocCards("k", boundCards, projectB, roleA, 0))
	if len(got) != 3 {
		t.Fatalf("combined scope mismatch: %+v", got)
	}
	// 会话无绑定：只命中全局。
	got = ids(matchDocCards("k", boundCards, 0, 0, 0))
	if len(got) != 1 || got[0] != "g" {
		t.Fatalf("global-only mismatch: %+v", got)
	}
}

func TestFormatDocCardsContext(t *testing.T) {
	long := strings.Repeat("汉", 3000)
	cards := []appdoccard.CardView{
		{CardPublicID: "c1", Title: "世界设定", Content: long, Keywords: []string{"世界"}},
	}
	out := formatDocCardsContext(cards, docCardContentLimit)
	if !strings.Contains(out, "<cards>") || !strings.Contains(out, "</cards>") {
		t.Fatalf("missing cards wrapper: %q", out)
	}
	if !strings.Contains(out, `<card k="世界设定">`) {
		t.Fatalf("missing card entry: %q", out)
	}
	// 内容截断到 2000 rune + 省略号。
	idx := strings.Index(out, `k="世界设定">`)
	inner := out[idx+len(`k="世界设定">`) : len(out)-len("</card>\n</cards>")]
	if runes := []rune(inner); len(runes) != docCardContentLimit+1 {
		t.Fatalf("content must be truncated to %d+1 runes, got %d", docCardContentLimit, len(runes))
	}
	// 空列表。
	if formatDocCardsContext(nil, 0) != "" {
		t.Fatalf("empty cards must produce empty string")
	}
	// XML 转义。
	out = formatDocCardsContext([]appdoccard.CardView{
		{CardPublicID: "c2", Title: "a\"b", Content: "x<y&z", Keywords: []string{"k"}},
	}, 0)
	if !strings.Contains(out, `k="a&#34;b"`) || !strings.Contains(out, "x&lt;y&amp;z") {
		t.Fatalf("xml escaping missing: %q", out)
	}
}
