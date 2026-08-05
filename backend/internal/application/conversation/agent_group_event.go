package conversation

import (
	"context"
	"fmt"
	"strings"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/traceid"
	"go.uber.org/zap"
)

// Agent 群组流式事件类型（chapter 15 协议）。
// 事件 key 统一使用 camelCase：groupRunID/stepID/attemptID/sequence/actorMemberID/actorName/...。
const (
	AgentGroupEventStepStarted      = "group_step_started"
	AgentGroupEventStepRetryStarted = "group_step_retry_started"
	AgentGroupEventStepDelta        = "group_step_output_delta"
	AgentGroupEventStepCompleted    = "group_step_completed"
	AgentGroupEventStepFailed       = "group_step_failed"
	AgentGroupEventRunPaused        = "group_run_paused"
	AgentGroupEventRunAbandoned     = "group_run_abandoned"
	AgentGroupEventRunCompleted     = "group_run_completed"
)

// agentGroupActorPayload 返回群组事件的公共 Actor 载荷。
func agentGroupActorPayload(run *domainagentgroup.Run, step *domainagentgroup.Step, attempt *domainagentgroup.Attempt, member *domainagentgroup.RunSnapshotMember) map[string]interface{} {
	payload := map[string]interface{}{
		"groupRunID":    run.PublicID,
		"status":        run.Status,
		"stepType":      "",
		"sequence":      0,
		"actorType":     "",
		"actorIcon":     "",
		"actorColor":    "",
		"model":         "",
		"attemptNumber": 0,
	}
	if step != nil {
		payload["stepID"] = step.PublicID
		payload["stepType"] = step.StepType
		payload["sequence"] = step.Sequence
	}
	if attempt != nil {
		payload["attemptID"] = attempt.PublicID
		payload["attemptNumber"] = attempt.AttemptNo
	}
	if member != nil {
		payload["actorMemberID"] = member.PublicID
		payload["actorName"] = member.RoleName
		payload["actorType"] = member.MemberType
		payload["actorIcon"] = member.Icon
		payload["actorColor"] = member.Color
		payload["model"] = member.EffectiveModel
	}
	return payload
}

// emitAgentGroupStepStarted 发布 group_step_started 事件。
func (st *agentGroupRunState) emitAgentGroupStepStarted(ctx context.Context, step *domainagentgroup.Step, attempt *domainagentgroup.Attempt, member *domainagentgroup.RunSnapshotMember) {
	st.emitAgentGroupEvent(ctx, AgentGroupEventStepStarted, agentGroupActorPayload(st.run, step, attempt, member))
}

// emitAgentGroupStepCompleted 发布 group_step_completed 事件；Stream=false 时由调用方补发完整输出。
func (st *agentGroupRunState) emitAgentGroupStepCompleted(ctx context.Context, step *domainagentgroup.Step, attempt *domainagentgroup.Attempt, member *domainagentgroup.RunSnapshotMember, output *AgentTurnOutput) {
	payload := agentGroupActorPayload(st.run, step, attempt, member)
	payload["status"] = domainagentgroup.StepStatusSuccess
	if output != nil {
		if text := strings.TrimSpace(output.Text); text != "" {
			payload["outputMarkdown"] = text
		}
	}
	st.emitAgentGroupEvent(ctx, AgentGroupEventStepCompleted, payload)
}

// emitAgentGroupStepFailed 发布 group_step_failed 事件。
// 事件只携带通用错误码与简短说明，不暴露上游细节/诊断信息。
func (st *agentGroupRunState) emitAgentGroupStepFailed(ctx context.Context, step *domainagentgroup.Step, attempt *domainagentgroup.Attempt, member *domainagentgroup.RunSnapshotMember, code string, message string) {
	payload := agentGroupActorPayload(st.run, step, attempt, member)
	payload["status"] = step.Status
	payload["errorCode"] = code
	if message != "" {
		payload["message"] = message
	}
	st.emitAgentGroupEvent(ctx, AgentGroupEventStepFailed, payload)
}

// emitAgentGroupStepRetryStarted 发布 group_step_retry_started 事件（重试尝试开始时）。
func (st *agentGroupRunState) emitAgentGroupStepRetryStarted(ctx context.Context, step *domainagentgroup.Step, attempt *domainagentgroup.Attempt, member *domainagentgroup.RunSnapshotMember) {
	payload := agentGroupActorPayload(st.run, step, attempt, member)
	payload["status"] = domainagentgroup.StepStatusRunning
	st.emitAgentGroupEvent(ctx, AgentGroupEventStepRetryStarted, payload)
}

// emitAgentGroupRunAbandoned 发布 group_run_abandoned 事件。
func (st *agentGroupRunState) emitAgentGroupRunAbandoned(ctx context.Context) {
	payload := agentGroupActorPayload(st.run, nil, nil, nil)
	payload["status"] = domainagentgroup.RunStatusAbandoned
	st.emitAgentGroupEvent(ctx, AgentGroupEventRunAbandoned, payload)
}

// emitAgentGroupRunPaused 发布 group_run_paused 事件（含 blocked 场景，通过 status 区分）。
func (st *agentGroupRunState) emitAgentGroupRunPaused(ctx context.Context, step *domainagentgroup.Step, runStatus string, code string) {
	payload := agentGroupActorPayload(st.run, step, nil, nil)
	payload["status"] = runStatus
	if code != "" {
		payload["errorCode"] = code
	}
	st.emitAgentGroupEvent(ctx, AgentGroupEventRunPaused, payload)
}

