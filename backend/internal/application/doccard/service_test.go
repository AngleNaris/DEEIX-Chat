package doccard

import (
	"context"
	"errors"
	"testing"

	domaindoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/doccard"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

type fakeDocCardRepository struct {
	existing *domaindoccard.DocCard
	upserted *domaindoccard.DocCard
	getErr   error
}

func (f *fakeDocCardRepository) UpsertDocCard(_ context.Context, item *domaindoccard.DocCard) error {
	copy := *item
	f.upserted = &copy
	return nil
}

func (f *fakeDocCardRepository) DeleteDocCard(context.Context, uint, string) error {
	return nil
}

func (f *fakeDocCardRepository) ListDocCards(context.Context, uint) ([]domaindoccard.DocCard, error) {
	return nil, nil
}

func (f *fakeDocCardRepository) GetDocCardByPublicID(context.Context, uint, string) (*domaindoccard.DocCard, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.existing == nil {
		return nil, repository.ErrNotFound
	}
	copy := *f.existing
	return &copy, nil
}

func TestUpsertDocCardUpdatePreservesEnabledWhenOmitted(t *testing.T) {
	repo := &fakeDocCardRepository{
		existing: &domaindoccard.DocCard{CardPublicID: "card-1", UserID: 7, Enabled: false},
	}
	svc := NewService(repo)

	item, err := svc.UpsertDocCard(context.Background(), 7, "card-1", UpsertInput{
		Title:   "updated",
		Content: "content",
	}, "ai")
	if err != nil {
		t.Fatalf("update doc card: %v", err)
	}
	if item.Enabled || repo.upserted == nil || repo.upserted.Enabled {
		t.Fatalf("omitted enabled must preserve disabled state: item=%+v upserted=%+v", item, repo.upserted)
	}
}

func TestUpsertDocCardUpdateRejectsMissingCard(t *testing.T) {
	repo := &fakeDocCardRepository{getErr: repository.ErrNotFound}
	svc := NewService(repo)

	_, err := svc.UpsertDocCard(context.Background(), 7, "missing", UpsertInput{
		Title:   "updated",
		Content: "content",
	}, "ai")
	if !errors.Is(err, ErrCardNotFound) {
		t.Fatalf("expected ErrCardNotFound, got %v", err)
	}
	if repo.upserted != nil {
		t.Fatalf("missing update must not create a new card")
	}
}
