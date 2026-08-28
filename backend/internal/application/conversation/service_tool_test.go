package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/mcp"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/security"
)

func TestCallMCPWithRetryReusesLogicalCallID(t *testing.T) {
	var mu sync.Mutex
	callIDs := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     interface{}            `json:"id"`
			Method string                 `json:"method"`
			Params map[string]interface{} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch request.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "retry-session")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result":  map[string]interface{}{},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			meta, _ := request.Params["_meta"].(map[string]interface{})
			callID, _ := meta["call_id"].(string)
			mu.Lock()
			callIDs = append(callIDs, callID)
			attempt := len(callIDs)
			mu.Unlock()
			if attempt == 1 {
				http.Error(w, "response lost after execution", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": map[string]interface{}{
					"content": []map[string]string{{"type": "text", "text": "ok"}},
				},
			})
		default:
			t.Errorf("unexpected method %q", request.Method)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := mcp.NewClient(security.NewStrictOutboundPolicy(true), "retry-hmac-key")
	service := &Service{mcpClient: client}
	output, err := service.callMCPWithRetry(t.Context(), mcp.CallConfig{BaseURL: server.URL}, mcp.CallInput{
		ToolName:       "sandbox_exec",
		ArgumentsJSON:  `{"command":"true"}`,
		UserID:         7,
		ConversationID: 11,
		RequestID:      "request-retry",
	}, 1)
	if err != nil {
		t.Fatalf("retry MCP call: %v", err)
	}
	if !strings.Contains(output, `"text":"ok"`) {
		t.Fatalf("unexpected retry output: %s", output)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(callIDs) != 2 || strings.TrimSpace(callIDs[0]) == "" || callIDs[0] != callIDs[1] {
		t.Fatalf("logical call ID changed across retry: %#v", callIDs)
	}
}

