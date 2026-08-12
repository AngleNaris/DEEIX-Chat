package conversation

import (
	"context"
	"fmt"
	"strings"

	appcredentials "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/credentials"
)

// 凭据平台工具处理器：用户凭据是系统级功能（与提示词插入一致），
// 默认启用（不依赖 platform_tools.enabled 开关）；写操作不进入 ask 审批
// （审批摘要会回显参数，避免密钥在确认卡片中明文展示），始终直接执行并记录审计。
// 模型上下文只见凭据描述；执行时通过 {{credential: name}} 占位符在工具调用层展开。

// platformCredentialUnavailableError 凭据服务未注入时返回。
func platformCredentialUnavailableError() error {
	return fmt.Errorf("credential service is unavailable")
}

// platformListCredentials 列出用户凭据描述（不含密钥）。
func (s *Service) platformListCredentials(ctx context.Context, call platformToolCallContext) (string, error) {	if s.credentials == nil {
		return "", platformCredentialUnavailableError()
	}
	views, err := s.credentials.ListCredentials(ctx, call.UserID)
	if err != nil {
		return "", err
	}
	summary := make([]map[string]interface{}, 0, len(views))
	for _, view := range views {
		item := map[string]interface{}{
			"name":        view.Name,
			"type":        view.Type,
			"description": view.Description,
			"updated_at":  view.UpdatedAt,
		}
		if len(view.Meta) > 0 {
			item["meta"] = view.Meta
		}
		summary = append(summary, item)
	}
	return marshalPlatformResult(map[string]interface{}{
		"count":       len(summary),
		"credentials": summary,
		"usage_hint":  "凭据值从不直接返回。需要在命令/参数中使用凭据时，请用 {{credential: name}} 占位符引用（name 为上表中的名称），执行时会自动展开为真实值。不要在回答或命令中输出密钥明文。",
	})
}

// platformCreateCredential 创建凭据。
func (s *Service) platformCreateCredential(ctx context.Context, call platformToolCallContext) (string, error) {
	if s.credentials == nil {
		return "", platformCredentialUnavailableError()
	}
	var args struct {
		Name        string            `json:"name"`
		Type        string            `json:"type"`
		Description string            `json:"description"`
		Value       string            `json:"value"`
		Meta        map[string]string `json:"meta"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Name) == "" {
		return "", fmt.Errorf("credential name is required")
	}
	if strings.TrimSpace(args.Value) == "" {
		return "", fmt.Errorf("credential value is required")
	}
	view, err := s.credentials.CreateCredential(ctx, call.UserID, appcredentials.UpsertInput{
		Name:        args.Name,
		Type:        args.Type,
		Description: args.Description,
		Value:       args.Value,
		Meta:        args.Meta,
	})
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.credential_create", view.Name, map[string]interface{}{
		"type": view.Type,
	})
	return marshalPlatformResult(map[string]interface{}{
		"created":     true,
		"name":        view.Name,
		"type":        view.Type,
		"description": view.Description,
	})
}

// platformUpdateCredential 更新凭据（value 为空表示不修改密钥）。
func (s *Service) platformUpdateCredential(ctx context.Context, call platformToolCallContext) (string, error) {
	if s.credentials == nil {
		return "", platformCredentialUnavailableError()
	}
	var args struct {
		Name        string            `json:"name"`
		Type        string            `json:"type"`
		Description string            `json:"description"`
		Value       string            `json:"value"`
		Meta        map[string]string `json:"meta"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Name) == "" {
		return "", fmt.Errorf("credential name is required")
	}
	view, err := s.credentials.UpdateCredentialByName(ctx, call.UserID, args.Name, appcredentials.UpsertInput{
		Name:        args.Name,
		Type:        args.Type,
		Description: args.Description,
		Value:       args.Value,
		Meta:        args.Meta,
	})
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.credential_update", view.Name, nil)
	return marshalPlatformResult(map[string]interface{}{
		"updated": true,
		"name":    view.Name,
	})
}

// platformDeleteCredential 删除凭据。
func (s *Service) platformDeleteCredential(ctx context.Context, call platformToolCallContext) (string, error) {
	if s.credentials == nil {
		return "", platformCredentialUnavailableError()
	}
	var args struct {
		Name string `json:"name"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return "", fmt.Errorf("credential name is required")
	}
	if err := s.credentials.DeleteCredentialByName(ctx, call.UserID, name); err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.credential_delete", name, nil)
	return marshalPlatformResult(map[string]interface{}{
		"deleted": true,
		"name":    name,
	})
}
