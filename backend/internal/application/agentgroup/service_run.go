package agentgroup

import (
	"context"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
)

// IsEnabled 返回 Agent 群组功能开关状态。
func (s *Service) IsEnabled(ctx context.Context) (bool, error) {
	return s.agentGroupEnabled(ctx)
}

// GetAgentGroupRun 查询群组运行。
func (s *Service) GetAgentGroupRun(ctx context.Context, userID uint, publicID string) (*domainagentgroup.Run, error) {
	run, err := s.repo.GetAgentGroupRunByPublicID(ctx, userID, publicID)
	if err != nil {
		return nil, s.translateRepoError(err)
	}
	return run, nil
}

// GetAgentGroupRunDetail 查询群组运行及其步骤、尝试完整视图。
func (s *Service) GetAgentGroupRunDetail(ctx context.Context, userID uint, publicID string) (*domainagentgroup.RunDetail, error) {
	detail, err := s.repo.GetAgentGroupRunDetail(ctx, userID, publicID)
	if err != nil {
		return nil, s.translateRepoError(err)
	}
	return detail, nil
}

// GetAgentGroupRunDetailByClientRunID 按会话与父流式运行 ID（client_run_id）查询运行完整视图。
// 前端消息只携带 clientRunID，刷新恢复时间线时经此查询（会话公开 ID 已由调用方解析为数值 ID）。
func (s *Service) GetAgentGroupRunDetailByClientRunID(ctx context.Context, userID uint, conversationID uint, clientRunID string) (*domainagentgroup.RunDetail, error) {
	run, err := s.repo.GetAgentGroupRunByClientRunID(ctx, conversationID, clientRunID)
	if err != nil {
		return nil, s.translateRepoError(err)
	}
	detail, err := s.repo.GetAgentGroupRunDetail(ctx, userID, run.PublicID)
	if err != nil {
		return nil, s.translateRepoError(err)
	}
	return detail, nil
}

// GetActiveAgentGroupRun 查询会话当前未结束的群组运行。
func (s *Service) GetActiveAgentGroupRun(ctx context.Context, conversationID uint) (*domainagentgroup.Run, error) {
	run, err := s.repo.GetActiveAgentGroupRunByConversation(ctx, conversationID)
	if err != nil {
		return nil, s.translateRepoError(err)
	}
	return run, nil
}
