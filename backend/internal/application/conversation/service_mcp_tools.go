package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	domainmcp "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/mcp"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/mcp"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/secretbox"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/security"
)

type selectedToolRuntime struct {
	definitions          []llm.ToolDefinition
	nameMap              map[string]string
	mcpConfigs           map[string]mcp.CallConfig
	schemas              map[string]json.RawMessage
	attachmentProcessor  *selectedAttachmentProcessor
	platformEntries      map[string]platformToolEntry // 平台内置工具（本地执行，无 MCP 配置）
	platformDefinitions  []llm.ToolDefinition
	platformNameMap      map[string]string
	authorizedMCPTools   map[string]authorizedMCPTool
	authorizedMCPOrder   []string
	authorizedMCPServers map[uint]authorizedMCPServer
	mcpActivation        *mcpActivationState
	onMCPActivation      func(context.Context, []uint) error
}

const mcpActivateServerToolName = "mcp_activate_server"

var mcpActivateServerInputSchema = json.RawMessage(`{
	"type":"object",
	"properties":{"server_id":{"type":"integer","minimum":1,"description":"Authorized MCP server ID to activate"}},
	"required":["server_id"]
}`)

type authorizedMCPTool struct {
	serverID   uint
	definition llm.ToolDefinition
	toolName   string
	config     mcp.CallConfig
	schema     json.RawMessage
}

type authorizedMCPServer struct {
	id          uint
	name        string
	description string
}

type mcpActivationState struct {
	mu        sync.Mutex
	serverIDs map[uint]struct{}
}

func newMCPActivationState(serverIDs []uint) *mcpActivationState {
	state := &mcpActivationState{serverIDs: make(map[uint]struct{}, len(serverIDs))}
	for _, serverID := range serverIDs {
		if serverID != 0 {
			state.serverIDs[serverID] = struct{}{}
		}
	}
	return state
}

func (s *mcpActivationState) activeServerIDs() []uint {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedMCPServerIDs(s.serverIDs)
}

func (s *mcpActivationState) retainAuthorizedServers(servers map[uint]authorizedMCPServer) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for serverID := range s.serverIDs {
		if _, ok := servers[serverID]; !ok {
			delete(s.serverIDs, serverID)
		}
	}
}

