package mcp

import (
	"strings"
	"time"
)

const (
	AttachmentInputModeNone  = "none"
	AttachmentInputModeImage = "image"
	AttachmentInputModeAudio = "audio"
	AttachmentInputModeFile  = "file"

	AttachmentEncodingBase64  = "base64"
	AttachmentEncodingDataURL = "data_url"
)

// IsValidAttachmentMode 判断是否为可用的附件注入模式（none 之外）。
func IsValidAttachmentMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case AttachmentInputModeImage, AttachmentInputModeAudio, AttachmentInputModeFile:
		return true
	}
	return false
}

// Server 表示管理员维护的 MCP 服务。
type Server struct {
	ID                                   uint
	Name                                 string
	Description                          string
	BaseURL                              string
	AuthTokenEnc                         string
	HeadersJSON                          string
	Status                               string
	SortOrder                            int
	ToolCount                            int
	ActiveToolCount                      int
	RequiresToolMetadataSyncConfirmation bool
	LastSyncedAt                         *time.Time
	LastError                            string
	CreatedAt                            time.Time
	UpdatedAt                            time.Time
}

type ServerWithTools struct {
	Server Server
	Tools  []Tool
}

// Tool 表示从 MCP 服务发现并由管理员控制可用性的工具。
type Tool struct {
	ID                       uint
	ServerID                 uint
	ServerName               string
	Name                     string
	DisplayName              string
	Description              string
	InputSchemaJSON          string
	AttachmentInputMode      string
	AttachmentArgument       string
	AttachmentEncoding       string
	AttachmentPromptArgument string
	Status                   string
	SortOrder                int
	CreatedAt                time.Time
	UpdatedAt                time.Time
}
