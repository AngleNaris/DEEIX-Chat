package agentgroup

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.uber.org/zap"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/conv"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

// roleReader 提供角色查询的窄接口（由 conversation 服务实现）。
type roleReader interface {
	GetConversationRole(ctx context.Context, userID uint, publicID string) (*domainconversation.ConversationRole, error)
}

// projectReader 提供项目查询的窄接口（由 conversation 服务实现）。
type projectReader interface {
	GetConversationProject(ctx context.Context, userID uint, publicID string) (*domainconversation.ConversationProject, error)
}

// settingsReader 提供运行时系统设置读取的窄接口（由 settings 服务实现）。
type settingsReader interface {
	RuntimeValuesByNamespace(ctx context.Context, namespace string) (map[string]string, error)
}

// auditWriter 写入审计日志（由审计服务注入；nil 时静默跳过）。
type auditWriter interface {
	Write(ctx context.Context, requestID string, actorUserID uint, action string, resource string, resourceID string, ip string, userAgent string, detail interface{})
}

// Service 封装 Agent 群组业务能力。
type Service struct {
	repo          repository.AgentGroupRepository
	roleReader    roleReader
	projectReader projectReader
	settings      settingsReader
	logger        *zap.Logger
	now           func() time.Time
	auditWriter   auditWriter
}

// NewService 创建群组服务。
func NewService(
	repo repository.AgentGroupRepository,
	roleReader roleReader,
	projectReader projectReader,
	settings settingsReader,
	logger *zap.Logger,
) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		repo:          repo,
		roleReader:    roleReader,
		projectReader: projectReader,
		settings:      settings,
		logger:        logger,
		now:           time.Now,
	}
}

// SetAuditWriter 注入审计写入器（应用启动时接线）。
func (s *Service) SetAuditWriter(writer auditWriter) {
	s.auditWriter = writer
}

// RecordAudit 记录群组域审计日志（§18：所有变更记录操作者与资源 ID）。
func (s *Service) RecordAudit(ctx context.Context, input AuditInput) {
	if s.auditWriter == nil {
		return
	}
	s.auditWriter.Write(
		ctx,
		strings.TrimSpace(input.RequestID),
		input.UserID,
		strings.TrimSpace(input.Action),
		strings.TrimSpace(input.Resource),
		strings.TrimSpace(input.ResourceID),
		strings.TrimSpace(input.ClientIP),
		strings.TrimSpace(input.UserAgent),
		input.Detail,
	)
}

// agentGroupEnabled 读取运行时系统设置 agent_group.enabled（默认关闭）。
func (s *Service) agentGroupEnabled(ctx context.Context) (bool, error) {
	values, err := s.settings.RuntimeValuesByNamespace(ctx, domainagentgroup.FeatureFlagNamespace)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(values[domainagentgroup.FeatureFlagKeyEnabled]) == "true", nil
}

// requireEnabled 校验功能开关，关闭时拒绝操作。
func (s *Service) requireEnabled(ctx context.Context) error {
	enabled, err := s.agentGroupEnabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrAgentGroupFeatureDisabled
	}
	return nil
}

// runtimeLimits 读取运行时限制（缺失时使用默认值）。
func (s *Service) runtimeLimits(ctx context.Context) domainagentgroup.RunSnapshotLimits {
	values, err := s.settings.RuntimeValuesByNamespace(ctx, domainagentgroup.FeatureFlagNamespace)
	if err != nil {
		s.logger.Warn("agentgroup: read runtime limits failed, using defaults", zap.Error(err))
		return domainagentgroup.RunSnapshotLimits{
			MaxStepsPerRun:     domainagentgroup.DefaultMaxStepsPerRun,
			MaxAttemptsPerStep: domainagentgroup.DefaultMaxAttemptsPerStep,
		}
	}
	return domainagentgroup.RunSnapshotLimits{
		MaxStepsPerRun:     parsePositiveInt(values[domainagentgroup.SettingKeyMaxStepsPerRun], domainagentgroup.DefaultMaxStepsPerRun),
		MaxAttemptsPerStep: parsePositiveInt(values[domainagentgroup.SettingKeyMaxAttemptsPerStep], domainagentgroup.DefaultMaxAttemptsPerStep),
	}
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

// newPublicID 生成公开 ID（UUID 去连字符）。
func newPublicID() string {
	return conv.NormalizePublicID(uuid.NewString())
}

// exceedsRuneLimit 判断字符串按 rune 计是否超限。
func exceedsRuneLimit(value string, limit int) bool {
	return limit >= 0 && utf8.RuneCountInString(value) > limit
}
