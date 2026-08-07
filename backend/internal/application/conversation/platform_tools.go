package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/traceid"
	"go.uber.org/zap"
)

// 平台工具（platform tools）：平台内置、本地执行、代表用户访问平台数据的工具。
// 与 MCP 工具共用同一条通道：定义经 llm.ToolDefinition 注入模型，执行分发在
// executeAssistantToolCalls / executeAgentTurnToolCalls 中按模型名命中平台注册表。
// 读操作不受批准限制；写操作受管理员开关（platform_tools.write_enabled）与
// 用户批准模式（platform_tools.write_approval = auto|ask）双重管控。
const (
	platformToolsNamespace       = "platform_tools"
	platformToolsKeyEnabled      = "enabled"
	platformToolsKeyWriteEnabled = "write_enabled"
	platformToolsKeyReindexDelay = "file_reindex_delay_seconds"

	// platformToolsWriteApprovalKey 用户设置键：写操作批准模式。
	platformToolsWriteApprovalKey  = "platform_tools.write_approval"
	platformToolsWriteApprovalAuto = "auto"
	platformToolsWriteApprovalAsk  = "ask"

	// 平台工具大小预算（与技能包 read_file 的 32KB/文件口径一致）。
	platformFileReadLimitBytes  = 32 << 10 // 单次读取文件/提取文本上限 32KB
	platformSkillReadLimitBytes = 32 << 10 // 单次读取技能包文件上限 32KB
	platformFileWriteLimitBytes = 1 << 20  // 单次写入文件内容上限 1MB
	platformListPageSize        = 20       // 列表类工具默认每页条数
)

// platformToolKind 区分读/写工具，写工具受批准模式管控。
type platformToolKind string

const (
	platformToolRead  platformToolKind = "read"
	platformToolWrite platformToolKind = "write"
)

// platformToolCallContext 平台工具执行上下文（身份来自鉴权后的 UserID，全部访问限定本人数据）。
type platformToolCallContext struct {
	UserID         uint
	ConversationID uint
	RequestID      string
	Arguments      json.RawMessage
}

// platformToolHandler 平台工具执行函数（方法表达式注册，绑定 Service 依赖）。
type platformToolHandler func(s *Service, ctx context.Context, call platformToolCallContext) (string, error)

// platformToolEntry 注册表中的单条平台工具。
type platformToolEntry struct {
	definition  llm.ToolDefinition
	kind        platformToolKind
	handler     platformToolHandler
	auditAction string
}

