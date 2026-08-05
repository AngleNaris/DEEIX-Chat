package agentgroup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	appagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/agentgroup"
	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
)

// 列表接口的 projectID 是 query 参数（前端以 ?projectID= 传递），
// 曾误用读取路由参数的 stringParam 导致恒为 400 —— 回归测试锁定此行为。
func TestListAgentGroupsQueryProjectID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("missing project id rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Set(middleware.ContextKeyUserID, uint(42))
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/conversation-agent-groups", nil)
		handler := newListTestHandler(t)
		handler.ListAgentGroups(c)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		var body map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if msg := body["errorMsg"]; msg != "invalid project id" {
			t.Fatalf("errorMsg = %v, want invalid project id", msg)
		}
	})

	t.Run("project id query accepted", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Set(middleware.ContextKeyUserID, uint(42))
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/conversation-agent-groups?projectID=proj_pub", nil)
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
	})
}

type listSettingsStub struct{}

func (listSettingsStub) RuntimeValuesByNamespace(_ context.Context, _ string) (map[string]string, error) {
	return map[string]string{domainagentgroup.FeatureFlagKeyEnabled: "true"}, nil
}

type listProjectStub struct{}

func (listProjectStub) GetConversationProject(_ context.Context, _ uint, publicID string) (*domainconversation.ConversationProject, error) {
	if publicID != "proj_pub" {
		return nil, repository.ErrNotFound
	}
	return &domainconversation.ConversationProject{ID: 1, PublicID: "proj_pub", Name: "P1"}, nil
}

type listRepoStub struct {
	repository.AgentGroupRepository
}

func (listRepoStub) ListAgentGroupsByProject(_ context.Context, _ uint, projectID uint) ([]domainagentgroup.Group, error) {
	if projectID != 1 {
		return nil, nil
	}
	return []domainagentgroup.Group{{PublicID: "grp_pub", Name: "G1", Status: domainagentgroup.GroupStatusActive}}, nil
}

func newListTestHandler(t *testing.T) *Handler {
	t.Helper()
	service := appagentgroup.NewService(listRepoStub{}, nil, listProjectStub{}, listSettingsStub{}, nil)
	return NewHandler(service, nil)
}
