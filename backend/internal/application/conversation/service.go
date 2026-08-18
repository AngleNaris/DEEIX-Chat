package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	appartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/artifact"
	appbilling "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/billing"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	appcompact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/compact"
	appcredentials "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/credentials"
	appdoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/doccard"
	appdynamicprompt "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/dynamicprompt"
	appembedding "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/embedding"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/extraction"
	appstorage "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/objectstorage"
	appprocessing "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/processing"
	apppromptpreset "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/promptpreset"
	apprag "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/rag"
	appskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/skill"
	appupload "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/upload"
	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	domaindoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/doccard"
	domaindynamicprompt "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/dynamicprompt"
	domainmcp "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/mcp"
	domainmemory "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/memory"
	domainpromptpreset "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/promptpreset"
	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/embedding"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/mcp"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"go.uber.org/zap"
)

const (
	// semanticRecallDeadline：语义召回截止时限，超时后优雅跳过，不阻塞 LLM 关键路径。
	semanticRecallDeadline = 200 * time.Millisecond
)

type routeResolver interface {
	ResolveRoute(ctx context.Context, input channel.ResolveRouteInput) (*channel.ResolvedRoute, error)
	MarkRouteFailure(ctx context.Context, route *channel.ResolvedRoute, cause error)
	MarkRouteSuccess(ctx context.Context, route *channel.ResolvedRoute)
}

// defaultRouteResolver 表示按任务类型解析默认路由的可选能力。
// conversation 只依赖这个窄接口，不直接感知 channel.Service 的具体实现。
type defaultRouteResolver interface {
	ResolveDefaultRoute(ctx context.Context, input channel.ResolveRouteInput) (*channel.ResolvedRoute, error)
}

// agentGroupRoutePreflightResolver 仅校验群组快照中的模型引用并冻结默认模型名，
// 不选择具体执行路由、API key 或半开熔断探针。
type agentGroupRoutePreflightResolver interface {
	ValidateModelRouteReference(ctx context.Context, input channel.ResolveRouteInput) error
	ResolveDefaultModel(ctx context.Context, input channel.ResolveRouteInput) (string, error)
}

type memoryRecorder interface {
	UpsertUserMemory(ctx context.Context, userID uint, memoryKey string, value string, scope string, updatedBy string) error
	// DeleteUserMemory 供平台工具 delete_memory 删除用户长期记忆。
	DeleteUserMemory(ctx context.Context, userID uint, memoryKey string) error
	ListUserMemories(ctx context.Context, userID uint) ([]domainmemory.UserMemory, error)
	SearchUserMemoriesByEmbedding(ctx context.Context, userID uint, queryEmbedding []float32, topK int, minSimilarity float64) ([]domainmemory.UserMemory, error)
	UpsertUserMemoryEmbedding(ctx context.Context, userID uint, memoryKey string, expectedValue string, embedding []float32) error
}

type skillResolver interface {
	ResolveAvailable(ctx context.Context, userID uint, id uint) (*domainskill.Skill, error)
	ListVisible(ctx context.Context, userID uint, input appskill.ListInput) ([]domainskill.Skill, int64, error)
	GetPackageFile(ctx context.Context, userID uint, skillID uint, filePath string) ([]byte, error)
	// CreateUser 供平台工具 create_skill 创建用户自己的技能。
	CreateUser(ctx context.Context, userID uint, input appskill.WriteInput) (*domainskill.Skill, error)
	// UpdateUser 供平台工具 update_skill 更新用户自己的技能。
	UpdateUser(ctx context.Context, userID uint, id uint, input appskill.PatchInput) (*domainskill.Skill, error)
	// DeleteUser 供平台工具 delete_skill 删除用户自己的技能。
	DeleteUser(ctx context.Context, userID uint, id uint) error
}

type mcpToolResolver interface {
	ListToolsByIDs(ctx context.Context, toolIDs []uint) ([]domainmcp.Tool, error)
	ListServers(ctx context.Context) ([]domainmcp.Server, error)
	GetServer(ctx context.Context, serverID uint) (*domainmcp.Server, error)
}

