package llm

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestBuildChatCompletionsContentSerializesAudioAndVideo(t *testing.T) {
	content := buildChatCompletionsContent(Message{
		Role: "user",
		Parts: []ContentPart{
			{Kind: ContentPartAudio, MimeType: "audio/wav", Data: []byte("audio")},
			{Kind: ContentPartVideo, MimeType: "video/mp4", Data: []byte("video")},
		},
	}, nil)
	parts, ok := content.([]map[string]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("expected two media parts, got %#v", content)
	}
	audio := parts[0]["input_audio"].(map[string]string)
	if parts[0]["type"] != "input_audio" || audio["format"] != "wav" || audio["data"] != base64.StdEncoding.EncodeToString([]byte("audio")) {
		t.Fatalf("unexpected audio payload: %#v", parts[0])
	}
	video := parts[1]["video_url"].(map[string]string)
	if parts[1]["type"] != "video_url" || video["url"] != "data:video/mp4;base64,"+base64.StdEncoding.EncodeToString([]byte("video")) {
		t.Fatalf("unexpected video payload: %#v", parts[1])
	}
}

func TestBuildGeminiPartsSerializesAudioAndVideoInlineData(t *testing.T) {
	parts := buildGeminiParts(Message{
		Role: "user",
		Parts: []ContentPart{
			{Kind: ContentPartAudio, MimeType: "audio/mpeg", Data: []byte("audio")},
			{Kind: ContentPartVideo, MimeType: "video/webm", Data: []byte("video")},
		},
	})
	if len(parts) != 2 {
		t.Fatalf("expected two media parts, got %#v", parts)
	}
	audio := parts[0]["inlineData"].(map[string]interface{})
	video := parts[1]["inlineData"].(map[string]interface{})
	if audio["mimeType"] != "audio/mpeg" || audio["data"] != base64.StdEncoding.EncodeToString([]byte("audio")) {
		t.Fatalf("unexpected Gemini audio payload: %#v", audio)
	}
	if video["mimeType"] != "video/webm" || video["data"] != base64.StdEncoding.EncodeToString([]byte("video")) {
		t.Fatalf("unexpected Gemini video payload: %#v", video)
	}
}

func TestValidateResponsesMediaPartsRejectsAudioAndVideo(t *testing.T) {
	for _, kind := range []string{ContentPartAudio, ContentPartVideo} {
		err := validateResponsesMediaParts([]Message{{
			Role:  "user",
			Parts: []ContentPart{{Kind: kind, Data: []byte("media")}},
		}})
		if err == nil || !strings.Contains(err.Error(), "responses_api_unsupported_media") {
			t.Fatalf("expected explicit unsupported media error for %s, got %v", kind, err)
		}
	}
	if err := validateResponsesMediaParts([]Message{{
		Role:  "user",
		Parts: []ContentPart{{Kind: ContentPartImage, Data: []byte("image")}},
	}}); err != nil {
		t.Fatalf("expected image input to remain supported, got %v", err)
	}
}
