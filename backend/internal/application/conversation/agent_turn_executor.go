package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	apprag "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/rag"
	domainbilling "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/billing"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/mcp"
	platformtracing "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/observability/tracing"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/traceid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Agent 群组回合事件类型。编排器会把事件加上 groupRunID/stepID/attemptID/sequence
// 后转发为群组流式事件（group_step_*）。
const (
	AgentTurnEventStatus     = "status"      // 中间状态（如 rag_search）
	AgentTurnEventDelta      = "delta"       // 流式正文增量
	AgentTurnEventThinking   = "thinking"    // 思考增量
	AgentTurnEventUsage      = "usage"       // 用量事件（实时展示与最终账单同口径）
	AgentTurnEventToolCall   = "tool_call"   // 模型发起工具调用
	AgentTurnEventToolResult = "tool_result" // 工具执行结果
)

// AgentTurnEvent 是一次内部 Actor 回合中的流式事件。
// Payload 始终携带 actor 元数据（actor_id/actor_name/actor_type/actor_icon/actor_color/model），
// 供编排器原样转发为群组流式事件。
type AgentTurnEvent struct {
	Type    string
	Payload map[string]interface{}
}

// AgentTurnInput 描述一次内部 Actor 回合的输入。
// Actor 不创建顶层聊天消息，也不写入消息树；历史通过 DomainMessages 显式传入。
type AgentTurnInput struct {
	UserID         uint
	ConversationID uint
	RequestID      string
	// ClientRunID 是计费与事件幂等键，由编排器以 groupRunID:stepID:attemptNo 合成。
	ClientRunID string
	ActorID     string
	ActorName   string
	ActorType   string
	ActorIcon   string
	ActorColor  string
	// PlatformModelName 是最终生效的模型（成员覆盖 > 角色默认 > 平台默认由编排器解析）。
	PlatformModelName string
	// ReasoningEffort 是思考强度语义档位（""/low/medium/high/xhigh）。
	// 空串表示继承用户全局默认（chat.default_reasoning_effort）；协议不支持时不注入。
	ReasoningEffort string
	// SystemPrompt 只包含项目级提示词层（项目提示词 + 角色提示词 + 群组协调协议），
	// 平台级与模型级规则由本执行器内部经 resolveMessageSystemPromptInjection 注入，避免重复。
	SystemPrompt string
	UserContent  string
	// DomainMessages 是会话历史（不包含本次用户需求）。
	DomainMessages  []model.Message
	FileIDs         []string
	SkillIDs        []uint
	SelectedToolIDs []uint
	Options         map[string]interface{}
	// Stream 为 true 时通过 OnEvent 推送正文增量；思考与用量事件始终推送。
	Stream  bool
	OnEvent func(AgentTurnEvent) error
	// Ledger 是工具执行幂等账本；非 nil 时由调用方（群组编排器）注入，
	// 使重试步骤可复用上次尝试已成功的工具结果，避免重复副作用与重复计费。
	Ledger *toolExecutionLedger
	// PersistToolCalls 为 true 时把工具行写入 conversation_tool_calls（MessageID=0），
	// 供跨尝试幂等、失败诊断与运行详情展示；普通 Actor 回合不落盘。
	PersistToolCalls bool
}

// AgentTurnOutput 是一次内部 Actor 回合的执行结果。
type AgentTurnOutput struct {
	Text              string
	ReasoningText     string
	ToolCallRows      []model.ToolCall
	Usage             *domainbilling.UsageLedger
	Route             *channel.ResolvedRoute
	PlatformModelName string
	UpstreamModelName string
	UpstreamProtocol  string
	RoutedBindingCode string
	EffectiveOptions  map[string]interface{}
	LatencyMS         int64
	StartedAt         time.Time
}

const (
	agentTurnUsageRenewalInterval = 30 * time.Minute
	agentTurnUsageRenewalTimeout  = 5 * time.Second
)

