package conversation

import (
	"errors"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

func TestTranslateConversationRoleError(t *testing.T) {
	if err := translateConversationRoleError(repository.ErrNotFound); !errors.Is(err, ErrConversationRoleNotFound) {
		t.Fatalf("not-found error = %v, want ErrConversationRoleNotFound", err)
	}
	want := errors.New("database unavailable")
	if err := translateConversationRoleError(want); !errors.Is(err, want) {
		t.Fatalf("unexpected error = %v, want original error", err)
	}
}
