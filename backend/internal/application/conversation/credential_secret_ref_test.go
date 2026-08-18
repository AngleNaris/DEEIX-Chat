package conversation

import (
	"strings"
	"testing"
)

func TestCredentialSecretRefProtectReusesOpaqueReference(t *testing.T) {
	store := newCredentialSecretRefStore(7, 11, "run-1")
	first := store.protect("plain-secret")
	second := store.protect("plain-secret")
	if first == "" || first != second || strings.Contains(first, "plain-secret") {
		t.Fatalf("unexpected secret references: first=%q second=%q", first, second)
	}
	if value, ok := store.resolve(first); !ok || value != "plain-secret" {
		t.Fatalf("secret reference did not resolve inside its run: value=%q ok=%v", value, ok)
	}
}

func TestCredentialSecretRefExpansionRejectsCrossScopeAndExpiredReferences(t *testing.T) {
	runtime := selectedToolRuntime{}
	runtime.bindCredentialSecretRefs(7, 11, "run-1")
	ref := runtime.credentialSecrets.protect("plain-secret")
	arguments := `{"value":"` + ref + `","meta":{"host":"example.com"}}`

	for _, tt := range []struct {
		name           string
		userID         uint
		conversationID uint
		runID          string
		wantErr        string
	}{
		{name: "user", userID: 8, conversationID: 11, runID: "run-1", wantErr: "not valid"},
		{name: "conversation", userID: 7, conversationID: 12, runID: "run-1", wantErr: "not valid"},
		{name: "run", userID: 7, conversationID: 11, runID: "run-2", wantErr: "not valid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runtime.expandCredentialSecretValueInJSON(tt.userID, tt.conversationID, tt.runID, arguments)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expandCredentialSecretValueInJSON() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}

	expanded, err := runtime.expandCredentialSecretValueInJSON(7, 11, "run-1", arguments)
	if err != nil {
		t.Fatalf("expand valid secret_ref: %v", err)
	}
	if strings.Contains(expanded, ref) || strings.Count(expanded, "plain-secret") != 1 {
		t.Fatalf("credential value secret_ref was not expanded exactly once: %s", expanded)
	}

	runtime.credentialSecrets.destroy(ref)
	_, err = runtime.expandCredentialSecretValueInJSON(7, 11, "run-1", arguments)
	if err == nil || !strings.Contains(err.Error(), "unknown or expired") {
		t.Fatalf("expired secret_ref error = %v", err)
	}
}

func TestCredentialSecretRefExpansionRejectsMalformedAndNonValueReferences(t *testing.T) {
	runtime := selectedToolRuntime{}
	runtime.bindCredentialSecretRefs(7, 11, "run-1")
	ref := runtime.credentialSecrets.protect("plain-secret")
	tests := []struct {
		name      string
		arguments string
		wantErr   string
	}{
		{
			name:      "malformed value",
			arguments: `{"value":"{{secret_ref:not-a-uuid}}"}`,
			wantErr:   "malformed",
		},
		{
			name:      "embedded value",
			arguments: `{"value":"prefix-` + ref + `"}`,
			wantErr:   "malformed",
		},
		{
			name:      "description",
			arguments: `{"value":"safe","description":"` + ref + `"}`,
			wantErr:   "only allowed",
		},
		{
			name:      "nested metadata",
			arguments: `{"value":"safe","meta":{"token":"` + ref + `"}}`,
			wantErr:   "only allowed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runtime.expandCredentialSecretValueInJSON(7, 11, "run-1", tt.arguments)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expandCredentialSecretValueInJSON() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestCredentialSecretRefDestroySuccessfulWritesOnly(t *testing.T) {
	runtime := selectedToolRuntime{}
	runtime.bindCredentialSecretRefs(7, 11, "run-1")
	successRef := runtime.credentialSecrets.protect("success-secret")
	failedRef := runtime.credentialSecrets.protect("failed-secret")

	runtime.destroyCredentialSecretWrites([]credentialWrite{{Name: "saved", Ref: successRef}})
	if _, ok := runtime.credentialSecrets.resolve(successRef); ok {
		t.Fatal("successful secret_ref was not destroyed")
	}
	if value, ok := runtime.credentialSecrets.resolve(failedRef); !ok || value != "failed-secret" {
		t.Fatalf("unrelated failed secret_ref was destroyed: value=%q ok=%v", value, ok)
	}
}
