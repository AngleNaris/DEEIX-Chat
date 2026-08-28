package conversation

import (
	"context"
	"errors"
	"testing"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

type agentGroupConversationCreateRepositoryStub struct {
	repository.ConversationRepository
	created *domainconversation.Conversation
}

func (r *agentGroupConversationCreateRepositoryStub) CreateConversation(
	_ context.Context,
	conversation *domainconversation.Conversation,
) error {
	copy := *conversation
	r.created = &copy
	return nil
}

type agentGroupConversationCreateResolverStub struct {
	group *domainagentgroup.Group
}

func (r *agentGroupConversationCreateResolverStub) GetAgentGroupByPublicID(
	context.Context,
	uint,
	string,
) (*domainagentgroup.Group, error) {
	if r.group == nil {
		return nil, repository.ErrNotFound
	}
	return r.group, nil
}

func (*agentGroupConversationCreateResolverStub) CountAgentGroupReferencesByRole(context.Context, uint) (int64, error) {
	return 0, nil
}

func (*agentGroupConversationCreateResolverStub) CountAgentGroupReferencesByProject(context.Context, uint) (int64, error) {
	return 0, nil
}

func newAgentGroupConversationCreateService() (*Service, *agentGroupConversationCreateRepositoryStub) {
	repo := &agentGroupConversationCreateRepositoryStub{}
	return &Service{
		repo: repo,
		agentGroupRepo: &agentGroupConversationCreateResolverStub{
			group: &domainagentgroup.Group{
				ID:       7,
				PublicID: "group-1",
				Name:     "Group",
			},
		},
	}, repo
}

func TestCreateAgentGroupConversationRejectsExplicitModel(t *testing.T) {
	service, repo := newAgentGroupConversationCreateService()

	_, err := service.CreateConversation(
		context.Background(),
		11,
		"Group",
		"openai/gpt-5",
		"",
		"",
		"group-1",
	)
	if !errors.Is(err, ErrConversationModelNotAllowedWithGroup) {
		t.Fatalf("CreateConversation() error = %v, want %v", err, ErrConversationModelNotAllowedWithGroup)
	}
	if repo.created != nil {
		t.Fatal("explicit group model must be rejected before persistence")
	}
}

func TestCreateAgentGroupConversationPersistsEmptyRequestModel(t *testing.T) {
	service, repo := newAgentGroupConversationCreateService()

	created, err := service.CreateConversation(
		context.Background(),
		11,
		"Group",
		"   ",
		"",
		"",
		"group-1",
	)
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if created.Model != "" || repo.created == nil || repo.created.Model != "" {
		t.Fatalf("group conversation model = %q, persisted = %#v; want empty", created.Model, repo.created)
	}
	if created.AgentGroupID == nil || *created.AgentGroupID != 7 {
		t.Fatalf("group conversation binding = %#v, want group ID 7", created.AgentGroupID)
	}
}

func TestCreateOrdinaryConversationPreservesSelectedModel(t *testing.T) {
	service, repo := newAgentGroupConversationCreateService()

	created, err := service.CreateConversation(
		context.Background(),
		11,
		"Chat",
		"  openai/gpt-5  ",
		"",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if created.Model != "openai/gpt-5" || repo.created == nil || repo.created.Model != "openai/gpt-5" {
		t.Fatalf("ordinary conversation model = %q, persisted = %#v", created.Model, repo.created)
	}
}
