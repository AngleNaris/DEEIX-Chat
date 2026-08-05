package conversation

import (
	"errors"
	"strings"
	"testing"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
)

// agentGroupPromptTestSnapshot 构造测试快照：nWorkers 个启用的 worker 成员 + 一个主管。
func agentGroupPromptTestSnapshot(nWorkers int) *domainagentgroup.RunSnapshot {
	snapshot := &domainagentgroup.RunSnapshot{
		Supervisor: domainagentgroup.RunSnapshotMember{
			PublicID:   "sup-00000000000000000000000000000000",
			RoleName:   "AI音乐团队主管",
			MemberType: domainagentgroup.MemberTypeSupervisor,
			Enabled:    true,
		},
	}
	for i := 0; i < nWorkers; i++ {
		snapshot.Members = append(snapshot.Members, domainagentgroup.RunSnapshotMember{
			PublicID:   strings.Repeat(string(rune('a'+i)), 32),
			RoleName:   []string{"歌词创作专家", "音乐风格设计师", "质量审核官"}[i],
			MemberType: domainagentgroup.MemberTypeWorker,
			Enabled:    true,
		})
	}
	return snapshot
}

// TestValidateAgentGroupDelegation_UniqueWorkerFallback 验证唯一 worker 兜底：
// 模型把中文角色名翻译成英文（如 lyricist）导致 memberID 无法解析时，
// 若清单中启用 worker 恰好唯一，采用该成员并归一化 decision.MemberID。
func TestValidateAgentGroupDelegation_UniqueWorkerFallback(t *testing.T) {
	decision := &agentGroupSupervisorDecision{
		Action:          agentGroupSupervisorActionDelegate,
		MemberID:        "lyricist", // 翻译名，不在清单中
		Instruction:     "请创作一首中文流行歌曲歌词",
		ExpectedOutcome: "完整的歌词文本",
	}
	if err := validateAgentGroupDelegation(agentGroupPromptTestSnapshot(1), decision); err != nil {
		t.Fatalf("unique worker fallback should accept: %v", err)
	}
	if got := decision.MemberID; got != strings.Repeat("a", 32) {
		t.Fatalf("decision.MemberID not normalized to unique worker PublicID, got %q", got)
	}
}

// TestValidateAgentGroupDelegation_AmbiguousTranslationRejected 验证多 worker 时
// 无法解析的翻译名仍被拒绝（不能猜测指派）。
func TestValidateAgentGroupDelegation_AmbiguousTranslationRejected(t *testing.T) {
	decision := &agentGroupSupervisorDecision{
		Action:      agentGroupSupervisorActionDelegate,
		MemberID:    "lyricist",
		Instruction: "请创作一首中文流行歌曲歌词",
	}
	if err := validateAgentGroupDelegation(agentGroupPromptTestSnapshot(3), decision); err == nil {
		t.Fatal("ambiguous translated memberID should be rejected")
	}
}

// TestValidateAgentGroupDelegation_ChineseNameAccepted 验证中文名原样输出即可解析。
func TestValidateAgentGroupDelegation_ChineseNameAccepted(t *testing.T) {
	snapshot := agentGroupPromptTestSnapshot(3)
	decision := &agentGroupSupervisorDecision{
		Action:      agentGroupSupervisorActionDelegate,
		MemberID:    "歌词创作专家",
		Instruction: "请创作歌词",
	}
	if err := validateAgentGroupDelegation(snapshot, decision); err != nil {
		t.Fatalf("exact RoleName should be accepted: %v", err)
	}
	if got := decision.MemberID; got != strings.Repeat("a", 32) {
		t.Fatalf("decision.MemberID not normalized, got %q", got)
	}
}

// TestAgentGroupSupervisorOutputProtocol_NoEnglishExampleAnchor 防回归：
// 输出协议不得再出现英文角色名示例（如 lyricist），否则会锚定模型输出翻译名。
func TestAgentGroupSupervisorOutputProtocol_NoEnglishExampleAnchor(t *testing.T) {
	lower := strings.ToLower(agentGroupSupervisorOutputProtocol)
	for _, anchor := range []string{"lyricist", "producer", "reviewer", "composer", "writer"} {
		if strings.Contains(lower, anchor) {
			t.Fatalf("output protocol must not contain English role-name example %q (anchors model to translate)", anchor)
		}
	}
}

