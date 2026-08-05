package conversation

import (
	"strings"
	"testing"

	domainagentgroup "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/agentgroup"
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
		Action:       agentGroupSupervisorActionDelegate,
		MemberID:     "lyricist", // 翻译名，不在清单中
		Instruction:  "请创作一首中文流行歌曲歌词",
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
