package embedding

import (
	"context"
	"math"
	"testing"

	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	infraembedding "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/embedding"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/security"
)

func TestPostProcessEmbeddingsSupportsMaximumConfiguredDimensions(t *testing.T) {
	input := make([]float32, 4096)
	input[0], input[len(input)-1] = 3, 4

	processed := postProcessEmbeddings([][]float32{input}, 4096, true)
	if len(processed) != 1 {
		t.Fatalf("processed vector count = %d, want 1", len(processed))
	}
	if len(processed[0]) != 4096 {
		t.Fatalf("processed dimensions = %d, want 4096", len(processed[0]))
	}
	if math.Abs(float64(processed[0][0]-0.6)) > 1e-6 || math.Abs(float64(processed[0][4095]-0.8)) > 1e-6 {
		t.Fatalf("unexpected normalized boundary values: first=%v last=%v", processed[0][0], processed[0][4095])
	}
}

func TestShouldTriggerIncludesOCRImages(t *testing.T) {
	service := NewService(config.Config{
		RAGEnabled:             true,
		EmbeddingEnabled:       true,
		EmbedTriggerOnUpload:   true,
		RAGModel:               "text-embedding-test",
		EmbeddingHost:          "http://127.0.0.1:8081",
		ExtractImageOCREnabled: true,
	}, nil, nil, nil, nil)

	fileObj := domainconversation.FileObject{
		FileID:       "file_1",
		FileName:     "photo.png",
		MimeType:     "image/png",
		FileCategory: "image",
		StoragePath:  "uploads/photo.png",
		Status:       "active",
	}
	if !service.ShouldTrigger(fileObj) {
		t.Fatal("expected OCR image to trigger embedding")
	}
}

func TestShouldTriggerSkipsImagesWhenOCRDisabled(t *testing.T) {
	service := NewService(config.Config{
		RAGEnabled:             true,
		EmbeddingEnabled:       true,
		EmbedTriggerOnUpload:   true,
		RAGModel:               "text-embedding-test",
		EmbeddingHost:          "http://127.0.0.1:8081",
		ExtractImageOCREnabled: false,
	}, nil, nil, nil, nil)

	fileObj := domainconversation.FileObject{
		FileID:       "file_1",
		FileName:     "photo.png",
		MimeType:     "image/png",
		FileCategory: "image",
		StoragePath:  "uploads/photo.png",
		Status:       "active",
	}
	if service.ShouldTrigger(fileObj) {
		t.Fatal("expected image embedding to stay disabled when OCR is disabled")
	}
}

func TestShouldTriggerSkipsVideos(t *testing.T) {
	service := NewService(config.Config{
		RAGEnabled:           true,
		EmbeddingEnabled:     true,
		EmbedTriggerOnUpload: true,
		RAGModel:             "text-embedding-test",
		EmbeddingHost:        "http://127.0.0.1:8081",
	}, nil, nil, nil, nil)

	fileObj := domainconversation.FileObject{
		FileID:       "file_video",
		FileName:     "clip.mp4",
		MimeType:     "video/mp4",
		FileCategory: "video",
		StoragePath:  "uploads/clip.mp4",
		Status:       "active",
	}
	if service.ShouldTrigger(fileObj) {
		t.Fatal("expected videos to skip embedding")
	}
}

func TestShouldTriggerDoesNotRequireRAGEnabled(t *testing.T) {
	service := NewService(config.Config{
		RAGEnabled:           false,
		EmbeddingEnabled:     true,
		EmbedTriggerOnUpload: true,
		RAGModel:             "text-embedding-test",
		EmbeddingHost:        "http://127.0.0.1:8081",
	}, nil, nil, nil, nil)

	fileObj := domainconversation.FileObject{
		FileID:      "file_1",
		FileName:    "doc.txt",
		MimeType:    "text/plain",
		StoragePath: "uploads/doc.txt",
		Status:      "active",
	}
	if !service.ShouldTrigger(fileObj) {
		t.Fatal("expected embedding trigger to ignore chat RAG switch")
	}
}

