package conversation

import (
	"encoding/json"
	"strings"
)

// 模型图片输入能力（vision）判定：capabilities JSON 显式声明优先，
// 未声明时按已知多模态模型名前缀启发式推断（保守方向：宁可降级为路径标记
// 也不把图片直传给不支持视觉的模型导致上游 400）。
//
// 参考 Hermes image_routing.py：supports_vision=True → native 直传；
// 否则 text 降级（模型只见图片路径标记，不注入像素）。

// visionModelNamePrefixes 已知支持图片输入的模型名前缀（小写）。
var visionModelNamePrefixes = []string{
	// OpenAI 多模态对话模型
	"gpt-4o", "gpt-4.1", "gpt-5", "chatgpt-4o", "chatgpt-5",
	// Anthropic Claude 3 起全系多模态
	"claude-3", "claude-4", "claude-sonnet", "claude-opus", "claude-haiku",
	// Google Gemini 全系
	"gemini",
	// xAI
	"grok-3", "grok-4",
	// 开源多模态（Qwen-VL / LLaVA / InternVL / GLM-4V / MiniCPM-V / Phi-3-Vision 等）
	"qwen-vl", "qwen2-vl", "qwen2.5-vl", "qwen3-vl", "qwen2.5-omni",
	"deepseek-vl", "llava", "internvl", "glm-4v", "glm-4.5v", "minicpm-v", "phi-3-vision", "phi-4-vision",
}

// modelSupportsVision 判定模型是否原生支持图片输入。
// capabilitiesJSON 显式声明 "vision": true/false 时以声明为准；
// 未声明时按模型名前缀启发式推断（默认 false，走降级路径）。
func modelSupportsVision(platformModelName string, capabilitiesJSON string) bool {
	if trimmed := strings.TrimSpace(capabilitiesJSON); trimmed != "" {
		var caps struct {
			Vision *bool `json:"vision"`
		}
		if err := json.Unmarshal([]byte(trimmed), &caps); err == nil && caps.Vision != nil {
			return *caps.Vision
		}
	}
	name := strings.ToLower(strings.TrimSpace(platformModelName))
	for _, prefix := range visionModelNamePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func modelSupportsMedia(platformModelName string, capabilitiesJSON string, modality string) bool {
	modality = strings.ToLower(strings.TrimSpace(modality))
	if modality == "image" {
		return modelSupportsVision(platformModelName, capabilitiesJSON)
	}
	if trimmed := strings.TrimSpace(capabilitiesJSON); trimmed != "" {
		var caps struct {
			Audio *bool `json:"audio"`
			Video *bool `json:"video"`
		}
		if err := json.Unmarshal([]byte(trimmed), &caps); err == nil {
			switch modality {
			case "audio":
				return caps.Audio != nil && *caps.Audio
			case "video":
				return caps.Video != nil && *caps.Video
			}
		}
	}
	return false
}

// buildImageAttachmentHint 构造图片路径标记（Hermes text 模式同款）：
// 模型看不到像素，只看到引用；fileID 保留供模型后续调用工具时引用。
func buildImageAttachmentHint(fileName string, fileID string) string {
	name := strings.TrimSpace(fileName)
	if name == "" {
		name = "image"
	}
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return "[Image attachment: " + name + "]"
	}
	return "[Image attachment: " + name + " (fileID: " + fileID + ")]"
}
