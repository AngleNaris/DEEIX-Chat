package credentials

import (
	"encoding/json"
	"testing"

	appcredentials "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/credentials"
)

func TestCredentialResponseUsesStableSnakeCaseKeys(t *testing.T) {
	item := toCredentialResponseItem(appcredentials.View{
		PublicID: "cred_123", Name: "deploy", Type: "api_key", Description: "deployment key",
		Meta: map[string]string{"host": "example.test"}, CreatedAt: "2026-08-15 12:00:00", UpdatedAt: "2026-08-15 12:01:00",
	})
	encoded, err := json.Marshal(CredentialResponse{Credential: item})
	if err != nil {
		t.Fatal(err)
	}
	const expected = `{"credential":{"public_id":"cred_123","name":"deploy","type":"api_key","description":"deployment key","meta":{"host":"example.test"},"created_at":"2026-08-15 12:00:00","updated_at":"2026-08-15 12:01:00"}}`
	if string(encoded) != expected {
		t.Fatalf("unexpected credential JSON: %s", encoded)
	}
}

func TestCredentialListResponseUsesEmptyArray(t *testing.T) {
	encoded, err := json.Marshal(CredentialListResponse{Results: toCredentialResponseItems(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"results":[]}` {
		t.Fatalf("unexpected empty credential list JSON: %s", encoded)
	}
}