// agentGroupResolver 解析会话绑定的 Agent 群组（由应用层注入）。
type agentGroupResolver interface {
	GetAgentGroupByPublicID(ctx context.Context, userID uint, publicID string) (*domainagentgroup.Group, error)
	// CountAgentGroupReferencesByRole 统计引用角色的未移除群组成员关系数量（角色删除保护，§18）。
	CountAgentGroupReferencesByRole(ctx context.Context, roleID uint) (int64, error)
	// CountAgentGroupReferencesByProject 统计项目下群组数量（项目删除保护，§18）。
	CountAgentGroupReferencesByProject(ctx context.Context, projectID uint) (int64, error)
}

// agentGroupSettingsReader 读取 agent_group 运行时设置（由 settings 服务注入）。
// platformToolsSettings 复用同一接口读取 platform_tools 命名空间。
type agentGroupSettingsReader interface {
	RuntimeValuesByNamespace(ctx context.Context, namespace string) (map[string]string, error)
}

// userProfileReader 读取用户档案字段，供系统提示词模板变量
// {{language}} / {{username}} / 时区（{{date}} 等）使用（由 user 服务适配注入）。
type userProfileReader interface {
	GetUserProfile(ctx context.Context, userID uint) (locale string, username string, timezone string, err error)
}

// docCardReader 文档卡片能力（由 doccard 服务注入）：
// 发送路径用 ListDocCards 做关键字触发注入；平台工具 save/delete_doc_card 走读写方法。
type docCardReader interface {
	ListDocCards(ctx context.Context, userID uint) ([]appdoccard.CardView, error)
	UpsertDocCard(ctx context.Context, userID uint, publicID string, input appdoccard.UpsertInput, updatedBy string) (*domaindoccard.DocCard, error)
	DeleteDocCard(ctx context.Context, userID uint, publicID string) error
}

// dynamicPromptReader 动态提示词能力（由 dynamicprompt 服务注入），
// 供提示词模板变量 {{script: name}} 展开与平台工具 create/update/delete/run_dynamic_prompt 使用。
type dynamicPromptReader interface {
	ListDynamicPrompts(ctx context.Context, userID uint) ([]appdynamicprompt.PromptView, error)
	UpsertDynamicPrompt(ctx context.Context, userID uint, publicID string, input appdynamicprompt.UpsertInput, updatedBy string) (*domaindynamicprompt.DynamicPrompt, error)
	DeleteDynamicPrompt(ctx context.Context, userID uint, publicID string) error
	RunDynamicPrompt(ctx context.Context, userID uint, publicID string) (string, error)
}

// credentialResolver 用户凭据能力（由 credentials 服务注入）：
// 模型上下文只见凭据描述，执行层经 ResolveValue 解密展开 {{credential: name}} 占位符。
// 平台工具按 name 操作（模型只认识 name）；HTTP API 按 publicID。
type credentialResolver interface {
	// ListCredentials 列出凭据视图（无密钥，平台工具 credential_list 用）。
	ListCredentials(ctx context.Context, userID uint) ([]appcredentials.View, error)
	// CreateCredential 创建凭据（平台工具 credential_create 用）。
	CreateCredential(ctx context.Context, userID uint, input appcredentials.UpsertInput) (*appcredentials.View, error)
	// UpdateCredentialByName 按名称更新凭据（平台工具 credential_update 用）。
	UpdateCredentialByName(ctx context.Context, userID uint, name string, input appcredentials.UpsertInput) (*appcredentials.View, error)
	// DeleteCredentialByName 按名称删除凭据（平台工具 credential_delete 用）。
	DeleteCredentialByName(ctx context.Context, userID uint, name string) error
	// ResolveValue 解密凭据值（{{credential: name}} 执行层展开用；未找到返回空串）。
	ResolveValue(ctx context.Context, userID uint, name string) (string, error)
}

// promptPresetResolver 预制提示词能力（由 promptpreset 服务注入），
// 供平台工具 list/create/update/delete_prompt_preset 使用。
type promptPresetResolver interface {
	ListVisible(ctx context.Context, userID uint, input apppromptpreset.ListInput) ([]domainpromptpreset.PromptPreset, int64, error)
	CreateUser(ctx context.Context, userID uint, input apppromptpreset.WriteInput) (*domainpromptpreset.PromptPreset, error)
	UpdateUser(ctx context.Context, userID uint, id uint, input apppromptpreset.PatchInput) (*domainpromptpreset.PromptPreset, error)
	DeleteUser(ctx context.Context, userID uint, id uint) error
}

