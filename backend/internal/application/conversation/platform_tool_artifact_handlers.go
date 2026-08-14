package conversation

import (
	"context"
	"fmt"
	"strings"

	appartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/artifact"
)

// 平台工具制品域：save_artifact / list_artifacts / delete_artifact / share_artifact。
// 制品是用户保存的 AI 生成内容（HTML/JS/CSS/文本），支持公开分享；
// 写工具（save/delete/share）受 write_enabled 开关与批准模式管控。

// platformSaveArtifact 保存或更新制品（写操作，受批准模式管控）。
func (s *Service) platformSaveArtifact(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		ArtifactID string `json:"artifact_id"`
		Title      string `json:"title"`
		Kind       string `json:"kind"`
		Code       string `json:"code"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	title := strings.TrimSpace(args.Title)
	if title == "" {
		return "", fmt.Errorf("title is required")
	}
	if len(title) > appartifact.MaxTitleLen {
		return "", fmt.Errorf("title exceeds %d characters", appartifact.MaxTitleLen)
	}
	code := strings.TrimSpace(args.Code)
	if code == "" {
		return "", fmt.Errorf("code is required")
	}
	if len(code) > appartifact.MaxCodeLen {
		return "", fmt.Errorf("code exceeds %d bytes limit", appartifact.MaxCodeLen)
	}
	if s.artifactSvc == nil {
		return "", fmt.Errorf("artifact service is unavailable")
	}
	input := appartifact.CreateInput{
		Title:          title,
		Kind:           strings.ToLower(strings.TrimSpace(args.Kind)),
		Code:           code,
		ConversationID: call.ConversationID,
	}
	var savedID string
	var err error
	if strings.TrimSpace(args.ArtifactID) != "" {
		item, updateErr := s.artifactSvc.UpdateArtifact(ctx, call.UserID, strings.TrimSpace(args.ArtifactID), input)
		err = updateErr
		if item != nil {
			savedID = item.ArtifactPublicID
		}
	} else {
		item, createErr := s.artifactSvc.CreateArtifact(ctx, call.UserID, "", input)
		err = createErr
		if item != nil {
			savedID = item.ArtifactPublicID
		}
	}
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.save_artifact", savedID, map[string]interface{}{
		"title": title,
		"kind":  input.Kind,
		"bytes": len(code),
	})
	return marshalPlatformResult(map[string]interface{}{
		"artifact_id": savedID,
		"title":       title,
		"kind":        input.Kind,
		"status":      "saved",
		"note":        "artifact saved; user can share it from the artifact list",
	})
}

// platformListArtifacts 列出用户制品（只读）。
func (s *Service) platformListArtifacts(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if s.artifactSvc == nil {
		return "", fmt.Errorf("artifact service is unavailable")
	}
	items, total, err := s.artifactSvc.ListArtifacts(ctx, call.UserID, args.Page, args.PageSize)
	if err != nil {
		return "", err
	}
	type artifactSummary struct {
		ArtifactID string `json:"artifact_id"`
		Title      string `json:"title"`
		Kind       string `json:"kind"`
		ShareID    string `json:"share_id,omitempty"`
		UpdatedAt  string `json:"updated_at"`
	}
	summaries := make([]artifactSummary, 0, len(items))
	for _, item := range items {
		summary := artifactSummary{
			ArtifactID: item.ArtifactPublicID,
			Title:      item.Title,
			Kind:       item.Kind,
			UpdatedAt:  item.UpdatedAt,
		}
		if item.Share != nil {
			summary.ShareID = item.Share.ShareID
		}
		summaries = append(summaries, summary)
	}
	return marshalPlatformResult(map[string]interface{}{
		"total":     total,
		"page":      args.Page,
		"artifacts": summaries,
	})
}

// platformDeleteArtifact 删除制品（写操作，受批准模式管控）。
func (s *Service) platformDeleteArtifact(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		ArtifactID string `json:"artifact_id"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	artifactID := strings.TrimSpace(args.ArtifactID)
	if artifactID == "" {
		return "", fmt.Errorf("artifact_id is required")
	}
	if s.artifactSvc == nil {
		return "", fmt.Errorf("artifact service is unavailable")
	}
	if err := s.artifactSvc.DeleteArtifact(ctx, call.UserID, artifactID); err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.delete_artifact", artifactID, nil)
	return marshalPlatformResult(map[string]interface{}{
		"artifact_id": artifactID,
		"deleted":     true,
	})
}

// platformShareArtifact 创建制品的公开分享（写操作，受批准模式管控）。
func (s *Service) platformShareArtifact(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		ArtifactID string `json:"artifact_id"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	artifactID := strings.TrimSpace(args.ArtifactID)
	if artifactID == "" {
		return "", fmt.Errorf("artifact_id is required")
	}
	if s.artifactSvc == nil {
		return "", fmt.Errorf("artifact service is unavailable")
	}
	share, err := s.artifactSvc.CreateShare(ctx, call.UserID, artifactID)
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.share_artifact", artifactID, map[string]interface{}{
		"share_id": share.ShareID,
	})
	return marshalPlatformResult(map[string]interface{}{
		"artifact_id": artifactID,
		"title":       share.TitleSnapshot,
		"share_id":    share.ShareID,
		"share_url":   s.absoluteArtifactShareURL(share.ShareID),
		"note":        "anyone with the share link can view this artifact",
	})
}

// absoluteArtifactShareURL 拼接制品分享绝对链接（配置了公开域名时），否则退回相对路径。
func (s *Service) absoluteArtifactShareURL(shareID string) string {
	if s == nil || s.cfg == nil {
		return "/share/artifact?artifact_id=" + shareID
	}
	base := strings.TrimRight(strings.TrimSpace(s.cfg.Snapshot().PublicWebBaseURL), "/")
	if base == "" {
		return "/share/artifact?artifact_id=" + shareID
	}
	return base + "/share/artifact?artifact_id=" + shareID
}
