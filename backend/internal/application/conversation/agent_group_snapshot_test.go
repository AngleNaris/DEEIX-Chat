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
	conversation    *domainconversation.Conversation
	roles           map[string]*domainconversation.ConversationRole
	pairCreateCalls int
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

func (r *agentGroupSnapshotRepositoryStub) CreateMessagePairWithUserAttachments(
	context.Context,
	*domainconversation.Message,
	*domainconversation.Message,
	[]domainconversation.Attachment,
) error {
	r.pairCreateCalls++
	return nil
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
	defaultModel    string
	defaultErr      error
	validModels     map[string]struct{}
	validationErrs  map[string]error
	defaultCalls    []channel.ResolveRouteInput
	validationCalls []channel.ResolveRouteInput
	routeCalls      []channel.ResolveRouteInput
}

func (r *agentGroupRouteResolverStub) ResolveDefaultModel(_ context.Context, input channel.ResolveRouteInput) (string, error) {
	r.defaultCalls = append(r.defaultCalls, input)
	return r.defaultModel, r.defaultErr
}

func (r *agentGroupRouteResolverStub) ValidateModelRouteReference(_ context.Context, input channel.ResolveRouteInput) error {
	r.validationCalls = append(r.validationCalls, input)
	if err := r.validationErrs[input.PlatformModelName]; err != nil {
		return err
	}
	if _, ok := r.validModels[input.PlatformModelName]; ok {
		return nil
	}
	return channel.ErrModelNotFound
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
	createCalls int
}

func (*agentGroupRunStoreStub) GetActiveAgentGroupRunByConversation(context.Context, uint) (*domainagentgroup.Run, error) {
	return nil, repository.ErrNotFound
}

func (*agentGroupRunStoreStub) GetAgentGroupRunByClientRunID(context.Context, uint, string) (*domainagentgroup.Run, error) {
	return nil, repository.ErrNotFound
}

