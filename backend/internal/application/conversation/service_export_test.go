package conversation

import (
	"errors"
	"testing"

	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
)

func TestExportUserConversationDataRejectsWrongUser(t *testing.T) {
	svc := &Service{}
	conv := &model.Conversation{ID: 1, UserID: 42}

	_, err := svc.ExportUserConversationData(nil, 99, conv)
	if !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}
}

func TestExportDefaultMessagePublicIDsFiltersVisibleBranch(t *testing.T) {
	rootID := uint(1)
	messages := []model.Message{
		{ID: 1, PublicID: "msg_user", Role: "user", Status: "success"},
		{ID: 2, PublicID: "msg_assistant_v1", ParentMessageID: &rootID, Role: "assistant", Status: "success"},
		{ID: 3, PublicID: "msg_assistant_v2", ParentMessageID: &rootID, Role: "assistant", Status: "success"},
	}
	ids := exportDefaultMessagePublicIDs(messages)
	if len(ids) == 0 {
		t.Fatal("expected non-empty default message IDs")
	}
	for _, id := range ids {
		if id == "" {
			t.Error("default message public ID should not be empty")
		}
	}
}

func TestCollectExportMessageRunIDsDeduplicates(t *testing.T) {
	messages := []model.Message{
		{RunID: "run_1"},
		{RunID: "run_2"},
		{RunID: "run_1"},
		{RunID: ""},
		{RunID: "run_3"},
	}
	runIDs := collectExportMessageRunIDs(messages)
	if len(runIDs) != 3 {
		t.Fatalf("expected 3 unique run IDs, got %d: %v", len(runIDs), runIDs)
	}
	expected := map[string]bool{"run_1": true, "run_2": true, "run_3": true}
	for _, id := range runIDs {
		if !expected[id] {
			t.Errorf("unexpected run ID: %s", id)
		}
	}
}

func TestCollectExportMessageRunIDsSkipsEmpty(t *testing.T) {
	messages := []model.Message{
		{RunID: ""},
		{RunID: "  "},
	}
	runIDs := collectExportMessageRunIDs(messages)
	if len(runIDs) != 0 {
		t.Fatalf("expected 0 run IDs for empty inputs, got %d", len(runIDs))
	}
}

func TestParseToolExportItemsConcatenatedFormat(t *testing.T) {
	// image_gen 形态：{"__export__":[...]} 后接 markdown 图片引用（拼接非纯 JSON）。
	output := "{\"__export__\":[{\"path\":\"file://file_abc\",\"name\":\"gen.png\"}]}\n\n![Generated image](/api/v1/files/file_abc/content)"
	items := parseToolExportItems(output)
	if len(items) != 1 {
		t.Fatalf("expected 1 export item, got %d", len(items))
	}
	if items[0].Path != "file://file_abc" || items[0].Name != "gen.png" {
		t.Fatalf("unexpected export item: %+v", items[0])
	}

	// content 块包装形态（MCP 工具兼容）。
	wrapped := `{"content":[{"type":"text","text":"{\"__export__\":[{\"path\":\"/shared/deeix-1-2/a.png\",\"name\":\"a.png\"}]}"}]}`
	items = parseToolExportItems(wrapped)
	if len(items) != 1 || items[0].Path != "/shared/deeix-1-2/a.png" {
		t.Fatalf("unexpected wrapped items: %+v", items)
	}

	// 无标记：返回 nil。
	if items := parseToolExportItems("just some text output"); items != nil {
		t.Fatalf("expected nil for text output, got %+v", items)
	}
}
