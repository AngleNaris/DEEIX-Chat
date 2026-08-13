package mcp

import (
	"encoding/json"
	"testing"
)

func TestMergeRedactedMCPHeadersPreservesStoredSecrets(t *testing.T) {
	got := mergeRedactedMCPHeaders(
		`{"Authorization":"Bearer real","X-API-Key":"real-key","X-Title":"old"}`,
		`{"Authorization":"********","X-API-Key":"********","X-Title":"new"}`,
	)
	var headers map[string]string
	if err := json.Unmarshal([]byte(got), &headers); err != nil {
		t.Fatalf("decode merged headers: %v", err)
	}
	if headers["Authorization"] != "Bearer real" || headers["X-API-Key"] != "real-key" || headers["X-Title"] != "new" {
		t.Fatalf("unexpected merged headers: %#v", headers)
	}
}

func TestMergeRedactedMCPHeadersAllowsSecretReplacementAndRemoval(t *testing.T) {
	replaced := mergeRedactedMCPHeaders(
		`{"Authorization":"Bearer old","X-Title":"old"}`,
		`{"Authorization":"Bearer new","X-Title":"new"}`,
	)
	var headers map[string]string
	if err := json.Unmarshal([]byte(replaced), &headers); err != nil {
		t.Fatalf("decode replaced headers: %v", err)
	}
	if headers["Authorization"] != "Bearer new" || headers["X-Title"] != "new" {
		t.Fatalf("unexpected replaced headers: %#v", headers)
	}

	removed := mergeRedactedMCPHeaders(
		`{"Authorization":"Bearer old","X-Title":"old"}`,
		`{"X-Title":"new"}`,
	)
	headers = nil
	if err := json.Unmarshal([]byte(removed), &headers); err != nil {
		t.Fatalf("decode removed headers: %v", err)
	}
	if _, exists := headers["Authorization"]; exists || headers["X-Title"] != "new" {
		t.Fatalf("unexpected removed headers: %#v", headers)
	}
}

func TestValidateServerBaseURLAllowsAdministratorConfiguredPrivateOrigin(t *testing.T) {
	service := &Service{}
	if err := service.validateServerBaseURL("http://mcp-server:8080/mcp"); err != nil {
		t.Fatalf("private MCP endpoint rejected: %v", err)
	}
	if err := service.validateServerBaseURL("http://169.254.169.254/latest/meta-data"); err == nil {
		t.Fatal("metadata endpoint must remain blocked")
	}
}
