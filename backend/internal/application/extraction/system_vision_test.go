package extraction

import (
	"context"
	"testing"

	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
)

func TestExtractStoredImageWithSystemVision(t *testing.T) {
	wantFile := domainconversation.FileObject{
		ID: 7, FileID: "file-one", UserID: 11, FileName: "screen.png",
		DetectedMIME: "image/png", FileCategory: "image", StoragePath: "users/11/screen.png",
	}
	service := &Service{}
	service.SetVisionAnalyzer(func(_ context.Context, got domainconversation.FileObject) (string, error) {
		if got != wantFile {
			t.Fatalf("vision analyzer file = %#v, want %#v", got, wantFile)
		}
		return "Visible title and a blue button.", nil
	})

	result, err := service.ExtractStoredFile(context.Background(), ExtractInput{
		File: wantFile, OCREngine: OCREngineSystemVision, ImageOCREnabled: true,
	})
	if err != nil {
		t.Fatalf("ExtractStoredFile() error = %v", err)
	}
	if result.Text != "Visible title and a blue button." || result.Engine != "ocr_system_vision" || !result.OCRUsed {
		t.Fatalf("ExtractStoredFile() result = %#v", result)
	}
}