func sortedMCPServerIDs(items map[uint]struct{}) []uint {
	result := make([]uint, 0, len(items))
	for serverID := range items {
		result = append(result, serverID)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

type selectedAttachmentProcessor struct {
	toolID         uint
	serverID       uint
	modelName      string
	toolName       string
	displayName    string
	mode           string // image / audio / file（决定注入哪些附件）
	argument       string
	encoding       string
	promptArgument string
	config         mcp.CallConfig
	schema         json.RawMessage
}

func injectMCPToolGuidance(messages []llm.Message, runtime selectedToolRuntime, customPrompt string) []llm.Message {
	if len(runtime.definitions) == 0 {
		return messages
	}

	content := strings.TrimSpace(customPrompt)
	if content == "" {
		content = defaultMCPToolGuidancePrompt()
	}
	if len(runtime.platformEntries) > 0 {
		content = content + "\n\n" + platformToolGuidancePrompt()
	}

	insertAt := 0
	for insertAt < len(messages) && messages[insertAt].Role == "system" {
		insertAt++
	}
	next := make([]llm.Message, 0, len(messages)+1)
	next = append(next, messages[:insertAt]...)
	next = append(next, llm.Message{Role: "system", Content: content})
	next = append(next, messages[insertAt:]...)
	return next
}

func defaultMCPToolGuidancePrompt() string {
	var builder strings.Builder
	builder.WriteString("# tool_use\n")
	builder.WriteString("- Tools are declared separately via the API schema; follow that schema exactly.\n")
	builder.WriteString("- Use tools only for external, realtime, private, or explicitly requested data.\n")
	builder.WriteString("- Use the fewest useful calls; each call must add new information.\n")
	builder.WriteString("- Do not repeat an identical failed call. Adjust arguments, use another tool, or answer from available evidence.\n")
	builder.WriteString("- If tools fail or lack enough data, state the gap in the final answer.\n")
	builder.WriteString("- Do not expose raw tool JSON, internal fields, or tool logs unless the user asks.\n")
	return strings.TrimSpace(builder.String())
}

func summarizeToolInputSchema(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return ""
	}
	properties, _ := schema["properties"].(map[string]interface{})
	if len(properties) == 0 {
		return "无需参数"
	}
	required := map[string]struct{}{}
	if items, ok := schema["required"].([]interface{}); ok {
		for _, item := range items {
			if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
				required[strings.TrimSpace(name)] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(properties))
	for name := range properties {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		prop, _ := properties[name].(map[string]interface{})
		fieldType := schemaFieldType(prop)
		label := name
		if fieldType != "" {
			label = fmt.Sprintf("%s:%s", name, fieldType)
		}
		if _, ok := required[name]; ok {
			label += " 必填"
		}
		parts = append(parts, label)
	}
	if len(parts) > 6 {
		parts = append(parts[:6], fmt.Sprintf("等 %d 个字段", len(parts)))
	}
	return "参数 " + strings.Join(parts, "，")
}

func schemaFieldType(prop map[string]interface{}) string {
	if len(prop) == 0 {
		return ""
	}
	if value, ok := prop["type"].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	if items, ok := prop["type"].([]interface{}); ok && len(items) > 0 {
		types := make([]string, 0, len(items))
		for _, item := range items {
			if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
				types = append(types, strings.TrimSpace(value))
			}
		}
		if len(types) > 0 {
			return strings.Join(types, "|")
		}
	}
	if _, ok := prop["enum"].([]interface{}); ok {
		return "enum"
	}
	return ""
}

func (s *Service) resolveSelectedToolRuntime(ctx context.Context, toolIDs []uint) (selectedToolRuntime, error) {
	return s.resolveSelectedToolRuntimeWithActivation(ctx, toolIDs, nil, nil)
}

func (s *Service) resolveSelectedToolRuntimeWithActivation(
	ctx context.Context,
	toolIDs []uint,
	activation *mcpActivationState,
	onActivation func(context.Context, []uint) error,
) (selectedToolRuntime, error) {
	if activation == nil {
		activation = newMCPActivationState(nil)
	}
	result := selectedToolRuntime{
		definitions:          make([]llm.ToolDefinition, 0, len(toolIDs)+8),
		nameMap:              map[string]string{},
		mcpConfigs:           map[string]mcp.CallConfig{},
		schemas:              map[string]json.RawMessage{},
		authorizedMCPTools:   map[string]authorizedMCPTool{},
		authorizedMCPServers: map[uint]authorizedMCPServer{},
		mcpActivation:        activation,
		onMCPActivation:      onActivation,
	}
	if len(toolIDs) > 0 && s.cfg.Snapshot().MCPEnable {
		if s.mcpRepo == nil {
			return selectedToolRuntime{}, fmt.Errorf("resolve selected MCP tools: repository unavailable")
		}
		if err := s.resolveMCPToolRuntime(ctx, toolIDs, &result); err != nil {
			return selectedToolRuntime{}, err
		}
	}
	// 平台内置工具（本地执行）：读工具受 platform_tools.enabled 控制，写工具另需 write_enabled。
	if err := s.appendPlatformToolRuntime(ctx, &result); err != nil {
		return selectedToolRuntime{}, err
	}
	activation.retainAuthorizedServers(result.authorizedMCPServers)
	return result.visibleRuntime(), nil
}

// resolveMCPToolRuntime 解析用户勾选的 MCP 工具（原 resolveSelectedToolRuntime 主体）。
func (s *Service) resolveMCPToolRuntime(ctx context.Context, toolIDs []uint, result *selectedToolRuntime) error {
	tools, err := s.mcpRepo.ListToolsByIDs(ctx, uniqueToolIDs(toolIDs))
	if err != nil {
		return fmt.Errorf("resolve selected MCP tools: %w", err)
	}
	if len(tools) == 0 {
		return nil
	}

	cfg := s.cfg.Snapshot()
	outboundPolicy := cfg.TrustedOutboundPolicy()
	usedNames := map[string]int{mcpActivateServerToolName: 1}
	serverCache := map[uint]*domainmcp.Server{}
	for _, tool := range tools {
		if tool.Status != "active" {
			continue
		}
		isAttachmentProcessor := domainmcp.IsValidAttachmentMode(strings.TrimSpace(tool.AttachmentInputMode))
		server, ok := serverCache[tool.ServerID]
		if !ok {
			server, err = s.mcpRepo.GetServer(ctx, tool.ServerID)
			if err != nil {
				return fmt.Errorf("resolve MCP server %d: %w", tool.ServerID, err)
			}
			if server == nil || server.Status != "active" {
				if isAttachmentProcessor {
					return fmt.Errorf("%w: processor server is unavailable", ErrImageAttachmentProcessingFailed)
				}
				continue
			}
			if validateErr := security.ValidateOutboundHTTPURL(server.BaseURL, outboundPolicy); validateErr != nil {
				if isAttachmentProcessor {
					return fmt.Errorf("%w: processor server URL is not allowed", ErrImageAttachmentProcessingFailed)
				}
				continue
			}
			serverCache[tool.ServerID] = server
		}
		baseModelName := llm.NormalizeToolName(tool.Name)
		if strings.TrimSpace(baseModelName) == "" {
			continue
		}
		schema := json.RawMessage(strings.TrimSpace(tool.InputSchemaJSON))
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		token, err := secretbox.DecryptString(cfg.DataEncryptionKey, server.AuthTokenEnc)
		if err != nil {
			if isAttachmentProcessor {
				return fmt.Errorf("%w: processor credentials are unavailable", ErrImageAttachmentProcessingFailed)
			}
			continue
		}
		headers := parseMCPHeaders(server.HeadersJSON)
		callConfig := mcp.CallConfig{
			BaseURL:   server.BaseURL,
			AuthToken: token,
			TimeoutMS: cfg.MCPToolTimeoutSeconds * 1000,
			Headers:   headers,
		}
		if isAttachmentProcessor {
			if bindErr := result.bindAttachmentProcessor(selectedAttachmentProcessor{
				toolID:         tool.ID,
				serverID:       tool.ServerID,
				modelName:      baseModelName,
				toolName:       tool.Name,
				displayName:    firstNonEmptyString(tool.DisplayName, tool.Name),
				mode:           strings.ToLower(strings.TrimSpace(tool.AttachmentInputMode)),
				argument:       strings.TrimSpace(tool.AttachmentArgument),
				encoding:       strings.TrimSpace(tool.AttachmentEncoding),
				promptArgument: strings.TrimSpace(tool.AttachmentPromptArgument),
				config:         callConfig,
				schema:         schema,
			}); bindErr != nil {
				return bindErr
			}
			result.authorizedMCPServers[tool.ServerID] = authorizedMCPServer{
				id:          tool.ServerID,
				name:        strings.TrimSpace(server.Name),
				description: strings.TrimSpace(server.Description),
			}
			continue

		}
		modelName := uniqueModelToolName(baseModelName, usedNames)
		if modelName == "" {
			continue
		}
		definition := llm.ToolDefinition{
			Name:        modelName,
			Description: strings.TrimSpace(tool.Description),
			InputSchema: schema,
		}
		result.authorizedMCPTools[modelName] = authorizedMCPTool{
			serverID:   tool.ServerID,
			definition: definition,
			toolName:   tool.Name,
			config:     callConfig,
			schema:     schema,
		}
		result.authorizedMCPOrder = append(result.authorizedMCPOrder, modelName)
		result.authorizedMCPServers[tool.ServerID] = authorizedMCPServer{
			id:          tool.ServerID,
			name:        strings.TrimSpace(server.Name),
			description: strings.TrimSpace(server.Description),
		}
	}
	return nil
}

func (r selectedToolRuntime) visibleRuntime() selectedToolRuntime {
	r.definitions = append([]llm.ToolDefinition(nil), r.platformDefinitions...)
	r.nameMap = make(map[string]string, len(r.platformEntries)+len(r.authorizedMCPTools)+1)
	r.schemas = make(map[string]json.RawMessage, len(r.platformEntries)+len(r.authorizedMCPTools)+1)
	r.mcpConfigs = make(map[string]mcp.CallConfig, len(r.authorizedMCPTools))
	for modelName, entry := range r.platformEntries {
		executionName := strings.TrimSpace(r.platformNameMap[modelName])
		if executionName == "" {
			executionName = entry.definition.Name
		}
		r.nameMap[modelName] = executionName
		r.schemas[modelName] = entry.definition.InputSchema
	}
	if len(r.authorizedMCPServers) == 0 {
		return r
	}
	r.definitions = append(r.definitions, llm.ToolDefinition{
		Name:        mcpActivateServerToolName,
		Description: r.mcpActivationDescription(),
		InputSchema: mcpActivateServerInputSchema,
	})
	r.nameMap[mcpActivateServerToolName] = mcpActivateServerToolName
	r.schemas[mcpActivateServerToolName] = mcpActivateServerInputSchema
	active := make(map[uint]struct{})
	if r.mcpActivation != nil {
		for _, serverID := range r.mcpActivation.activeServerIDs() {
			active[serverID] = struct{}{}
		}
	}
	for _, modelName := range r.authorizedMCPOrder {
		tool, ok := r.authorizedMCPTools[modelName]
		if !ok {
			continue
		}
		if _, ok := active[tool.serverID]; !ok {
			continue
		}
		r.definitions = append(r.definitions, tool.definition)
		r.nameMap[modelName] = tool.toolName
		r.schemas[modelName] = tool.schema
		r.mcpConfigs[modelName] = tool.config
	}
	return r
}

func (r selectedToolRuntime) mcpActivationDescription() string {
	serverIDs := make([]uint, 0, len(r.authorizedMCPServers))
	for serverID := range r.authorizedMCPServers {
		serverIDs = append(serverIDs, serverID)
	}
	sort.Slice(serverIDs, func(i, j int) bool { return serverIDs[i] < serverIDs[j] })
	var builder strings.Builder
	builder.WriteString("Activate one MCP server authorized for this run. Its selected tool schemas become available on the next model request. Authorized servers:\n")
	for _, serverID := range serverIDs {
		server := r.authorizedMCPServers[serverID]
		description := server.description
		if description == "" {
			description = "No administrator description provided."
		}
		fmt.Fprintf(&builder, "- server_id=%d; name=%s; description=%s\n", server.id, server.name, description)
	}
	builder.WriteString("Only activate a server when its described capability is needed. Do not guess or invoke undisclosed MCP tool names.")
	return strings.TrimSpace(builder.String())
}

func (r *selectedToolRuntime) activateMCPServer(ctx context.Context, serverID uint) (bool, error) {
	if r == nil || serverID == 0 {
		return false, fmt.Errorf("invalid MCP server id")
	}
	if _, ok := r.authorizedMCPServers[serverID]; !ok {
		return false, fmt.Errorf("MCP server %d is not authorized for this run", serverID)
	}
	if r.mcpActivation == nil {
		return false, fmt.Errorf("MCP activation state is unavailable")
	}
	r.mcpActivation.mu.Lock()
	defer r.mcpActivation.mu.Unlock()
	if _, ok := r.mcpActivation.serverIDs[serverID]; ok {
		return false, nil
	}
	next := make(map[uint]struct{}, len(r.mcpActivation.serverIDs)+1)
	for activeServerID := range r.mcpActivation.serverIDs {
		next[activeServerID] = struct{}{}
	}
	next[serverID] = struct{}{}
	serverIDs := sortedMCPServerIDs(next)
	if r.onMCPActivation != nil {
		if err := r.onMCPActivation(ctx, serverIDs); err != nil {
			return false, err
		}
	}
	r.mcpActivation.serverIDs[serverID] = struct{}{}
	return true, nil
}

func (r *selectedToolRuntime) bindAttachmentProcessor(processor selectedAttachmentProcessor) error {
	if r.attachmentProcessor != nil {
		return ErrMultipleImageAttachmentProcessors
	}
	r.attachmentProcessor = &processor
	return nil
}

func (r selectedToolRuntime) attachmentProcessorActive() bool {
	if r.attachmentProcessor == nil || r.attachmentProcessor.serverID == 0 || r.mcpActivation == nil {
		return false
	}
	for _, serverID := range r.mcpActivation.activeServerIDs() {
		if serverID == r.attachmentProcessor.serverID {
			return true
		}
	}
	return false
}

func (r selectedToolRuntime) withoutAttachmentProcessor() selectedToolRuntime {
	r.attachmentProcessor = nil
	return r.visibleRuntime()
}

func (r selectedToolRuntime) withoutDefinitions() selectedToolRuntime {
	r.definitions = nil
	r.nameMap = nil
	r.mcpConfigs = nil
	r.schemas = nil
	r.attachmentProcessor = nil
	r.platformEntries = nil
	r.platformDefinitions = nil
	r.platformNameMap = nil
	r.authorizedMCPTools = nil
	r.authorizedMCPOrder = nil
	r.authorizedMCPServers = nil
	return r
}

func uniqueToolIDs(items []uint) []uint {
	seen := make(map[uint]struct{}, len(items))
	result := make([]uint, 0, len(items))
	for _, item := range items {
		if item == 0 {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func uniqueModelToolName(base string, used map[string]int) string {
	value := strings.TrimSpace(base)
	if value == "" {
		return ""
	}
	count := used[value]
	used[value] = count + 1
	if count == 0 {
		return value
	}
	suffix := "_" + strconv.Itoa(count+1)
	if len(value)+len(suffix) > 64 {
		value = value[:64-len(suffix)]
	}
	return value + suffix
}

func parseMCPHeaders(raw string) map[string]string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return map[string]string{}
	}
	payload := map[string]string{}
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(payload))
	for key, item := range payload {
		headerKey := strings.TrimSpace(key)
		if headerKey == "" {
			continue
		}
		result[headerKey] = strings.TrimSpace(item)
	}
	return result
}
