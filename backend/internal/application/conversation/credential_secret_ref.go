package conversation

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"
)

var credentialSecretRefPattern = regexp.MustCompile(`^\{\{secret_ref:([0-9a-fA-F-]{36})\}\}$`)

type credentialSecretRefStore struct {
	mu             sync.Mutex
	userID         uint
	conversationID uint
	runID          string
	values         map[string]string
	refsByValue    map[string]string
}

func newCredentialSecretRefStore(userID uint, conversationID uint, runID string) *credentialSecretRefStore {
	return &credentialSecretRefStore{
		userID:         userID,
		conversationID: conversationID,
		runID:          strings.TrimSpace(runID),
		values:         make(map[string]string),
		refsByValue:    make(map[string]string),
	}
}

func (r *selectedToolRuntime) bindCredentialSecretRefs(userID uint, conversationID uint, runID string) {
	if r == nil {
		return
	}
	runID = strings.TrimSpace(runID)
	if r.credentialSecrets != nil &&
		r.credentialSecrets.userID == userID &&
		r.credentialSecrets.conversationID == conversationID &&
		r.credentialSecrets.runID == runID {
		return
	}
	r.credentialSecrets = newCredentialSecretRefStore(userID, conversationID, runID)
}

func (s *credentialSecretRefStore) protect(value string) string {
	if s == nil || value == "" {
		return ""
	}
	if credentialSecretRefPattern.MatchString(value) {
		return value
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ref := s.refsByValue[value]; ref != "" {
		return ref
	}
	ref := "{{secret_ref:" + uuid.NewString() + "}}"
	s.values[ref] = value
	s.refsByValue[value] = ref
	return ref
}

func (s *credentialSecretRefStore) resolve(ref string) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[strings.TrimSpace(ref)]
	return value, ok
}

func (s *credentialSecretRefStore) destroy(ref string) {
	if s == nil || strings.TrimSpace(ref) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ref = strings.TrimSpace(ref)
	value, ok := s.values[ref]
	if !ok {
		return
	}
	delete(s.values, ref)
	if s.refsByValue[value] == ref {
		delete(s.refsByValue, value)
	}
}

func (r *selectedToolRuntime) protectCredentialWrite(write credentialWrite) credentialWrite {
	if r == nil || r.credentialSecrets == nil || write.Value == "" {
		return write
	}
	if strings.Contains(write.Value, "{{secret_ref:") {
		write.Ref = strings.TrimSpace(write.Value)
		if credentialSecretRefPattern.MatchString(write.Ref) {
			if value, ok := r.credentialSecrets.resolve(write.Ref); ok {
				write.Value = value
			}
		}
		return write
	}
	write.Ref = r.credentialSecrets.protect(write.Value)
	return write
}

func (r *selectedToolRuntime) resolveCredentialSecretWrite(write credentialWrite) (credentialWrite, bool) {
	if write.Value == "" {
		return credentialWrite{}, false
	}
	if !credentialSecretRefPattern.MatchString(write.Value) {
		return r.protectCredentialWrite(write), true
	}
	if r == nil || r.credentialSecrets == nil {
		return credentialWrite{}, false
	}
	ref := strings.TrimSpace(write.Value)
	value, ok := r.credentialSecrets.resolve(ref)
	if !ok {
		return credentialWrite{}, false
	}
	write.Value = value
	write.Ref = ref
	return write, true
}

func (r *selectedToolRuntime) expandCredentialSecretValueInJSON(
	userID uint,
	conversationID uint,
	runID string,
	argumentsJSON string,
) (string, error) {
	if !strings.Contains(argumentsJSON, "{{secret_ref:") {
		return argumentsJSON, nil
	}
	if r == nil || r.credentialSecrets == nil ||
		r.credentialSecrets.userID != userID ||
		r.credentialSecrets.conversationID != conversationID ||
		r.credentialSecrets.runID != strings.TrimSpace(runID) {
		return "", errors.New("secret_ref is not valid for this user, conversation, or run")
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err != nil {
		return "", err
	}
	for key, value := range payload {
		if key == "value" {
			continue
		}
		if containsCredentialSecretRefLike(value) {
			return "", fmt.Errorf("secret_ref is only allowed as the credential value, not %q", key)
		}
	}
	rawValue, ok := payload["value"]
	if !ok {
		return "", errors.New("secret_ref requires a credential value")
	}
	value, ok := rawValue.(string)
	if !ok {
		return "", errors.New("credential value containing secret_ref must be a string")
	}
	ref := strings.TrimSpace(value)
	if ref != value || !credentialSecretRefPattern.MatchString(ref) {
		return "", errors.New("credential value contains a malformed secret_ref")
	}
	resolved, ok := r.credentialSecrets.resolve(ref)
	if !ok {
		return "", errors.New("secret_ref is unknown or expired for this run")
	}
	payload["value"] = resolved
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func containsCredentialSecretRefLike(value interface{}) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, "{{secret_ref:")
	case map[string]interface{}:
		for _, child := range typed {
			if containsCredentialSecretRefLike(child) {
				return true
			}
		}
	case []interface{}:
		for _, child := range typed {
			if containsCredentialSecretRefLike(child) {
				return true
			}
		}
	}
	return false
}

func (r *selectedToolRuntime) destroyCredentialSecretWrites(writes []credentialWrite) {
	if r == nil || r.credentialSecrets == nil {
		return
	}
	for _, write := range writes {
		r.credentialSecrets.destroy(write.Ref)
	}
}
