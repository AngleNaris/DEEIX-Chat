package agentgroup

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	appagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/agentgroup"
	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
)

func TestAgentGroupReasoningEffortBindingsAcceptMax(t *testing.T) {
	validate := binding.Validator
	max := "max"
	cases := []struct {
		name  string
		value interface{}
	}{
		{name: "create member", value: AgentGroupMemberRequest{RolePublicID: "role-1", ReasoningEffort: "max"}},
		{name: "add member", value: AddAgentGroupMemberRequest{RolePublicID: "role-1", ReasoningEffort: "max"}},
		{name: "update member", value: UpdateAgentGroupMemberRequest{ReasoningEffort: &max}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validate.ValidateStruct(tc.value); err != nil {
				t.Fatalf("max should be accepted: %v", err)
			}
		})
	}

	invalid := "highest"
	for _, value := range []interface{}{
		AgentGroupMemberRequest{RolePublicID: "role-1", ReasoningEffort: invalid},
		AddAgentGroupMemberRequest{RolePublicID: "role-1", ReasoningEffort: invalid},
		UpdateAgentGroupMemberRequest{ReasoningEffort: &invalid},
	} {
		if err := validate.ValidateStruct(value); err == nil {
			t.Fatalf("invalid reasoning effort should be rejected: %#v", value)
		}
	}
}

func TestResolveErrorMapsInvalidReasoningEffortToBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/conversation-agent-groups", bytes.NewReader(nil))

	resolveError(c, appagentgroup.ErrInvalidReasoningEffort, http.StatusInternalServerError, "create agent group failed")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(appagentgroup.ErrInvalidReasoningEffort.Error())) {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}

// 群组已从项目绑定中拆除（§C1）：列表接口不再要求 projectID query 参数，
// 无参数时返回当前用户全部群组 —— 回归测试锁定此行为。
func TestListAgentGroupsWithoutProjectID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set(middleware.ContextKeyUserID, uint(42))
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/conversation-agent-groups", nil)
	handler := newListTestHandler(t)
	handler.ListAgentGroups(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []AgentGroupResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("data len = %d, want 1", len(body.Data))
	}
	if body.Data[0].PublicID != "grp_pub" || body.Data[0].Name != "G1" {
		t.Fatalf("unexpected group: %+v", body.Data[0])
	}
}

type listSettingsStub struct{}

func (listSettingsStub) RuntimeValuesByNamespace(_ context.Context, _ string) (map[string]string, error) {
	return map[string]string{domainagentgroup.FeatureFlagKeyEnabled: "true"}, nil
}

type listRepoStub struct {
	repository.AgentGroupRepository
}

// ListAgentGroups 全局列表：projectID 为 0 时返回全部群组。
func (listRepoStub) ListAgentGroups(_ context.Context, _ uint, projectID uint) ([]domainagentgroup.Group, error) {
	if projectID != 0 {
		return nil, nil
	}
	return []domainagentgroup.Group{{PublicID: "grp_pub", Name: "G1", Status: domainagentgroup.GroupStatusActive}}, nil
}

func newListTestHandler(t *testing.T) *Handler {
	t.Helper()
	service := appagentgroup.NewService(listRepoStub{}, nil, listSettingsStub{}, nil)
	return NewHandler(service, nil)
}
