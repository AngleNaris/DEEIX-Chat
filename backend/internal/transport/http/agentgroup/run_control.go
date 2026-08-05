package agentgroup

import (
	"context"

	appconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/conversation"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
)

// runControlService 群组运行控制服务（由 application/conversation.Service 实现）。
// 重试/取消/放弃的编排与检查点恢复逻辑位于应用层，HTTP 层只做协议转换。
type runControlService interface {
	// RetryAgentGroupRunStep 从暂停的失败步骤原地重试：复用原快照，
	// 仅为被重试步骤追加一次新 Attempt，流式转发群组事件与正文增量。
	RetryAgentGroupRunStep(ctx context.Context, input appconversation.RetryAgentGroupRunInput, onDelta func(string) error) (*appconversation.SendMessageResult, error)
	// CancelAgentGroupRun 取消进行中的群组运行（回到 paused_retryable）。
	CancelAgentGroupRun(ctx context.Context, userID uint, runPublicID string) (bool, error)
	// AbandonAgentGroupRun 放弃已暂停/被阻塞的群组运行。
	AbandonAgentGroupRun(ctx context.Context, userID uint, runPublicID string) (*appconversation.SendMessageResult, error)
	// GetConversationByPublicID 按公开 ID 查询用户会话（刷新恢复路径解析会话数值 ID）。
	GetConversationByPublicID(ctx context.Context, userID uint, publicID string) (*model.Conversation, error)
}