// TestAgentGroupSupervisorCorrectionHint_ContainsMemberList 验证纠错提示
// 包含成员清单与禁止翻译的指引。
func TestAgentGroupSupervisorCorrectionHint_ContainsMemberList(t *testing.T) {
	snapshot := agentGroupPromptTestSnapshot(3)
	hint := agentGroupSupervisorCorrectionHint(ErrAgentGroupInvalidMember, snapshot.Members)
	for _, member := range snapshot.Members {
		if !strings.Contains(hint, member.PublicID) || !strings.Contains(hint, member.RoleName) {
			t.Fatalf("correction hint must list member %q / %q", member.PublicID, member.RoleName)
		}
	}
	for _, required := range []string{"禁止翻译", "逐字复制"} {
		if !strings.Contains(hint, required) {
			t.Fatalf("correction hint must contain %q", required)
		}
	}
}

func TestAgentGroupSupervisorCorrectionHint_DuplicateDelegation(t *testing.T) {
	snapshot := agentGroupPromptTestSnapshot(1)
	hint := agentGroupSupervisorCorrectionHint(ErrAgentGroupDuplicateDelegation, snapshot.Members)
	for _, required := range []string{"已经成功完成", "<completed_steps>", "禁止再次提交相同委派", "finish"} {
		if !strings.Contains(hint, required) {
			t.Fatalf("duplicate correction hint must contain %q: %s", required, hint)
		}
	}
}

func TestValidateAgentGroupDelegationHistory_RejectsCompletedDuplicate(t *testing.T) {
	memberID := strings.Repeat("a", 32)
	summaries := []agentGroupContextSummary{{
		sequence:      2,
		stepType:      domainagentgroup.StepTypeMemberExecute,
		actorMemberID: memberID,
		actorName:     "歌词创作专家",
		instruction:   "请创作一首完整歌词",
		outputSummary: "完整歌词结果",
	}}
	duplicate := &agentGroupSupervisorDecision{
		Action:      agentGroupSupervisorActionDelegate,
		MemberID:    memberID,
		Instruction: "  请创作一首完整歌词  ",
	}
	if err := validateAgentGroupDelegationHistory(summaries, duplicate); !errors.Is(err, ErrAgentGroupDuplicateDelegation) {
		t.Fatalf("completed duplicate should be rejected, got %v", err)
	}

	revision := *duplicate
	revision.Instruction = "请重写副歌，并增强押韵与记忆点"
	if err := validateAgentGroupDelegationHistory(summaries, &revision); err != nil {
		t.Fatalf("materially different revision instruction should be allowed: %v", err)
	}
}

