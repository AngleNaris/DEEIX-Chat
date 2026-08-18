package conversation

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	appcredentials "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/credentials"
)

type capturingCredentialResolver struct {
	createdInput *appcredentials.UpsertInput
	updatedInput *appcredentials.UpsertInput
}

func (f *capturingCredentialResolver) ListCredentials(context.Context, uint) ([]appcredentials.View, error) {
	return nil, nil
}

func (f *capturingCredentialResolver) CreateCredential(_ context.Context, _ uint, input appcredentials.UpsertInput) (*appcredentials.View, error) {
	f.createdInput = &input
	return &appcredentials.View{
		Name:        input.Name,
		Type:        input.Type,
		Description: input.Description,
	}, nil
}

func (f *capturingCredentialResolver) UpdateCredentialByName(_ context.Context, _ uint, _ string, input appcredentials.UpsertInput) (*appcredentials.View, error) {
	f.updatedInput = &input
	return &appcredentials.View{
		Name:        input.Name,
		Type:        input.Type,
		Description: input.Description,
	}, nil
}

func (f *capturingCredentialResolver) DeleteCredentialByName(context.Context, uint, string) error {
	return nil
}

func (f *capturingCredentialResolver) ResolveValue(context.Context, uint, string) (string, error) {
	return "", nil
}

func TestPlatformCredentialHandlersNormalizeScalarMeta(t *testing.T) {
	wantMeta := map[string]string{
		"host":   "example.test",
		"port":   "22",
		"strict": "true",
		"ratio":  "1.5",
	}

	t.Run("create", func(t *testing.T) {
		resolver := &capturingCredentialResolver{}
		svc := &Service{credentials: resolver}
		_, err := svc.platformCreateCredential(context.Background(), platformToolCallContext{
			UserID: 1,
			Arguments: json.RawMessage(
				`{"name":"test-vps","type":"ssh","value":"test-secret","meta":{"host":"example.test","port":22,"strict":true,"ratio":1.5}}`,
			),
		})
		if err != nil {
			t.Fatalf("platformCreateCredential returned error: %v", err)
		}
		if resolver.createdInput == nil {
			t.Fatal("credential create was not called")
		}
		if !reflect.DeepEqual(resolver.createdInput.Meta, wantMeta) {
			t.Fatalf("unexpected normalized meta: got %#v want %#v", resolver.createdInput.Meta, wantMeta)
		}
	})

	t.Run("update", func(t *testing.T) {
		resolver := &capturingCredentialResolver{}
		svc := &Service{credentials: resolver}
		_, err := svc.platformUpdateCredential(context.Background(), platformToolCallContext{
			UserID: 1,
			Arguments: json.RawMessage(
				`{"name":"test-vps","meta":{"host":"example.test","port":22,"strict":true,"ratio":1.5}}`,
			),
		})
		if err != nil {
			t.Fatalf("platformUpdateCredential returned error: %v", err)
		}
		if resolver.updatedInput == nil {
			t.Fatal("credential update was not called")
		}
		if !reflect.DeepEqual(resolver.updatedInput.Meta, wantMeta) {
			t.Fatalf("unexpected normalized meta: got %#v want %#v", resolver.updatedInput.Meta, wantMeta)
		}
	})
}

func TestPlatformCredentialHandlersRejectNonScalarMeta(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "array", value: `[22]`},
		{name: "object", value: `{"nested":"value"}`},
		{name: "null", value: `null`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &capturingCredentialResolver{}
			svc := &Service{credentials: resolver}
			_, err := svc.platformCreateCredential(context.Background(), platformToolCallContext{
				UserID:    1,
				Arguments: json.RawMessage(`{"name":"test-vps","value":"test-secret","meta":{"port":` + tc.value + `}}`),
			})
			if err == nil {
				t.Fatalf("expected %s meta value to be rejected", tc.name)
			}
			if resolver.createdInput != nil {
				t.Fatal("credential create must not run after invalid metadata")
			}
		})
	}
}

func TestPlatformCredentialSchemasAllowScalarMetaValues(t *testing.T) {
	for _, toolName := range []string{"credential_create", "credential_update"} {
		t.Run(toolName, func(t *testing.T) {
			entry := platformToolRegistry()[toolName]
			var schema struct {
				Properties map[string]struct {
					AdditionalProperties struct {
						AnyOf []struct {
							Type string `json:"type"`
						} `json:"anyOf"`
					} `json:"additionalProperties"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(entry.definition.InputSchema, &schema); err != nil {
				t.Fatalf("decode %s schema: %v", toolName, err)
			}

			gotTypes := make([]string, 0, len(schema.Properties["meta"].AdditionalProperties.AnyOf))
			for _, option := range schema.Properties["meta"].AdditionalProperties.AnyOf {
				gotTypes = append(gotTypes, option.Type)
			}
			wantTypes := []string{"string", "number", "boolean"}
			if !reflect.DeepEqual(gotTypes, wantTypes) {
				t.Fatalf("unexpected %s meta schema types: got %v want %v", toolName, gotTypes, wantTypes)
			}
		})
	}
}