// userSettingsWriter 读写用户个人设置（白名单 key，由 usersettings 服务注入）。
type userSettingsWriter interface {
	ListSettings(ctx context.Context, userID uint) (map[string]string, error)
	PatchSettings(ctx context.Context, userID uint, patches map[string]string) (map[string]string, error)
}

// AgentGroupMemberCreateInput 平台工具创建群组成员输入（导出供 app 层适配器转换，避免导入环）。
type AgentGroupMemberCreateInput struct {
	RolePublicID    string
	MemberType      string
	ModelOverride   string
	ReasoningEffort string
	DutyInstruction string
}

// AgentGroupCreateInput 平台工具创建群组输入。
type AgentGroupCreateInput struct {
	Name               string
	Description        string
	CoordinationPrompt string
	Supervisor         AgentGroupMemberCreateInput
	Workers            []AgentGroupMemberCreateInput
}

// AgentGroupUpdateInput 平台工具更新群组元数据输入。
type AgentGroupUpdateInput struct {
	Name               *string
	Description        *string
	CoordinationPrompt *string
}

// agentGroupWriter 创建/列出/更新/删除 Agent 群组（由 agentgroup 服务注入，平台工具 create/list/update/delete_agent_group 使用）。
type agentGroupWriter interface {
	CreateAgentGroup(ctx context.Context, userID uint, input AgentGroupCreateInput) (*domainagentgroup.Group, error)
	ListAgentGroups(ctx context.Context, userID uint) ([]domainagentgroup.Group, error)
	UpdateAgentGroup(ctx context.Context, userID uint, publicID string, input AgentGroupUpdateInput) (*domainagentgroup.Group, error)
	DeleteAgentGroup(ctx context.Context, userID uint, publicID string) error
}

type auditWriter interface {
	Write(ctx context.Context, requestID string, actorUserID uint, action string, resource string, resourceID string, ip string, userAgent string, detail interface{})
}

// generatedMediaDownloader 定义会话用例所需的最小媒体下载端口，避免应用层感知 HTTP 细节。
type generatedMediaDownloader interface {
	DownloadImage(ctx context.Context, sourceURL string, trustedProviderEndpoint string, maxBytes int64) ([]byte, string, error)
	DownloadVideo(ctx context.Context, sourceURL string, trustedProviderEndpoint string, apiKey string, maxBytes int64) ([]byte, string, error)
}

// mediaArtifactResponseTooLarge 是跨层错误能力契约，不要求应用层依赖具体适配器错误类型。
type mediaArtifactResponseTooLarge interface {
	MediaArtifactResponseTooLarge()
}

// isMediaArtifactResponseTooLarge 判断下载错误是否应映射为既有文件大小业务错误。
func isMediaArtifactResponseTooLarge(err error) bool {
	var target mediaArtifactResponseTooLarge
	return errors.As(err, &target)
}

type basicServiceBillingContextKey struct{}

type basicServiceBillingContext struct {
	UserID         uint
	ConversationID uint
}

