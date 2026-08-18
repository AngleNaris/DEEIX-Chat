package ocr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/security"
)

func TestTraditionalOCRPayloadPagesCount(t *testing.T) {
	var payload traditionalOCRPayload
	if err := json.Unmarshal([]byte(`{"pages":3,"text":"hello"}`), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if got := payload.PageCount(); got != 3 {
		t.Fatalf("PageCount() = %d, want 3", got)
	}
	if got := payload.ExtractedText(); got != "hello" {
		t.Fatalf("ExtractedText() = %q, want hello", got)
	}
}

func TestTraditionalOCRPayloadPagesItems(t *testing.T) {
	raw := `{
		"pages": [
			{"page": 1, "text": "first"},
			{"page_number": 2, "markdown": "second"}
		]
	}`
	var payload traditionalOCRPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if got := payload.PageCount(); got != 2 {
		t.Fatalf("PageCount() = %d, want 2", got)
	}

	pageTexts := payload.ExtractedPageTexts()
	if len(pageTexts) != 2 {
		t.Fatalf("len(ExtractedPageTexts()) = %d, want 2", len(pageTexts))
	}
	if pageTexts[0].PageNumber != 1 || pageTexts[0].Text != "first" {
		t.Fatalf("pageTexts[0] = %+v, want page 1 first", pageTexts[0])
	}
	if pageTexts[1].PageNumber != 2 || pageTexts[1].Text != "second" {
		t.Fatalf("pageTexts[1] = %+v, want page 2 second", pageTexts[1])
	}
}

func TestLLMOCRImageRequestUsesConfiguredModelAndAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("request path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ocr-key" {
			t.Fatalf("Authorization = %q, want Bearer ocr-key", got)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got := payload["model"]; got != "vision-ocr-model" {
			t.Fatalf("model = %#v, want vision-ocr-model", got)
		}
		messages, ok := payload["messages"].([]interface{})
		if !ok || len(messages) != 2 {
			t.Fatalf("messages = %#v, want system and user messages", payload["messages"])
		}
		userMessage, _ := messages[1].(map[string]interface{})
		content, ok := userMessage["content"].([]interface{})
		if !ok || len(content) != 2 {
			t.Fatalf("user content = %#v, want text and image", userMessage["content"])
		}
		imagePart, _ := content[1].(map[string]interface{})
		imageURL, _ := imagePart["image_url"].(map[string]interface{})
		url, _ := imageURL["url"].(string)
		if imagePart["type"] != "image_url" || !strings.HasPrefix(url, "data:image/png;base64,") {
			t.Fatalf("image part = %#v, want PNG data URL", imagePart)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"识别结果"}}]}`))
	}))
	defer server.Close()

	imagePath := filepath.Join(t.TempDir(), "sample.png")
	if err := os.WriteFile(imagePath, []byte("png-data"), 0o600); err != nil {
		t.Fatalf("write image fixture: %v", err)
	}
	client := NewLLM(ClientConfig{
		BaseURL:        server.URL,
		AuthToken:      "ocr-key",
		Model:          "vision-ocr-model",
		TimeoutSeconds: 60,
		OutboundPolicy: security.OutboundPolicy{},
	})
	response, err := client.ExtractText(context.Background(), Request{
		AbsolutePath: imagePath,
		FileName:     "sample.png",
		MimeType:     "image/png",
	})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if response.Text != "识别结果" {
		t.Fatalf("response text = %q, want 识别结果", response.Text)
	}
}

func TestLLMOCRMapsHTTPFailuresAndEmptyContent(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{name: "unauthorized", statusCode: http.StatusUnauthorized, body: `{"error":{"message":"bad key"}}`, want: "ocr_unauthorized"},
		{name: "forbidden", statusCode: http.StatusForbidden, body: `{"error":{"message":"denied"}}`, want: "ocr_forbidden"},
		{name: "unprocessable", statusCode: http.StatusUnprocessableEntity, body: `{"error":{"message":"model cannot read image"}}`, want: "ocr_unprocessable: model cannot read image"},
		{name: "server error", statusCode: http.StatusInternalServerError, body: `{"error":{"message":"upstream failed"}}`, want: "ocr_http_500: upstream failed"},
		{name: "empty content", statusCode: http.StatusOK, body: `{"choices":[{"message":{"role":"assistant","content":"   "}}]}`, want: errOCREmptyContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			imagePath := filepath.Join(t.TempDir(), "sample.png")
			if err := os.WriteFile(imagePath, []byte("png-data"), 0o600); err != nil {
				t.Fatalf("write image fixture: %v", err)
			}
			client := NewLLM(ClientConfig{
				BaseURL:        server.URL,
				Model:          "vision-ocr-model",
				TimeoutSeconds: 60,
				OutboundPolicy: security.OutboundPolicy{},
			})
			_, err := client.ExtractText(context.Background(), Request{
				AbsolutePath: imagePath,
				FileName:     "sample.png",
				MimeType:     "image/png",
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ExtractText error = %v, want substring %q", err, tt.want)
			}
		})
	}
}