func (r *agentGroupRunStoreStub) CreateAgentGroupRun(context.Context, *domainagentgroup.Run) error {
	r.createCalls++
	return nil
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

func TestSendMessageInternalRejectsInvalidGroupModelBeforePersistence(t *testing.T) {
	members := []domainagentgroup.Member{
		{ID: 1, PublicID: "member-supervisor", RolePublicID: "role-supervisor", MemberType: domainagentgroup.MemberTypeSupervisor, Enabled: true, ModelOverride: "stale-model"},
	}
	roles := map[string]*domainconversation.ConversationRole{
		"role-supervisor": {ID: 11, PublicID: "role-supervisor", Name: "Supervisor"},
	}
	resolver := &agentGroupRouteResolverStub{}
	service, _ := newAgentGroupSnapshotService("", members, roles, resolver)
	repo := service.repo.(*agentGroupSnapshotRepositoryStub)
	runStore := &agentGroupRunStoreStub{}
	service.agentGroupRunStore = runStore
	service.agentGroupSettings = &agentGroupRetrySettingsFake{values: map[string]string{
		domainagentgroup.FeatureFlagKeyEnabled: "true",
	}}

	_, err := service.sendMessageInternal(context.Background(), SendMessageInput{
		UserID: 11, ConversationID: 22, ClientRunID: "run_invalid-model", Content: "hello",
	}, nil, false)
	if !errors.Is(err, ErrModelRouteNotConfigured) {
		t.Fatalf("expected model route error, got %v", err)
	}
	if repo.pairCreateCalls != 0 || runStore.createCalls != 0 {
		t.Fatalf("invalid model persisted state: message_pairs=%d group_runs=%d", repo.pairCreateCalls, runStore.createCalls)
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
	resolver := &agentGroupRouteResolverStub{
		defaultModel: "default-model",
		validModels: map[string]struct{}{
			"member-model":       {},
			"role-model":         {},
			"conversation-model": {},
		},
	}
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
	if len(resolver.validationCalls) != 3 {
		t.Fatalf("explicit route validation calls = %d, want 3", len(resolver.validationCalls))
	}
	for _, call := range resolver.validationCalls {
		if call.TaskType != channel.TaskTypeChat || call.Scope != channel.RouteScopeUser || call.UserID != 11 || call.ConversationID != 22 || call.RequestID != "request-1" {
			t.Fatalf("unexpected explicit route input: %#v", call)
		}
	}
	if len(resolver.routeCalls) != 0 {
		t.Fatalf("snapshot preflight selected executable routes: %#v", resolver.routeCalls)
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
	resolver := &agentGroupRouteResolverStub{defaultModel: " default-model "}
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
	if len(resolver.routeCalls) != 0 || len(resolver.validationCalls) != 0 {
		t.Fatalf("default model preflight selected or revalidated a route: route=%d validation=%d", len(resolver.routeCalls), len(resolver.validationCalls))
	}
	call := resolver.defaultCalls[0]
	if call.TaskType != channel.TaskTypeChat || call.Scope != channel.RouteScopeUser || call.UserID != 11 || call.ConversationID != 22 || call.RequestID != "request-1" {
		t.Fatalf("unexpected default route input: %#v", call)
	}
}

func TestBuildAgentGroupRunSnapshotValidatesSharedExplicitModelOnce(t *testing.T) {
	members := []domainagentgroup.Member{
		{ID: 1, PublicID: "member-supervisor", RolePublicID: "role-supervisor", MemberType: domainagentgroup.MemberTypeSupervisor, Enabled: true},
		{ID: 2, PublicID: "member-worker", RolePublicID: "role-worker", MemberType: domainagentgroup.MemberTypeWorker, Enabled: true},
	}
	roles := map[string]*domainconversation.ConversationRole{
		"role-supervisor": {ID: 11, PublicID: "role-supervisor", Name: "Supervisor", Model: "shared-model"},
		"role-worker":     {ID: 12, PublicID: "role-worker", Name: "Worker", Model: "shared-model"},
	}
	resolver := &agentGroupRouteResolverStub{validModels: map[string]struct{}{"shared-model": {}}}
	service, conversation := newAgentGroupSnapshotService("", members, roles, resolver)

	snapshot, err := service.buildAgentGroupRunSnapshot(context.Background(), SendMessageInput{
		UserID: 11, ConversationID: 22,
	}, conversation)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if snapshot.Supervisor.EffectiveModel != "shared-model" || snapshot.Members[0].EffectiveModel != "shared-model" {
		t.Fatalf("shared model was not frozen: %#v", snapshot)
	}
	if len(resolver.validationCalls) != 1 || len(resolver.routeCalls) != 0 {
		t.Fatalf("shared model preflight was not deduplicated: validation=%d route=%d", len(resolver.validationCalls), len(resolver.routeCalls))
	}
}

func TestBuildAgentGroupRunSnapshotRejectsExplicitInvalidModel(t *testing.T) {
	members := []domainagentgroup.Member{
		{ID: 1, PublicID: "member-supervisor", RolePublicID: "role-supervisor", MemberType: domainagentgroup.MemberTypeSupervisor, Enabled: true, ModelOverride: "stale-model"},
	}
	roles := map[string]*domainconversation.ConversationRole{
		"role-supervisor": {ID: 11, PublicID: "role-supervisor", Name: "Supervisor"},
	}
	resolver := &agentGroupRouteResolverStub{defaultModel: "default-model"}
	service, conversation := newAgentGroupSnapshotService("", members, roles, resolver)

	_, err := service.buildAgentGroupRunSnapshot(context.Background(), SendMessageInput{
		UserID: 11, ConversationID: 22, RequestID: "request-1",
	}, conversation)
	if !errors.Is(err, ErrModelRouteNotConfigured) {
		t.Fatalf("explicit invalid model should fail before run creation, got %v", err)
	}
	if len(resolver.defaultCalls) != 0 || len(resolver.validationCalls) != 1 || len(resolver.routeCalls) != 0 {
		t.Fatalf("explicit invalid model must not fall back or resolve a route: default=%d validation=%d route=%d", len(resolver.defaultCalls), len(resolver.validationCalls), len(resolver.routeCalls))
	}
	call := resolver.validationCalls[0]
	if call.PlatformModelName != "stale-model" || call.TaskType != channel.TaskTypeChat || call.Scope != channel.RouteScopeUser || call.UserID != 11 || call.ConversationID != 22 || call.RequestID != "request-1" {
		t.Fatalf("unexpected explicit route input: %#v", call)
	}
}

func TestBuildAgentGroupRunSnapshotMapsExplicitRouteErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "access denied", err: channel.ErrModelAccessDenied, want: ErrModelAccessDenied},
		{name: "all routes unavailable", err: channel.ErrAllRoutesUnavailable, want: ErrUpstreamRequestFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			members := []domainagentgroup.Member{
				{ID: 1, PublicID: "member-supervisor", RolePublicID: "role-supervisor", MemberType: domainagentgroup.MemberTypeSupervisor, Enabled: true, ModelOverride: "configured-model"},
			}
			roles := map[string]*domainconversation.ConversationRole{
				"role-supervisor": {ID: 11, PublicID: "role-supervisor", Name: "Supervisor"},
			}
			resolver := &agentGroupRouteResolverStub{validationErrs: map[string]error{"configured-model": tt.err}}
			service, conversation := newAgentGroupSnapshotService("", members, roles, resolver)

			_, err := service.buildAgentGroupRunSnapshot(context.Background(), SendMessageInput{
				UserID: 11, ConversationID: 22,
			}, conversation)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if len(resolver.defaultCalls) != 0 || len(resolver.validationCalls) != 1 || len(resolver.routeCalls) != 0 {
				t.Fatalf("unexpected validation effects: default=%d validation=%d route=%d", len(resolver.defaultCalls), len(resolver.validationCalls), len(resolver.routeCalls))
			}
		})
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
