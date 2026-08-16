package conversation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
)

type toolExportAttachmentRepositoryStub struct {
	err error
}

func (r *toolExportAttachmentRepositoryStub) CreateAttachments(context.Context, []model.Attachment) error {
	return r.err
}

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

func TestExportFilePathAndScopedOpen(t *testing.T) {
	root := t.TempDir()
	scope := filepath.Join(root, "deeix-4-9", "mm-call")
	if err := os.MkdirAll(scope, 0700); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(scope, "result.png")
	if err := os.WriteFile(filePath, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	svc := &Service{cfg: config.NewRuntime(config.Config{SandboxSharedDir: root})}
	input := executeAssistantToolCallsInput{UserID: 4, ConversationID: 9}
	rel, err := svc.exportFilePath(input, filepath.ToSlash(filePath))
	if err != nil {
		t.Fatal(err)
	}
	wantRel := filepath.Join("deeix-4-9", "mm-call", "result.png")
	if rel != wantRel {
		t.Fatalf("relative export path = %q, want %q", rel, wantRel)
	}
	reader, size, err := openExportRegularFile(root, rel)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	if size != 5 {
		t.Fatalf("export size = %d, want 5", size)
	}
	if _, err := svc.exportFilePath(input, filepath.ToSlash(filepath.Join(root, "deeix-4-10", "result.png"))); err == nil {
		t.Fatal("cross-scope export path accepted")
	}
}

func TestOpenExportRegularFileRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	scope := filepath.Join(root, "deeix-4-9")
	if err := os.MkdirAll(scope, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(scope, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, _, err := openExportRegularFile(root, filepath.Join("deeix-4-9", "escape.txt")); err == nil {
		t.Fatal("symlink export accepted")
	}
}

func TestPersistToolExportAttachmentsKeepsSourceAfterPersistence(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("deeix-4-9", "sandbox", "result.txt")
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("result"), 0600); err != nil {
		t.Fatal(err)
	}

	persistErr := errors.New("persist failed")
	repo := &toolExportAttachmentRepositoryStub{err: persistErr}
	attachments := []model.Attachment{{FileID: "file_1"}}
	if err := persistToolExportAttachments(t.Context(), repo, attachments); !errors.Is(err, persistErr) {
		t.Fatalf("expected persistence error, got %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("source file removed after persistence failure: %v", err)
	}

	repo.err = nil
	if err := persistToolExportAttachments(t.Context(), repo, attachments); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("source file removed after persistence success: %v", err)
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