// TestResolveAgentGroupSupervisorDecision_DelegateWithAnswerNull 回归：
// 模型在 delegate 时附带 "answer": null（schema 允许的可选字段）不应影响解析。
func TestResolveAgentGroupSupervisorDecision_DelegateWithAnswerNull(t *testing.T) {
	raw := `{ "action": "delegate", "memberID": "lyricist", "instruction": "请创作歌词", "expectedOutcome": "歌词文本", "answer": null }`
	decision, err := resolveAgentGroupSupervisorDecision(raw)
	if err != nil {
		t.Fatalf("delegate with answer:null should parse: %v", err)
	}
	if decision.Action != agentGroupSupervisorActionDelegate || decision.MemberID != "lyricist" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

// TestAgentGroupUserOnlyContext 验证群组内部回合上下文只保留用户消息：
// assistant 历史回复（可能声称"无法调用其他成员"/伪造团队成果）必须被剔除，
// 用户消息序列（意图来源）完整保留。
func TestAgentGroupUserOnlyContext(t *testing.T) {
	messages := []domainconversation.Message{
		{Role: "user", Content: "看看流程是否正常"},
		{Role: "assistant", Content: "流程正常"},
		{Role: "user", Content: "先随便创作一首歌"},
		{Role: "assistant", Content: "我无法调用其他独立成员，只能模拟团队分工"},
		{Role: "user", Content: "你要实际走一遍调用群组成员的流程"},
	}
	filtered := agentGroupUserOnlyContext(messages)
	if len(filtered) != 3 {
		t.Fatalf("expected 3 user messages, got %d", len(filtered))
	}
	for _, message := range filtered {
		if message.Role != "user" {
			t.Fatalf("non-user message leaked into internal context: %+v", message)
		}
	}
	if filtered[0].Content != "看看流程是否正常" || filtered[2].Content != "你要实际走一遍调用群组成员的流程" {
		t.Fatalf("user message order/content changed: %+v", filtered)
	}
}

// TestAgentGroupUserOnlyContext_EmptyAndAllAssistant 边界：空输入与全 assistant 输入。
func TestAgentGroupUserOnlyContext_EmptyAndAllAssistant(t *testing.T) {
	if got := agentGroupUserOnlyContext(nil); len(got) != 0 {
		t.Fatalf("nil input should return empty, got %+v", got)
	}
	all := []domainconversation.Message{{Role: "assistant", Content: "a"}, {Role: "assistant", Content: "b"}}
	if got := agentGroupUserOnlyContext(all); len(got) != 0 {
		t.Fatalf("all-assistant input should return empty, got %+v", got)
	}
}

// TestAgentGroupBriefSnippet 验证成员产出头尾双摘：短文本全文保留；
// 长文本保留开头与结尾实质内容并注明中略字数（避免单侧截断丢失核心成果）。
func TestAgentGroupBriefSnippet(t *testing.T) {
	if got := agentGroupBriefSnippet("", 512); got != "" {
		t.Fatalf("empty content should return empty, got %q", got)
	}
	short := "简短产出"
	if got := agentGroupBriefSnippet(short, 512); got != short {
		t.Fatalf("short content should stay intact, got %q", got)
	}
	head := "好的，我马上开始创作这首歌的歌词。"
	body := strings.Repeat("主歌部分月色照亮了窗台，", 100)
	tail := "副歌：风经过的地方都留下我们的名字。"
	long := head + body + tail
	got := agentGroupBriefSnippet(long, 512)
	if !strings.HasPrefix(got, head) {
		t.Fatalf("brief snippet must keep head, got prefix %q", got[:20])
	}
	if !strings.HasSuffix(got, tail) {
		t.Fatalf("brief snippet must keep tail, got suffix %q", got[len(got)-20:])
	}
	if !strings.Contains(got, "中略") {
		t.Fatalf("brief snippet must annotate omitted middle, got %q", got)
	}
	// 内容压缩到上限内，中略标注（"…[中略 N 字]…"）本身有少量字符开销。
	if len([]rune(got)) > 520+16 {
		t.Fatalf("brief snippet exceeds cap, got %d runes", len([]rune(got)))
	}
}

// TestAgentGroupContextBrief_IncludesSupervisorDecision 验证 brief 同时渲染
// 主管决策步骤（委派/完成记录）与成员执行步骤：主管跨轮次决策依赖的
// 委派历史不再缺失。
func TestAgentGroupContextBrief_IncludesSupervisorDecision(t *testing.T) {
	summaries := []agentGroupContextSummary{
		{
			sequence: 3, stepType: domainagentgroup.StepTypeSupervisorDecide,
			actorName: "AI音乐团队主管", instruction: "委派：歌词创作专家",
			outputSummary: "任务指令：请创作一首完整的歌词",
		},
		{
			sequence: 4, stepType: domainagentgroup.StepTypeMemberExecute,
			actorName: "歌词创作专家", instruction: "请创作一首完整的歌词",
			outputSummary: "月色照亮了窗台……",
		},
	}
	brief := agentGroupContextBrief(summaries)
	for _, want := range []string{
		`status="completed"`, `actor="AI音乐团队主管"`, `role="supervisor"`, `instruction="委派：歌词创作专家"`,
		`actor="歌词创作专家"`, `role="member"`, "<result>",
		"任务指令：请创作一首完整的歌词", "月色照亮了窗台",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief missing %q:\n%s", want, brief)
		}
	}
}