func TestShouldTriggerIncludesPresentations(t *testing.T) {
	service := NewService(config.Config{
		EmbeddingEnabled:     true,
		EmbedTriggerOnUpload: true,
		RAGModel:             "text-embedding-test",
		EmbeddingHost:        "http://127.0.0.1:8081",
	}, nil, nil, nil, nil)

	cases := []domainconversation.FileObject{
		{
			FileID:       "file_pptx",
			FileName:     "deck.pptx",
			MimeType:     "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			FileCategory: "presentation",
			StoragePath:  "uploads/deck.pptx",
			Status:       "active",
		},
		{
			FileID:       "file_ppt",
			FileName:     "legacy.ppt",
			MimeType:     "application/vnd.ms-powerpoint",
			FileCategory: "presentation",
			StoragePath:  "uploads/legacy.ppt",
			Status:       "active",
		},
	}
	for _, fileObj := range cases {
		if !service.ShouldTrigger(fileObj) {
			t.Fatalf("expected %s to trigger embedding", fileObj.FileName)
		}
	}
}

func TestIndexingAvailableDoesNotRequireRAGEnabled(t *testing.T) {
	repo := &reindexRepo{vectorAvailable: true}
	service := NewService(config.Config{
		RAGEnabled:       false,
		EmbeddingEnabled: true,
		RAGModel:         "text-embedding-test",
		EmbeddingHost:    "http://127.0.0.1:8081",
	}, repo, nil, infraembedding.New(security.OutboundPolicy{}), nil)

	available, reason := service.IndexingAvailable(context.Background())
	if !available {
		t.Fatalf("expected indexing to ignore chat RAG switch, got %s", reason)
	}
}

func TestReindexStaleFilesDoesNotRequireRAGEnabled(t *testing.T) {
	repo := &reindexRepo{vectorAvailable: true}
	service := NewService(config.Config{
		RAGEnabled:       false,
		EmbeddingEnabled: true,
		RAGModel:         "text-embedding-test",
		EmbeddingHost:    "http://127.0.0.1:8081",
	}, repo, nil, infraembedding.New(security.OutboundPolicy{}), nil)

	submitted, err := service.ReindexStaleFiles(context.Background())
	if err != nil {
		t.Fatalf("expected reindex to ignore chat RAG switch, got %v", err)
	}
	if submitted != 0 {
		t.Fatalf("expected no submitted files, got %d", submitted)
	}
	if repo.listCalls != 1 {
		t.Fatalf("expected reindex list query to run once, got %d", repo.listCalls)
	}
}

func TestCanEmbedFileDoesNotRequireAutoTrigger(t *testing.T) {
	cfg := config.Config{
		EmbeddingEnabled:     true,
		EmbedTriggerOnUpload: false,
		RAGModel:             "text-embedding-test",
		EmbeddingHost:        "http://127.0.0.1:8081",
	}
	fileObj := domainconversation.FileObject{
		FileID:      "file_pptx",
		FileName:    "deck.pptx",
		MimeType:    "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		StoragePath: "uploads/deck.pptx",
		Status:      "active",
	}

	service := NewService(cfg, nil, nil, nil, nil)
	if service.ShouldTrigger(fileObj) {
		t.Fatal("auto-trigger should honor EmbedTriggerOnUpload=false")
	}
	if !canEmbedFile(cfg, fileObj) {
		t.Fatal("manual reindex eligibility should not depend on EmbedTriggerOnUpload")
	}
}

func TestReindexStaleFilesSkipsUnsupportedCandidates(t *testing.T) {
	repo := &reindexRepo{
		vectorAvailable: true,
		files: []domainconversation.FileObject{
			{
				ID:          2,
				UserID:      1,
				FileID:      "file_bin",
				FileName:    "archive.bin",
				MimeType:    "application/octet-stream",
				StoragePath: "uploads/archive.bin",
				Status:      "active",
			},
		},
	}
	service := NewService(config.Config{
		EmbeddingEnabled: true,
		RAGModel:         "text-embedding-test",
		EmbeddingHost:    "http://127.0.0.1:8081",
	}, repo, nil, infraembedding.New(security.OutboundPolicy{}), nil)

	submitted, err := service.ReindexStaleFiles(context.Background())
	if err != nil {
		t.Fatalf("expected reindex to succeed, got %v", err)
	}
	if submitted != 0 {
		t.Fatalf("expected no unsupported files submitted, got %d", submitted)
	}
}