// Service 封装会话业务能力。
type Service struct {
	cfg                   *config.Runtime
	repo                  repository.ConversationRepository
	cache                 repository.ConversationCacheRepository
	routeResolver         routeResolver
	memoryRecorder        memoryRecorder
	mcpRepo               mcpToolResolver
	agentGroupRepo        agentGroupResolver
	agentGroupRunStore    repository.AgentGroupRunRepository
	agentGroupSettings    agentGroupSettingsReader
	agentGroupRunLocks    sync.Map                    // conversationID (uint) → *sync.Mutex，同会话串行
	platformToolsSettings agentGroupSettingsReader    // platform_tools 运行时设置
	platformApprovals     *platformWriteApprovalStore // ask 模式待批准写操作
	reindexScheduler      *fileReindexScheduler       // write_file 延迟重建（debounce）
	userSettingsSvc       userSettingsWriter          // 用户个人设置读写（平台工具 list/update_user_setting）
	agentGroupWriter      agentGroupWriter            // Agent 群组创建/列表（平台工具 create/list_agent_group）
	userProfile           userProfileReader           // 用户档案读取（模板变量 {{language}}/{{username}}）
	artifactSvc           *appartifact.Service        // 制品保存/分享（平台工具 save/list/delete/share_artifact）
	docCards              docCardReader               // 文档卡片读取（关键字触发注入）
	docCardCache          sync.Map                    // userID (uint) → *cachedDocCards
	dynamicPrompts        dynamicPromptReader         // 动态提示词读取（{{script: name}} 展开 + 平台工具脚本管理）
	credentials           credentialResolver          // 用户凭据（{{credential: name}} 展开 + 平台工具管理）
	dynamicPromptCache    sync.Map                    // userID (uint) → *cachedDynamicPrompts
	promptPresets         promptPresetResolver        // 预制提示词（平台工具 list/create/update/delete_prompt_preset）
	llmClient             *llm.Client
	mediaDownloader       generatedMediaDownloader
	mcpClient             *mcp.Client
	uploadSvc             *appupload.Service
	compactSvc            *appcompact.Service
	embeddingSvc          *appembedding.Service
	processingSvc         *appprocessing.Service
	extractSvc            *extraction.Service
	ragSvc                *apprag.Service
	skillResolver         skillResolver
	billingSvc            *appbilling.Service
	auditWriter           auditWriter
	storeProvider         appstorage.Provider
	logger                *zap.Logger
	toolLimiters          sync.Map
	generationStreams     *generationStreamRegistry
	snapshotCache         sync.Map // conversationID (uint) → *cachedSnapshot
	userMemCache          sync.Map // userID (uint) → *cachedUserMemories
	userSettingCache      sync.Map // "userID:key" (string) → *cachedUserSetting
	imageContextCache     *preparedConversationImageCache
}

func (s *Service) llmAttribution() (string, string) {
	if s == nil || s.cfg == nil {
		return "", ""
	}
	cfg := s.cfg.Snapshot()
	return cfg.PublicWebBaseURL, cfg.AppName
}

// AttachmentInput 是消息附件入参（应用层内部传递，无序列化标签）。
type AttachmentInput struct {
	FileObjID              uint
	FileID                 string
	Kind                   string
	FileName               string
	MimeType               string
	DetectedMIME           string
	FileCategory           string
	FileSize               int64
	SHA256                 string
	StoragePath            string
	MetaJSON               string
	PageCount              int
	ProcessingStatus       string
	ProcessingReady        bool
	ProcessingErrorCode    string
	ProcessingErrorMessage string
	ExtractStatus          string
	EmbedStatus            string
	ExtractedText          string
	RagOptOut              bool // 用户是否关闭该文件的 RAG；RAG 段直接复用，无需重查 DB
	ChunkCount             int  // 向量分块数；RAG 缓存 key 需要
	Current                bool // 是否为本轮用户显式上传的附件
	MessageRole            string
	ContextMode            string
	DurationSeconds        int64 // 仅生成视频附件使用。
}

// SendMessageInput 定义消息发送请求。
type SendMessageInput struct {
	UserID                  uint
	ConversationID          uint
	RequestID               string
	ContentType             string
	Content                 string
	PlatformModelName       string
	Options                 map[string]interface{}
	ClientRunID             string
	FileIDs                 []string
	SelectedToolIDs         []uint
	SkillIDs                []uint
	HTMLVisualPromptEnabled bool
	ParentMessagePublicID   string
	SourceMessagePublicID   string
	BranchReason            string
	Cancelable              bool
	// OnEvent 用于向调用方推送中间事件（如 rag_search），流式场景使用。
	OnEvent func(eventType string, payload map[string]interface{}) error
}

// SetSkillResolver 注入会话技能解析器。
func (s *Service) SetSkillResolver(resolver skillResolver) {
	s.skillResolver = resolver
}

// SendMessageResult 返回用户消息与 AI 消息。
type SendMessageResult struct {
	UserMessage           model.Message
	AssistantMessage      model.Message
	MetadataRefreshHint   string
	Billable              bool
	UpstreamID            uint
	UpstreamName          string
	PlatformModelName     string
	RoutedBindingCode     string
	UpstreamModelName     string
	UpstreamProtocol      string
	EffectiveOptions      map[string]interface{}
	UsageSpeed            string
	UsageServiceTier      string
	UsageSource           string
	RawUsageJSON          string
	CacheWrite5mTokens    int64
	CacheWrite1hTokens    int64
	ServerSideToolUsage   map[string]int64
	LatencyMS             int64
	DurationSeconds       int64
	StartedAt             time.Time
	postBillingCompaction *postBillingCompactionTask
}

