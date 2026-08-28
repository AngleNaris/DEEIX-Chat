package dynamicprompt

import (
	"context"
	"errors"
	"strings"
	"testing"

	domaindynamicprompt "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/dynamicprompt"
)

type dynamicPromptRepoFake struct {
	upserts []*domaindynamicprompt.DynamicPrompt
}

func (f *dynamicPromptRepoFake) UpsertDynamicPrompt(_ context.Context, item *domaindynamicprompt.DynamicPrompt) error {
	copy := *item
	f.upserts = append(f.upserts, &copy)
	return nil
}

func (*dynamicPromptRepoFake) DeleteDynamicPrompt(context.Context, uint, string) error {
	return nil
}

func (*dynamicPromptRepoFake) ListDynamicPrompts(context.Context, uint) ([]domaindynamicprompt.DynamicPrompt, error) {
	return nil, nil
}

func TestUpsertDynamicPromptEnforcesRuneLimits(t *testing.T) {
	tests := []struct {
		name    string
		input   UpsertInput
		wantErr error
	}{
		{name: "name at limit", input: UpsertInput{Name: strings.Repeat("名", MaxNameLen), Kind: domaindynamicprompt.KindText, Content: "ok"}},
		{name: "name over limit", input: UpsertInput{Name: strings.Repeat("名", MaxNameLen+1), Kind: domaindynamicprompt.KindText, Content: "ok"}, wantErr: ErrPromptNameTooLong},
		{name: "content at limit", input: UpsertInput{Name: "prompt", Kind: domaindynamicprompt.KindText, Content: strings.Repeat("文", MaxContentLen)}},
		{name: "content over limit", input: UpsertInput{Name: "prompt", Kind: domaindynamicprompt.KindText, Content: strings.Repeat("文", MaxContentLen+1)}, wantErr: ErrPromptContentTooLong},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &dynamicPromptRepoFake{}
			_, err := NewService(repo).UpsertDynamicPrompt(t.Context(), 7, "", tt.input, "user")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				if len(repo.upserts) != 0 {
					t.Fatalf("repository was called %d times for invalid input", len(repo.upserts))
				}
				return
			}
			if err != nil {
				t.Fatalf("valid boundary input failed: %v", err)
			}
			if len(repo.upserts) != 1 {
				t.Fatalf("repository calls = %d, want 1", len(repo.upserts))
			}
		})
	}
}
