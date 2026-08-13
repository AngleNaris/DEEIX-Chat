package conversation

import (
	"context"
	"errors"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

type agentGroupSnapshotRepositoryStub struct {
	repository.ConversationRepository
	conversation *domainconversation.Conversation
	roles        map[string]*domainconversation.ConversationRole
}

func (r *agentGroupSnapshotRepositoryStub) GetConversationByUser(context.Context, uint, uint) (*domainconversation.Conversation, error) {
	if r.conversation == nil {
		return nil, repository.ErrNotFound
	}
	return r.conversation, nil
}

func (r *agentGroupSnapshotRepositoryStub) GetConversationRoleByPublicID(_ context.Context, _ uint, publicID string) (*domainconversation.ConversationRole, error) {
	role := r.roles[publicID]
	if role == nil {
		return nil, repository.ErrNotFound
	}
	return role, nil
}

type agentGroupSnapshotResolverStub struct {
	group *domainagentgroup.Group
}

func (r *agentGroupSnapshotResolverStub) GetAgentGroupByPublicID(context.Context, uint, string) (*domainagentgroup.Group, error) {
	if r.group == nil {
		return nil, repository.ErrNotFound
	}
	return r.group, nil
}

func (*agentGroupSnapshotResolverStub) CountAgentGroupReferencesByRole(context.Context, uint) (int64, error) {
	return 0, nil
}

func (*agentGroupSnapshotResolverStub) CountAgentGroupReferencesByProject(context.Context, uint) (int64, error) {
	return 0, nil
}

type agentGroupRouteResolverStub struct {
	defaultRoute *channel.ResolvedRoute
	defaultErr   error
	defaultCalls []channel.ResolveRouteInput
	routeCalls   []channel.ResolveRouteInput
}

func (r *agentGroupRouteResolverStub) ResolveDefaultRoute(_ context.Context, input channel.ResolveRouteInput) (*channel.ResolvedRoute, error) {
	r.defaultCalls = append(r.defaultCalls, input)
	return r.defaultRoute, r.defaultErr
}

func (r *agentGroupRouteResolverStub) ResolveRoute(_ context.Context, input channel.ResolveRouteInput) (*channel.ResolvedRoute, error) {
	r.routeCalls = append(r.routeCalls, input)
	return nil, channel.ErrModelNotFound
}

func (*agentGroupRouteResolverStub) MarkRouteFailure(context.Context, *channel.ResolvedRoute, error) {
}
func (*agentGroupRouteResolverStub) MarkRouteSuccess(context.Context, *channel.ResolvedRoute) {}

type agentGroupRunStoreStub struct {
	repository.AgentGroupRunRepository
}

func newAgentGroupSnapshotService(conversationModel string, members []domainagentgroup.Member, roles map[string]*domainconversation.ConversationRole, resolver *agentGroupRouteResolverStub) (*Service, *domainconversation.Conversation) {
	groupID := uint(7)
	conversation := &domainconversation.Conversation{
		ID:                 22,
		UserID:             11,
		AgentGroupID:       &groupID,
		AgentGroupPublicID: "group-1",
		Model:              conversationModel,
	}
	return &Service{
		repo: &agentGroupSnapshotRepositoryStub{
			conversation: conversation,
			roles:        roles,
		},
		agentGroupRepo: &agentGroupSnapshotResolverStub{group: &domainagentgroup.Group{
			ID:       groupID,
			PublicID: "group-1",
			Name:     "Group",
			Revision: 3,
			Members:  members,
		}},
		routeResolver: resolver,
	}, conversation
}

func TestSendMessageInternalDispatchesAgentGroupConversation(t *testing.T) {
	groupID := uint(7)
	service := &Service{
		repo: &agentGroupSnapshotRepositoryStub{conversation: &domainconversation.Conversation{
			ID: 22, UserID: 11, AgentGroupID: &groupID, AgentGroupPublicID: "group-1",
		}},
		agentGroupRunStore: &agentGroupRunStoreStub{},
	}

	_, err := service.sendMessageInternal(context.Background(), SendMessageInput{
		UserID: 11, ConversationID: 22, Content: "hello",
	}, nil, false)
	if !errors.Is(err, ErrAgentGroupFeatureDisabled) {
		t.Fatalf("expected agent group dispatch, got %v", err)
	}
}

func TestBuildAgentGroupRunSnapshotModelPrecedenceAndDefault(t *testing.T) {
	members := []domainagentgroup.Member{
		{ID: 1, PublicID: "member-supervisor", RolePublicID: "role-supervisor", MemberType: domainagentgroup.MemberTypeSupervisor, Enabled: true, ModelOverride: "member-model", ReasoningEffort: "max"},
		{ID: 2, PublicID: "member-role", RolePublicID: "role-model", MemberType: domainagentgroup.MemberTypeWorker, Enabled: true},
		{ID: 3, PublicID: "member-conversation", RolePublicID: "role-empty", MemberType: domainagentgroup.MemberTypeWorker, Enabled: true},
	}
	roles := map[string]*domainconversation.ConversationRole{
		"role-supervisor": {ID: 11, PublicID: "role-supervisor", Name: "Supervisor", Model: "role-supervisor-model"},
		"role-model":      {ID: 12, PublicID: "role-model", Name: "Role model", Model: "role-model"},
		"role-empty":      {ID: 13, PublicID: "role-empty", Name: "Conversation model"},
	}
	resolver := &agentGroupRouteResolverStub{defaultRoute: &channel.ResolvedRoute{PlatformModelName: "default-model"}}
	service, conversation := newAgentGroupSnapshotService("conversation-model", members, roles, resolver)

	snapshot, err := service.buildAgentGroupRunSnapshot(context.Background(), SendMessageInput{
		UserID: 11, ConversationID: 22, RequestID: "request-1",
	}, conversation)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if snapshot.Supervisor.EffectiveModel != "member-model" {
		t.Fatalf("member override = %q", snapshot.Supervisor.EffectiveModel)
	}
	if snapshot.Supervisor.ReasoningEffort != "max" {
		t.Fatalf("reasoning effort = %q", snapshot.Supervisor.ReasoningEffort)
	}
	if snapshot.Members[0].EffectiveModel != "role-model" {
		t.Fatalf("role model = %q", snapshot.Members[0].EffectiveModel)
	}
	if snapshot.Members[1].EffectiveModel != "conversation-model" {
		t.Fatalf("conversation model = %q", snapshot.Members[1].EffectiveModel)
	}
	if len(resolver.defaultCalls) != 0 {
		t.Fatalf("default route should not resolve when all members have explicit effective models: %#v", resolver.defaultCalls)
	}
}

func TestBuildAgentGroupRunSnapshotUsesDefaultModelOnce(t *testing.T) {
	members := []domainagentgroup.Member{
		{ID: 1, PublicID: "member-supervisor", RolePublicID: "role-supervisor", MemberType: domainagentgroup.MemberTypeSupervisor, Enabled: true},
		{ID: 2, PublicID: "member-worker", RolePublicID: "role-worker", MemberType: domainagentgroup.MemberTypeWorker, Enabled: true},
	}
	roles := map[string]*domainconversation.ConversationRole{
		"role-supervisor": {ID: 11, PublicID: "role-supervisor", Name: "Supervisor"},
		"role-worker":     {ID: 12, PublicID: "role-worker", Name: "Worker"},
	}
	resolver := &agentGroupRouteResolverStub{defaultRoute: &channel.ResolvedRoute{PlatformModelName: " default-model "}}
	service, conversation := newAgentGroupSnapshotService("", members, roles, resolver)

	snapshot, err := service.buildAgentGroupRunSnapshot(context.Background(), SendMessageInput{
		UserID: 11, ConversationID: 22, RequestID: " request-1 ",
	}, conversation)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if snapshot.Supervisor.EffectiveModel != "default-model" || snapshot.Members[0].EffectiveModel != "default-model" {
		t.Fatalf("default model was not frozen for all empty members: %#v", snapshot)
	}
	if len(resolver.defaultCalls) != 1 {
		t.Fatalf("default route calls = %d, want 1", len(resolver.defaultCalls))
	}
	call := resolver.defaultCalls[0]
	if call.TaskType != channel.TaskTypeChat || call.Scope != channel.RouteScopeUser || call.UserID != 11 || call.ConversationID != 22 || call.RequestID != "request-1" {
		t.Fatalf("unexpected default route input: %#v", call)
	}
}

func TestBuildAgentGroupRunSnapshotDoesNotFallbackExplicitInvalidModel(t *testing.T) {
	members := []domainagentgroup.Member{
		{ID: 1, PublicID: "member-supervisor", RolePublicID: "role-supervisor", MemberType: domainagentgroup.MemberTypeSupervisor, Enabled: true, ModelOverride: "stale-model"},
	}
	roles := map[string]*domainconversation.ConversationRole{
		"role-supervisor": {ID: 11, PublicID: "role-supervisor", Name: "Supervisor"},
	}
	resolver := &agentGroupRouteResolverStub{defaultRoute: &channel.ResolvedRoute{PlatformModelName: "default-model"}}
	service, conversation := newAgentGroupSnapshotService("", members, roles, resolver)

	snapshot, err := service.buildAgentGroupRunSnapshot(context.Background(), SendMessageInput{UserID: 11, ConversationID: 22}, conversation)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if snapshot.Supervisor.EffectiveModel != "stale-model" {
		t.Fatalf("explicit model should remain fail-closed for execution, got %q", snapshot.Supervisor.EffectiveModel)
	}
	if len(resolver.defaultCalls) != 0 || len(resolver.routeCalls) != 0 {
		t.Fatalf("snapshot must not validate or replace explicit models: default=%d route=%d", len(resolver.defaultCalls), len(resolver.routeCalls))
	}

	_, err = service.resolveAgentTurnRoute(context.Background(), channel.ResolveRouteInput{
		PlatformModelName: snapshot.Supervisor.EffectiveModel,
		TaskType:          channel.TaskTypeChat,
		Scope:             channel.RouteScopeUser,
		UserID:            11,
		ConversationID:    22,
	})
	if !errors.Is(err, ErrModelRouteNotConfigured) {
		t.Fatalf("explicit invalid model should fail, got %v", err)
	}
	if len(resolver.defaultCalls) != 0 || len(resolver.routeCalls) != 1 {
		t.Fatalf("explicit invalid model must not fall back: default=%d route=%d", len(resolver.defaultCalls), len(resolver.routeCalls))
	}
}

func TestBuildAgentGroupRunSnapshotMapsDefaultRouteErrors(t *testing.T) {
	members := []domainagentgroup.Member{
		{ID: 1, PublicID: "member-supervisor", RolePublicID: "role-supervisor", MemberType: domainagentgroup.MemberTypeSupervisor, Enabled: true},
	}
	roles := map[string]*domainconversation.ConversationRole{
		"role-supervisor": {ID: 11, PublicID: "role-supervisor", Name: "Supervisor"},
	}
	resolver := &agentGroupRouteResolverStub{defaultErr: channel.ErrRouteNotFound}
	service, conversation := newAgentGroupSnapshotService("", members, roles, resolver)

	_, err := service.buildAgentGroupRunSnapshot(context.Background(), SendMessageInput{UserID: 11, ConversationID: 22}, conversation)
	if !errors.Is(err, ErrModelRouteNotConfigured) {
		t.Fatalf("expected model route error, got %v", err)
	}
}