// emitAgentGroupRunCompleted 发布 group_run_completed 事件。
func (st *agentGroupRunState) emitAgentGroupRunCompleted(ctx context.Context) {
	payload := agentGroupActorPayload(st.run, nil, nil, nil)
	payload["status"] = domainagentgroup.RunStatusCompleted
	if answer := strings.TrimSpace(st.finalAnswer); answer != "" {
		payload["answer"] = answer
	}
	payload["stepCount"] = st.completedStepCount
	st.emitAgentGroupEvent(ctx, AgentGroupEventRunCompleted, payload)
}

// emitAgentGroupEvent 将群组事件推送到输入流；OnEvent 为空时静默跳过。
func (st *agentGroupRunState) emitAgentGroupEvent(ctx context.Context, eventType string, payload map[string]interface{}) {
	if st == nil || st.input.OnEvent == nil {
		return
	}
	if err := st.input.OnEvent(eventType, payload); err != nil {
		st.service.logger.Warn("agent_group_event_forward_failed",
			zap.String("trace_id", traceid.FromContext(ctx)),
			zap.String("event", eventType),
			zap.Uint("group_run_id", st.run.ID),
			zap.Error(err),
		)
	}
}

// forwardAgentGroupTurnEvent 将内部 Actor 回合事件归一化后转发为群组流式事件。
// key 归一化：actor_id→actorMemberID 等；delta→group_step_output_delta、thinking→upstream_think_delta；
// 其余事件（status/usage/tool_call/tool_result）原样转发并补群组上下文。
func (st *agentGroupRunState) forwardAgentGroupTurnEvent(step *domainagentgroup.Step, attempt *domainagentgroup.Attempt, member *domainagentgroup.RunSnapshotMember) func(AgentTurnEvent) error {
	return func(event AgentTurnEvent) error {
		if st == nil || st.input.OnEvent == nil {
			return nil
		}
		payload := map[string]interface{}{
			"groupRunID": st.run.PublicID,
		}
		if step != nil {
			payload["stepID"] = step.PublicID
			payload["stepType"] = step.StepType
			payload["sequence"] = step.Sequence
		}
		if attempt != nil {
			payload["attemptID"] = attempt.PublicID
			payload["attemptNumber"] = attempt.AttemptNo
		}
		if member != nil {
			payload["actorMemberID"] = member.PublicID
			payload["actorName"] = member.RoleName
			payload["actorType"] = member.MemberType
			payload["actorIcon"] = member.Icon
			payload["actorColor"] = member.Color
			payload["model"] = member.EffectiveModel
		}
		eventType := event.Type
		switch event.Type {
		case AgentTurnEventDelta:
			eventType = AgentGroupEventStepDelta
		case AgentTurnEventThinking:
			eventType = "upstream_think_delta"
		}
		for key, value := range event.Payload {
			normalized := key
			switch key {
			case "actor_id":
				normalized = "actorMemberID"
			case "actor_name":
				normalized = "actorName"
			case "actor_type":
				normalized = "actorType"
			case "actor_icon":
				normalized = "actorIcon"
			case "actor_color":
				normalized = "actorColor"
			}
			payload[normalized] = value
		}
		return st.input.OnEvent(eventType, payload)
	}
}

// agentGroupContextSummary 描述一步已完成步骤的摘要（供主管/成员上下文复用）。
type agentGroupContextSummary struct {
	sequence      int
	stepType      string
	actorName     string
	instruction   string
	outputSummary string
}

// agentGroupContextBrief 将已完成步骤摘要渲染为上下文文本。
func agentGroupContextBrief(summaries []agentGroupContextSummary) string {
	if len(summaries) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("<completed_steps>\n")
	for _, item := range summaries {
		actorRole := "supervisor"
		if item.stepType == domainagentgroup.StepTypeMemberExecute {
			actorRole = "member"
		}
		fmt.Fprintf(&builder, "  <step sequence=\"%d\" actor=\"%s\" role=\"%s\" instruction=\"%s\">\n",
			item.sequence, xmlEscapeText(item.actorName), actorRole, xmlEscapeText(item.instruction))
		if text := strings.TrimSpace(item.outputSummary); text != "" {
			builder.WriteString("    " + strings.TrimSpace(text) + "\n")
		}
		builder.WriteString("  </step>\n")
	}
	builder.WriteString("</completed_steps>")
	return builder.String()
}

// agentGroupMemberSummaries 渲染可指派成员清单。
func agentGroupMemberSummaries(members []domainagentgroup.RunSnapshotMember) string {
	var builder strings.Builder
	builder.WriteString("<members>\n")
	for _, member := range members {
		if member.MemberType != domainagentgroup.MemberTypeWorker || !member.Enabled {
			continue
		}
		fmt.Fprintf(&builder, "  <member memberID=\"%s\" name=\"%s\" model=\"%s\">%s</member>\n",
			xmlEscapeText(member.PublicID), xmlEscapeText(member.RoleName), xmlEscapeText(member.EffectiveModel), xmlEscapeText(strings.TrimSpace(member.DutyInstruction)))
	}
	builder.WriteString("</members>")
	return builder.String()
}