// MessageFeedbackResult 返回反馈后的当前状态（内部传输，不携带序列化标记）。
type MessageFeedbackResult struct {
	MessageID       uint
	MessagePublicID string
	MyFeedback      string
	ThumbsUpCount   int64
	ThumbsDownCount int64
}

// NewService 创建服务。
func NewService(
	cfg config.Config,
	repo repository.ConversationRepository,
	cache repository.ConversationCacheRepository,
	routeResolver routeResolver,
	memoryRecorder memoryRecorder,
	llmClient *llm.Client,
	mediaDownloader generatedMediaDownloader,
	mcpClient *mcp.Client,
	embedClient *embedding.Client,
	uploadSvc *appupload.Service,
	compactSvc *appcompact.Service,
	embeddingSvc *appembedding.Service,
	processingSvc *appprocessing.Service,
	extractSvc *extraction.Service,
	ragSvc *apprag.Service,
	logger *zap.Logger,
) *Service {
	return NewServiceWithRuntime(config.NewRuntime(cfg), repo, cache, routeResolver, memoryRecorder, llmClient, mediaDownloader, mcpClient, embedClient, uploadSvc, compactSvc, embeddingSvc, processingSvc, extractSvc, ragSvc, logger)
}

// NewServiceWithRuntime 创建使用运行时配置容器的服务。
func NewServiceWithRuntime(
	cfg *config.Runtime,
	repo repository.ConversationRepository,
	cache repository.ConversationCacheRepository,
	routeResolver routeResolver,
	memoryRecorder memoryRecorder,
	llmClient *llm.Client,
	mediaDownloader generatedMediaDownloader,
	mcpClient *mcp.Client,
	embedClient *embedding.Client,
	uploadSvc *appupload.Service,
	compactSvc *appcompact.Service,
	embeddingSvc *appembedding.Service,
	processingSvc *appprocessing.Service,
	extractSvc *extraction.Service,
	ragSvc *apprag.Service,
	logger *zap.Logger,
) *Service {
	svc := &Service{
		cfg:               cfg,
		repo:              repo,
		cache:             cache,
		routeResolver:     routeResolver,
		memoryRecorder:    memoryRecorder,
		llmClient:         llmClient,
		mediaDownloader:   mediaDownloader,
		mcpClient:         mcpClient,
		compactSvc:        compactSvc,
		embeddingSvc:      embeddingSvc,
		processingSvc:     processingSvc,
		extractSvc:        extractSvc,
		ragSvc:            ragSvc,
		storeProvider:     appstorage.NewRuntimeProvider(cfg, nil),
		logger:            logger,
		generationStreams: newGenerationStreamRegistry(cache, defaultGenerationStreamOptions()),
		imageContextCache: defaultPreparedConversationImageCache(),
	}
	if extractSvc == nil {
		extractSvc = extraction.NewServiceWithRuntime(cfg)
	}
	extractSvc.SetObjectStoreProvider(svc.storeProvider)
	if embeddingSvc == nil {
		embeddingSvc = appembedding.NewServiceWithRuntime(cfg, repo, extractSvc, embedClient, logger)
	}
	if processingSvc == nil {
		processingSvc = appprocessing.NewServiceWithRuntime(cfg, repo, cache, extractSvc, embeddingSvc, logger, appprocessing.DefaultExtractorVersion)
	}
	if uploadSvc == nil {
		uploadSvc = appupload.NewServiceWithRuntime(cfg, repo, logger, appupload.Hooks{
			ResolveCapability: func(ctx context.Context) appupload.FileCapability {
				capability := svc.resolveChatFileCapability(ctx)
				return appupload.FileCapability{
					RAGAvailable:         capability.RAGAvailable,
					EffectiveDocMaxBytes: capability.EffectiveDocMaxBytes,
				}
			},
			InitializeUploadedFile:   processingSvc.InitializeUploadedFile,
			EnsureImageOCRProcessing: processingSvc.EnsureImageOCRProcessing,
		}, appupload.ErrorSet{
			InvalidFileReference: ErrInvalidFileReference,
			InvalidFileName:      ErrInvalidFileName,
			FileNotFound:         ErrFileNotFound,
			FileInUse:            ErrFileInUse,
			StorageQuotaExceeded: ErrStorageQuotaExceeded,
			FileTooLarge:         ErrFileTooLarge,
			MIMEBlocked:          ErrMIMEBlocked,
			EmbeddingUnavailable: ErrEmbeddingUnavailable,
			DangerousMIMEType:    ErrDangerousMIMEType,
		}, appprocessing.DefaultExtractorVersion)
	}
	uploadSvc.SetObjectStoreProvider(svc.storeProvider)
	if compactSvc == nil {
		compactSvc = appcompact.NewServiceWithRuntime(cfg, repo, logger)
	}
	if ragSvc == nil {
		ragSvc = apprag.NewServiceWithRuntime(cfg, repo, cache, embedClient)
	}
	svc.uploadSvc = uploadSvc
	svc.compactSvc = compactSvc
	svc.embeddingSvc = embeddingSvc
	svc.processingSvc = processingSvc
	svc.extractSvc = extractSvc
	svc.ragSvc = ragSvc
	// 平台工具：ask 批准存储 + write_file 延迟重建调度器（debounce，缓冲窗口读运行时设置）。
	svc.platformApprovals = newPlatformWriteApprovalStore()
	svc.reindexScheduler = newFileReindexScheduler(
		10*time.Second,
		func() time.Duration { return svc.ResolvePlatformReindexDelay(context.Background()) },
		processingSvc.ProcessFile,
		logger,
	)
	// 注入 LLM 语义压缩回调（在 svc 完全初始化后绑定）
	svc.compactSvc.SetLLMSummarizer(svc.callCompactLLM)
	return svc
}

