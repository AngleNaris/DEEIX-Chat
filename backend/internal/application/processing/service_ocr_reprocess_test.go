package processing

import (
	"context"
	"sync"
	"testing"
	"time"

	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

type ocrReprocessRepo struct {
	mu         sync.Mutex
	file       domainconversation.FileObject
	processing domainconversation.FileObjectProcessing
}

func (r *ocrReprocessRepo) GetActiveFileObjectByID(context.Context, uint, string) (*domainconversation.FileObject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.file
	return &item, nil
}

func (r *ocrReprocessRepo) UpdateFileObjectProcessingState(_ context.Context, item *domainconversation.FileObjectProcessing) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.processing = *item
	return nil
}

func (r *ocrReprocessRepo) GetFileObjectProcessingByObjectID(context.Context, uint) (*domainconversation.FileObjectProcessing, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.processing
	return &item, nil
}

func (r *ocrReprocessRepo) CloneFileObjectProcessingState(context.Context, uint, uint, uint) error {
	return nil
}

func (r *ocrReprocessRepo) UpdateFileObjectProcessing(_ context.Context, _ uint, _ string, input repository.UpdateFileObjectProcessingInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if input.ProcessingStatus != nil {
		r.file.ProcessingStatus = *input.ProcessingStatus
	}
	if input.ProcessingReady != nil {
		r.file.ProcessingReady = *input.ProcessingReady
	}
	if input.ExtractStatus != nil {
		r.file.ExtractStatus = *input.ExtractStatus
	}
	return nil
}

func (r *ocrReprocessRepo) CanRemoveExtractStoragePath(context.Context, uint, uint, string) (bool, error) {
	return true, nil
}

func (r *ocrReprocessRepo) ReplaceFileObjectContent(context.Context, uint, string, string, string, int64) (repository.ReplaceFileObjectContentResult, error) {
	return repository.ReplaceFileObjectContentResult{}, nil
}

func (r *ocrReprocessRepo) ClaimFileProcessingQueue(context.Context, uint, string, string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file.ProcessingStatus != "pending" {
		return false, nil
	}
	r.file.ProcessingStatus = "queued"
	return true, nil
}

func (r *ocrReprocessRepo) ReleaseFileProcessingQueueClaim(context.Context, uint, string, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file.ProcessingStatus == "queued" {
		r.file.ProcessingStatus = "pending"
	}
	return nil
}

func (r *ocrReprocessRepo) ClaimFileProcessingExecution(_ context.Context, _ uint, _ string, _ string, _ string, processingStartedAt time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file.ProcessingStatus != "queued" && r.file.ProcessingStatus != "failed" {
		return false, nil
	}
	r.file.ProcessingStatus = "extracting"
	r.file.ProcessingStartedAt = &processingStartedAt
	return true, nil
}

func (r *ocrReprocessRepo) ClaimRecoverableFilesForProcessing(context.Context, time.Time, time.Time, time.Time, int) ([]domainconversation.FileObject, error) {
	return nil, nil
}

type ocrReprocessQueue struct {
	mu      sync.Mutex
	enqueue int
}

func (q *ocrReprocessQueue) InitFileProcessingStream(context.Context) error { return nil }
func (q *ocrReprocessQueue) EnqueueFileProcessing(context.Context, uint, string, int, string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.enqueue++
	return nil
}
func (q *ocrReprocessQueue) ClaimTimedOutFileProcessingMessages(context.Context, string) ([]repository.FileProcessingMessage, error) {
	return nil, nil
}
func (q *ocrReprocessQueue) ReadFileProcessingMessages(context.Context, string) ([]repository.FileProcessingMessage, error) {
	return nil, nil
}
func (q *ocrReprocessQueue) AckFileProcessingMessage(context.Context, string) error { return nil }
func (q *ocrReprocessQueue) DeleteFileProcessingMessage(context.Context, string) error {
	return nil
}
func (q *ocrReprocessQueue) SendFileProcessingToDLQ(context.Context, uint, string, int, string) error {
	return nil
}

func TestEnsureImageOCRProcessingQueuesLegacyReadyImageOnce(t *testing.T) {
	repo := &ocrReprocessRepo{file: domainconversation.FileObject{
		ID:               1,
		UserID:           7,
		FileID:           "file_legacy",
		FileCategory:     "image",
		DetectedMIME:     "image/png",
		StoragePath:      "users/7/file_legacy.png",
		ProcessingStatus: "ready",
		ProcessingReady:  true,
		ExtractStatus:    "none",
	}}
	queue := &ocrReprocessQueue{}
	cfg := config.Config{ExtractImageOCREnabled: true}
	service := NewService(cfg, repo, queue, nil, nil, nil, DefaultExtractorVersion)

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := service.ensureImageOCRProcessing(t.Context(), 7, "file_legacy"); err != nil {
				t.Errorf("ensureImageOCRProcessing() error = %v", err)
			}
		}()
	}
	wg.Wait()

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.enqueue != 1 {
		t.Fatalf("expected one OCR reprocess enqueue, got %d", queue.enqueue)
	}
	if repo.file.ProcessingReady || repo.file.ProcessingStatus != "queued" {
		t.Fatalf("expected legacy image to be requeued, got %#v", repo.file)
	}
}