func TestExecuteToolCallRejectsToolsNotEnabledForRun(t *testing.T) {
	svc := &Service{}
	_, err := svc.executeToolCall(context.Background(), ExecuteToolInput{
		ToolName:      "memory.upsert",
		ArgumentsJSON: `{"memory_key":"k","value":"v"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "not enabled for this run") {
		t.Fatalf("expected disabled tool error, got %v", err)
	}
}

func TestExecuteAssistantToolCallsStopsWhenToolNotEnabledForRun(t *testing.T) {
	svc := &Service{}
	result := svc.executeAssistantToolCalls(context.Background(), executeAssistantToolCallsInput{
		RunID: "run_1",
		ToolCalls: []llm.ToolCall{{
			ToolCallID:    "toolu_1",
			ToolType:      "function",
			ToolName:      "web_search",
			ArgumentsJSON: `{"query":"weather"}`,
			Status:        "requested",
		}},
	})

	if result.FatalErr == nil || !strings.Contains(result.FatalErr.Error(), "not enabled for this run") {
		t.Fatalf("expected fatal disabled tool error, got %v", result.FatalErr)
	}
	if len(result.Rows) != 1 || result.Rows[0].Status != "error" || result.Rows[0].ToolName != "web_search" {
		t.Fatalf("expected one failed tool row, got %#v", result.Rows)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Status != "error" {
		t.Fatalf("expected failed model tool result, got %#v", result.ToolResults)
	}
}

func TestValidateSelectedToolIDsUsesRuntimeLimit(t *testing.T) {
	service := &Service{cfg: config.NewRuntime(config.Config{MCPMaxSelectedToolsPerMessage: 2})}

	if err := service.ValidateSelectedToolIDs([]uint{1, 2}); err != nil {
		t.Fatalf("expected two selected tools to pass, got %v", err)
	}
	if err := service.ValidateSelectedToolIDs([]uint{1, 2, 3}); err != ErrTooManySelectedTools {
		t.Fatalf("expected ErrTooManySelectedTools, got %v", err)
	}
}

func TestExecuteAgentTurnToolCallsStopsBeforeCredentialHandlerWhenDetectionFails(t *testing.T) {
	const secret = "pre-detection-secret"
	callCount := 0
	detectionErr := context.Canceled
	entry := platformToolEntry{
		definition: llm.ToolDefinition{
			Name:        "credential_create",
			InputSchema: []byte(`{"type":"object","properties":{"name":{"type":"string"},"value":{"type":"string"}},"required":["name","value"]}`),
		},
		kind: platformToolWrite,
		handler: func(_ *Service, _ context.Context, _ platformToolCallContext) (string, error) {
			callCount++
			return `{"status":"created"}`, nil
		},
	}
	result := (&Service{}).executeAgentTurnToolCalls(t.Context(), AgentTurnInput{
		OnCredentialAttemptsDetected: func(_ context.Context, attempts []credentialWrite) error {
			if len(attempts) != 1 || attempts[0].Name != "deploy-key" || attempts[0].Value != secret {
				t.Fatalf("unexpected detected attempts: %#v", attempts)
			}
			return detectionErr
		},
	}, executeAgentTurnToolCallsInput{
		RunID: "group-run:step:attempt",
		ToolCalls: []llm.ToolCall{{
			ToolCallID:    "credential-call",
			ToolType:      "function",
			ToolName:      "credential_create_model",
			ArgumentsJSON: `{"name":"deploy-key","value":"` + secret + `"}`,
		}},
		ToolNameMap: map[string]string{"credential_create_model": "credential_create"},
		PlatformTools: map[string]platformToolEntry{
			"credential_create_model": entry,
		},
	})

	if !errors.Is(result.FatalErr, detectionErr) {
		t.Fatalf("fatal error = %v, want detection error", result.FatalErr)
	}
	if callCount != 0 {
		t.Fatalf("credential handler ran before detection checkpoint: calls=%d", callCount)
	}
	if len(result.Rows) != 0 || len(result.ToolResults) != 0 {
		t.Fatalf("pre-detection failure produced tool side effects: %#v", result)
	}
}

func TestExecuteAssistantToolCallsMasksAndReusesCredentialWrites(t *testing.T) {
	const secret = "credential-secret-value"
	callCount := 0
	entry := platformToolEntry{
		definition: llm.ToolDefinition{
			Name:        "credential_create",
			InputSchema: []byte(`{"type":"object","properties":{"name":{"type":"string"},"value":{"type":"string"}},"required":["name","value"]}`),
		},
		kind: platformToolWrite,
		handler: func(_ *Service, _ context.Context, _ platformToolCallContext) (string, error) {
			callCount++
			return `{"status":"created"}`, nil
		},
	}
	ledger := newToolExecutionLedger()
	execute := func(toolCallID string) executeAssistantToolCallsResult {
		return (&Service{}).executeAssistantToolCalls(t.Context(), executeAssistantToolCallsInput{
			RunID: toolCallID,
			ToolCalls: []llm.ToolCall{{
				ToolCallID:    toolCallID,
				ToolType:      "function",
				ToolName:      "credential_create_model",
				ArgumentsJSON: `{"name":"deploy-key","value":"` + secret + `"}`,
			}},
			ToolNameMap: map[string]string{"credential_create_model": "credential_create"},
			PlatformTools: map[string]platformToolEntry{
				"credential_create_model": entry,
			},
			Ledger:          ledger,
			SkipPersistence: true,
		})
	}

	first := execute("call-1")
	second := execute("call-2")
	if callCount != 1 {
		t.Fatalf("expected repeated credential call to reuse the in-memory ledger, handler calls=%d", callCount)
	}
	for label, result := range map[string]executeAssistantToolCallsResult{"first": first, "second": second} {
		if result.FatalErr != nil || len(result.Rows) != 1 || len(result.ToolResults) != 1 || len(result.ExecutedToolCalls) != 1 {
			t.Fatalf("%s execution returned unexpected result: %#v", label, result)
		}
		if len(result.CredentialWrites) != 1 || result.CredentialWrites[0].Name != "deploy-key" || result.CredentialWrites[0].Value != secret {
			t.Fatalf("%s execution lost credential replacement side channel: %#v", label, result.CredentialWrites)
		}
		serialized := result.Rows[0].InputJSON + result.Rows[0].OutputJSON + result.Rows[0].ErrorJSON +
			result.ToolResults[0].OutputJSON + result.ToolResults[0].Error + result.ExecutedToolCalls[0].ArgumentsJSON

		if strings.Contains(serialized, secret) {
			t.Fatalf("%s execution leaked credential plaintext: %s", label, serialized)
		}
		if !strings.Contains(result.Rows[0].InputJSON, "[REDACTED]") {
			t.Fatalf("%s execution did not redact persisted row input: %s", label, result.Rows[0].InputJSON)
		}
		if !strings.Contains(result.ExecutedToolCalls[0].ArgumentsJSON, "{{credential: deploy-key}}") {
			t.Fatalf("%s execution did not scrub model tool-call arguments: %s", label, result.ExecutedToolCalls[0].ArgumentsJSON)
		}
	}
	if second.Rows[0].Status != "reused" {
		t.Fatalf("expected second credential call to be marked reused, got %q", second.Rows[0].Status)
	}
}

func TestExecuteAssistantToolCallsDoesNotTreatPendingCredentialApprovalAsWrite(t *testing.T) {
	const secret = "pending-approval-secret"
	resolver := &recordingCredentialResolver{}
	service := newAskCredentialService(t, resolver)
	runtime := selectedToolRuntime{}
	runtime.bindCredentialSecretRefs(7, 11, "run-pending-approval")
	entry := platformToolRegistry()["credential_create"]

	ledger := newToolExecutionLedger()
	execute := func(toolCallID string) executeAssistantToolCallsResult {
		return service.executeAssistantToolCalls(t.Context(), executeAssistantToolCallsInput{
			UserID:         7,
			ConversationID: 11,
			RunID:          "run-pending-approval",
			RequestID:      "req-pending-approval",
			ToolCalls: []llm.ToolCall{{
				ToolCallID:    toolCallID,
				ToolType:      "function",
				ToolName:      "credential_create_model",
				ArgumentsJSON: `{"name":"deploy-key","value":"` + secret + `"}`,
			}},
			ToolRuntime: &runtime,
			ToolNameMap: map[string]string{"credential_create_model": "credential_create"},
			PlatformTools: map[string]platformToolEntry{
				"credential_create_model": entry,
			},
			Ledger:          ledger,
			SkipPersistence: true,
		})
	}
	result := execute("call-pending-approval-1")
	reused := execute("call-pending-approval-2")

	if resolver.totalWrites() != 0 {
		t.Fatalf("pending approval executed credential handler: %d", resolver.totalWrites())
	}
	if len(result.CredentialWrites) != 0 {
		t.Fatalf("pending approval was reported as a successful credential write: %#v", result.CredentialWrites)
	}
	if len(reused.CredentialWrites) != 0 || len(reused.Rows) != 1 || reused.Rows[0].Status != "reused" {
		t.Fatalf("reused pending approval was treated as a successful write: %#v", reused)
	}
	if len(result.Rows) != 1 || result.Rows[0].Status != "success" || !strings.Contains(result.Rows[0].OutputJSON, "pending_approval") {
		t.Fatalf("unexpected pending approval tool result: %#v", result.Rows)
	}
	serialized := result.Rows[0].InputJSON + result.Rows[0].OutputJSON + result.Rows[0].ErrorJSON +
		result.ToolResults[0].OutputJSON + result.ToolResults[0].Error + result.ExecutedToolCalls[0].ArgumentsJSON
	if strings.Contains(serialized, secret) {
		t.Fatalf("pending approval result leaked plaintext: %s", serialized)
	}
	if !strings.Contains(result.ExecutedToolCalls[0].ArgumentsJSON, "{{secret_ref:") {
		t.Fatalf("model-visible pending call lost its opaque secret ref: %s", result.ExecutedToolCalls[0].ArgumentsJSON)
	}
}

func TestExecuteAssistantToolCallsRetriesSecretRefAfterFailureAndDestroysItAfterSuccess(t *testing.T) {
	const secret = "credential-secret-ref-value"
	runtime := selectedToolRuntime{}
	runtime.bindCredentialSecretRefs(7, 11, "run-secret-ref")
	ref := runtime.credentialSecrets.protect(secret)
	if ref == "" || strings.Contains(ref, secret) {
		t.Fatalf("unexpected secret reference: %q", ref)
	}

	callCount := 0
	var handlerValues []string
	entry := platformToolEntry{
		definition: llm.ToolDefinition{
			Name:        "credential_create",
			InputSchema: []byte(`{"type":"object","properties":{"name":{"type":"string"},"value":{"type":"string"}},"required":["name","value"]}`),
		},
		kind: platformToolWrite,
		handler: func(_ *Service, _ context.Context, call platformToolCallContext) (string, error) {
			callCount++
			var arguments struct {
				Value string `json:"value"`
			}
			if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
				return "", err
			}
			handlerValues = append(handlerValues, arguments.Value)
			if callCount == 1 {
				return "", errors.New("temporary credential store failure")
			}
			return `{"status":"created"}`, nil
		},
	}
	service := &Service{cfg: config.NewRuntime(config.Config{})}
	ledger := newToolExecutionLedger()
	execute := func(toolCallID string) executeAssistantToolCallsResult {
		return service.executeAssistantToolCalls(t.Context(), executeAssistantToolCallsInput{
			UserID:         7,
			ConversationID: 11,
			RunID:          "run-secret-ref",
			ToolCalls: []llm.ToolCall{{
				ToolCallID:    toolCallID,
				ToolType:      "function",
				ToolName:      "credential_create_model",
				ArgumentsJSON: `{"name":"deploy-key","value":"` + ref + `"}`,
			}},
			ToolRuntime: &runtime,
			ToolNameMap: map[string]string{"credential_create_model": "credential_create"},
			PlatformTools: map[string]platformToolEntry{
				"credential_create_model": entry,
			},
			Ledger:          ledger,
			SkipPersistence: true,
		})
	}

	first := execute("call-1")
	if len(first.Rows) != 1 || first.Rows[0].Status != "error" {
		t.Fatalf("expected first credential write to fail, got %#v", first)
	}
	if _, ok := runtime.credentialSecrets.resolve(ref); !ok {
		t.Fatal("failed credential write destroyed its secret_ref")
	}
	if !strings.Contains(first.ExecutedToolCalls[0].ArgumentsJSON, ref) {
		t.Fatalf("failed credential write did not keep the model-visible secret_ref: %s", first.ExecutedToolCalls[0].ArgumentsJSON)
	}

	second := execute("call-2")
	if callCount != 2 {
		t.Fatalf("expected failed credential write to execute again, handler calls=%d", callCount)
	}
	if len(handlerValues) != 2 || handlerValues[0] != secret || handlerValues[1] != secret {
		t.Fatalf("credential handler did not receive the original plaintext on both attempts: %#v", handlerValues)
	}
	if len(second.Rows) != 1 || second.Rows[0].Status != "success" || len(second.CredentialWrites) != 1 {
		t.Fatalf("expected retry to succeed, got %#v", second)
	}
	if _, ok := runtime.credentialSecrets.resolve(ref); ok {
		t.Fatal("successful credential write did not destroy its secret_ref")
	}
	if !strings.Contains(second.ExecutedToolCalls[0].ArgumentsJSON, "{{credential: deploy-key}}") {
		t.Fatalf("successful credential write did not replace the ref with a durable credential placeholder: %s", second.ExecutedToolCalls[0].ArgumentsJSON)
	}
	for label, result := range map[string]executeAssistantToolCallsResult{"first": first, "second": second} {
		serialized := result.Rows[0].InputJSON + result.Rows[0].OutputJSON + result.Rows[0].ErrorJSON +
			result.ToolResults[0].OutputJSON + result.ToolResults[0].Error
		if strings.Contains(serialized, secret) || strings.Contains(serialized, ref) {
			t.Fatalf("%s persisted/model result leaked secret material: %s", label, serialized)
		}
	}
}

func TestExecuteAssistantToolCallsRejectsSecretRefOutsideCredentialValue(t *testing.T) {
	runtime := selectedToolRuntime{}
	runtime.bindCredentialSecretRefs(7, 11, "run-secret-ref-fields")
	ref := runtime.credentialSecrets.protect("credential-secret")
	handlerCalls := 0
	entry := platformToolEntry{
		definition: llm.ToolDefinition{Name: "credential_create"},
		kind:       platformToolWrite,
		handler: func(_ *Service, _ context.Context, _ platformToolCallContext) (string, error) {
			handlerCalls++
			return `{"status":"created"}`, nil
		},
	}
	result := (&Service{cfg: config.NewRuntime(config.Config{})}).executeAssistantToolCalls(
		t.Context(),
		executeAssistantToolCallsInput{
			UserID:         7,
			ConversationID: 11,
			RunID:          "run-secret-ref-fields",
			ToolCalls: []llm.ToolCall{{
				ToolCallID:    "call-invalid-field",
				ToolType:      "function",
				ToolName:      "credential_create_model",
				ArgumentsJSON: `{"name":"deploy-key","value":"safe","description":"` + ref + `"}`,
			}},
			ToolRuntime: &runtime,
			ToolNameMap: map[string]string{"credential_create_model": "credential_create"},
			PlatformTools: map[string]platformToolEntry{
				"credential_create_model": entry,
			},
			SkipPersistence: true,
		},
	)
	if handlerCalls != 0 {
		t.Fatalf("credential handler received a secret_ref from a non-value field: calls=%d", handlerCalls)
	}
	if len(result.Rows) != 1 || result.Rows[0].Status != "error" {
		t.Fatalf("expected invalid secret_ref field to fail, got %#v", result)
	}
}

func TestExecuteAssistantToolCallsRejectsUndisclosedSystemMultimodalTool(t *testing.T) {
	runtime := selectedToolRuntime{
		multimodalAnalyzer: &selectedMultimodalAnalyzer{
			attachments: map[string]AttachmentInput{
				"image-1": {FileID: "image-1", Current: true},
			},
		},
	}
	result := (&Service{}).executeAssistantToolCalls(t.Context(), executeAssistantToolCallsInput{
		RunID: "run-undisclosed-multimodal",
		ToolCalls: []llm.ToolCall{{
			ToolCallID:    "call-undisclosed",
			ToolType:      "function",
			ToolName:      systemMultimodalAnalyzeToolName,
			ArgumentsJSON: `{"file_ids":["image-1"],"prompt":"Read the visible title"}`,
		}},
		ToolRuntime:     &runtime,
		ToolNameMap:     map[string]string{},
		ToolSchemas:     map[string]json.RawMessage{},
		SkipPersistence: true,
	})
	if result.FatalErr == nil || !strings.Contains(result.FatalErr.Error(), "not enabled for this run") {
		t.Fatalf("expected undisclosed system tool to be rejected, got %#v", result)
	}
	if len(result.Rows) != 1 || result.Rows[0].Status != "error" {
		t.Fatalf("expected one failed undisclosed tool row, got %#v", result.Rows)
	}
}

func TestResolveMaxLLMCallsPerRunRequiresFollowUpRound(t *testing.T) {
	svc := &Service{cfg: config.NewRuntime(config.Config{MCPMaxLLMCallsPerRun: 1})}
	if got := svc.resolveMaxLLMCallsPerRun(); got != 2 {
		t.Fatalf("expected minimum LLM calls per run to be 2, got %d", got)
	}
}

func TestDiffLLMUsageTreatsStreamUsageAsCallCumulative(t *testing.T) {
	previous := llm.Usage{
		InputTokens:     10,
		OutputTokens:    3,
		CacheReadTokens: 2,
		ReasoningTokens: 1,
		Speed:           "standard",
		ServiceTier:     "default",
	}
	current := llm.Usage{
		InputTokens:     18,
		OutputTokens:    7,
		CacheReadTokens: 2,
		ReasoningTokens: 4,
		Speed:           "fast",
		ServiceTier:     "priority",
	}

	got := diffLLMUsage(current, previous)
	if got.InputTokens != 8 || got.OutputTokens != 4 || got.CacheReadTokens != 0 || got.ReasoningTokens != 3 {
		t.Fatalf("unexpected usage delta: %#v", got)
	}
	if got.Speed != "fast" || got.ServiceTier != "priority" {
		t.Fatalf("expected latest usage metadata to be kept, got %#v", got)
	}
}

func TestAddServerSideToolUsageAggregatesPositiveCounts(t *testing.T) {
	got := addServerSideToolUsage(
		map[string]int64{"web_search": 1, "ignored": 0},
		map[string]int64{"web_search": 2, "code_interpreter": 1, " ": 3},
	)

	if got["web_search"] != 3 || got["code_interpreter"] != 1 {
		t.Fatalf("unexpected server-side tool usage: %#v", got)
	}
	if _, ok := got["ignored"]; ok {
		t.Fatalf("expected non-positive usage to be ignored: %#v", got)
	}
}

func TestSyncUpstreamOutputThinkingDoesNotReturnThinkingOnlyContent(t *testing.T) {
	output := &llm.GenerateOutput{
		Text: "<think>Need to call a tool.</think>",
		ToolCalls: []llm.ToolCall{{
			ToolCallID:    "call_1",
			ToolType:      "function",
			ToolName:      "memory.list",
			ArgumentsJSON: "{}",
			Status:        "requested",
		}},
	}

	if got := syncUpstreamOutputThinking(nil, output); got != "" {
		t.Fatalf("expected thinking-only tool call content to stay out of assistant text, got %q", got)
	}
}

func TestOutputReasoningContentPrefersStructuredReasoning(t *testing.T) {
	output := &llm.GenerateOutput{
		Reasoning: &llm.ReasoningOutput{Text: "need a tool"},
		Text:      "<think>fallback</think>",
	}

	got := outputReasoningContent(output)
	if got != "need a tool" {
		t.Fatalf("expected structured reasoning content, got %q", got)
	}

	got = outputReasoningContent(&llm.GenerateOutput{Text: "<think>fallback</think>"})
	if got != "fallback" {
		t.Fatalf("expected parsed thinking fallback, got %q", got)
	}
}

func TestToolRunFinalAnswerMissingWhenBudgetEndsWithStructuredToolCall(t *testing.T) {
	output := &llm.GenerateOutput{
		ToolCalls: []llm.ToolCall{{
			ToolCallID:    "call_1",
			ToolType:      "function",
			ToolName:      "search",
			ArgumentsJSON: `{"query":"mcp"}`,
			Status:        "requested",
		}},
	}

	if !toolRunFinalAnswerMissing(output, true, 5, 5, 1) {
		t.Fatalf("expected exhausted tool run with pending tool call to be missing a final answer")
	}
}

func TestToolRunFinalAnswerMissingAcceptsNaturalFinalAnswer(t *testing.T) {
	text := "没有更多工具调用空间时，应基于已获取的结果直接回答。"

	if toolRunFinalAnswerMissing(&llm.GenerateOutput{Text: text}, true, 5, 5, 1) {
		t.Fatalf("expected natural final answer to be accepted")
	}
}

func TestToolRunFinalAnswerMissingSeesStrippedTextToolCalls(t *testing.T) {
	// 工具禁用轮中模型仍输出文本编码工具调用（DSML 已剥离）时，同样视为尚未收尾。
	output := &llm.GenerateOutput{
		Text:                  "已查询到部分结果",
		TextToolCallsStripped: true,
	}
	if !toolRunFinalAnswerMissing(output, true, 5, 5, 1) {
		t.Fatalf("expected stripped text tool calls at budget end to be missing a final answer")
	}
	if toolRunFinalAnswerMissing(output, true, 3, 5, 4) {
		t.Fatalf("expected stripped text tool calls inside budget to be accepted")
	}
	plain := &llm.GenerateOutput{Text: "普通回答"}
	if toolRunFinalAnswerMissing(plain, true, 5, 5, 1) {
		t.Fatalf("expected plain answer without tool attempts to be accepted")
	}
	if toolRunFinalAnswerMissing(nil, true, 5, 5, 1) {
		t.Fatalf("expected nil output to be accepted")
	}
}

func TestBuildToolStageInstructionsCoverFinalizePath(t *testing.T) {
	merge := buildToolStageMergeInstruction()
	finalize := buildToolStageFinalizeInstruction()
	if merge == "" || finalize == "" {
		t.Fatal("merge and finalize instructions must be non-empty")
	}
	if strings.Contains(finalize, "tool budget") && !strings.Contains(finalize, "may not call tools") {
		t.Fatalf("finalize instruction must forbid tools explicitly: %s", finalize)
	}
	if !strings.Contains(finalize, "missing") || !strings.Contains(finalize, "user") {
		t.Fatalf("finalize instruction must guide the model to state missing info and request from user: %s", finalize)
	}
}

func TestCountBudgetedToolCallsExcludesProgressiveActivation(t *testing.T) {
	rows := []model.ToolCall{
		{ToolName: mcpActivateServerToolName},
		{ToolName: "credential_create"},
	}
	if got := countBudgetedToolCalls(rows); got != 1 {
		t.Fatalf("countBudgetedToolCalls() = %d, want 1", got)
	}
}
