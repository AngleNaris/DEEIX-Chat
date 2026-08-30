package filecontent

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appupload "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/upload"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/gin-gonic/gin"
)

func TestSafeContentTypeDowngradesActiveContent(t *testing.T) {
	tests := []struct {
		contentType string
		want        string
	}{
		{contentType: "text/html; charset=utf-8", want: "text/plain; charset=utf-8"},
		{contentType: "application/javascript", want: "text/plain; charset=utf-8"},
		{contentType: "image/svg+xml", want: "text/plain; charset=utf-8"},
		{contentType: "application/pdf", want: "application/pdf"},
	}
	for _, tt := range tests {
		if got := safeContentType(tt.contentType); got != tt.want {
			t.Fatalf("safeContentType(%q) = %q, want %q", tt.contentType, got, tt.want)
		}
	}
}

func TestBuildContentDispositionDefaultsToAttachment(t *testing.T) {
	got := buildContentDisposition("report.html", false)
	want := `attachment; filename="report.html"; filename*=UTF-8''report.html`
	if got != want {
		t.Fatalf("unexpected disposition: got %q want %q", got, want)
	}
}

func TestWritePublicContentDisablesCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	result := &appupload.FileContentResult{
		File:        domainconversation.FileObject{FileName: "shared.txt"},
		Reader:      io.NopCloser(strings.NewReader("shared")),
		ContentType: "text/plain; charset=utf-8",
		SizeBytes:   6,
		ModTime:     time.Now(),
	}
	if err := Write(c, result, true); err != nil {
		t.Fatal(err)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}