// platformToolRegistry 返回平台工具注册表（读 + 写）。
func platformToolRegistry() map[string]platformToolEntry {
	return map[string]platformToolEntry{
		"list_files": {
			definition: llm.ToolDefinition{
				Name: "list_files",
				Description: "List the user's uploaded files (text, images, documents). " +
					"Use this before read_file to find the file_id of a file. Results are truncated to 20 items per page.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"query":{"type":"string","description":"Optional search keyword in file names"},
						"kind":{"type":"string","description":"Optional file category filter: text|image|pdf|word|excel|presentation|unknown"},
						"page":{"type":"integer","description":"Page number, starting at 1 (default 1)"}
					},"required":[]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformListFiles,
		},
		"read_file": {
			definition: llm.ToolDefinition{
				Name: "read_file",
				Description: "Read the content of one of the user's uploaded files. " +
					"Text files return raw content; other types return the extracted text. " +
					"Output is truncated to 32KB per call; use offset to continue reading larger files.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"file_id":{"type":"string","description":"file_id from list_files"},
						"offset":{"type":"integer","description":"Byte offset to start reading from (default 0)"}
					},"required":["file_id"]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformReadFile,
		},
		"read_skill_file": {
			definition: llm.ToolDefinition{
				Name: "read_skill_file",
				Description: "Read a file bundled inside a skill package (must be listed in the skill's files manifest). " +
					"Output is truncated to 32KB per call. Use list_skills first to find skill_id and its file paths.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"skill_id":{"type":"integer","description":"Numeric skill id from list_skills"},
						"path":{"type":"string","description":"Relative file path inside the skill package, e.g. scripts/tool.py"}
					},"required":["skill_id","path"]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformReadSkillFile,
		},
		"list_skills": {
			definition: llm.ToolDefinition{
				Name: "list_skills",
				Description: "List skills available to the user (title, trigger, description, package file list). " +
					"Results are truncated to 20 items per page.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"query":{"type":"string","description":"Optional search keyword"},
						"page":{"type":"integer","description":"Page number, starting at 1 (default 1)"}
					},"required":[]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformListSkills,
		},
		"list_conversations": {
			definition: llm.ToolDefinition{
				Name: "list_conversations",
				Description: "List the user's conversations (title, model, updated time) to locate past discussion history. " +
					"Results are truncated to 20 items per page. Use read_conversation to read message content.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"query":{"type":"string","description":"Optional search keyword in titles"},
						"page":{"type":"integer","description":"Page number, starting at 1 (default 1)"}
					},"required":[]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformListConversations,
		},
		"read_conversation": {
			definition: llm.ToolDefinition{
				Name: "read_conversation",
				Description: "Read messages from one of the user's past conversations. " +
					"Returns the most recent messages (default 20, max 50), each truncated to 4000 characters.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"conversation_id":{"type":"integer","description":"Numeric conversation id from list_conversations"},
						"limit":{"type":"integer","description":"Number of recent messages to read (default 20, max 50)"}
					},"required":["conversation_id"]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformReadConversation,
		},
		"write_file": {
			definition: llm.ToolDefinition{
				Name: "write_file",
				Description: "Overwrite the content of one of the user's uploaded text files. " +
					"Only text files are writable; content is limited to 1MB. " +
					"The file will be re-processed (text extraction / RAG rebuild) shortly after the change. " +
					"This is a WRITE operation: it may require user approval depending on the user's approval mode.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"file_id":{"type":"string","description":"file_id from list_files; the file must be a text file"},
						"content":{"type":"string","description":"Full new content of the file (overwrites existing content)"}
					},"required":["file_id","content"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformWriteFile,
			auditAction: "platform_tools.write_file",
		},
		"update_skill": {
			definition: llm.ToolDefinition{
				Name: "update_skill",
				Description: "Update one of the user's own skills (title, trigger, description, markdown/SKILL.md, enabled). " +
					"Only skills owned by the user can be updated. " +
					"This is a WRITE operation: it may require user approval depending on the user's approval mode.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"skill_id":{"type":"integer","description":"Numeric skill id from list_skills"},
						"title":{"type":"string","description":"New title"},
						"trigger":{"type":"string","description":"New trigger keyword"},
						"description":{"type":"string","description":"New description"},
						"markdown":{"type":"string","description":"New SKILL.md content"},
						"enabled":{"type":"boolean","description":"Whether the skill is enabled"}
					},"required":["skill_id"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformUpdateSkill,
			auditAction: "platform_tools.update_skill",
		},
	}
}

// appendPlatformToolRuntime 按开关把平台工具并入工具运行时：
// 读工具在 platform_tools.enabled=true 时注入；写工具另需 platform_tools.write_enabled=true。
// 与 MCP 工具共用 uniqueModelToolName 防止重名；本地执行无需 SSRF/DB 校验。
func (s *Service) appendPlatformToolRuntime(ctx context.Context, result *selectedToolRuntime) error {
	if s.platformToolsSettings == nil || s == nil {
		return nil
	}
	values, err := s.platformToolsSettings.RuntimeValuesByNamespace(ctx, platformToolsNamespace)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("platform_tools_settings_read_failed",
				zap.String("trace_id", traceIDFromContext(ctx)),
				zap.Error(err),
			)
		}
		return nil
	}
	if strings.TrimSpace(values[platformToolsKeyEnabled]) != "true" {
		return nil
	}
	writeEnabled := strings.TrimSpace(values[platformToolsKeyWriteEnabled]) == "true"

	usedNames := make(map[string]int, len(result.definitions))
	for _, def := range result.definitions {
		if name := strings.TrimSpace(def.Name); name != "" {
			usedNames[name]++
		}
	}
	if result.nameMap == nil {
		result.nameMap = map[string]string{}
	}
	if result.schemas == nil {
		result.schemas = map[string]json.RawMessage{}
	}
	for name, entry := range platformToolRegistry() {
		if entry.kind == platformToolWrite && !writeEnabled {
			continue
		}
		modelName := uniqueModelToolName(name, usedNames)
		if modelName == "" {
			continue
		}
		entry.definition.Name = modelName
		result.definitions = append(result.definitions, entry.definition)
		result.nameMap[modelName] = name
		result.schemas[modelName] = entry.definition.InputSchema
		if result.platformEntries == nil {
			result.platformEntries = map[string]platformToolEntry{}
		}
		result.platformEntries[modelName] = entry
	}
	return nil
}

