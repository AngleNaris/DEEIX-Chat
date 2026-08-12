package conversation

import (
	"context"
	"encoding/json"
	"testing"

	appcredentials "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/credentials"
)

// fakeCredentialResolver 模拟凭据服务。
type fakeCredentialResolver struct {
	values map[string]string
}

func (f *fakeCredentialResolver) ListCredentials(ctx context.Context, userID uint) ([]appcredentials.View, error) {
	return nil, nil
}

func (f *fakeCredentialResolver) CreateCredential(ctx context.Context, userID uint, input appcredentials.UpsertInput) (*appcredentials.View, error) {
	return nil, nil
}

func (f *fakeCredentialResolver) UpdateCredentialByName(ctx context.Context, userID uint, name string, input appcredentials.UpsertInput) (*appcredentials.View, error) {
	return nil, nil
}

func (f *fakeCredentialResolver) DeleteCredentialByName(ctx context.Context, userID uint, name string) error {
	return nil
}

func (f *fakeCredentialResolver) ResolveValue(ctx context.Context, userID uint, name string) (string, error) {
	if f == nil || f.values == nil {
		return "", nil
	}
	return f.values[name], nil
}

func TestExpandCredentialRefsInJSON(t *testing.T) {
	svc := &Service{credentials: &fakeCredentialResolver{values: map[string]string{
		"vpsssh": "sup3r-s3cret",
	}}}

	raw := `{"command":"sshpass -p '{{credential: vpsssh}}' ssh root@10.0.0.5","cwd":"/tmp"}`
	expanded := svc.expandCredentialRefsInJSON(context.Background(), 1, raw)
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(expanded), &obj); err != nil {
		t.Fatalf("expanded output is not valid JSON: %v\n%s", err, expanded)
	}
	if obj["command"] != "sshpass -p 'sup3r-s3cret' ssh root@10.0.0.5" {
		t.Fatalf("placeholder not expanded: %v", obj["command"])
	}
}

func TestExpandCredentialRefsUnknownKeepsPlaceholder(t *testing.T) {
	svc := &Service{credentials: &fakeCredentialResolver{values: map[string]string{}}}
	raw := `{"command":"echo {{credential: nope}}"}`
	expanded := svc.expandCredentialRefsInJSON(context.Background(), 1, raw)
	if expanded != raw {
		t.Fatalf("unknown credential should keep placeholder, got: %s", expanded)
	}
}

func TestExpandCredentialRefsNestedAndSpecialChars(t *testing.T) {
	svc := &Service{credentials: &fakeCredentialResolver{values: map[string]string{
		// 值含引号/反斜杠：展开后仍必须是合法 JSON。
		"weird": `a"b\c`,
	}}}
	raw := `{"meta":{"host":"x"},"script":"run {{credential: weird}} now","list":["{{credential: weird}}"]}`
	expanded := svc.expandCredentialRefsInJSON(context.Background(), 1, raw)
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(expanded), &obj); err != nil {
		t.Fatalf("expanded output is not valid JSON: %v\n%s", err, expanded)
	}
	if obj["script"] != `run a"b\c now` {
		t.Fatalf("nested placeholder not expanded: %v", obj["script"])
	}
}

func TestExpandCredentialRefsNilService(t *testing.T) {
	svc := &Service{}
	raw := `{"command":"echo {{credential: vpsssh}}"}`
	if got := svc.expandCredentialRefsInJSON(context.Background(), 1, raw); got != raw {
		t.Fatalf("nil resolver should keep placeholder, got: %s", got)
	}
}

func TestMaskCredentialToolInput(t *testing.T) {
	cases := []struct {
		name           string
		toolName       string
		isPlatformTool bool
		input          string
		wantMasked     bool
	}{
		{name: "credential_create platform", toolName: "credential_create", isPlatformTool: true, input: `{"name":"v","value":"secret","type":"ssh"}`, wantMasked: true},
		{name: "credential_update platform", toolName: "credential_update", isPlatformTool: true, input: `{"name":"v","value":"new-secret"}`, wantMasked: true},
		{name: "non-credential platform", toolName: "execute_js", isPlatformTool: true, input: `{"code":"1+1","value":"keep"}`, wantMasked: false},
		{name: "mcp tool with credential name", toolName: "credential_create", isPlatformTool: false, input: `{"value":"keep"}`, wantMasked: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := maskCredentialToolInput(tc.toolName, tc.isPlatformTool, tc.input)
			if tc.wantMasked {
				if got == tc.input || !containsStr(got, "[REDACTED]") {
					t.Fatalf("expected value masked, got: %s", got)
				}
			} else if got != tc.input {
				t.Fatalf("expected unchanged, got: %s", got)
			}
		})
	}
}

func containsStr(s string, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
