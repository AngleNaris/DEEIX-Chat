package conversation

import (
	"context"
	"encoding/json"
	"testing"

	appdoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/doccard"
	domaindoccard "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/doccard"
)

type fakePlatformDocCards struct {
	existing *domaindoccard.DocCard
	list     []appdoccard.CardView
	input    appdoccard.UpsertInput
}

func (f *fakePlatformDocCards) ListDocCards(context.Context, uint) ([]appdoccard.CardView, error) {
	return f.list, nil
}

func (f *fakePlatformDocCards) GetDocCard(context.Context, uint, string) (*domaindoccard.DocCard, error) {
	return f.existing, nil
}

func (f *fakePlatformDocCards) UpsertDocCard(_ context.Context, userID uint, publicID string, input appdoccard.UpsertInput, updatedBy string) (*domaindoccard.DocCard, error) {
	f.input = input
	return &domaindoccard.DocCard{
		CardPublicID: publicID,
		UserID:       userID,
		Category:     input.Category,
		ProjectID:    input.ProjectID,
		RoleID:       input.RoleID,
		Title:        input.Title,
		Content:      input.Content,
		Keywords:     input.Keywords,
		Enabled:      false,
		UpdatedBy:    updatedBy,
	}, nil
}

func (f *fakePlatformDocCards) DeleteDocCard(context.Context, uint, string) error {
	return nil
}

func TestPlatformSaveDocCardPreservesBindingsWhenOmitted(t *testing.T) {
	projectID := uint(11)
	roleID := uint(22)
	cards := &fakePlatformDocCards{
		existing: &domaindoccard.DocCard{
			CardPublicID: "card-1",
			Category:     "world",
			ProjectID:    &projectID,
			RoleID:       &roleID,
			Keywords:     []string{"forest"},
		},
	}
	svc := &Service{docCards: cards}

	output, err := svc.platformSaveDocCard(context.Background(), platformToolCallContext{
		UserID:    7,
		Arguments: json.RawMessage(`{"card_id":"card-1","title":"updated","content":"body"}`),
	})
	if err != nil {
		t.Fatalf("save doc card: %v", err)
	}
	if cards.input.ProjectID == nil || *cards.input.ProjectID != projectID {
		t.Fatalf("project binding was not preserved: %+v", cards.input.ProjectID)
	}
	if cards.input.RoleID == nil || *cards.input.RoleID != roleID {
		t.Fatalf("role binding was not preserved: %+v", cards.input.RoleID)
	}
	if cards.input.Category != "world" || len(cards.input.Keywords) != 1 || cards.input.Keywords[0] != "forest" {
		t.Fatalf("optional card fields were not preserved: category=%q keywords=%v", cards.input.Category, cards.input.Keywords)
	}
	if !json.Valid([]byte(output)) {
		t.Fatalf("invalid output: %s", output)
	}
}

func TestPlatformSaveDocCardCanClearBindingsWithZero(t *testing.T) {
	projectID := uint(11)
	roleID := uint(22)
	cards := &fakePlatformDocCards{
		existing: &domaindoccard.DocCard{
			CardPublicID: "card-1",
			ProjectID:    &projectID,
			RoleID:       &roleID,
		},
	}
	svc := &Service{docCards: cards}

	_, err := svc.platformSaveDocCard(context.Background(), platformToolCallContext{
		UserID:    7,
		Arguments: json.RawMessage(`{"card_id":"card-1","project_id":0,"role_id":0,"title":"updated","content":"body"}`),
	})
	if err != nil {
		t.Fatalf("save doc card: %v", err)
	}
	if cards.input.ProjectID != nil || cards.input.RoleID != nil {
		t.Fatalf("zero binding ids must clear bindings: project=%v role=%v", cards.input.ProjectID, cards.input.RoleID)
	}
}

func TestPlatformListDocCardsReturnsBindings(t *testing.T) {
	projectID := uint(11)
	roleID := uint(22)
	svc := &Service{docCards: &fakePlatformDocCards{
		list: []appdoccard.CardView{{
			CardPublicID: "card-1",
			ProjectID:    &projectID,
			RoleID:       &roleID,
			Title:        "card",
			Content:      "body",
			Enabled:      true,
		}},
	}}

	output, err := svc.platformListDocCards(context.Background(), platformToolCallContext{UserID: 7})
	if err != nil {
		t.Fatalf("list doc cards: %v", err)
	}
	var payload struct {
		Cards []struct {
			ProjectID *uint `json:"project_id"`
			RoleID    *uint `json:"role_id"`
		} `json:"cards"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(payload.Cards) != 1 || payload.Cards[0].ProjectID == nil || *payload.Cards[0].ProjectID != projectID ||
		payload.Cards[0].RoleID == nil || *payload.Cards[0].RoleID != roleID {
		t.Fatalf("bindings missing from list output: %s", output)
	}
}

func TestSaveDocCardSchemaIncludesBindings(t *testing.T) {
	entry := platformToolRegistry()["save_doc_card"]
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(entry.definition.InputSchema, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	if schema.Properties["project_id"] == nil || schema.Properties["role_id"] == nil {
		t.Fatalf("save_doc_card schema must expose project_id and role_id")
	}
}
