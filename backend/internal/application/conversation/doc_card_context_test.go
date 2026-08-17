package conversation

import (
	"context"
	"strings"
	"testing"

	appdoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/doccard"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
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
	// 会话在项目 B、角色 A：命中 global + role A + both（两维度均匹配，交集语义）。
	got = ids(matchDocCards("k", boundCards, projectB, roleA, 0))
	if len(got) != 3 {
		t.Fatalf("combined scope mismatch: %+v", got)
	}
	// AND 语义：both（项目 B + 角色 A）在只满足单一维度时不命中。
	// 会话在项目 B、无角色：角色绑定卡（role）也不命中（会话无角色维度），仅 global。
	got = ids(matchDocCards("k", boundCards, projectB, 0, 0))
	if len(got) != 1 || got[0] != "g" {
		t.Fatalf("intersection must reject missing role dimension: %+v", got)
	}
	// 会话在项目 A、角色 A：global + project A + role A 命中，both（绑定项目 B）不命中。
	got = ids(matchDocCards("k", boundCards, projectA, roleA, 0))
	if len(got) != 3 || got[0] != "g" || got[1] != "p" || got[2] != "r" {
		t.Fatalf("intersection must reject missing project dimension: %+v", got)
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

func TestInjectUserContextNonVisionHintAppendsExtractedText(t *testing.T) {
	messages := []llm.Message{{Role: "user", Content: "看看这张图"}}
	input := userContextInput{
		Attachments: []AttachmentInput{{
			FileID:   "file_vision_hint",
			FileName: "ssh.png",
			Kind:     "image",
			MimeType: "image/png",
			Current:  true,
		}},
		SupportsVision: false,
		UserID:         7,
		NonVisionExtractReader: func(ctx context.Context, userID uint, fileID string) string {
			if userID != 7 || fileID != "file_vision_hint" {
				return ""
			}
			return "root@203.0.113.10:22 password: s3cr3t"
		},
	}
	out := injectUserContext(context.Background(), messages, input, config.Config{}, nil)
	content := userMessageText(out[len(out)-1])
	if !strings.Contains(content, "[Image attachment: ssh.png (fileID: file_vision_hint)]") {
		t.Fatalf("hint missing: %s", content)
	}
	if !strings.Contains(content, "root@203.0.113.10:22") {
		t.Fatalf("extracted text not appended to hint: %s", content)
	}
}

func TestInjectUserContextNonVisionHintWithoutExtractGuidesUser(t *testing.T) {
	messages := []llm.Message{{Role: "user", Content: "看看这张图"}}
	input := userContextInput{
		Attachments: []AttachmentInput{{
			FileID:   "file_vision_no_extract",
			FileName: "scan.png",
			Kind:     "image",
			MimeType: "image/png",
			Current:  true,
		}},
		SupportsVision:         false,
		UserID:                 7,
		NonVisionExtractReader: func(ctx context.Context, userID uint, fileID string) string { return "" },
	}
	out := injectUserContext(context.Background(), messages, input, config.Config{}, nil)
	content := userMessageText(out[len(out)-1])
	if !strings.Contains(content, "图片文字尚未提取") {
		t.Fatalf("missing extraction-pending guidance: %s", content)
	}
}
