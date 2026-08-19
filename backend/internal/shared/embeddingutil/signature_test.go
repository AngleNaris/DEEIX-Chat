package embeddingutil

import "testing"

func TestModelSignatureNormalizesModelWhitespace(t *testing.T) {
	left := ModelSignature(" model-name ", 4096)
	right := ModelSignature("model-name", 4096)
	if left != right || left == ModelSignature("model-name", 1536) {
		t.Fatalf("unexpected model signatures: left=%q right=%q", left, right)
	}
}