// ExecuteAgentTurn 执行一次内部 Actor 回合。
//
// 与 SendMessage 共享模型路由、文件/RAG、Skill、MCP、工具、Trace 与计费链路，
// 但存在三点关键差异：
//  1. 默认不创建顶层聊天消息：工具调用不写入 conversation_tool_calls 表（群组尝试经
//     PersistToolCalls 显式开启），也无 TraceRecorder 挂载；
//  2. 输出不触发压缩、标题生成、历史反馈等消息级收尾；
//  3. Actor 使用独立模型（input.PlatformModelName），且不跨轮复用上游 stateful 续接。
func (s *Service) ExecuteAgentTurn(ctx context.Context, input AgentTurnInput) (*AgentTurnOutput, error) {
	startedAt := time.Now()
	if s.routeResolver == nil || s.llmClient == nil {
		return nil, ErrModelRouteNotConfigured
	}
	conversation, err := s.repo.GetConversationByUser(ctx, input.ConversationID, input.UserID)
	if err != nil {
		return nil, err
	}
	if conversation == nil {
		return nil, ErrConversationNotFound
	}

	// 1. 路由解析：与普通消息同一套路由/错误映射，Actor 可走独立模型。
	routeResolveInput := channel.ResolveRouteInput{
		PlatformModelName: strings.TrimSpace(input.PlatformModelName),
		TaskType:          channel.TaskTypeChat,
		Scope:             channel.RouteScopeUser,
		UserID:            input.UserID,
		ConversationID:    input.ConversationID,
		RequestID:         strings.TrimSpace(input.RequestID),
	}
	route, err := s.resolveAgentTurnRoute(ctx, routeResolveInput)
	if err != nil {
		return nil, err
	}
	cfg := s.cfg.Snapshot()
	reasoningContentPassback := s.reasoningContentPassbackEnabled(ctx, input.UserID, route)

	// 2. 工具运行时：Actor 不落消息树，图片附件处理工具不可用，附件改走直传上下文。
	toolRuntime, err := s.resolveSelectedToolRuntime(ctx, input.SelectedToolIDs)
	if err != nil {
		return nil, err
	}
	if toolRuntime.attachmentProcessor != nil {
		toolRuntime = toolRuntime.withoutAttachmentProcessor()
	}

	// 3. 文件上下文：复用与普通消息一致的解析、就绪等待与全量/RAG 规划。
	fileMode := "auto"
	if fm, fmErr := s.getUserSettingCached(ctx, input.UserID, "chat.file_mode"); fmErr == nil && strings.TrimSpace(fm) != "" {
		fileMode = strings.TrimSpace(fm)
	}
	capability := s.resolveChatFileCapability(ctx)
	conversationAttachments, err := s.resolveConversationFileContext(ctx, input.UserID, input.FileIDs, input.FileIDs)
	if err != nil {
		return nil, err
	}
	conversationAttachments = bindAttachmentMessageRoles(conversationAttachments, nil)
	conversationAttachments, err = s.hydrateAttachmentsForSend(ctx, input.UserID, conversationAttachments, func(eventType string, payload map[string]interface{}) error {
		payload["status"] = eventType
		return emitAgentTurnEvent(input, AgentTurnEventStatus, payload)
	})
	if err != nil {
		return nil, err
	}
	fileContextPlan := buildConversationFileContextPlan(conversationAttachments, fileMode, cfg, route.UpstreamModel, route.ModelCapabilitiesJSON, capability.RAGAvailable)

	// RAG 检索：与普通消息同口径，失败时优雅降级为附件全文/跳过。
	promptScopeMessages := s.applyContextTokenBudget(input.DomainMessages, route.UpstreamModel, route.ModelCapabilitiesJSON, reasoningContentPassback)
	ragQuery := buildRAGQuery(promptScopeMessages, input.UserContent, cfg.RAGQueryHistoryTurns)
	retrievalRAGFallbacks := make([]ragFallbackEvidence, 0)
	ragContextChunks := make([]model.RAGChunk, 0)
	if cfg.RAGEnabled && s.ragSvc != nil && len(fileContextPlan.RAGAttachments) > 0 {
		readyObjs := fileContextPlanRAGObjects(fileContextPlan.RAGAttachments)
		_ = emitAgentTurnEvent(input, AgentTurnEventStatus, map[string]interface{}{
			"status":  "rag_search",
			"message": "正在检索相关内容…",
		})
		ragCtx, ragSpan := platformtracing.Start(ctx, "conversation.rag.retrieve",
			trace.WithAttributes(
				attribute.Int64("conversation.id", int64(input.ConversationID)),
				attribute.Int64("user.id", int64(input.UserID)),
				attribute.Int("conversation.rag.file_count", len(readyObjs)),
			),
		)
		ragCallCtx := ragCtx
		ragCancel := func() {}
		if cfg.RAGWaitReadyMS > 0 {
			ragCallCtx, ragCancel = context.WithTimeout(ragCtx, time.Duration(cfg.RAGWaitReadyMS)*time.Millisecond)
		}
		ragResult, ragErr := s.ragSvc.RetrieveWithStatus(ragCallCtx, apprag.RetrieveInput{
			UserID:   input.UserID,
			Query:    ragQuery,
			FileObjs: readyObjs,
		})
		ragCancel()
		platformtracing.RecordError(ragSpan, ragErr)
		ragSpan.SetAttributes(
			attribute.String("conversation.rag.status", string(ragResult.Status)),
			attribute.String("conversation.rag.reason", strings.TrimSpace(ragResult.Reason)),
			attribute.Int("conversation.rag.candidate_count", ragResult.CandidateCount),
			attribute.Int("conversation.rag.filtered_count", ragResult.FilteredCount),
			attribute.Float64("conversation.rag.max_score", float64(ragResult.MaxScore)),
			attribute.Bool("conversation.rag.cached", ragResult.Cached),
		)
		ragSpan.End()
		contextAssembler := NewContextAssembler(int64(cfg.ContextMaxInputTokens))
		ragChunks := contextAssembler.DeduplicateRAGChunks(ragResult.Chunks)
		if ragErr != nil {
			if s.logger != nil {
				s.logger.Warn("rag_retrieval_failed",
					zap.String("trace_id", traceid.FromContext(ctx)),
					zap.Uint("user_id", input.UserID),
					zap.Error(ragErr),
				)
			}
			fallbacks, _ := splitRetrievalFallbackAttachments(fileContextPlan.RAGAttachments, cfg)
			fallbackReason := normalizeRAGFallbackReason(ragResult.Status, "rag_error")
			evidences := ragFallbackEvidencesFromAttachments(fallbacks, fallbackReason, strings.TrimSpace(ragErr.Error()))
			retrievalRAGFallbacks = append(retrievalRAGFallbacks, evidences...)
		} else if len(ragChunks) == 0 {
			fallbacks, _ := splitRetrievalFallbackAttachments(fileContextPlan.RAGAttachments, cfg)
			ragStatus := normalizeRAGFallbackReason(ragResult.Status, "rag_empty")
			evidences := ragFallbackEvidencesFromAttachments(fallbacks, ragStatus, "")
			retrievalRAGFallbacks = append(retrievalRAGFallbacks, evidences...)
		} else {
			ragContextChunks = append(ragContextChunks, ragChunks...)
		}
	}
	stableFullContextAttachments := append([]AttachmentInput{}, fileContextPlan.FullAttachments...)
	stableFullContextAttachments = append(stableFullContextAttachments, ragFallbackEvidenceAttachments(retrievalRAGFallbacks)...)
	userCtx := userContextInput{
		Attachments: imageAttachmentsForCurrentUser(stableFullContextAttachments),
		RAGChunks:   ragContextChunks,
	}

	// 4. Skill：与普通消息同口径（按用户级最大可选数收敛）。
	skillPrompts, err := s.resolveSkillPrompts(ctx, SendMessageInput{
		UserID:         input.UserID,
		ConversationID: input.ConversationID,
		SkillIDs:       input.SkillIDs,
	})
	if err != nil {
		return nil, err
	}

	// 5. 提示词组装：SystemPrompt 只含项目级层，平台/模型级规则由 executor 注入。
	routePromptInput := messageRoutePromptInput{
		UserContent:             input.UserContent,
		AppendUserContent:       true,
		ProjectSystemPrompt:     strings.TrimSpace(input.SystemPrompt),
		HTMLVisualPromptEnabled: false,
		DomainMessages:          input.DomainMessages,
		StableAttachments:       stableFullContextAttachments,
		DynamicContext:          userCtx,
		PreferencePrompt:        "",
		SkillPrompts:            skillPrompts,
		ToolRuntime:             toolRuntime,
		SkipImageAttachments:    false,
		Config:                  cfg,
	}
	buildRoutePrompt := func(currentRoute *channel.ResolvedRoute) (PromptPlan, bool, error) {
		passbackEnabled := s.reasoningContentPassbackEnabled(ctx, input.UserID, currentRoute)
		currentInput := routePromptInput
		currentInput.ReasoningContentPassback = passbackEnabled
		plan, buildErr := s.buildMessageRoutePrompt(ctx, currentRoute, currentInput)
		return plan, passbackEnabled, buildErr
	}
	promptPlan, reasoningContentPassback, err := buildRoutePrompt(route)
	if err != nil {
		return nil, err
	}

	// 6. 计费授权：镜像 transport 层模式（30min 续约 + 5s 释放超时）。
	authorization, stopRenewal, err := s.authorizeAgentTurnUsage(ctx, conversation, input, route)
	if err != nil {
		return nil, err
	}
	authorizationSettled := false
	defer func() {
		stopRenewal()
		if !authorizationSettled {
			releaseCtx, cancel := context.WithTimeout(context.Background(), agentTurnUsageRenewalTimeout)
			_ = s.ReleaseSendMessageUsageAuthorization(releaseCtx, authorization)
			cancel()
		}
	}()

	// 7. 生成准备（含实时事件、用量累积与流式/非流式切换）。
	llmMessages := promptPlan.Messages
	attributionReferer, attributionTitle := s.llmAttribution()
	routeConfig := messageRouteConfig(route, attributionReferer, attributionTitle)
	optionsWithReasoningEffort := s.injectReasoningEffortOptions(ctx, input.UserID, input.ReasoningEffort, route, input.Options)
	filteredOptions := filterModelOptions(optionsWithReasoningEffort, route.Protocol, modelOptionPolicyConfig{
		Mode:                  cfg.ModelOptionPolicyMode,
		AllowedPathsJSON:      cfg.ModelOptionAllowedPaths,
		DeniedPathsJSON:       cfg.ModelOptionDeniedPaths,
		ModelCapabilitiesJSON: route.ModelCapabilitiesJSON,
	})
	filteredOptions = withMessageRouteReasoningPassbackOptions(
		filteredOptions,
		input.Options,
		route,
		reasoningContentPassback,
		llmMessages,
	)
	promptCacheSessionKey := strings.TrimSpace(conversation.SessionKey)
	if promptCacheSessionKey == "" {
		promptCacheSessionKey = strings.TrimSpace(conversation.PublicID)
	}
	promptCacheKey, filteredOptions, llmMessages := configureOpenAIPromptCacheRequestForRoute(
		route,
		promptCacheSessionKey,
		filteredOptions,
		llmMessages,
	)
	generateInput := llm.GenerateInput{
		RequestID:              strings.TrimSpace(input.RequestID),
		ConversationID:         input.ConversationID,
		ConversationPublicID:   strings.TrimSpace(conversation.PublicID),
		ConversationSessionKey: strings.TrimSpace(conversation.SessionKey),
		PromptCacheKey:         promptCacheKey,
		Messages:               cloneLLMMessages(llmMessages),
		Tools:                  toolRuntime.definitions,
		Options:                filteredOptions,
	}
	applyOpenAIResponsesInstructions(route, routeConfig.Endpoint, &generateInput)
	estimatedPromptTokens := estimateGenerateInputTokens(generateInput)

	maxLLMCalls := s.resolveMaxLLMCallsPerRun()
	llmRequestCount := 0
	attemptHadSideEffect := false
	visibleDeltaCount := 0
	usageAccumulator := &messageUsageAccumulator{}
	toolLedger := input.Ledger
	if toolLedger == nil {
		toolLedger = newToolExecutionLedger()
	}
	toolCallRows := make([]model.ToolCall, 0)
	var streamedText strings.Builder
	preferStream := input.Stream
	onDelta := func(delta string) error {
		if delta == "" {
			return nil
		}
		visibleDeltaCount++
		if err := emitAgentTurnEvent(input, AgentTurnEventDelta, map[string]interface{}{"delta": delta}); err != nil {
			return err
		}
		streamedText.WriteString(delta)
		return nil
	}
	emitThinkingDelta := func(delta string, kind string) error {
		if delta == "" {
			return nil
		}
		return emitAgentTurnEvent(input, AgentTurnEventThinking, map[string]interface{}{"delta": delta, "kind": kind})
	}

	runGenerate := func(currentInput llm.GenerateInput) (*llm.GenerateOutput, error) {
		attemptObservation := &generationAttemptObservation{}
		streamRequested := preferStream && onDelta != nil
		streamSupported := llm.SupportsStreamingAdapter(routeConfig.Protocol)
		var callVisibleText strings.Builder
		emitCallVisibleDelta := func(delta string) error {
			if delta != "" {
				attemptObservation.markObservable()
			}
			if err := onDelta(delta); err != nil {
				return err
			}
			callVisibleText.WriteString(delta)
			return nil
		}
		emitNonStreamingOutput := func(output *llm.GenerateOutput) error {
			if output == nil || (strings.TrimSpace(output.Text) == "" && output.Reasoning == nil) {
				return nil
			}
			cleanText, thinkText := splitAssistantOutputThinkingContent(output.Text)
			if output.Reasoning != nil {
				if err := emitThinkingDelta(output.Reasoning.Text, messageTraceThinkKindContent); err != nil {
					return err
				}
			} else if strings.TrimSpace(thinkText) != "" {
				if err := emitThinkingDelta(thinkText, messageTraceThinkKindContent); err != nil {
					return err
				}
			}
			if cleanText == "" && strings.TrimSpace(thinkText) == "" {
				cleanText = strings.TrimSpace(output.Text)
			}
			if err := emitCallVisibleDelta(cleanText); err != nil {
				return err
			}
			output.Text = cleanText
			return nil
		}
		usageAccumulator.beginCall(currentInput)
		generationCtx, generationSpan := platformtracing.Start(ctx, "conversation.agent_turn.llm.generate",
			trace.WithAttributes(
				attribute.Int64("conversation.id", int64(input.ConversationID)),
				attribute.Int64("user.id", int64(input.UserID)),
				attribute.String("agent.actor_id", input.ActorID),
				attribute.String("agent.actor_type", input.ActorType),
				attribute.String("llm.model", routeConfig.UpstreamModel),
				attribute.String("llm.protocol", routeConfig.Protocol),
				attribute.String("llm.endpoint", routeConfig.Endpoint),
				attribute.Bool("llm.stream", streamRequested && streamSupported),
				attribute.Bool("llm.tools_disabled", currentInput.DisableTools),
				attribute.Int("llm.message_count", len(currentInput.Messages)),
				attribute.Int("llm.tool_count", len(currentInput.Tools)),
			),
		)
		var generateErr error
		if !streamRequested || !streamSupported {
			llmRequestCount++
			output, callErr := s.llmClient.Generate(generationCtx, routeConfig, currentInput)
			generateErr = callErr
			if callErr == nil && streamRequested {
				generateErr = emitNonStreamingOutput(output)
			}
			if generateErr == nil {
				usageAccumulator.finishCall(output != nil && output.Usage.InputTokens > 0)
			}
			platformtracing.RecordError(generationSpan, generateErr)
			generationSpan.End()
			return output, generateErr
		}
		thinkingRouter := &thinkingDeltaRouter{}
		callStreamUsage := llm.Usage{}
		llmRequestCount++
		output, streamErr := s.llmClient.GenerateStream(generationCtx, routeConfig, currentInput, func(event llm.GenerateStreamEvent) error {
			if event.Usage != (llm.Usage{}) {
				attemptHadSideEffect = true
				// 上游流式 usage 通常是“本次 LLM 调用累计值”，先换算成增量再累加，保证实时展示与最终账单一致。
				usageDelta := diffLLMUsage(event.Usage, callStreamUsage)
				callStreamUsage = event.Usage
				currentUsage := usageAccumulator.addObservedUsage(usageDelta)
				if input.OnEvent != nil {
					attemptObservation.markObservable()
					if err := emitAgentTurnUsageEvent(input, currentUsage); err != nil {
						return err
					}
				}
			}
			if event.Reasoning != nil && event.Reasoning.Text != "" {
				attemptHadSideEffect = true
				if err := emitThinkingDelta(event.Reasoning.Text, event.Reasoning.Kind); err != nil {
					return err
				}
			}
			if onDelta == nil || event.Delta == "" {
				return nil
			}
			visibleDelta, thinkDelta := thinkingRouter.consume(event.Delta)
			if thinkDelta != "" {
				attemptHadSideEffect = true
				if err := emitThinkingDelta(thinkDelta, messageTraceThinkKindContent); err != nil {
					return err
				}
			}
			if visibleDelta == "" {
				return nil
			}
			return emitCallVisibleDelta(visibleDelta)
		})
		generateErr = streamErr
		if generateErr == nil {
			visibleTail, thinkTail := thinkingRouter.flush()
			if thinkTail != "" {
				if err := emitThinkingDelta(thinkTail, messageTraceThinkKindContent); err != nil {
					generateErr = err
				}
			}
			if generateErr == nil && visibleTail != "" {
				generateErr = emitCallVisibleDelta(visibleTail)
			}
			if generateErr == nil && output != nil {
				output.Text = callVisibleText.String()
			}
		}
		if !attemptHadSideEffect && llmRequestCount < maxLLMCalls &&
			attemptObservation.canRetry(generateErr, shouldFallbackToNonStreaming) {
			llmRequestCount++
			output, generateErr = s.llmClient.Generate(generationCtx, routeConfig, currentInput)
			if generateErr == nil {
				generateErr = emitNonStreamingOutput(output)
			}
		}
		if generateErr == nil {
			usageAccumulator.finishCall((callStreamUsage.InputTokens > 0) || (output != nil && output.Usage.InputTokens > 0))
		}
		platformtracing.RecordError(generationSpan, generateErr)
		generationSpan.End()
		return output, generateErr
	}

	handleCanceledGeneration := func(generateErr error) bool {
		return generateErr != nil && ctx.Err() != nil
	}

	var upstreamOutput *llm.GenerateOutput
	upstreamOutput, err = runGenerate(generateInput)
	if handleCanceledGeneration(err) {
		return nil, ErrMessageGenerationCanceled
	}
	attemptedRouteIDs := []uint{route.RouteID}
	routeFailureRecorded := false
	for canFailoverMessageRoute(len(attemptedRouteIDs), llmRequestCount, maxLLMCalls, visibleDeltaCount, attemptHadSideEffect, err) {
		failedRoute := route
		failedErr := err
		s.routeResolver.MarkRouteFailure(ctx, failedRoute, failedErr)
		routeFailureRecorded = true

		routeResolveInput.ExcludedRouteIDs = append([]uint(nil), attemptedRouteIDs...)
		nextRoute, resolveErr := s.routeResolver.ResolveRoute(ctx, routeResolveInput)
		if resolveErr != nil {
			if s.logger != nil {
				s.logger.Warn("upstream_route_failover_unavailable",
					zap.String("trace_id", traceid.FromContext(ctx)),
					zap.Uint("conversation_id", input.ConversationID),
					zap.Uint("failed_route_id", failedRoute.RouteID),
					zap.Error(resolveErr),
				)
			}
			err = failedErr
			break
		}

		route = nextRoute
		attemptedRouteIDs = append(attemptedRouteIDs, route.RouteID)
		routeFailureRecorded = false
		nextPromptPlan, nextReasoningContentPassback, buildErr := buildRoutePrompt(route)
		if buildErr != nil {
			return nil, buildErr
		}
		promptPlan = nextPromptPlan
		reasoningContentPassback = nextReasoningContentPassback
		llmMessages = promptPlan.Messages
		routeConfig = messageRouteConfig(route, attributionReferer, attributionTitle)
		optionsWithReasoningEffort := s.injectReasoningEffortOptions(ctx, input.UserID, input.ReasoningEffort, route, input.Options)
		filteredOptions = filterModelOptions(optionsWithReasoningEffort, route.Protocol, modelOptionPolicyConfig{
			Mode:                  cfg.ModelOptionPolicyMode,
			AllowedPathsJSON:      cfg.ModelOptionAllowedPaths,
			DeniedPathsJSON:       cfg.ModelOptionDeniedPaths,
			ModelCapabilitiesJSON: route.ModelCapabilitiesJSON,
		})
		filteredOptions = withMessageRouteReasoningPassbackOptions(
			filteredOptions,
			input.Options,
			route,
			reasoningContentPassback,
			llmMessages,
		)
		promptCacheKey, filteredOptions, llmMessages = configureOpenAIPromptCacheRequestForRoute(
			route,
			promptCacheSessionKey,
			filteredOptions,
			llmMessages,
		)
		generateInput = llm.GenerateInput{
			RequestID:              strings.TrimSpace(input.RequestID),
			ConversationID:         input.ConversationID,
			ConversationPublicID:   strings.TrimSpace(conversation.PublicID),
			ConversationSessionKey: strings.TrimSpace(conversation.SessionKey),
			PromptCacheKey:         promptCacheKey,
			Messages:               cloneLLMMessages(llmMessages),
			Tools:                  toolRuntime.definitions,
			Options:                filteredOptions,
		}
		applyOpenAIResponsesInstructions(route, routeConfig.Endpoint, &generateInput)
		estimatedPromptTokens = estimateGenerateInputTokens(generateInput)
		if s.logger != nil {
			s.logger.Warn("upstream_route_failover",
				zap.String("trace_id", traceid.FromContext(ctx)),
				zap.Uint("conversation_id", input.ConversationID),
				zap.Uint("failed_route_id", failedRoute.RouteID),
				zap.Uint("next_route_id", route.RouteID),
				zap.Int("attempt", len(attemptedRouteIDs)),
				zap.Error(failedErr),
			)
		}
		attemptHadSideEffect = false
		streamedText.Reset()
		upstreamOutput, err = runGenerate(generateInput)
		if handleCanceledGeneration(err) {
			return nil, ErrMessageGenerationCanceled
		}
	}
	if err != nil {
		if !routeFailureRecorded {
			s.routeResolver.MarkRouteFailure(ctx, route, err)
		}
		return nil, wrapUpstreamRequestError(err)
	}
	s.routeResolver.MarkRouteSuccess(ctx, route)

	// 8. 工具循环：执行、事件转发、内存幂等（重复调用 success→reused/error），但绝不落消息树。
	assistantText, nativeToolRows := syncUpstreamOutputTrace(nil, upstreamOutput, input.ClientRunID)
	toolCallRows = append(toolCallRows, nativeToolRows...)
	totalUsage := upstreamOutput.Usage
	if totalUsage == (llm.Usage{}) {
		totalUsage = usageAccumulator.usage()
	} else {
		usageAccumulator.setObservedUsage(totalUsage)
	}
	totalServerSideToolUsage := addServerSideToolUsage(nil, upstreamOutput.ServerSideToolUsage)
	remainingToolCalls := s.resolveMaxToolCallsPerRun()
	llmCallCount := llmRequestCount

	for len(upstreamOutput.ToolCalls) > 0 && llmCallCount < maxLLMCalls && remainingToolCalls > 0 {
		pendingToolCalls := upstreamOutput.ToolCalls
		if len(pendingToolCalls) > remainingToolCalls {
			pendingToolCalls = pendingToolCalls[:remainingToolCalls]
		}
		reasoningContent := ""
		if reasoningContentPassback {
			reasoningContent = outputReasoningContent(upstreamOutput)
		}
		assistantToolMessage := llm.Message{
			Role:             "assistant",
			Content:          assistantText,
			ReasoningContent: reasoningContent,
			ToolCalls:        pendingToolCalls,
		}
		toolResultTokenBudget := resolveToolResultTokenBudget(
			generateInput,
			llmMessages,
			assistantToolMessage,
			route.UpstreamModel,
			route.ModelCapabilitiesJSON,
		)
		toolCtx, toolSpan := platformtracing.Start(ctx, "conversation.agent_turn.tool.execute",
			trace.WithAttributes(
				attribute.Int64("conversation.id", int64(input.ConversationID)),
				attribute.Int64("user.id", int64(input.UserID)),
				attribute.String("agent.actor_id", input.ActorID),
				attribute.Int("conversation.tool.request_count", len(upstreamOutput.ToolCalls)),
				attribute.Int("conversation.tool.remaining_count", remainingToolCalls),
				attribute.Int64("conversation.tool.result_token_budget", toolResultTokenBudget),
			),
		)
		toolResult := s.executeAgentTurnToolCalls(toolCtx, input, executeAgentTurnToolCallsInput{
			UserID:            input.UserID,
			ConversationID:    input.ConversationID,
			RequestID:         input.RequestID,
			RunID:             input.ClientRunID,
			ToolCalls:         pendingToolCalls,
			ToolCallLimit:     remainingToolCalls,
			ToolNameMap:       toolRuntime.nameMap,
			MCPConfigs:        toolRuntime.mcpConfigs,
			ToolSchemas:       toolRuntime.schemas,
			Ledger:            toolLedger,
			ResultTokenBudget: toolResultTokenBudget,
		})
		toolSpan.SetAttributes(
			attribute.Int("conversation.tool.executed_count", len(toolResult.Rows)),
			attribute.Int("conversation.tool.result_count", len(toolResult.ToolResults)),
		)
		if toolExecutionHasError(toolResult.Rows) {
			toolSpan.SetStatus(codes.Error, "tool execution failed")
		}
		toolSpan.End()
		toolCallRows = append(toolCallRows, toolResult.Rows...)
		remainingToolCalls -= len(toolResult.Rows)
		if toolResult.FatalErr != nil {
			return nil, wrapUpstreamRequestError(toolResult.FatalErr)
		}
		if len(toolResult.ToolResults) == 0 {
			break
		}
		assistantToolMessage.ToolCalls = toolResult.ExecutedToolCalls
		llmMessages = append(llmMessages,
			assistantToolMessage,
			llm.Message{
				Role:        "tool",
				ToolResults: toolResult.ToolResults,
			},
		)
		var toolHistoryTrimmed bool
		llmMessages, toolHistoryTrimmed = trimToolFollowUpHistory(
			generateInput,
			llmMessages,
			route.UpstreamModel,
			route.ModelCapabilitiesJSON,
		)
		if toolHistoryTrimmed {
			toolSpan.SetAttributes(attribute.Bool("conversation.tool.history_trimmed", true))
		}
		var toolResultsRebalanced bool
		llmMessages, toolResultsRebalanced = rebalanceToolFollowUpResults(
			generateInput,
			llmMessages,
			route.UpstreamModel,
			route.ModelCapabilitiesJSON,
		)
		if toolResultsRebalanced {
			toolSpan.SetAttributes(attribute.Bool("conversation.tool.results_rebalanced", true))
		}

		followUpInput := generateInput
		if llmCallCount+1 >= maxLLMCalls {
			followUpInput.Messages = buildFinalToolSynthesisMessages(llmMessages, "The maximum number of LLM calls for this run has been reached. Stop calling tools and produce the final answer based on the tool results already available. If the information is insufficient, state the missing information directly.")
			followUpInput.Tools = nil
			followUpInput.DisableTools = true
			followUpInput.PreviousResponseID = ""
			applyOpenAIResponsesInstructions(route, routeConfig.Endpoint, &followUpInput)
		} else if !toolHistoryTrimmed && !toolResultsRebalanced && routeConfig.Endpoint == llm.EndpointResponses && supportsPreviousResponseIDRoute(route) && strings.TrimSpace(upstreamOutput.ResponseID) != "" {
			followUpInput.PreviousResponseID = strings.TrimSpace(upstreamOutput.ResponseID)
			followUpInput.Messages = []llm.Message{{Role: "tool", ToolResults: toolResult.ToolResults}}
		} else {
			followUpInput.Messages = llmMessages
			followUpInput.PreviousResponseID = ""
			applyOpenAIResponsesInstructions(route, routeConfig.Endpoint, &followUpInput)
		}

		nextOutput, nextErr := runGenerate(followUpInput)
		if handleCanceledGeneration(nextErr) {
			return nil, ErrMessageGenerationCanceled
		}
		if nextErr != nil {
			s.routeResolver.MarkRouteFailure(ctx, route, nextErr)
			return nil, wrapUpstreamRequestError(nextErr)
		}
		s.routeResolver.MarkRouteSuccess(ctx, route)
		totalUsage = addLLMUsage(totalUsage, nextOutput.Usage)
		if nextOutput.Usage != (llm.Usage{}) {
			usageAccumulator.setObservedUsage(totalUsage)
		} else if usageAccumulator.usage() != (llm.Usage{}) {
			totalUsage = usageAccumulator.usage()
		}
		totalServerSideToolUsage = addServerSideToolUsage(totalServerSideToolUsage, nextOutput.ServerSideToolUsage)
		upstreamOutput = nextOutput
		llmCallCount = llmRequestCount
		var nextNativeToolRows []model.ToolCall
		assistantText, nextNativeToolRows = syncUpstreamOutputTrace(nil, upstreamOutput, input.ClientRunID)
		toolCallRows = append(toolCallRows, nextNativeToolRows...)
	}
	if len(upstreamOutput.ToolCalls) > 0 && remainingToolCalls <= 0 && llmCallCount < maxLLMCalls {
		finalInput := generateInput
		finalInput.Messages = buildFinalToolSynthesisMessages(llmMessages, "The maximum number of tool calls for this run has been reached. Stop calling tools and produce the final answer based on the tool results already available. If the information is insufficient, state the missing information directly.")
		finalInput.Tools = nil
		finalInput.DisableTools = true
		finalInput.PreviousResponseID = ""
		applyOpenAIResponsesInstructions(route, routeConfig.Endpoint, &finalInput)
		nextOutput, nextErr := runGenerate(finalInput)
		if handleCanceledGeneration(nextErr) {
			return nil, ErrMessageGenerationCanceled
		}
		if nextErr != nil {
			s.routeResolver.MarkRouteFailure(ctx, route, nextErr)
			return nil, wrapUpstreamRequestError(nextErr)
		}
		s.routeResolver.MarkRouteSuccess(ctx, route)
		totalUsage = addLLMUsage(totalUsage, nextOutput.Usage)
		if nextOutput.Usage != (llm.Usage{}) {
			usageAccumulator.setObservedUsage(totalUsage)
		} else if usageAccumulator.usage() != (llm.Usage{}) {
			totalUsage = usageAccumulator.usage()
		}
		totalServerSideToolUsage = addServerSideToolUsage(totalServerSideToolUsage, nextOutput.ServerSideToolUsage)
		upstreamOutput = nextOutput
		llmCallCount++
		var nextNativeToolRows []model.ToolCall
		assistantText, nextNativeToolRows = syncUpstreamOutputTrace(nil, upstreamOutput, input.ClientRunID)
		toolCallRows = append(toolCallRows, nextNativeToolRows...)
	}

	// 9. 收尾：用量口径、空响应检查、最终 usage 事件。
	effectiveInputTokens := usageAccumulator.effectiveInputTokens(estimatedPromptTokens)
	effectiveOutputTokens := resolveObservedOrEstimatedOutputTokens(totalUsage.OutputTokens, assistantText)

	if toolRunFinalAnswerMissing(upstreamOutput, len(toolCallRows) > 0, llmCallCount, maxLLMCalls, remainingToolCalls) {
		return nil, ErrToolRunFinalAnswerMissing
	}
	if strings.TrimSpace(assistantText) == "" && len(upstreamOutput.GeneratedImages) == 0 {
		return nil, ErrUpstreamEmptyResponse
	}
	finalUsageEvent := totalUsage
	finalUsageEvent.InputTokens = effectiveInputTokens
	finalUsageEvent.OutputTokens = effectiveOutputTokens
	if err := emitAgentTurnUsageEvent(input, finalUsageEvent); err != nil {
		return nil, err
	}

	// 10. 计费落账：复用普通消息的 ledger 构建，ClientRunID 作为幂等键。
	result := &SendMessageResult{
		PlatformModelName:   route.PlatformModelName,
		RoutedBindingCode:   route.BindingCode,
		UpstreamName:        route.UpstreamName,
		UpstreamProtocol:    route.Protocol,
		UpstreamModelName:   route.UpstreamModel,
		EffectiveOptions:    filteredOptions,
		UsageSpeed:          totalUsage.Speed,
		UsageServiceTier:    totalUsage.ServiceTier,
		RawUsageJSON:        totalUsage.RawUsageJSON,
		CacheWrite5mTokens:  totalUsage.CacheWrite5mTokens,
		CacheWrite1hTokens:  totalUsage.CacheWrite1hTokens,
		ServerSideToolUsage: totalServerSideToolUsage,
		LatencyMS:           time.Since(startedAt).Milliseconds(),
		StartedAt:           startedAt,
	}
	result.UserMessage.InputTokens = effectiveInputTokens
	result.UserMessage.CacheReadTokens = totalUsage.CacheReadTokens
	result.UserMessage.CacheWriteTokens = totalUsage.CacheWriteTokens
	result.AssistantMessage.OutputTokens = effectiveOutputTokens
	result.AssistantMessage.ReasoningTokens = totalUsage.ReasoningTokens
	usageLedger, billingErr := s.RecordSendMessageBilling(ctx, SendMessageBillingInput{
		UserID:            input.UserID,
		ConversationID:    input.ConversationID,
		Conversation:      conversation,
		PlatformModelName: route.PlatformModelName,
		ConversationModel: conversation.Model,
		ClientRunID:       strings.TrimSpace(input.ClientRunID),
		Result:            result,
	}, authorization)
	if billingErr != nil {
		return nil, billingErr
	}
	authorizationSettled = true

	latency := time.Since(startedAt).Milliseconds()
	if latency < 0 {
		latency = 0
	}
	return &AgentTurnOutput{
		Text:              strings.TrimSpace(assistantText),
		ReasoningText:     outputReasoningContent(upstreamOutput),
		ToolCallRows:      toolCallRows,
		Usage:             usageLedger,
		Route:             route,
		PlatformModelName: route.PlatformModelName,
		UpstreamModelName: route.UpstreamModel,
		UpstreamProtocol:  route.Protocol,
		RoutedBindingCode: route.BindingCode,
		EffectiveOptions:  filteredOptions,
		LatencyMS:         latency,
		StartedAt:         startedAt,
	}, nil
}

