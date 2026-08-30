package artifact

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	appartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/artifact"
)

func TestArtifactRenderResponseIsSandboxedAndSingleUse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := appartifact.NewService(nil)
	view, err := svc.CreateRenderToken(1, "<!doctype html><script>document.body.textContent='ok'</script>")
	if err != nil {
		t.Fatalf("CreateRenderToken() error = %v", err)
	}
	token := strings.TrimPrefix(view.RenderURL, "/api/v1/artifact-renders/")
	router := gin.New()
	router.GET("/artifact-renders/:render_token", NewHandler(svc).GetArtifactRender)

	request := httptest.NewRequest(http.MethodGet, "/artifact-renders/"+token, nil)
	responseRecorder := httptest.NewRecorder()
	router.ServeHTTP(responseRecorder, request)
	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", responseRecorder.Code, responseRecorder.Body.String())
	}
	for header, expected := range map[string]string{
		"Cache-Control":          "no-store",
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := responseRecorder.Header().Get(header); got != expected {
			t.Fatalf("%s = %q, want %q", header, got, expected)
		}
	}
	csp := responseRecorder.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"sandbox allow-scripts", "connect-src 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, directive) {
			t.Fatalf("CSP missing %q: %q", directive, csp)
		}
	}

	secondRecorder := httptest.NewRecorder()
	router.ServeHTTP(secondRecorder, request)
	if secondRecorder.Code != http.StatusNotFound {
		t.Fatalf("second status = %d, want %d", secondRecorder.Code, http.StatusNotFound)
	}
}
