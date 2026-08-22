package conversation

import (
	"strings"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

func TestResolveMultimodalDelegationOutput(t *testing.T) {
	tests := []struct {
		name    string
		output  *llm.GenerateOutput
		want    string
		wantErr string
	}{
		{
			name:   "structured success",
			output: &llm.GenerateOutput{Text: `{"status":"ok","analysis":"A terminal shows an SSH host form."}`},
			want:   "A terminal shows an SSH host form.",
		},
		{
			name:   "fenced structured success",
			output: &llm.GenerateOutput{Text: "```json\n{\"status\":\"ok\",\"analysis\":\"Speech says hello.\"}\n```"},
			want:   "Speech says hello.",
		},
		{
			name:    "unsupported status",
			output:  &llm.GenerateOutput{Text: `{"status":"unsupported","analysis":"I cannot inspect the video frames."}`},
			wantErr: "unsupported media input",
		},
		{
			name:    "refusal disguised as success",
			output:  &llm.GenerateOutput{Text: `{"status":"ok","analysis":"I cannot analyze this video in the current environment."}`},
			wantErr: "could not be analyzed",
		},
		{
			name:    "plain refusal",
			output:  &llm.GenerateOutput{Text: "无法查看视频帧，因此不能识别内容。"},
			wantErr: "could not be analyzed",
		},
		{
			name:    "video upload request refusal",
			output:  &llm.GenerateOutput{Text: `{"status":"ok","analysis":"I can't watch videos. Please upload the video as frames."}`},
			wantErr: "could not be analyzed",
		},
		{
			name:    "audio upload request refusal",
			output:  &llm.GenerateOutput{Text: `{"status":"ok","analysis":"Please upload the audio so I can inspect it."}`},
			wantErr: "could not be analyzed",
		},
		{
			name:    "malformed response",
			output:  &llm.GenerateOutput{Text: "The image contains a server address."},
			wantErr: "invalid multimodal analysis envelope",
		},
		{
			name:    "empty response",
			output:  &llm.GenerateOutput{},
			wantErr: "no analysis",
		},
		{
			name:    "nil response",
			output:  nil,
			wantErr: "no response",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveMultimodalDelegationOutput(tt.output)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveMultimodalDelegationOutput() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveMultimodalDelegationOutput() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveMultimodalDelegationOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMultimodalDelegationModelForModality(t *testing.T) {
	cfg := config.Config{
		MultimodalDelegationModel:      "legacy-model",
		MultimodalDelegationImageModel: "image-model",
		MultimodalDelegationVideoModel: "video-model",
	}
	tests := []struct {
		modality string
		want     string
	}{
		{modality: "image", want: "image-model"},
		{modality: "audio", want: ""},
		{modality: "video", want: "video-model"},
	}
	for _, tt := range tests {
		if got := multimodalDelegationModelForModality(cfg, tt.modality); got != tt.want {
			t.Fatalf("multimodalDelegationModelForModality(%q) = %q, want %q", tt.modality, got, tt.want)
		}
	}
}

func TestMultimodalDelegationModelForModalityUsesLegacyOnlyWithoutDedicatedModels(t *testing.T) {
	cfg := config.Config{
		MultimodalDelegationModel: "legacy-model",
	}
	for _, modality := range []string{"image", "audio", "video"} {
		if got := multimodalDelegationModelForModality(cfg, modality); got != "legacy-model" {
			t.Fatalf("multimodalDelegationModelForModality(%q) = %q, want legacy-model", modality, got)
		}
	}
}

func TestSelectMultimodalDelegationGroupsUsesPerModalityModels(t *testing.T) {
	cfg := config.Config{
		MultimodalDelegationImageModel: "shared-media-model",
		MultimodalDelegationAudioModel: "audio-model",
		MultimodalDelegationVideoModel: "shared-media-model",
		MultimodalDelegationModalities: "image,audio,video",
	}
	groups := selectMultimodalDelegationGroups(cfg, &channel.ResolvedRoute{
		PlatformModelName:     "text-model",
		ModelCapabilitiesJSON: `{}`,
	}, []AttachmentInput{
		{FileID: "image-1", FileName: "screen.png", DetectedMIME: "image/png", FileSize: 10, Current: true},
		{FileID: "audio-1", FileName: "speech.mp3", DetectedMIME: "audio/mpeg", FileSize: 20, Current: true},
		{FileID: "video-1", FileName: "clip.mp4", DetectedMIME: "video/mp4", FileSize: 30, Current: true},
		{FileID: "history-1", FileName: "old.png", DetectedMIME: "image/png", Current: false},
	})
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2: %#v", len(groups), groups)
	}
	if groups[0].Model != "shared-media-model" || len(groups[0].Attachments) != 2 {
		t.Fatalf("unexpected shared media group: %#v", groups[0])
	}
	if groups[0].AuditFiles[0].Modality != "image" || groups[0].AuditFiles[1].Modality != "video" {
		t.Fatalf("unexpected shared media modalities: %#v", groups[0].AuditFiles)
	}
	if groups[1].Model != "audio-model" || len(groups[1].Attachments) != 1 || groups[1].AuditFiles[0].Modality != "audio" {
		t.Fatalf("unexpected audio group: %#v", groups[1])
	}
}

func TestSelectMultimodalDelegationGroupsSkipsSupportedAndKeepsMissingConfig(t *testing.T) {
	cfg := config.Config{
		MultimodalDelegationImageModel: "image-model",
		MultimodalDelegationModalities: "image,audio,video",
	}
	groups := selectMultimodalDelegationGroups(cfg, &channel.ResolvedRoute{
		PlatformModelName:     "vision-model",
		ModelCapabilitiesJSON: `{"vision":true}`,
	}, []AttachmentInput{
		{FileID: "image-1", DetectedMIME: "image/png", Current: true},
		{FileID: "audio-1", DetectedMIME: "audio/wav", Current: true},
	})
	if len(groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1: %#v", len(groups), groups)
	}
	if groups[0].Model != "" || len(groups[0].AuditFiles) != 1 || groups[0].AuditFiles[0].Modality != "audio" {
		t.Fatalf("expected an explicit unconfigured audio group, got %#v", groups[0])
	}
}

func TestBindMultimodalAnalyzerDisclosesCurrentUnsupportedAttachmentWithoutConfiguredModel(t *testing.T) {
	cfg := config.Config{
		MultimodalDelegationEnabled:    true,
		MultimodalDelegationModalities: "image,audio,video",
	}
	route := &channel.ResolvedRoute{
		PlatformModelName:     "text-model",
		ModelCapabilitiesJSON: `{}`,
	}
	runtime := selectedToolRuntime{}
	if !runtime.bindMultimodalAnalyzer(cfg, route, []AttachmentInput{{
		FileID:       "audio-1",
		FileName:     "meeting.mp3",
		DetectedMIME: "audio/mpeg",
		Current:      true,
	}}) {
		t.Fatal("expected unsupported current audio to be owned by the system multimodal tool")
	}

	visible := runtime.visibleRuntime()
	if len(visible.definitions) != 1 || visible.definitions[0].Name != systemMultimodalAnalyzeToolName {
		t.Fatalf("unexpected visible definitions: %#v", visible.definitions)
	}
	guidance := visible.multimodalAnalyzerGuidance()
	for _, required := range []string{
		"Existing analysis is reference only",
		"write the prompt yourself",
		"file_id=audio-1",
		"scope=current",
		"modality=audio",
		"name=meeting.mp3",
	} {
		if !strings.Contains(guidance, required) {
			t.Fatalf("guidance missing %q: %s", required, guidance)
		}
	}
}

func TestSelectedMultimodalAnalyzerRejectsInvalidAttachmentSelections(t *testing.T) {
	analyzer := &selectedMultimodalAnalyzer{
		attachments: map[string]AttachmentInput{
			"image-1": {FileID: "image-1", Current: true},
		},
	}
	tests := []struct {
		name    string
		fileIDs []string
		wantErr string
	}{
		{name: "empty list", wantErr: "at least one"},
		{name: "empty value", fileIDs: []string{" "}, wantErr: "empty values"},
		{name: "duplicate", fileIDs: []string{"image-1", " image-1 "}, wantErr: "duplicate"},
		{name: "not current", fileIDs: []string{"history-1"}, wantErr: "not an authorized conversation attachment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := analyzer.resolveAttachments(tt.fileIDs)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("resolveAttachments() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSelectMultimodalDelegationGroupsAllowsHistoricalImagesOnlyWhenRequested(t *testing.T) {
	cfg := config.Config{
		MultimodalDelegationImageModel: "image-model",
		MultimodalDelegationModalities: "image",
	}
	route := &channel.ResolvedRoute{PlatformModelName: "text-model", ModelCapabilitiesJSON: `{}`}
	attachments := []AttachmentInput{
		{FileID: "current-image", DetectedMIME: "image/png", Current: true},
		{FileID: "history-image", DetectedMIME: "image/png", Current: false},
	}
	if groups := selectMultimodalDelegationGroups(cfg, route, attachments); len(groups) != 1 || len(groups[0].Attachments) != 1 || groups[0].Attachments[0].FileID != "current-image" {
		t.Fatalf("automatic delegation must skip historical images, got %#v", groups)
	}
	groups := selectMultimodalDelegationGroupsWithHistory(cfg, route, attachments, true)
	if len(groups) != 1 || len(groups[0].Attachments) != 2 {
		t.Fatalf("explicit historical delegation should include both images, got %#v", groups)
	}
	if !groups[0].AuditFiles[1].Historical {
		t.Fatalf("expected historical audit marker, got %#v", groups[0].AuditFiles)
	}
}

func TestSelectedMultimodalAnalyzerTracksCurrentAndHistoricalAttachments(t *testing.T) {
	runtime := selectedToolRuntime{}
	cfg := config.Config{MultimodalDelegationEnabled: true, MultimodalDelegationModalities: "image", MultimodalDelegationImageModel: "image-model"}
	route := &channel.ResolvedRoute{PlatformModelName: "text-model", ModelCapabilitiesJSON: `{}`}
	if !runtime.bindMultimodalAnalyzerWithHistory(cfg, route, []AttachmentInput{{FileID: "history-image", Kind: "image", MimeType: "image/png", Current: false}}, true) {
		t.Fatal("expected historical image analyzer binding")
	}
	if runtime.multimodalAnalyzerHandlesCurrentAttachments() {
		t.Fatal("historical-only analyzer must not claim current attachments")
	}
	if !runtime.bindMultimodalAnalyzerWithHistory(cfg, route, []AttachmentInput{{FileID: "current-image", Kind: "image", MimeType: "image/png", Current: true}}, true) {
		t.Fatal("expected current image analyzer binding")
	}
	if !runtime.multimodalAnalyzerHandlesCurrentAttachments() {
		t.Fatal("current analyzer must claim current attachments")
	}
}
func TestSelectedMultimodalAnalyzerToolSchemaRequiresFileIDsAndModelPrompt(t *testing.T) {
	definition := (&selectedMultimodalAnalyzer{}).toolDefinition()
	schema := string(definition.InputSchema)
	for _, required := range []string{
		`"required":["file_ids","prompt"]`,
		`"minItems":1`,
		`"uniqueItems":true`,
		`"minLength":1`,
		"Your precise analysis request",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("tool schema missing %q: %s", required, schema)
		}
	}
	if !strings.Contains(definition.Description, "may already have an analysis") {
		t.Fatalf("tool description must disclose that existing analysis may be reused: %s", definition.Description)
	}
}

func TestMultimodalDelegationOptionsDoesNotMutateBase(t *testing.T) {
	base := map[string]interface{}{"temperature": 0.2}
	options := multimodalDelegationOptions(base)
	if _, ok := base["response_format"]; ok {
		t.Fatal("base options were mutated")
	}
	format, ok := options["response_format"].(map[string]interface{})
	if !ok || format["type"] != "json_schema" {
		t.Fatalf("unexpected response format: %#v", options["response_format"])
	}
}

func TestBuildMultimodalDelegationPromptRequiresDirectMediaAccess(t *testing.T) {
	prompt := buildMultimodalDelegationPrompt("Describe it", []multimodalDelegationAuditFile{{
		FileID:   "file-1",
		FileName: "clip.mp4",
		Modality: "video",
	}})
	for _, required := range []string{
		"Directly inspect every supplied media attachment",
		`"status":"unsupported"`,
		"clip.mp4",
		"Describe it",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing %q: %s", required, prompt)
		}
	}
}
