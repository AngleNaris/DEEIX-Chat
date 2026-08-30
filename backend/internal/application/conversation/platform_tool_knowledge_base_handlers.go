package conversation

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	appknowledgebase "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/knowledgebase"
	domainknowledgebase "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/knowledgebase"
)

func (s *Service) platformListKnowledgeBases(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Query string `json:"query"`
		Page  int    `json:"page"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.Page < 1 {
		args.Page = 1
	}
	if s.knowledgeBaseTools == nil {
		return "", fmt.Errorf("knowledge base service is unavailable")
	}
	items, total, err := s.knowledgeBaseTools.ListVisible(ctx, call.UserID, appknowledgebase.ListInput{
		Query: strings.TrimSpace(args.Query), Page: args.Page, PageSize: platformListPageSize,
	})
	if err != nil {
		return "", err
	}
	type summary struct {
		ID          string `json:"knowledge_base_id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Scope       string `json:"scope"`
		ReadOnly    bool   `json:"read_only"`
		FileCount   int64  `json:"content_count"`
		ReadyCount  int64  `json:"ready_content_count"`
	}
	result := make([]summary, 0, len(items))
	for _, item := range items {
		result = append(result, summary{
			ID: item.PublicID, Name: item.Name, Description: item.Description, Scope: item.Scope,
			ReadOnly: item.Scope == domainknowledgebase.ScopeBuiltin, FileCount: item.FileCount, ReadyCount: item.ReadyFileCount,
		})
	}
	return marshalPlatformResult(map[string]interface{}{"knowledge_bases": result, "total": total, "page": args.Page})
}

func (s *Service) platformListKnowledgeBaseContents(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		KnowledgeBaseID string `json:"knowledge_base_id"`
		Page            int    `json:"page"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	knowledgeBaseID := strings.TrimSpace(args.KnowledgeBaseID)
	if knowledgeBaseID == "" {
		return "", fmt.Errorf("knowledge_base_id is required")
	}
	if args.Page < 1 {
		args.Page = 1
	}
	if s.knowledgeBaseTools == nil {
		return "", fmt.Errorf("knowledge base service is unavailable")
	}
	items, total, err := s.knowledgeBaseTools.ListVisibleFiles(ctx, call.UserID, knowledgeBaseID, args.Page, platformListPageSize)
	if err != nil {
		return "", err
	}
	type summary struct {
		ID               string `json:"content_id"`
		Title            string `json:"title"`
		Kind             string `json:"kind"`
		SizeBytes        int64  `json:"size_bytes"`
		ProcessingStatus string `json:"processing_status"`
		RAGReady         bool   `json:"rag_ready"`
		ChunkCount       int    `json:"chunk_count"`
	}
	result := make([]summary, 0, len(items))
	for _, item := range items {
		result = append(result, summary{
			ID: item.FileID, Title: item.FileName, Kind: item.FileCategory, SizeBytes: item.SizeBytes,
			ProcessingStatus: item.ProcessingStatus, RAGReady: item.RAGReady, ChunkCount: item.ChunkCount,
		})
	}
	return marshalPlatformResult(map[string]interface{}{
		"knowledge_base_id": knowledgeBaseID, "contents": result, "total": total, "page": args.Page,
	})
}

func (s *Service) platformCreateKnowledgeBaseContent(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		KnowledgeBaseID string `json:"knowledge_base_id"`
		Title           string `json:"title"`
		Content         string `json:"content"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	knowledgeBaseID := strings.TrimSpace(args.KnowledgeBaseID)
	title := strings.TrimSpace(args.Title)
	if knowledgeBaseID == "" || title == "" {
		return "", fmt.Errorf("knowledge_base_id and title are required")
	}
	if utf8.RuneCountInString(title) > 255 {
		return "", fmt.Errorf("title exceeds 255 characters")
	}
	if len(args.Content) > platformFileWriteLimitBytes {
		return "", fmt.Errorf("content exceeds %d bytes limit", platformFileWriteLimitBytes)
	}
	if s.knowledgeBaseTools == nil {
		return "", fmt.Errorf("knowledge base service is unavailable")
	}
	file, err := s.knowledgeBaseTools.CreateUserContent(ctx, call.UserID, knowledgeBaseID, appknowledgebase.UserContentInput{
		FileName: title, Content: args.Content,
	})
	if err != nil {
		return "", err
	}
	if file == nil || strings.TrimSpace(file.FileID) == "" {
		return "", fmt.Errorf("knowledge base content creation returned no file")
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.create_knowledge_base_content", file.FileID, map[string]interface{}{
		"knowledge_base_id": knowledgeBaseID, "title": title, "bytes": len(args.Content),
	})
	return marshalPlatformResult(map[string]interface{}{
		"knowledge_base_id": knowledgeBaseID, "content_id": file.FileID, "title": file.FileName,
		"status": "created", "note": "content is queued for extraction and RAG indexing",
	})
}