// InvalidateMemoryCache 清除指定用户的记忆缓存，使下一次请求重新从 DB 加载。
// 由外部（memory handler 写入后）通过回调触发，避免循环依赖。
func (s *Service) InvalidateMemoryCache(userID uint) {
	s.userMemCache.Delete(userID)
}

// SetBillingService 注入计费服务，用于记录标题、标签、上下文压缩等基础 LLM 服务用量。
func (s *Service) SetBillingService(billingSvc *appbilling.Service) {
	s.billingSvc = billingSvc
}

// SetAuditWriter 注入会话域审计写入器。
func (s *Service) SetAuditWriter(writer auditWriter) {
	s.auditWriter = writer
}

func (s *Service) SetObjectStoreProvider(provider appstorage.Provider) {
	if provider != nil {
		s.storeProvider = provider
		if s.uploadSvc != nil {
			s.uploadSvc.SetObjectStoreProvider(provider)
		}
		if s.extractSvc != nil {
			s.extractSvc.SetObjectStoreProvider(provider)
		}
	}
}

// SetMCPRepository 注入会话运行所需的 MCP 工具查询能力。
func (s *Service) SetMCPRepository(repo mcpToolResolver) {
	s.mcpRepo = repo
}

// SetAgentGroupResolver 注入会话群组解析器。
func (s *Service) SetAgentGroupResolver(resolver agentGroupResolver) {
	s.agentGroupRepo = resolver
}

// SetAgentGroupRunStore 注入群组运行持久化仓储（nil 时群组会话走普通消息路径之外的能力受限）。
func (s *Service) SetAgentGroupRunStore(store repository.AgentGroupRunRepository) {
	s.agentGroupRunStore = store
}

// SetAgentGroupSettings 注入 agent_group 运行时设置读取器。
func (s *Service) SetAgentGroupSettings(reader agentGroupSettingsReader) {
	s.agentGroupSettings = reader
}

// SetPlatformToolsSettings 注入 platform_tools 运行时设置读取器（enabled/write_enabled/延迟秒数）。
func (s *Service) SetPlatformToolsSettings(reader agentGroupSettingsReader) {
	s.platformToolsSettings = reader
}

// SetUserSettingsService 注入用户个人设置读写服务（平台工具 list_user_settings / update_user_setting 使用）。
func (s *Service) SetUserSettingsService(writer userSettingsWriter) {
	s.userSettingsSvc = writer
}

