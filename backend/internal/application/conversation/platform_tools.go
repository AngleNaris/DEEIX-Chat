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
		"list_memories": {
			definition: llm.ToolDefinition{
				Name: "list_memories",
				Description: "List the user's long-term memories (key, scope, value summary) to see what is stored about them. " +
					"Optional scope filter: preference, profile, custom.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"scope":{"type":"string","description":"Optional filter: preference, profile, or custom"}
					},"required":[]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformListMemories,
		},
		"save_memory": {
			definition: llm.ToolDefinition{
				Name: "save_memory",
				Description: "Save or update a long-term memory about the user (durable preference, background fact, or standing instruction). " +
					"Use the same key to update an existing memory. Scope: preference (injected every message, use sparingly), " +
					"profile/custom (recalled by relevance, default custom). " +
					"This is a WRITE operation: it may require user approval depending on the user's approval mode.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"key":{"type":"string","description":"Memory name/key, e.g. language_preference (max 128 chars)"},
						"value":{"type":"string","description":"Memory content (max 10000 chars)"},
						"scope":{"type":"string","enum":["preference","profile","custom"],"description":"preference=always injected, use sparingly; profile/custom=recalled by relevance (default custom)"}
					},"required":["key","value"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformSaveMemory,
			auditAction: "platform_tools.save_memory",
		},
		"delete_memory": {
			definition: llm.ToolDefinition{
				Name: "delete_memory",
				Description: "Delete a long-term memory by its key (use list_memories to find keys). " +
					"Use when the user says to forget or change something previously remembered. " +
					"This is a WRITE operation: it may require user approval depending on the user's approval mode.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"key":{"type":"string","description":"Memory key to delete"}
					},"required":["key"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformDeleteMemory,
			auditAction: "platform_tools.delete_memory",
		},
		"save_artifact": {
			definition: llm.ToolDefinition{
				Name: "save_artifact",
				Description: "Save an artifact (user-facing HTML/JS/CSS/text content produced for the user) so the user can revisit or share it later. " +
					"Pass artifact_id to update an existing artifact. " +
					"This is a WRITE operation: it may require user approval depending on the user's approval mode.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"artifact_id":{"type":"string","description":"Optional artifact id to update (from list_artifacts)"},
						"title":{"type":"string","description":"Artifact title (max 255 chars)"},
						"kind":{"type":"string","enum":["html","js","css","text"],"description":"Artifact type (default text)"},
						"code":{"type":"string","description":"Full artifact content (max 256KB)"}
					},"required":["title","code"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformSaveArtifact,
			auditAction: "platform_tools.save_artifact",
		},
		"list_artifacts": {
			definition: llm.ToolDefinition{
				Name: "list_artifacts",
				Description: "List the user's saved artifacts (id, title, kind, share link if active) " +
					"to see what has been saved or find an artifact id.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"page":{"type":"integer","description":"Page number, starting at 1 (default 1)"},
						"page_size":{"type":"integer","description":"Page size (default 20, max 50)"}
					},"required":[]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformListArtifacts,
		},
		"delete_artifact": {
			definition: llm.ToolDefinition{
				Name: "delete_artifact",
				Description: "Delete one of the user's saved artifacts (also revokes its public share). " +
					"This is a WRITE operation: it may require user approval depending on the user's approval mode.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"artifact_id":{"type":"string","description":"Artifact id from list_artifacts"}
					},"required":["artifact_id"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformDeleteArtifact,
			auditAction: "platform_tools.delete_artifact",
		},
		"share_artifact": {
			definition: llm.ToolDefinition{
				Name: "share_artifact",
				Description: "Create a public share link for one of the user's saved artifacts (anyone with the link can view it). " +
					"Use when the user wants to share an artifact's HTML preview publicly. " +
					"This is a WRITE operation: it may require user approval depending on the user's approval mode.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"artifact_id":{"type":"string","description":"Artifact id from list_artifacts"}
					},"required":["artifact_id"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformShareArtifact,
			auditAction: "platform_tools.share_artifact",
		},
		"save_doc_card": {
			definition: llm.ToolDefinition{
				Name: "save_doc_card",
				Description: "Save or update a document card (lorebook entry): a doc snippet that is injected into context " +
					"whenever the user message matches one of its keywords (e.g. world settings, character sheets, rule documents). " +
					"Pass card_id to update an existing card (from list_doc_cards). " +
					"This is a WRITE operation: it may require user approval depending on the user's approval mode.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"card_id":{"type":"string","description":"Optional card id to update (from list_doc_cards)"},
						"title":{"type":"string","description":"Card title (max 128 chars)"},
						"content":{"type":"string","description":"Card content injected on keyword match (max 20000 chars)"},
						"keywords":{"type":"array","items":{"type":"string"},"description":"Trigger keywords (max 20); card activates when the user message contains any of them"},
						"enabled":{"type":"boolean","description":"Whether the card is active (default true)"}
					},"required":["title","content"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformSaveDocCard,
			auditAction: "platform_tools.save_doc_card",
		},
		"list_doc_cards": {
			definition: llm.ToolDefinition{
				Name: "list_doc_cards",
				Description: "List the user's document cards (title, content, keywords, enabled) " +
					"to see which lorebook entries exist and find a card_id.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{},"required":[]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformListDocCards,
		},
		"delete_doc_card": {
			definition: llm.ToolDefinition{
				Name: "delete_doc_card",
				Description: "Delete one of the user's document cards by card_id. " +
					"This is a WRITE operation: it may require user approval depending on the user's approval mode.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"card_id":{"type":"string","description":"Card id from list_doc_cards"}
					},"required":["card_id"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformDeleteDocCard,
			auditAction: "platform_tools.delete_doc_card",
		},
		"execute_js": {
			definition: llm.ToolDefinition{
				Name: "execute_js",
				Description: "Run JavaScript code in a safe sandbox to compute values the user needs " +
					"(random numbers, math, data transforms, string/number processing). " +
					"The sandbox has NO filesystem, network, or process access; print results with console.log " +
					"(stdout up to 64KB). The completion value of the last expression is returned as result. " +
					"Default timeout 5s (max 15s via timeout_seconds). " +
					"This is a WRITE operation (code execution): it may require user approval depending on the user's approval mode.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"code":{"type":"string","description":"JavaScript source code to execute (max 32KB)"},
						"timeout_seconds":{"type":"integer","description":"Execution timeout in seconds (default 5, max 15)"}
					},"required":["code"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformExecuteJs,
			auditAction: "platform_tools.execute_js",
		},
		"execute_skill_script": {
			definition: llm.ToolDefinition{
				Name: "execute_skill_script",
				Description: "Execute a JavaScript file (.js/.mjs/.cjs) bundled inside a skill package. " +
					"Get the skill_id and script path from list_skills (package_files). " +
					"Optional args array is passed to the script as the global `args` variable. " +
					"Same sandbox as execute_js: no filesystem, network, or process access. " +
					"This is a WRITE operation (code execution): it may require user approval depending on the user's approval mode.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"skill_id":{"type":"integer","description":"Numeric skill id from list_skills"},
						"path":{"type":"string","description":"Relative path of the .js file inside the skill package, e.g. scripts/tool.js"},
						"args":{"type":"array","description":"Optional arguments passed to the script as the global args variable"},
						"timeout_seconds":{"type":"integer","description":"Execution timeout in seconds (default 5, max 15)"}
					},"required":["skill_id","path"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformExecuteSkillScript,
			auditAction: "platform_tools.execute_skill_script",
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
		"delete_file": {
			definition: llm.ToolDefinition{
				Name: "delete_file",
				Description: "Permanently delete one of the user's uploaded files and release its storage quota. " +
					"Files still referenced by active conversations cannot be deleted. " +
					"This is a WRITE operation: it may require user approval depending on the user's approval mode.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"file_id":{"type":"string","description":"file_id from list_files"}
					},"required":["file_id"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformDeleteFile,
			auditAction: "platform_tools.delete_file",
		},
		"list_user_settings": {
			definition: llm.ToolDefinition{
				Name: "list_user_settings",
				Description: "List the user's personal settings (default model, file mode, input behavior, write approval mode, etc.). " +
					"Read-only; use update_user_setting to change a value.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{},"required":[]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformListUserSettings,
		},
		"update_user_setting": {
			definition: llm.ToolDefinition{
				Name: "update_user_setting",
				Description: "Update one of the user's personal settings by key, e.g. chat.file_mode (auto|full_context|rag), " +
					"chat.default_model, chat.default_mcp_tool_ids, chat.send_on_enter, platform_tools.write_approval (auto|ask), etc. " +
					"Invalid keys are rejected. This is a WRITE operation: it may require user approval.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"key":{"type":"string","description":"Setting key from list_user_settings"},
						"value":{"type":"string","description":"New value"}
					},"required":["key","value"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformUpdateUserSetting,
			auditAction: "platform_tools.update_user_setting",
		},
		"update_conversation": {
			definition: llm.ToolDefinition{
				Name: "update_conversation",
				Description: "Update one of the user's conversations: title, starred, archived, or labels. " +
					"Use list_conversations / read_conversation first to find the conversation_id. " +
					"This is a WRITE operation: it may require user approval.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"conversation_id":{"type":"integer","description":"Numeric conversation id from list_conversations"},
						"title":{"type":"string","description":"New conversation title"},
						"starred":{"type":"boolean","description":"Mark / unmark as starred"},
						"archived":{"type":"boolean","description":"Archive / unarchive the conversation"},
						"labels":{"type":"array","items":{"type":"string"},"description":"Replace the conversation labels"}
					},"required":["conversation_id"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformUpdateConversation,
			auditAction: "platform_tools.update_conversation",
		},
		"list_roles": {
			definition: llm.ToolDefinition{
				Name: "list_roles",
				Description: "List the user's conversation roles (name, description, model, group). " +
					"Use before create_agent_group to find role public ids for supervisors/workers.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{},"required":[]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformListRoles,
		},
		"create_role": {
			definition: llm.ToolDefinition{
				Name: "create_role",
				Description: "Create a conversation role for the user (name required; optional description, system prompt, model, icon color, group). " +
					"This is a WRITE operation: it may require user approval.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"name":{"type":"string","description":"Role name (required)"},
						"description":{"type":"string","description":"Role description"},
						"system_prompt":{"type":"string","description":"System prompt for the role"},
						"model":{"type":"string","description":"Default model name (empty = platform default)"},
						"group_name":{"type":"string","description":"Display group for organizing roles"},
						"color":{"type":"string","description":"Role icon color"},
						"icon":{"type":"string","description":"Role icon"}
					},"required":["name"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformCreateRole,
			auditAction: "platform_tools.create_role",
		},
		"list_projects": {
			definition: llm.ToolDefinition{
				Name: "list_projects",
				Description: "List the user's conversation projects (name, description, system prompt, default tools/skills).",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{},"required":[]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformListProjects,
		},
		"create_project": {
			definition: llm.ToolDefinition{
				Name: "create_project",
				Description: "Create a conversation project for the user (name required; optional description, system prompt, color, icon). " +
					"This is a WRITE operation: it may require user approval.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"name":{"type":"string","description":"Project name (required)"},
						"description":{"type":"string","description":"Project description"},
						"system_prompt":{"type":"string","description":"Project system prompt applied to conversations"},
						"color":{"type":"string","description":"Project color"},
						"icon":{"type":"string","description":"Project icon"}
					},"required":["name"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformCreateProject,
			auditAction: "platform_tools.create_project",
		},
		"list_agent_groups": {
			definition: llm.ToolDefinition{
				Name: "list_agent_groups",
				Description: "List the user's agent groups (name, description, supervisor, members). " +
					"Requires the agent group feature to be enabled.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{},"required":[]
				}`),
			},
			kind:    platformToolRead,
			handler: (*Service).platformListAgentGroups,
		},
		"create_agent_group": {
			definition: llm.ToolDefinition{
				Name: "create_agent_group",
				Description: "Create an agent group for the user: one supervisor + optional workers, each referencing an existing role public id " +
					"(from list_roles). Requires the agent group feature to be enabled. " +
					"This is a WRITE operation: it may require user approval.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"name":{"type":"string","description":"Group name (required)"},
						"description":{"type":"string","description":"Group description"},
						"coordination_prompt":{"type":"string","description":"Coordination prompt shown to the supervisor"},
						"supervisor_role_id":{"type":"string","description":"Public id of the supervisor role (from list_roles)"},
						"supervisor_duty":{"type":"string","description":"Optional duty instruction for the supervisor"},
						"workers":{"type":"array","items":{"type":"object","properties":{
							"role_id":{"type":"string","description":"Public id of a worker role"},
							"duty_instruction":{"type":"string","description":"Optional duty instruction for this worker"}
						},"required":["role_id"]},"description":"Worker members"}
					},"required":["name","supervisor_role_id"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformCreateAgentGroup,
			auditAction: "platform_tools.create_agent_group",
		},
		"delete_skill": {
			definition: llm.ToolDefinition{
				Name: "delete_skill",
				Description: "Delete one of the user's own skills. Only skills owned by the user can be deleted. " +
					"This is a WRITE operation: it may require user approval.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"skill_id":{"type":"integer","description":"Numeric skill id from list_skills"}
					},"required":["skill_id"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformDeleteSkill,
			auditAction: "platform_tools.delete_skill",
		},
		"delete_role": {
			definition: llm.ToolDefinition{
				Name: "delete_role",
				Description: "Delete one of the user's conversation roles. Roles still referenced by agent groups cannot be deleted. " +
					"This is a WRITE operation: it may require user approval.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"role_id":{"type":"string","description":"Public role id from list_roles"}
					},"required":["role_id"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformDeleteRole,
			auditAction: "platform_tools.delete_role",
		},
		"delete_project": {
			definition: llm.ToolDefinition{
				Name: "delete_project",
				Description: "Delete one of the user's conversation projects. Conversations under the project are preserved (not deleted). " +
					"This is a WRITE operation: it may require user approval.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"project_id":{"type":"string","description":"Public project id from list_projects"}
					},"required":["project_id"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformDeleteProject,
			auditAction: "platform_tools.delete_project",
		},
		"delete_agent_group": {
			definition: llm.ToolDefinition{
				Name: "delete_agent_group",
				Description: "Delete one of the user's agent groups. Groups with run history cannot be deleted. " +
					"Requires the agent group feature to be enabled. " +
					"This is a WRITE operation: it may require user approval.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"group_id":{"type":"string","description":"Public group id from list_agent_groups"}
					},"required":["group_id"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformDeleteAgentGroup,
			auditAction: "platform_tools.delete_agent_group",
		},
		"delete_conversation": {
			definition: llm.ToolDefinition{
				Name: "delete_conversation",
				Description: "Delete one of the user's conversations. Optionally delete the files attached to it " +
					"(delete_files=false by default to be safe). " +
					"This is a WRITE operation: it may require user approval.",
				InputSchema: json.RawMessage(`{
					"type":"object","properties":{
						"conversation_id":{"type":"integer","description":"Numeric conversation id from list_conversations"},
						"delete_files":{"type":"boolean","description":"Also delete files attached to this conversation (default false)"}
					},"required":["conversation_id"]
				}`),
			},
			kind:        platformToolWrite,
			handler:     (*Service).platformDeleteConversation,
			auditAction: "platform_tools.delete_conversation",
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
- Platform tools access the user's own data (files, skills, conversations, memories). Only use them when the user asks or when the information is genuinely needed.
- read_file / read_skill_file / read_conversation / list_memories are read-only; write_file, update_skill, save_memory, delete_memory, execute_js and execute_skill_script modify or execute code and may be held for user approval — if a write returns pending_approval, tell the user it is waiting for their confirmation.
- Never fabricate file_id / skill_id / conversation_id; obtain them from the list_* tools first.
- Do not expose raw tool output or internal fields unless the user asks.
- Memories: use save_memory for durable facts about the user (long-term preferences, background, standing instructions) — not for transient task details or conversation-specific context. Before saving, call list_memories and update the existing entry with the same meaning instead of creating duplicates.
- Memory scopes: "preference" is injected into every message (use sparingly, high-value always-on preferences only); "profile" and "custom" are recalled by relevance. When the user asks to forget or change something remembered, use delete_memory / save_memory accordingly.
- JS execution: use execute_js to compute values on demand (random numbers, math, data transforms). The sandbox has no filesystem/network/process access; print results with console.log and rely on the returned stdout/result. For a script bundled in a skill, use execute_skill_script with the path from list_skills.
- Artifacts: when you produce a polished user-facing HTML/JS piece, offer save_artifact so the user can keep and share it; use list_artifacts to find saved items and share_artifact to create a public link when the user asks to share.`)
}

// traceIDFromContext 提取链路 trace id（缺失时返回空串）。
func traceIDFromContext(ctx context.Context) string {
	return traceid.FromContext(ctx)
}
