package conversation

import (
	"context"
	"strings"
	"testing"

	appknowledgebase "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/knowledgebase"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	domainknowledgebase "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/knowledgebase"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
)

func TestKnowledgeBasePlatformToolRegistry(t *testing.T) {
	registry := platformToolRegistry()
	for name, kind := range map[string]platformToolKind{
		"list_knowledge_bases":          platformToolRead,
		"list_knowledge_base_contents":  platformToolRead,
		"create_knowledge_base_content": platformToolWrite,
		"update_knowledge_base_content": platformToolWrite,
		"delete_knowledge_base_content": platformToolWrite,
	} {
		entry, ok := registry[name]
		if !ok || entry.kind != kind || entry.handler == nil {
			t.Fatalf("platform tool %q = %#v, want kind %q", name, entry, kind)
		}
	}
}

func TestListKnowledgeBasesIncludesReadOnlyScope(t *testing.T) {
	tools := &knowledgeBaseToolStub{bases: []domainknowledgebase.KnowledgeBase{
		{PublicID: "kb-builtin", Name: "Policies", Scope: domainknowledgebase.ScopeBuiltin, FileCount: 2},
		{PublicID: "kb-user", Name: "Notes", Scope: domainknowledgebase.ScopeUser, OwnerUserID: 7, FileCount: 1},
	}}
	service := &Service{knowledgeBaseTools: tools}

	output, err := service.platformListKnowledgeBases(context.Background(), platformToolCallContext{UserID: 7, Arguments: []byte(`{"page":1}`)})
	if err != nil {
		t.Fatalf("platformListKnowledgeBases() error = %v", err)
	}
	if !strings.Contains(output, `"knowledge_base_id":"kb-builtin"`) || !strings.Contains(output, `"read_only":true`) || !strings.Contains(output, `"knowledge_base_id":"kb-user"`) {
		t.Fatalf("platformListKnowledgeBases() output = %s", output)
	}
}

func TestCreateKnowledgeBaseContentWaitsForApproval(t *testing.T) {
	store := newPlatformWriteApprovalStore()
	t.Cleanup(store.Stop)
	tools := &knowledgeBaseToolStub{}
	service := &Service{
		cfg: config.NewRuntime(config.Config{}),
		repo: &mutableUserSettingsRepository{values: map[uint]map[string]string{
			7: {platformToolsWriteApprovalKey: platformToolsWriteApprovalAsk},
		}},
		platformToolsSettings: &fakePlatformSettingsReader{values: map[string]string{
			platformToolsKeyEnabled: "true", platformToolsKeyWriteEnabled: "true",
		}},
		platformApprovals:  store,
		knowledgeBaseTools: tools,
	}
	entry := platformToolRegistry()["create_knowledge_base_content"]
	output, err := service.executePlatformToolCall(context.Background(), entry, ExecuteToolInput{
		UserID: 7, ConversationID: 11, RequestID: "req-kb", ToolName: entry.definition.Name,
		ArgumentsJSON: `{"knowledge_base_id":"kb-user","title":"notes.md","content":"hello"}`,
		ToolRuntime:   &selectedToolRuntime{},
	})
	if err != nil {
		t.Fatalf("executePlatformToolCall() error = %v", err)
	}
	if tools.createCalls != 0 {
		t.Fatal("knowledge base write executed before approval")
	}
	record := approvalRecordFromOutput(t, service, output)
	if _, err := service.ApprovePlatformWrite(context.Background(), record.ID, 7, true); err != nil {
		t.Fatalf("ApprovePlatformWrite() error = %v", err)
	}
	if tools.createCalls != 1 || tools.lastUserID != 7 || tools.lastKnowledgeBaseID != "kb-user" {
		t.Fatalf("approved create calls=%d user=%d kb=%q", tools.createCalls, tools.lastUserID, tools.lastKnowledgeBaseID)
	}
}

func TestUpdateKnowledgeBaseContentRejectsCombinedPatch(t *testing.T) {
	tools := &knowledgeBaseToolStub{}
	service := &Service{knowledgeBaseTools: tools}
	_, err := service.platformUpdateKnowledgeBaseContent(context.Background(), platformToolCallContext{
		UserID:    7,
		Arguments: []byte(`{"knowledge_base_id":"kb-user","content_id":"file-one","title":"renamed.md","content":"updated"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one") || tools.updateCalls != 0 {
		t.Fatalf("platformUpdateKnowledgeBaseContent() error=%v calls=%d", err, tools.updateCalls)
	}
}

type knowledgeBaseToolStub struct {
	bases               []domainknowledgebase.KnowledgeBase
	files               []domainconversation.FileObject
	createCalls         int
	updateCalls         int
	lastUserID          uint
	lastKnowledgeBaseID string
}

func (s *knowledgeBaseToolStub) ListVisible(context.Context, uint, appknowledgebase.ListInput) ([]domainknowledgebase.KnowledgeBase, int64, error) {
	return s.bases, int64(len(s.bases)), nil
}

func (s *knowledgeBaseToolStub) ListVisibleFiles(context.Context, uint, string, int, int) ([]domainconversation.FileObject, int64, error) {
	return s.files, int64(len(s.files)), nil
}

func (s *knowledgeBaseToolStub) CreateUserContent(_ context.Context, userID uint, knowledgeBaseID string, input appknowledgebase.UserContentInput) (*domainconversation.FileObject, error) {
	s.createCalls++
	s.lastUserID = userID
	s.lastKnowledgeBaseID = knowledgeBaseID
	return &domainconversation.FileObject{FileID: "file-created", FileName: input.FileName}, nil
}

func (s *knowledgeBaseToolStub) UpdateUserContent(context.Context, uint, string, string, appknowledgebase.UserContentPatch) error {
	s.updateCalls++
	return nil
}

func (s *knowledgeBaseToolStub) DeleteUserContent(context.Context, uint, string, string) (appknowledgebase.UserContentDeleteResult, error) {
	return appknowledgebase.UserContentDeleteResult{}, nil
}