// resolveAgentTurnRoute 解析 Actor 回合的路由，并复用普通消息的错误映射。
func (s *Service) resolveAgentTurnRoute(ctx context.Context, input channel.ResolveRouteInput) (*channel.ResolvedRoute, error) {
	route, err := s.routeResolver.ResolveRoute(ctx, input)
	if err != nil {
		if errors.Is(err, channel.ErrModelAccessDenied) {
			return nil, ErrModelAccessDenied
		}
		if errors.Is(err, channel.ErrRouteNotFound) || errors.Is(err, channel.ErrModelNotFound) {
			return nil, ErrModelRouteNotConfigured
		}
		if errors.Is(err, channel.ErrAllRoutesUnavailable) {
			return nil, wrapUpstreamRequestError(err)
		}
		return nil, err
	}
	return route, nil
}

// authorizeAgentTurnUsage 为长时间运行的 Actor 回合申请计费授权并启动续约，
// 返回的停止函数会终止续约 goroutine。
func (s *Service) authorizeAgentTurnUsage(
	ctx context.Context,
	conversation *model.Conversation,
	input AgentTurnInput,
	route *channel.ResolvedRoute,
) (*domainbilling.UsageAuthorization, func(), error) {
	authorization, err := s.AuthorizeSendMessageUsage(ctx, SendMessageBillingInput{
		UserID:            input.UserID,
		ConversationID:    input.ConversationID,
		Conversation:      conversation,
		PlatformModelName: route.PlatformModelName,
		ConversationModel: conversation.Model,
		ClientRunID:       strings.TrimSpace(input.ClientRunID),
	})
	if err != nil {
		return nil, nil, err
	}
	if authorization == nil || authorization.Reservation == nil {
		return authorization, func() {}, nil
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(agentTurnUsageRenewalInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				renewCtx, cancel := context.WithTimeout(context.Background(), agentTurnUsageRenewalTimeout)
				_ = s.RenewSendMessageUsageAuthorization(renewCtx, authorization)
				cancel()
			}
		}
	}()
	return authorization, func() {
		close(stop)
		<-done
	}, nil
}

