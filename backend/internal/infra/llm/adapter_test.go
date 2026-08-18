package llm

import (
	"testing"
)

func TestSupportsStreamingAdapter(t *testing.T) {
	if !SupportsStreamingAdapter(AdapterOpenAIImageGenerations) {
		t.Fatalf("expected image generations adapter to support upstream streaming")
	}
	if !SupportsStreamingAdapter(AdapterOpenAIImageEdits) {
		t.Fatalf("expected image edits adapter to support upstream streaming")
	}
	if !SupportsStreamingAdapter(AdapterOpenAIResponses) {
		t.Fatalf("expected responses adapter to support streaming")
	}
}

func TestSupportsMediaInputAdapter(t *testing.T) {
	tests := []struct {
		name     string
		adapter  string
		modality string
		want     bool
	}{
		{name: "responses image", adapter: AdapterOpenAIResponses, modality: "image", want: true},
		{name: "responses audio", adapter: AdapterOpenAIResponses, modality: "audio", want: false},
		{name: "anthropic image", adapter: AdapterAnthropicMessages, modality: "image", want: true},
		{name: "anthropic video", adapter: AdapterAnthropicMessages, modality: "video", want: false},
		{name: "chat audio", adapter: AdapterOpenAIChatCompletions, modality: "audio", want: true},
		{name: "openrouter chat video", adapter: AdapterOpenRouterChat, modality: "video", want: true},
		{name: "gemini video", adapter: AdapterGoogleGenerateContent, modality: "video", want: true},
		{name: "gemini interactions audio", adapter: AdapterGeminiInteractions, modality: "audio", want: true},
		{name: "media generation adapter", adapter: AdapterXAIVideo, modality: "video", want: false},
		{name: "unknown modality", adapter: AdapterOpenAIChatCompletions, modality: "document", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SupportsMediaInputAdapter(tt.adapter, tt.modality); got != tt.want {
				t.Fatalf("SupportsMediaInputAdapter(%q, %q) = %v, want %v", tt.adapter, tt.modality, got, tt.want)
			}
		})
	}
}

func TestSupportsImageGenerationStream(t *testing.T) {
	if !SupportsImageGenerationStream(AdapterOpenAIImageGenerations, "gpt-image-1") {
		t.Fatalf("expected gpt-image models to support image generation streaming")
	}
	if !SupportsImageGenerationStream(AdapterOpenAIImageGenerations, "gpt-image-2") {
		t.Fatalf("expected gpt-image-2 to support image generation streaming")
	}
	if !SupportsStreamingAdapter(AdapterGoogleImageGeneration) {
		t.Fatalf("expected google image generation adapter to support upstream streaming")
	}
	if !SupportsImageGenerationStream(AdapterGoogleImageGeneration, "gemini-3-pro-image") {
		t.Fatalf("expected google image generation adapter to support image generation streaming")
	}
	if !SupportsImageGenerationStream(AdapterGeminiInteractions, "gemini-3.5-flash") {
		t.Fatalf("expected Gemini Interactions adapter to support image generation streaming")
	}
	if SupportsStreamingAdapter(AdapterXAIImage) {
		t.Fatalf("expected xAI image adapter to use non-streaming media flow")
	}
	if SupportsStreamingAdapter(AdapterXAIImageEdits) {
		t.Fatalf("expected xAI image edits adapter to use non-streaming media flow")
	}
	if SupportsImageGenerationStream(AdapterOpenAIImageGenerations, "dall-e-3") {
		t.Fatalf("expected DALL-E models to remain non-streaming")
	}
	if SupportsImageGenerationStream(AdapterOpenAIResponses, "gpt-image-1") {
		t.Fatalf("expected non-image protocol to remain non-streaming for image generation")
	}
	if !SupportsImageGenerationStream(AdapterOpenAIImageEdits, "gpt-image-1") {
		t.Fatalf("expected gpt-image edits to support image edit streaming")
	}
	if !SupportsImageGenerationStream(AdapterOpenAIImageEdits, "gpt-image-2") {
		t.Fatalf("expected gpt-image-2 edits to support image edit streaming")
	}
}

func TestImageAdapterCapabilities(t *testing.T) {
	if !IsImageGenerationAdapter(AdapterGoogleImageGeneration) {
		t.Fatalf("expected google image protocol to support image generation")
	}
	if !IsImageEditAdapter(AdapterGoogleImageGeneration) {
		t.Fatalf("expected google image protocol to support image editing")
	}
	if !IsImageGenerationAdapter(AdapterXAIImage) {
		t.Fatalf("expected xAI image protocol to support image generation")
	}
	if IsImageEditAdapter(AdapterXAIImage) {
		t.Fatalf("expected xAI image protocol to stay generation-only")
	}
	if !IsImageEditAdapter(AdapterXAIImageEdits) {
		t.Fatalf("expected xAI image edits protocol to support image editing")
	}
}

func TestXAIVideoAdapterCapabilities(t *testing.T) {
	if !IsKnownAdapter(AdapterXAIVideo) || !IsImplementedAdapter(AdapterXAIVideo) {
		t.Fatalf("expected xAI video adapter to be known and implemented")
	}
	if !IsVideoGenerationAdapter(AdapterXAIVideo) {
		t.Fatalf("expected xAI video adapter to support video generation")
	}
	if SupportsStreamingAdapter(AdapterXAIVideo) {
		t.Fatalf("expected xAI video adapter to use asynchronous polling instead of streaming")
	}
	if got := DefaultEndpointForAdapter(AdapterXAIVideo); got != EndpointVideoGenerations {
		t.Fatalf("expected xAI video endpoint, got %q", got)
	}
}
