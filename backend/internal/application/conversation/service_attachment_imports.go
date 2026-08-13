package conversation

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
	"github.com/google/uuid"
)

const attachmentImportLaneRetention = 24 * time.Hour

var attachmentImportComponentRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

type attachmentImportPath struct {
	FileID      string
	FileName    string
	SandboxPath string
	MMPath      string
}

func (s *Service) syncCurrentAttachmentsToImports(
	ctx context.Context,
	userID uint,
	conversationID uint,
	attachments []AttachmentInput,
) ([]attachmentImportPath, func(), error) {
	if s == nil || s.cfg == nil {
		return nil, func() {}, nil
	}
	importsRoot := strings.TrimSpace(s.cfg.Snapshot().SandboxImportsDir)
	if importsRoot == "" || len(attachments) == 0 {
		return nil, func() {}, nil
	}
	if userID == 0 || conversationID == 0 {
		return nil, func() {}, fmt.Errorf("invalid attachment import scope")
	}
	if s.storeProvider == nil {
		return nil, func() {}, fmt.Errorf("attachment object store is unavailable")
	}

	scope := fmt.Sprintf("deeix-%d-%d", userID, conversationID)
	importsRoot = filepath.Clean(importsRoot)
	scopeDir := filepath.Join(importsRoot, scope)
	if err := os.MkdirAll(scopeDir, 0755); err != nil {
		return nil, func() {}, fmt.Errorf("create attachment import scope: %w", err)
	}
	cutoff := time.Now().Add(-attachmentImportLaneRetention)
	cleanupStaleAttachmentImportLanes(scopeDir, cutoff, "run-")
	cleanupStaleAttachmentImportLanes(importsRoot, cutoff, ".tmp-")

	laneName := "run-" + uuid.NewString()
	tempDir := filepath.Join(importsRoot, ".tmp-"+uuid.NewString())
	laneDir := filepath.Join(scopeDir, laneName)
	if err := os.Mkdir(tempDir, 0755); err != nil {
		return nil, func() {}, fmt.Errorf("create attachment import lane: %w", err)
	}
	cleanupTemp := func() { _ = os.RemoveAll(tempDir) }
	defer cleanupTemp()

	store, err := s.storeProvider.Open(ctx)
	if err != nil {
		return nil, func() {}, fmt.Errorf("open attachment object store: %w", err)
	}
	cfg := s.cfg.Snapshot()
	maxBytes := cfg.MaxUploadFileBytes
	if maxBytes <= 0 {
		maxBytes = maxToolExportBytes
	}
	paths := make([]attachmentImportPath, 0, len(attachments))
	seen := make(map[string]struct{}, len(attachments))
	for _, attachment := range attachments {
		if !attachment.Current {
			continue
		}
		fileID := strings.TrimSpace(attachment.FileID)
		storagePath := strings.TrimSpace(attachment.StoragePath)
		if fileID == "" || storagePath == "" {
			return nil, func() {}, fmt.Errorf("attachment import source is incomplete")
		}
		if _, exists := seen[fileID]; exists {
			continue
		}
		seen[fileID] = struct{}{}

		fileName := attachmentImportFileName(fileID, attachment.FileName)
		if err := copyAttachmentImport(ctx, store, storagePath, filepath.Join(tempDir, fileName), maxBytes); err != nil {
			return nil, func() {}, fmt.Errorf("copy attachment %s to imports: %w", fileID, err)
		}
		paths = append(paths, attachmentImportPath{
			FileID:      fileID,
			FileName:    strings.TrimSpace(attachment.FileName),
			SandboxPath: "/imports/" + filepath.ToSlash(filepath.Join(laneName, fileName)),
			MMPath:      "/imports/" + filepath.ToSlash(filepath.Join(scope, laneName, fileName)),
		})
	}
	if len(paths) == 0 {
		return nil, func() {}, nil
	}
	if err := os.Rename(tempDir, laneDir); err != nil {
		return nil, func() {}, fmt.Errorf("publish attachment import lane: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(laneDir) }
	return paths, cleanup, nil
}

func copyAttachmentImport(
	ctx context.Context,
	store objectstore.Store,
	storagePath string,
	destination string,
	maxBytes int64,
) error {
	reader, info, err := store.Open(ctx, storagePath)
	if err != nil {
		return err
	}
	defer reader.Close() //nolint:errcheck
	if info.SizeBytes > maxBytes {
		return fmt.Errorf("attachment exceeds import limit")
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(reader, maxBytes+1))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maxBytes {
		_ = os.Remove(destination)
		return fmt.Errorf("attachment exceeds import limit")
	}
	return nil
}

func injectAttachmentImportGuidance(
	messages []llm.Message,
	runtime selectedToolRuntime,
	paths []attachmentImportPath,
) []llm.Message {
	if len(paths) == 0 || len(runtime.authorizedMCPServers) == 0 {
		return messages
	}
	var builder strings.Builder
	builder.WriteString("<current_attachment_imports>\n")
	builder.WriteString("These are the only current-message attachment paths prepared for MCP tools. Do not guess other import paths.\n")
	builder.WriteString("Sandbox session tools see the scope mounted at /imports; MM tools require the scope-qualified path.\n")
	for _, item := range paths {
		fmt.Fprintf(
			&builder,
			"file=%s sandbox=%s mm=%s\n",
			attachmentImportLabel(item),
			item.SandboxPath,
			item.MMPath,
		)
	}

	builder.WriteString("</current_attachment_imports>")
	guidance := builder.String()
	result := cloneLLMMessages(messages)
	for index := len(result) - 1; index >= 0; index-- {
		if result[index].Role != "user" {
			continue
		}
		if len(result[index].Parts) == 0 {
			content := strings.TrimSpace(result[index].Content)
			if content != "" {
				content += "\n\n"
			}
			result[index].Content = content + guidance
			return result
		}
		parts := append([]llm.ContentPart{}, result[index].Parts...)
		parts = append(parts, llm.ContentPart{Kind: llm.ContentPartText, Text: guidance})
		result[index].Parts = parts
		return result
	}
	return messages
}

func attachmentImportLabel(item attachmentImportPath) string {
	name := strings.TrimSpace(item.FileName)
	if name == "" {
		name = strings.TrimSpace(item.FileID)
	}
	return attachmentImportComponentRE.ReplaceAllString(name, "_")
}

func attachmentImportFileName(fileID, fileName string) string {
	id := strings.Trim(attachmentImportComponentRE.ReplaceAllString(strings.TrimSpace(fileID), "_"), "._-")
	if id == "" {
		id = "file"
	}
	name := filepath.Base(strings.TrimSpace(fileName))
	name = strings.Trim(attachmentImportComponentRE.ReplaceAllString(name, "_"), "._-")
	if name == "" {
		name = "attachment"
	}
	if len(name) > 120 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		stem := strings.TrimSuffix(name, ext)
		maxStem := 120 - len(ext)
		if len(stem) > maxStem {
			stem = stem[:maxStem]
		}
		name = stem + ext
	}
	return id + "_" + name
}

func cleanupStaleAttachmentImportLanes(root string, cutoff time.Time, prefix string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !strings.HasPrefix(name, prefix) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(root, name))
		}
	}
}