// executePlatformToolCall 执行平台工具。写工具先检查用户批准模式：
// auto 直接执行；ask 创建待批准记录并返回 pending 结果（前端确认后异步执行）。
func (s *Service) executePlatformToolCall(ctx context.Context, entry platformToolEntry, input ExecuteToolInput) (string, error) {
	if entry.handler == nil {
		return "", fmt.Errorf("platform tool %q has no handler", entry.definition.Name)
	}
	if entry.kind == platformToolWrite {
		approval, err := s.resolveWriteApprovalMode(ctx, input.UserID)
		if err != nil {
			return "", err
		}
		if approval == platformToolsWriteApprovalAsk {
			record := s.platformApprovals.create(input.UserID, input.ConversationID, input.RequestID, entry, input.ArgumentsJSON)
			return fmt.Sprintf(
				`{"status":"pending_approval","approval_id":%q,"message":"write operation submitted for user approval","tool":%q}`,
				record.ID,
				entry.definition.Name,
			), nil
		}
	}

	limit := s.resolvePlatformToolConcurrency()
	return s.executeWithToolLimiter(ctx, limit, func() (string, error) {
		return entry.handler(s, ctx, platformToolCallContext{
			UserID:         input.UserID,
			ConversationID: input.ConversationID,
			RequestID:      strings.TrimSpace(input.RequestID),
			Arguments:      json.RawMessage(strings.TrimSpace(input.ArgumentsJSON)),
		})
	})
}

func (s *Service) resolvePlatformToolConcurrency() int {
	limit := s.cfg.Snapshot().MCPMaxConcurrentCalls
	if limit <= 0 {
		limit = 8
	}
	return limit
}

// resolveWriteApprovalMode 读取用户写操作批准模式，默认 auto。
func (s *Service) resolveWriteApprovalMode(ctx context.Context, userID uint) (string, error) {
	if userID == 0 || s.repo == nil {
		return platformToolsWriteApprovalAuto, nil
	}
	value, err := s.repo.GetUserSettingValue(ctx, userID, platformToolsWriteApprovalKey)
	if err != nil || strings.TrimSpace(value) == "" {
		return platformToolsWriteApprovalAuto, nil
	}
	mode := strings.TrimSpace(value)
	if mode != platformToolsWriteApprovalAuto && mode != platformToolsWriteApprovalAsk {
		return platformToolsWriteApprovalAuto, nil
	}
	return mode, nil
}

// platformToolGuidancePrompt 平台工具使用纪律（追加在 MCP 工具引导之后）。
func platformToolGuidancePrompt() string {
	return strings.TrimSpace(`# platform_tools
- Platform tools access the user's own data (files, skills, conversations). Only use them when the user asks or when the information is genuinely needed.
- read_file / read_skill_file / read_conversation are read-only; write_file and update_skill modify data and may be held for user approval — if a write returns pending_approval, tell the user it is waiting for their confirmation.
- Never fabricate file_id / skill_id / conversation_id; obtain them from the list_* tools first.
- Do not expose raw tool output or internal fields unless the user asks.`)
}

// traceIDFromContext 提取链路 trace id（缺失时返回空串）。
func traceIDFromContext(ctx context.Context) string {
	return traceid.FromContext(ctx)
}
