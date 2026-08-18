package conversation

import "testing"

func TestModelSupportsMediaUsesExplicitCapabilities(t *testing.T) {
	capabilities := `{"vision":true,"audio":true,"video":false}`
	if !modelSupportsMedia("custom-model", capabilities, "image") {
		t.Fatal("expected vision capability")
	}
	if !modelSupportsMedia("custom-model", capabilities, "audio") {
		t.Fatal("expected audio capability")
	}
	if modelSupportsMedia("custom-model", capabilities, "video") {
		t.Fatal("did not expect video capability")
	}
}

func TestAttachmentMediaModality(t *testing.T) {
	cases := []struct {
		attachment AttachmentInput
		want       string
	}{
		{AttachmentInput{Kind: "image", DetectedMIME: "image/png"}, "image"},
		{AttachmentInput{Kind: "file", DetectedMIME: "audio/mpeg"}, "audio"},
		{AttachmentInput{Kind: "video", DetectedMIME: "video/mp4"}, "video"},
	}
	for _, item := range cases {
		if got := attachmentMediaModality(item.attachment); got != item.want {
			t.Fatalf("attachmentMediaModality() = %q, want %q", got, item.want)
		}
	}
}