// emitAgentTurnEvent 合并 actor 元数据后转发事件；OnEvent 为空时静默跳过。
func emitAgentTurnEvent(input AgentTurnInput, eventType string, payload map[string]interface{}) error {
	if input.OnEvent == nil {
		return nil
	}
	merged := agentTurnEventActorMeta(input)
	for key, value := range payload {
		merged[key] = value
	}
	return input.OnEvent(AgentTurnEvent{Type: eventType, Payload: merged})
}

func agentTurnEventActorMeta(input AgentTurnInput) map[string]interface{} {
	return map[string]interface{}{
		"actor_id":    input.ActorID,
		"actor_name":  input.ActorName,
		"actor_type":  input.ActorType,
		"actor_icon":  input.ActorIcon,
		"actor_color": input.ActorColor,
		"model":       strings.TrimSpace(input.PlatformModelName),
	}
}

// emitAgentTurnUsageEvent 发送用量事件，字段口径与普通消息的 emitLLMUsageEvent 一致。
func emitAgentTurnUsageEvent(input AgentTurnInput, usage llm.Usage) error {
	if usage == (llm.Usage{}) {
		return nil
	}
	return emitAgentTurnEvent(input, AgentTurnEventUsage, map[string]interface{}{
		"input_tokens":       usage.InputTokens,
		"output_tokens":      usage.OutputTokens,
		"cache_read_tokens":  usage.CacheReadTokens,
		"cache_write_tokens": usage.CacheWriteTokens,
		"reasoning_tokens":   usage.ReasoningTokens,
	})
}

