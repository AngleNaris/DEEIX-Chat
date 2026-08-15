package channel

import (
	"context"
	"strings"
)

// ResolveDefaultModel 返回指定任务类型的第一个可用模型名，但不选择具体路由、API key 或半开探针。
func (s *Service) ResolveDefaultModel(ctx context.Context, input ResolveRouteInput) (string, error) {
	models, err := s.ListActiveModels(ctx, 0)
	if err != nil {
		return "", err
	}
	for _, item := range models {
		name := strings.TrimSpace(item.PlatformModelName)
		if name == "" || !defaultRouteModelMatchesTask(item.KindsJSON, input.TaskType) {
			continue
		}
		candidate := input
		candidate.PlatformModelName = name
		candidate.ExcludedRouteIDs = nil
		if err := s.ValidateModelRouteReference(ctx, candidate); err == nil {
			return name, nil
		}
	}
	return "", ErrAllRoutesUnavailable
}

// ResolveDefaultRoute 返回指定任务类型的第一个可路由模型。
//
// 内部服务任务使用 follow 时，如果当前会话模型不支持该任务类型，会走这里兜底；
// 兜底仍必须经过任务类型过滤和真实路由解析，避免把图片模型误用于文本任务。
func (s *Service) ResolveDefaultRoute(ctx context.Context, input ResolveRouteInput) (*ResolvedRoute, error) {
	models, err := s.ListActiveModels(ctx, 0)
	if err != nil {
		return nil, err
	}
	for _, item := range models {
		name := strings.TrimSpace(item.PlatformModelName)
		if name == "" {
			continue
		}
		if !defaultRouteModelMatchesTask(item.KindsJSON, input.TaskType) {
			continue
		}
		route, routeErr := s.ResolveRoute(ctx, ResolveRouteInput{
			PlatformModelName: name,
			TaskType:          input.TaskType,
			Scope:             input.Scope,
			UserID:            input.UserID,
			ConversationID:    input.ConversationID,
			RequestID:         strings.TrimSpace(input.RequestID),
		})
		if routeErr == nil {
			return route, nil
		}
	}
	return nil, ErrAllRoutesUnavailable
}

// defaultRouteModelMatchesTask 先按模型 kind 做轻量过滤，减少默认兜底时对不匹配模型的无意义路由解析。
func defaultRouteModelMatchesTask(kindsJSON string, taskType string) bool {
	kinds := parseKinds(kindsJSON)
	if len(kinds) == 0 {
		return true
	}
	switch NormalizeTaskType(taskType) {
	case TaskTypeImageGeneration:
		return hasModelKind(kinds, modelKindImageGen)
	case TaskTypeImageEdit:
		return hasModelKind(kinds, modelKindImageEdit)
	case TaskTypeVideoGeneration:
		return hasModelKind(kinds, modelKindVideoGen)
	default:
		return hasModelKind(kinds, modelKindChat)
	}
}
