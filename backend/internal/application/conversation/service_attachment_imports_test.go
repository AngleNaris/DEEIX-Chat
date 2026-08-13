package conversation

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
)

func TestSyncCurrentAttachmentsToImportsPublishesAndCleansLane(t *testing.T) {
	storage := objectstore.NewLocal(t.TempDir())
	for key, contents := range map[string]string{
		"objects/current-a": "alpha",
		"objects/current-b": "beta",
		"objects/history":   "history",
	} {
		if _, err := storage.Put(t.Context(), key, bytes.NewBufferString(contents), objectstore.PutOptions{}); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	importsRoot := t.TempDir()
	provider := &conversationTestStoreProvider{store: storage}
	service := &Service{
		cfg: config.NewRuntime(config.Config{
			SandboxImportsDir:  importsRoot,
			MaxUploadFileBytes: 1024,
		}),
		storeProvider: provider,
	}
	attachments := []AttachmentInput{
		{FileID: "file-a", FileName: "same name.txt", StoragePath: "objects/current-a", Current: true},
		{FileID: "file-b", FileName: "same name.txt", StoragePath: "objects/current-b", Current: true},
		{FileID: "file-history", FileName: "history.txt", StoragePath: "objects/history"},
	}

	paths, cleanup, err := service.syncCurrentAttachmentsToImports(t.Context(), 4, 9, attachments)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("got %d import paths, want 2", len(paths))
	}
	if paths[0].SandboxPath == paths[1].SandboxPath || paths[0].MMPath == paths[1].MMPath {
		t.Fatalf("same-name attachments collided: %#v", paths)
	}
	contents := map[string]bool{}
	for _, item := range paths {
		if !strings.HasPrefix(item.SandboxPath, "/imports/run-") {
			t.Fatalf("unexpected sandbox path %q", item.SandboxPath)
		}
		if !strings.HasPrefix(item.MMPath, "/imports/deeix-4-9/run-") {
			t.Fatalf("unexpected MM path %q", item.MMPath)
		}
		relative := strings.TrimPrefix(item.MMPath, "/imports/")
		data, readErr := os.ReadFile(filepath.Join(importsRoot, filepath.FromSlash(relative)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		contents[string(data)] = true
	}
	if !contents["alpha"] || !contents["beta"] || contents["history"] {
		t.Fatalf("unexpected imported contents: %v", contents)
	}
	lane := filepath.Join(importsRoot, filepath.FromSlash(strings.TrimPrefix(filepath.Dir(paths[0].MMPath), "/imports/")))
	cleanup()
	if _, err := os.Stat(lane); !os.IsNotExist(err) {
		t.Fatalf("import lane remains after cleanup: %v", err)
	}
}

func TestSyncCurrentAttachmentsToImportsDoesNotPublishPartialLane(t *testing.T) {
	storage := objectstore.NewLocal(t.TempDir())
	if _, err := storage.Put(t.Context(), "objects/large", bytes.NewBufferString("too-large"), objectstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	importsRoot := t.TempDir()
	service := &Service{
		cfg: config.NewRuntime(config.Config{
			SandboxImportsDir:  importsRoot,
			MaxUploadFileBytes: 4,
		}),
		storeProvider: &conversationTestStoreProvider{store: storage},
	}
	paths, cleanup, err := service.syncCurrentAttachmentsToImports(t.Context(), 4, 9, []AttachmentInput{{
		FileID: "file-large", FileName: "large.bin", StoragePath: "objects/large", Current: true,
	}})
	cleanup()
	if err == nil || len(paths) != 0 {
		t.Fatalf("expected failed unpublished import, got paths=%#v err=%v", paths, err)
	}
	entries, readErr := os.ReadDir(filepath.Join(importsRoot, "deeix-4-9"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("partial lane was published: %v", entries)
	}
	rootEntries, readErr := os.ReadDir(importsRoot)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range rootEntries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatalf("temporary lane remains: %s", entry.Name())
		}
	}
}

func TestCleanupStaleAttachmentImportLanesRestrictsPrefix(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	for _, name := range []string{"run-old", "keep-old"} {
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	cleanupStaleAttachmentImportLanes(root, time.Now().Add(-24*time.Hour), "run-")
	if _, err := os.Stat(filepath.Join(root, "run-old")); !os.IsNotExist(err) {
		t.Fatalf("stale run lane remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "keep-old")); err != nil {
		t.Fatalf("unrelated directory removed: %v", err)
	}
}

func TestInjectAttachmentImportGuidanceRequiresAuthorizedMCP(t *testing.T) {
	messages := []llm.Message{{Role: "system", Content: "custom tool rules"}, {Role: "user", Content: "inspect"}}
	paths := []attachmentImportPath{{
		FileID: "file-a", FileName: "report.pdf",
		SandboxPath: "/imports/run-a/file-a_report.pdf",
		MMPath:      "/imports/deeix-4-9/run-a/file-a_report.pdf",
	}}
	withoutMCP := injectAttachmentImportGuidance(messages, selectedToolRuntime{}, paths)
	if strings.Contains(withoutMCP[1].Content, "current_attachment_imports") {
		t.Fatal("attachment paths exposed without authorized MCP")
	}
	withMCP := injectAttachmentImportGuidance(messages, selectedToolRuntime{
		authorizedMCPServers: map[uint]authorizedMCPServer{1: {id: 1, name: "sandbox"}},
	}, paths)
	for _, expected := range []string{"<current_attachment_imports>", paths[0].SandboxPath, paths[0].MMPath} {
		if !strings.Contains(withMCP[1].Content, expected) {
			t.Fatalf("missing %q from guidance: %s", expected, withMCP[1].Content)
		}
	}
	if strings.Contains(messages[1].Content, "current_attachment_imports") {
		t.Fatal("source messages were mutated")
	}
}

func TestInjectAttachmentImportGuidanceAppendsMultimodalTextPart(t *testing.T) {
	messages := []llm.Message{{Role: "user", Parts: []llm.ContentPart{{Kind: llm.ContentPartImage, Data: []byte("image")}}}}
	paths := []attachmentImportPath{{SandboxPath: "/imports/run-a/image.png", MMPath: "/imports/deeix-4-9/run-a/image.png"}}
	got := injectAttachmentImportGuidance(messages, selectedToolRuntime{
		authorizedMCPServers: map[uint]authorizedMCPServer{1: {id: 1}},
	}, paths)
	if len(got[0].Parts) != 2 || got[0].Parts[1].Kind != llm.ContentPartText || !strings.Contains(got[0].Parts[1].Text, paths[0].MMPath) {
		t.Fatalf("missing imports text part: %#v", got[0].Parts)
	}
	if len(messages[0].Parts) != 1 {
		t.Fatal("source multimodal parts were mutated")
	}
}