func TestReindexStaleFilesAdvancesCursorForUnsupportedCandidates(t *testing.T) {
	files := make([]domainconversation.FileObject, 0, 101)
	for i := 1; i <= 101; i++ {
		files = append(files, domainconversation.FileObject{
			ID:          uint(i),
			UserID:      1,
			FileID:      "file_bin",
			FileName:    "archive.bin",
			MimeType:    "application/octet-stream",
			StoragePath: "uploads/archive.bin",
			Status:      "active",
		})
	}
	repo := &reindexRepo{vectorAvailable: true, files: files}
	service := NewService(config.Config{
		EmbeddingEnabled: true,
		RAGModel:         "text-embedding-test",
		EmbeddingHost:    "http://127.0.0.1:8081",
	}, repo, nil, infraembedding.New(security.OutboundPolicy{}), nil)

	submitted, err := service.ReindexStaleFiles(context.Background())
	if err != nil {
		t.Fatalf("expected reindex to succeed, got %v", err)
	}
	if submitted != 0 {
		t.Fatalf("expected unsupported files to be skipped, got %d", submitted)
	}
	if len(repo.afterIDs) != 2 || repo.afterIDs[0] != 0 || repo.afterIDs[1] != 100 {
		t.Fatalf("expected cursor pagination after ids [0 100], got %#v", repo.afterIDs)
	}
}

func TestProcessFileDoesNotRequireRAGEnabled(t *testing.T) {
	repo := &reindexRepo{vectorAvailable: true}
	service := NewService(config.Config{
		RAGEnabled:       false,
		EmbeddingEnabled: true,
		RAGModel:         "text-embedding-test",
		EmbeddingHost:    "http://127.0.0.1:8081",
	}, repo, nil, infraembedding.New(security.OutboundPolicy{}), nil)

	_ = service.ProcessFile(context.Background(), domainconversation.FileObject{
		ID:          1,
		UserID:      1,
		FileID:      "file_1",
		FileName:    "doc.txt",
		MimeType:    "text/plain",
		StoragePath: "uploads/doc.txt",
		Status:      "active",
	})

	if repo.updateStatusCalls == 0 {
		t.Fatal("expected ProcessFile to start embedding even when chat RAG is disabled")
	}
}

func TestProcessFileIncludesOCRImages(t *testing.T) {
	repo := &reindexRepo{vectorAvailable: true}
	service := NewService(config.Config{
		RAGEnabled:             true,
		EmbeddingEnabled:       true,
		RAGModel:               "text-embedding-test",
		EmbeddingHost:          "http://127.0.0.1:8081",
		ExtractImageOCREnabled: true,
	}, repo, nil, infraembedding.New(security.OutboundPolicy{}), nil)

	_ = service.ProcessFile(context.Background(), domainconversation.FileObject{
		ID:           1,
		UserID:       1,
		FileID:       "file_image",
		FileName:     "photo.png",
		MimeType:     "image/png",
		FileCategory: "image",
		StoragePath:  "uploads/photo.png",
		Status:       "active",
	})

	if repo.updateStatusCalls == 0 {
		t.Fatal("expected ProcessFile to allow OCR image embedding")
	}
}

func TestProcessFileSkipsVideos(t *testing.T) {
	repo := &reindexRepo{vectorAvailable: true}
	service := NewService(config.Config{
		RAGEnabled:       true,
		EmbeddingEnabled: true,
		RAGModel:         "text-embedding-test",
		EmbeddingHost:    "http://127.0.0.1:8081",
	}, repo, nil, infraembedding.New(security.OutboundPolicy{}), nil)

	_ = service.ProcessFile(context.Background(), domainconversation.FileObject{
		ID:           1,
		UserID:       1,
		FileID:       "file_video",
		FileName:     "clip.mp4",
		MimeType:     "video/mp4",
		FileCategory: "video",
		StoragePath:  "uploads/clip.mp4",
		Status:       "active",
	})

	if repo.updateStatusCalls != 0 {
		t.Fatal("expected ProcessFile to skip video embedding")
	}
}