func TestAgentGroupContextBrief_PreservesDetailedMemberResult(t *testing.T) {
	detailedResult := strings.Repeat("前奏信息", 100) +
		"\n关键结论：第一位成员已经完成数据库迁移设计，回滚步骤为 restore-v2。\n" +
		strings.Repeat("验收信息", 100)
	brief := agentGroupContextBrief([]agentGroupContextSummary{{
		sequence:      2,
		stepType:      domainagentgroup.StepTypeMemberExecute,
		actorMemberID: strings.Repeat("a", 32),
		actorName:     "后端工程师",
		instruction:   "设计数据库迁移",
		outputSummary: detailedResult,
	}})
	for _, want := range []string{"关键结论", "restore-v2", `status="completed"`} {
		if !strings.Contains(brief, want) {
			t.Fatalf("completed member result must preserve %q:\n%s", want, brief)
		}
	}
}

// TestAgentGroupContextBrief_MaxStepsCap 验证超过 agentGroupContextBriefMaxSteps
// 时仅展示最近步骤并明示省略（对齐任务板"Showing n of m"机制）。
func TestAgentGroupContextBrief_MaxStepsCap(t *testing.T) {
	summaries := make([]agentGroupContextSummary, 0, agentGroupContextBriefMaxSteps+6)
	for i := 1; i <= agentGroupContextBriefMaxSteps+6; i++ {
		summaries = append(summaries, agentGroupContextSummary{
			sequence: i, stepType: domainagentgroup.StepTypeMemberExecute,
			actorName: "成员", instruction: "任务", outputSummary: "产出",
		})
	}
	brief := agentGroupContextBrief(summaries)
	if !strings.Contains(brief, "已完成 30 步") {
		t.Fatalf("brief must annotate total completed steps, got:\n%s", brief)
	}
	if !strings.Contains(brief, "仅展示最近 24 步") {
		t.Fatalf("brief must annotate hidden steps, got:\n%s", brief)
	}
	// 最早 6 步必须隐藏：sequence=1 不再出现，sequence=25 起保留。
	if strings.Contains(brief, `sequence="1"`) || strings.Contains(brief, `sequence="6"`) {
		t.Fatalf("brief must hide oldest steps, got:\n%s", brief)
	}
	if !strings.Contains(brief, `sequence="25"`) || !strings.Contains(brief, `sequence="30"`) {
		t.Fatalf("brief must keep newest steps, got:\n%s", brief)
	}
}

// TestAgentGroupDecisionHeadingAndBriefText 验证主管决策的标题与正文渲染：
// delegate 携带中文成员名与任务指令；finish 携带最终回答；nil 决策防御。
func TestAgentGroupDecisionHeadingAndBriefText(t *testing.T) {
	delegate := &agentGroupSupervisorDecision{
		Action: agentGroupSupervisorActionDelegate, MemberID: "歌词创作专家",
		Instruction: "请基于民谣风格创作完整歌词",
	}
	if got := agentGroupDecisionHeading(delegate); got != "委派：歌词创作专家" {
		t.Fatalf("unexpected heading %q", got)
	}
	if got := agentGroupDecisionBriefText(delegate); got != "任务指令：请基于民谣风格创作完整歌词" {
		t.Fatalf("unexpected brief text %q", got)
	}
	finish := &agentGroupSupervisorDecision{Action: agentGroupSupervisorActionFinish, Answer: "歌曲已完成：歌词见上。"}
	if got := agentGroupDecisionHeading(finish); got != "完成（finish）" {
		t.Fatalf("unexpected heading %q", got)
	}
	if got := agentGroupDecisionBriefText(finish); !strings.HasPrefix(got, "最终回答：") {
		t.Fatalf("unexpected brief text %q", got)
	}
	if got := agentGroupDecisionBriefText(nil); got != "" {
		t.Fatalf("nil decision should render empty, got %q", got)
	}
}