// SetAgentGroupWriter 注入 Agent 群组创建/列表能力（平台工具 create_agent_group / list_agent_groups 使用）。
func (s *Service) SetAgentGroupWriter(writer agentGroupWriter) {
	s.agentGroupWriter = writer
}

// SetUserProfileReader 注入用户档案读取（系统提示词模板变量 {{language}}/{{username}}）。
func (s *Service) SetUserProfileReader(reader userProfileReader) {
	s.userProfile = reader
}

// SetArtifactService 注入制品服务（平台工具 save/list/delete/share_artifact 使用）。
func (s *Service) SetArtifactService(svc *appartifact.Service) {
	s.artifactSvc = svc
}

// SetDocCardReader 注入文档卡片读取（关键字触发注入）。
func (s *Service) SetDocCardReader(reader docCardReader) {
	s.docCards = reader
}

// InvalidateDocCardCache 清除用户文档卡片缓存（写入/删除后即时生效）。
func (s *Service) InvalidateDocCardCache(userID uint) {
	if userID != 0 {
		s.docCardCache.Delete(userID)
	}
}

// SetDynamicPromptReader 注入动态提示词读取（{{script: name}} 展开 + 平台工具脚本管理）。
func (s *Service) SetDynamicPromptReader(reader dynamicPromptReader) {
	s.dynamicPrompts = reader
}

// SetCredentialReader 注入用户凭据（{{credential: name}} 展开 + 平台工具管理）。
func (s *Service) SetCredentialReader(reader credentialResolver) {
	s.credentials = reader
}

// SetPromptPresetResolver 注入预制提示词能力（平台工具 list/create/update/delete_prompt_preset）。
func (s *Service) SetPromptPresetResolver(resolver promptPresetResolver) {
	s.promptPresets = resolver
}

// InvalidateDynamicPromptCache 清除用户动态提示词缓存（写入/删除后即时生效）。
func (s *Service) InvalidateDynamicPromptCache(userID uint) {
	if userID != 0 {
		s.dynamicPromptCache.Delete(userID)
	}
}

// ResolvePlatformReindexDelay 读取平台工具文件重建缓冲窗口（秒），缺省 60。
func (s *Service) ResolvePlatformReindexDelay(ctx context.Context) time.Duration {
	if s.platformToolsSettings == nil {
		return 60 * time.Second
	}
	values, err := s.platformToolsSettings.RuntimeValuesByNamespace(ctx, platformToolsNamespace)
	if err != nil {
		return 60 * time.Second
	}
	seconds, parseErr := strconv.Atoi(strings.TrimSpace(values[platformToolsKeyReindexDelay]))
	if parseErr != nil || seconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

// ApprovePlatformWrite 批准一条待确认的写操作并执行（ask 模式；返回执行错误与记录摘要）。
func (s *Service) ApprovePlatformWrite(ctx context.Context, approvalID string, userID uint, approve bool) (string, error) {
	status := platformApprovalStatusRejected
	if approve {
		status = platformApprovalStatusApproved
	}
	record := s.platformApprovals.claim(approvalID, userID, status)
	if record == nil {
		return "", ErrPlatformApprovalNotFound
	}
	if !approve {
		if s.auditWriter != nil {
			s.auditWriter.Write(ctx, record.RequestID, userID, "platform_tools.reject", "platform_tools", record.ID, "", "", map[string]interface{}{
				"tool": record.ToolName,
			})
		}
		return marshalApprovalSummary(record)
	}
	executionToolName := strings.TrimSpace(record.ExecutionToolName)
	if executionToolName == "" {
		executionToolName = strings.TrimSpace(record.ToolName)
	}
	entry, ok := platformToolRegistry()[executionToolName]
	if !ok || entry.handler == nil {
		return "", fmt.Errorf("platform tool %q is not registered", executionToolName)
	}
	argumentsJSON := s.expandCredentialRefsInJSON(ctx, record.UserID, record.ArgumentsJSON)
	output, err := entry.handler(s, ctx, platformToolCallContext{
		UserID:         record.UserID,
		ConversationID: record.ConversationID,
		RequestID:      record.RequestID,
		Arguments:      json.RawMessage(argumentsJSON),
	})
	if err != nil {
		return "", err
	}
	summary, summaryErr := marshalApprovalSummary(record)
	if summaryErr != nil {
		return output, nil
	}
	return summary, nil
}