func (s *Service) platformUpdateKnowledgeBaseContent(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		KnowledgeBaseID string  `json:"knowledge_base_id"`
		ContentID       string  `json:"content_id"`
		Title           *string `json:"title"`
		Content         *string `json:"content"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	knowledgeBaseID := strings.TrimSpace(args.KnowledgeBaseID)
	contentID := strings.TrimSpace(args.ContentID)
	if knowledgeBaseID == "" || contentID == "" {
		return "", fmt.Errorf("knowledge_base_id and content_id are required")
	}
	if (args.Title == nil) == (args.Content == nil) {
		return "", fmt.Errorf("exactly one of title or content is required")
	}
	if args.Title != nil {
		trimmed := strings.TrimSpace(*args.Title)
		if trimmed == "" || utf8.RuneCountInString(trimmed) > 255 {
			return "", fmt.Errorf("title must be between 1 and 255 characters")
		}
		args.Title = &trimmed
	}
	if args.Content != nil && len(*args.Content) > platformFileWriteLimitBytes {
		return "", fmt.Errorf("content exceeds %d bytes limit", platformFileWriteLimitBytes)
	}
	if s.knowledgeBaseTools == nil {
		return "", fmt.Errorf("knowledge base service is unavailable")
	}
	if err := s.knowledgeBaseTools.UpdateUserContent(ctx, call.UserID, knowledgeBaseID, contentID, appknowledgebase.UserContentPatch{
		FileName: args.Title, Content: args.Content,
	}); err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.update_knowledge_base_content", contentID, map[string]interface{}{
		"knowledge_base_id": knowledgeBaseID, "renamed": args.Title != nil, "content_updated": args.Content != nil,
	})
	return marshalPlatformResult(map[string]interface{}{
		"knowledge_base_id": knowledgeBaseID, "content_id": contentID, "status": "updated",
		"note": "updated content will be re-extracted and re-indexed",
	})
}

func (s *Service) platformDeleteKnowledgeBaseContent(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		KnowledgeBaseID string `json:"knowledge_base_id"`
		ContentID       string `json:"content_id"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	knowledgeBaseID := strings.TrimSpace(args.KnowledgeBaseID)
	contentID := strings.TrimSpace(args.ContentID)
	if knowledgeBaseID == "" || contentID == "" {
		return "", fmt.Errorf("knowledge_base_id and content_id are required")
	}
	if s.knowledgeBaseTools == nil {
		return "", fmt.Errorf("knowledge base service is unavailable")
	}
	result, err := s.knowledgeBaseTools.DeleteUserContent(ctx, call.UserID, knowledgeBaseID, contentID)
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.delete_knowledge_base_content", contentID, map[string]interface{}{
		"knowledge_base_id": knowledgeBaseID,
	})
	return marshalPlatformResult(map[string]interface{}{
		"knowledge_base_id": knowledgeBaseID, "content_id": result.FileID, "status": "removed",
	})
}
