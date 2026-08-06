package agentgroup

import (
	"time"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
)

// AgentGroupMemberResponse 对外成员响应 DTO。
type AgentGroupMemberResponse struct {
	PublicID        string    `json:"publicID"`
	RolePublicID    string    `json:"rolePublicID"`
	RoleName        string    `json:"roleName"`
	RoleIcon        string    `json:"roleIcon"`
	RoleColor       string    `json:"roleColor"`
	RoleModel       string    `json:"roleModel"`
	RoleProvider    string    `json:"roleProvider"`
	MemberType      string    `json:"memberType"`
	Enabled         bool      `json:"enabled"`
	ModelOverride   string    `json:"modelOverride"`
	ReasoningEffort string    `json:"reasoningEffort"`
	DutyInstruction string    `json:"dutyInstruction"`
	SortOrder       int       `json:"sortOrder"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func toAgentGroupMemberResponse(item domainagentgroup.Member) AgentGroupMemberResponse {
	return AgentGroupMemberResponse{
		PublicID:        item.PublicID,
		RolePublicID:    item.RolePublicID,
		RoleName:        item.RoleName,
		RoleIcon:        item.RoleIcon,
		RoleColor:       item.RoleColor,
		RoleModel:       item.RoleModel,
		RoleProvider:    item.RoleProvider,
		MemberType:      item.MemberType,
		Enabled:         item.Enabled,
		ModelOverride:   item.ModelOverride,
		ReasoningEffort: item.ReasoningEffort,
		DutyInstruction: item.DutyInstruction,
		SortOrder:       item.SortOrder,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
}

// AgentGroupResponse 对外群组响应 DTO。
type AgentGroupResponse struct {
	PublicID           string                     `json:"publicID"`
	ProjectID          string                     `json:"projectID"`
	ProjectName        string                     `json:"projectName"`
	Name               string                     `json:"name"`
	Description        string                     `json:"description"`
	CoordinationPrompt string                     `json:"coordinationPrompt"`
	SupervisorMemberID string                     `json:"supervisorMemberID"`
	SortOrder          int                        `json:"sortOrder"`
	Status             string                     `json:"status"`
	Revision           int                        `json:"revision"`
	Members            []AgentGroupMemberResponse `json:"members"`
	CreatedAt          time.Time                  `json:"createdAt"`
	UpdatedAt          time.Time                  `json:"updatedAt"`
}

func toAgentGroupResponse(item *domainagentgroup.Group) AgentGroupResponse {
	result := AgentGroupResponse{
		PublicID:           item.PublicID,
		ProjectID:          item.ProjectPublicID,
		ProjectName:        item.ProjectName,
		Name:               item.Name,
		Description:        item.Description,
		CoordinationPrompt: item.CoordinationPrompt,
		SortOrder:          item.SortOrder,
		Status:             item.Status,
		Revision:           item.Revision,
		Members:            make([]AgentGroupMemberResponse, 0, len(item.Members)),
		CreatedAt:          item.CreatedAt,
		UpdatedAt:          item.UpdatedAt,
	}
	for _, member := range item.Members {
		if member.MemberType == domainagentgroup.MemberTypeSupervisor {
			result.SupervisorMemberID = member.PublicID
		}
		result.Members = append(result.Members, toAgentGroupMemberResponse(member))
	}
	return result
}

// AgentGroupRunResponse 对外运行响应 DTO。
type AgentGroupRunResponse struct {
	PublicID       string     `json:"publicID"`
	ClientRunID    string     `json:"clientRunID"`
	ConversationID uint       `json:"conversationID"`
	GroupPublicID  string     `json:"groupPublicID"`
	Status         string     `json:"status"`
	ErrorCode      string     `json:"errorCode"`
	ErrorMessage   string     `json:"errorMessage"`
	StartedAt      time.Time  `json:"startedAt"`
	EndedAt        *time.Time `json:"endedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

func toAgentGroupRunResponse(item *domainagentgroup.Run) AgentGroupRunResponse {
	return AgentGroupRunResponse{
		PublicID:       item.PublicID,
		ClientRunID:    item.ClientRunID,
		ConversationID: item.ConversationID,
		GroupPublicID:  item.GroupPublicID,
		Status:         item.Status,
		ErrorCode:      item.ErrorCode,
		ErrorMessage:   item.ErrorMessage,
		StartedAt:      item.StartedAt,
		EndedAt:        item.EndedAt,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
	}
}

// AgentGroupStepAttemptResponse 对外尝试响应 DTO。
type AgentGroupStepAttemptResponse struct {
	PublicID       string     `json:"publicID"`
	AttemptNo      int        `json:"attemptNo"`
	RequestedModel string     `json:"requestedModel"`
	ResolvedModel  string     `json:"resolvedModel"`
	Status         string     `json:"status"`
	ErrorCode      string     `json:"errorCode"`
	ErrorMessage   string     `json:"errorMessage"`
	OutputMarkdown string     `json:"outputMarkdown"`
	StartedAt      time.Time  `json:"startedAt"`
	EndedAt        *time.Time `json:"endedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
}

func toAgentGroupStepAttemptResponse(item *domainagentgroup.Attempt) AgentGroupStepAttemptResponse {
	return AgentGroupStepAttemptResponse{
		PublicID:       item.PublicID,
		AttemptNo:      item.AttemptNo,
		RequestedModel: item.RequestedModel,
		ResolvedModel:  item.ResolvedModel,
		Status:         item.Status,
		ErrorCode:      item.ErrorCode,
		ErrorMessage:   item.ErrorMessage,
		OutputMarkdown: item.OutputMarkdown,
		StartedAt:      item.StartedAt,
		EndedAt:        item.EndedAt,
		CreatedAt:      item.CreatedAt,
	}
}

// AgentGroupStepResponse 对外步骤响应 DTO。
type AgentGroupStepResponse struct {
	PublicID            string                          `json:"publicID"`
	Sequence            int                             `json:"sequence"`
	StepType            string                          `json:"stepType"`
	ActorMemberPublicID string                          `json:"actorMemberPublicID"`
	ActorNameSnapshot   string                          `json:"actorNameSnapshot"`
	ActorTypeSnapshot   string                          `json:"actorTypeSnapshot"`
	Status              string                          `json:"status"`
	Instruction         string                          `json:"instruction"`
	Attempts            []AgentGroupStepAttemptResponse `json:"attempts"`
	CreatedAt           time.Time                       `json:"createdAt"`
	UpdatedAt           time.Time                       `json:"updatedAt"`
}

func toAgentGroupStepResponse(item *domainagentgroup.Step, attempts []domainagentgroup.Attempt) AgentGroupStepResponse {
	result := AgentGroupStepResponse{
		PublicID:            item.PublicID,
		Sequence:            item.Sequence,
		StepType:            item.StepType,
		ActorMemberPublicID: item.ActorMemberPublicID,
		ActorNameSnapshot:   item.ActorNameSnapshot,
		ActorTypeSnapshot:   item.ActorTypeSnapshot,
		Status:              item.Status,
		Instruction:         item.Instruction,
		Attempts:            make([]AgentGroupStepAttemptResponse, 0, len(attempts)),
		CreatedAt:           item.CreatedAt,
		UpdatedAt:           item.UpdatedAt,
	}
	for _, attempt := range attempts {
		result.Attempts = append(result.Attempts, toAgentGroupStepAttemptResponse(&attempt))
	}
	return result
}

// AgentGroupRunDetailResponse 对外运行详情响应 DTO。
type AgentGroupRunDetailResponse struct {
	Run   AgentGroupRunResponse    `json:"run"`
	Steps []AgentGroupStepResponse `json:"steps"`
}

// AgentGroupFeatureResponse 功能开关响应 DTO。
type AgentGroupFeatureResponse struct {
	Enabled bool `json:"enabled"`
}

// AgentGroupRunCancelResponse 取消运行响应 DTO。
type AgentGroupRunCancelResponse struct {
	Canceled bool `json:"canceled"`
}

// AgentGroupRunAbandonResponse 放弃运行响应 DTO。
type AgentGroupRunAbandonResponse struct {
	Status string `json:"status"`
}

// AgentGroupRunControlResult 重试运行的最终结果 DTO（NDJSON completed 事件的 data）。
type AgentGroupRunControlResult struct {
	Status string `json:"status"`
}

// ---- swagger 文档类型 ----

// ErrorDoc 错误响应文档。
type ErrorDoc struct {
	ErrorMsg  string      `json:"errorMsg"`
	ErrorCode string      `json:"errorCode,omitempty"`
	Details   interface{} `json:"details,omitempty"`
	RequestID string      `json:"requestId,omitempty"`
	Data      interface{} `json:"data"`
}

// AgentGroupResponseDoc 群组响应文档。
type AgentGroupResponseDoc struct {
	ErrorMsg string             `json:"errorMsg"`
	Data     AgentGroupResponse `json:"data"`
}

// AgentGroupListResponseDoc 群组列表响应文档。
type AgentGroupListResponseDoc struct {
	ErrorMsg string               `json:"errorMsg"`
	Data     []AgentGroupResponse `json:"data"`
}

// AgentGroupRunDetailResponseDoc 运行详情响应文档。
type AgentGroupRunDetailResponseDoc struct {
	ErrorMsg string                      `json:"errorMsg"`
	Data     AgentGroupRunDetailResponse `json:"data"`
}

// AgentGroupFeatureResponseDoc 功能开关响应文档。
type AgentGroupFeatureResponseDoc struct {
	ErrorMsg string                    `json:"errorMsg"`
	Data     AgentGroupFeatureResponse `json:"data"`
}

// AgentGroupRunCancelResponseDoc 取消运行响应文档。
type AgentGroupRunCancelResponseDoc struct {
	ErrorMsg string                      `json:"errorMsg"`
	Data     AgentGroupRunCancelResponse `json:"data"`
}

// AgentGroupRunAbandonResponseDoc 放弃运行响应文档。
type AgentGroupRunAbandonResponseDoc struct {
	ErrorMsg string                       `json:"errorMsg"`
	Data     AgentGroupRunAbandonResponse `json:"data"`
}