func TestCompleteFileEmbeddingKeepsChangedConfigurationStale(t *testing.T) {
	repo := &reindexRepo{}
	runtime := config.NewRuntime(config.Config{RAGModel: "new-model", EmbeddingOutputDimensions: 4096})
	service := NewServiceWithRuntime(runtime, repo, nil, nil, nil)

	err := service.completeFileEmbedding(
		context.Background(),
		domainconversation.FileObject{UserID: 1, FileID: "file_1"},
		ComputeModelSignature("old-model", 1536),
	)
	if err != nil {
		t.Fatalf("completeFileEmbedding() error = %v", err)
	}
	if len(repo.statusHistory) != 1 || repo.statusHistory[0] != "stale" {
		t.Fatalf("status history = %#v, want [stale]", repo.statusHistory)
	}
}

func TestCompleteFileEmbeddingClosesReadyPublicationRace(t *testing.T) {
	repo := &reindexRepo{}
	initial := config.Config{RAGModel: "initial-model", EmbeddingOutputDimensions: 1536}
	runtime := config.NewRuntime(initial)
	service := NewServiceWithRuntime(runtime, repo, nil, nil, nil)
	repo.onStatus = func(status string) {
		if status == "ready" {
			runtime.Store(config.Config{RAGModel: "new-model", EmbeddingOutputDimensions: 4096})
		}
	}

	err := service.completeFileEmbedding(
		context.Background(),
		domainconversation.FileObject{UserID: 1, FileID: "file_1"},
		ComputeModelSignature(initial.RAGModel, initial.EmbeddingOutputDimensions),
	)
	if err != nil {
		t.Fatalf("completeFileEmbedding() error = %v", err)
	}
	if len(repo.statusHistory) != 2 || repo.statusHistory[0] != "ready" || repo.statusHistory[1] != "stale" {
		t.Fatalf("status history = %#v, want [ready stale]", repo.statusHistory)
	}
}

type reindexRepo struct {
	vectorAvailable   bool
	files             []domainconversation.FileObject
	afterIDs          []uint
	listCalls         int
	updateStatusCalls int
	statusHistory     []string
	onStatus          func(status string)
}

func (r *reindexRepo) VectorStoreAvailable(context.Context) (bool, error) {
	return r.vectorAvailable, nil
}

func (r *reindexRepo) GetActiveFileObjectByID(context.Context, uint, string) (*domainconversation.FileObject, error) {
	return nil, nil
}

func (r *reindexRepo) GetFileObjectProcessingByObjectID(context.Context, uint) (*domainconversation.FileObjectProcessing, error) {
	return nil, nil
}

func (r *reindexRepo) UpdateFileObjectEmbedStatus(_ context.Context, _ uint, _ string, _ string, status string, _ string) error {
	r.updateStatusCalls++
	r.statusHistory = append(r.statusHistory, status)
	if r.onStatus != nil {
		r.onStatus(status)
	}
	return nil
}

func (r *reindexRepo) UpdateFileObjectChunkCount(context.Context, uint, string, int) error {
	return nil
}

func (r *reindexRepo) ReplaceFileChunks(context.Context, uint, string, []domainconversation.FileChunk, [][]float32) error {
	return nil
}

func (r *reindexRepo) MarkAllEmbeddedFilesStale(context.Context) (int64, error) {
	return 0, nil
}

func (r *reindexRepo) CountFilesByEmbedStatus(context.Context, string) (int64, error) {
	return 0, nil
}

func (r *reindexRepo) ListFilesForReindex(_ context.Context, limit int, afterID uint) ([]domainconversation.FileObject, error) {
	r.listCalls++
	r.afterIDs = append(r.afterIDs, afterID)
	results := make([]domainconversation.FileObject, 0, limit)
	for _, file := range r.files {
		if file.ID <= afterID {
			continue
		}
		results = append(results, file)
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}