type executeAgentTurnToolCallsInput struct {
	UserID            uint
	ConversationID    uint
	RequestID         string
	RunID             string
	ToolCalls         []llm.ToolCall
	ToolCallLimit     int
	ToolNameMap       map[string]string
	MCPConfigs        map[string]mcp.CallConfig
	ToolSchemas       map[string]json.RawMessage
	Ledger            *toolExecutionLedger
	ResultTokenBudget int64
}

// executeAgentTurnToolCalls 是 executeAssistantToolCalls 的 Actor 变体：
// 执行/校验/内存幂等逻辑完全一致，但不挂 TraceRecorder；
// 仅当 turn.PersistToolCalls（群组尝试）时把工具行写入 conversation_tool_calls（MessageID=0），
// 并以 tool_call/tool_result 事件向外转发进度。
func (s *Service) executeAgentTurnToolCalls(ctx context.Context, turn AgentTurnInput, input executeAgentTurnToolCallsInput) executeAssistantToolCallsResult {
	toolCalls := input.ToolCalls
	if input.ToolCallLimit > 0 && len(toolCalls) > input.ToolCallLimit {
		toolCalls = toolCalls[:input.ToolCallLimit]
	}
	if len(toolCalls) == 0 {
		return executeAssistantToolCallsResult{}
	}
	executedToolCalls := append([]llm.ToolCall(nil), toolCalls...)
	slots := make([]toolExecutionSlot, len(toolCalls))
	var fatalErr error
	for i, item := range toolCalls {
		modelToolName := strings.TrimSpace(item.ToolName)
		executionToolName := resolveExecutionToolName(modelToolName, input.ToolNameMap)
		row := model.ToolCall{
			ConversationID: input.ConversationID,
			UserID:         input.UserID,
			RunID:          input.RunID,
			ToolCallID:     strings.TrimSpace(item.ToolCallID),
			ToolType:       normalizeToolType(item.ToolType),
			ToolName:       executionToolName,
			Status:         "requested",
			LatencyMS:      0,
			InputJSON:      strings.TrimSpace(item.ArgumentsJSON),
			OutputJSON:     "",
			ErrorJSON:      "",
		}
		_ = emitAgentTurnEvent(turn, AgentTurnEventToolCall, map[string]interface{}{
			"tool_name":    modelToolName,
			"tool_call_id": row.ToolCallID,
			"arguments":    row.InputJSON,
		})
		mcpConfig := resolveMCPConfig(modelToolName, input.MCPConfigs)
		if mcpConfig == nil {
			row.Status = "error"
			row.ErrorJSON = toolNotEnabledForRunMessage(modelToolName)
			slots[i] = toolExecutionSlot{row: row, result: buildToolResultForModel(row, modelToolName)}
			if fatalErr == nil {
				fatalErr = fmt.Errorf("model requested tool %q, but it is not enabled for this run", modelToolName)
			}
			if input.Ledger != nil {
				input.Ledger.store(row.ToolName, row.InputJSON, toolExecutionRecord{row: row, result: slots[i].result})
			}
			_ = emitAgentTurnEvent(turn, AgentTurnEventToolResult, map[string]interface{}{
				"tool_name":    modelToolName,
				"tool_call_id": row.ToolCallID,
				"status":       row.Status,
				"error":        row.ErrorJSON,
			})
			continue
		}
		normalizedInput, validationErr := normalizeToolArguments(row.InputJSON, input.ToolSchemas[modelToolName])
		if validationErr != nil {
			row.Status = "error"
			row.ErrorJSON = validationErr.Error()
			slots[i] = toolExecutionSlot{row: row, result: buildToolResultForModel(row, modelToolName)}
			if input.Ledger != nil {
				input.Ledger.store(row.ToolName, row.InputJSON, toolExecutionRecord{row: row, result: slots[i].result})
			}
			_ = emitAgentTurnEvent(turn, AgentTurnEventToolResult, map[string]interface{}{
				"tool_name":    modelToolName,
				"tool_call_id": row.ToolCallID,
				"status":       row.Status,
				"error":        row.ErrorJSON,
			})
			continue
		}
		row.InputJSON = normalizedInput
		if input.Ledger != nil {
			if previous, ok := input.Ledger.lookup(row.ToolName, row.InputJSON); ok {
				slot := buildRepeatedToolSlot(row, modelToolName, previous)
				if turn.PersistToolCalls {
					slot.persisted = s.persistToolCallResult(ctx, &slot.row)
				}
				slots[i] = slot
				_ = emitAgentTurnEvent(turn, AgentTurnEventToolResult, map[string]interface{}{
					"tool_name":    modelToolName,
					"tool_call_id": row.ToolCallID,
					"status":       slot.row.Status,
					"error":        slot.row.ErrorJSON,
				})
				continue
			}
		}
		toolStartedAt := time.Now()
		outputJSON, executeErr := s.executeToolCall(ctx, ExecuteToolInput{
			UserID:         input.UserID,
			ConversationID: input.ConversationID,
			RequestID:      strings.TrimSpace(input.RequestID),
			ToolName:       row.ToolName,
			ArgumentsJSON:  row.InputJSON,
			MCPConfig:      mcpConfig,
		})
		row.LatencyMS = time.Since(toolStartedAt).Milliseconds()
		if row.LatencyMS < 0 {
			row.LatencyMS = 0
		}
		if executeErr != nil {
			row.Status = "error"
			row.ErrorJSON = strings.TrimSpace(executeErr.Error())
		} else {
			row.Status = "success"
			row.OutputJSON = strings.TrimSpace(outputJSON)
			if row.OutputJSON == "" {
				row.OutputJSON = "{}"
			}
		}
		result := buildToolResultForModel(row, modelToolName)
		slot := toolExecutionSlot{row: row, result: result}
		if turn.PersistToolCalls {
			slot.persisted = s.persistToolCallResult(ctx, &row)
			slot.row = row
		}
		slots[i] = slot
		if input.Ledger != nil {
			input.Ledger.store(row.ToolName, row.InputJSON, toolExecutionRecord{row: row, result: result})
		}
		_ = emitAgentTurnEvent(turn, AgentTurnEventToolResult, map[string]interface{}{
			"tool_name":    modelToolName,
			"tool_call_id": row.ToolCallID,
			"status":       row.Status,
			"error":        row.ErrorJSON,
		})
	}
	rows := make([]model.ToolCall, 0, len(slots))
	toolResults := make([]llm.ToolResult, 0, len(slots))
	enforceToolResultAggregateBudget(slots, input.ResultTokenBudget)
	for _, slot := range slots {
		rows = append(rows, slot.row)
		toolResults = append(toolResults, slot.result)
	}
	return executeAssistantToolCallsResult{
		Rows:              rows,
		ToolResults:       toolResults,
		ExecutedToolCalls: executedToolCalls,
		FatalErr:          fatalErr,
	}
}
